// ai_agent.go 工具调用模式的 AI 回复循环。
// 模型通过 send_reply/skip_reply 表达发送意图，实际投递仍由 ReplyService.deliver 完成。

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
)

const (
	sendReplyToolName     = "send_reply"
	skipReplyToolName     = "skip_reply"
	maxAgentReplySegments = 5
	maxAgentSegmentRunes  = 1000
	// agentReplyTotalBudget 是一次工具循环的总预算，包含所有轮次和工具执行。
	agentReplyTotalBudget = 3 * time.Minute
)

// defaultAgentPromptTemplate 仅在单个助手和系统设置都没有配置时兜底。
const defaultAgentPromptTemplate = `你是闲鱼卖家的自动回复助手，请根据商品信息与买家自然沟通。

商品信息：
- 标题：{item_title}
- 价格：{item_price} 元
- 描述：{item_description}

回复规则：
1. 需要回复买家时调用 send_reply，把内容拆成 1-5 条简短消息，像真人聊天一样自然。
2. 无需回复（例如买家只发来表情或无关内容）时调用 skip_reply，并说明原因。
3. 买家明确要求人工、涉及售后纠纷，或问题超出商品信息范围时，通知卖家。
4. 只有买家确实在议价时才考虑改价工具，不要主动降价。
5. 不要编造商品信息，不要承诺无法提供的服务。`

type sendReplyArgs struct {
	Messages []string `json:"messages"`
}

type skipReplyArgs struct {
	Reason string `json:"reason"`
}

// agentTurnState 收集本回合模型表达的工具意图。
type agentTurnState struct {
	segments   []string
	skip       bool
	skipReason string
}

// agentRunOptions 携带仅模拟模式使用的覆盖项；默认为零值即真实回复。
type agentRunOptions struct {
	simulation *aiSimulationOrder
}

func sendReplyTool() openai.Tool {
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{
		Name:        sendReplyToolName,
		Description: "向买家发送回复。把回复拆成 1-5 条简短消息，按顺序发送。",
		Strict:      true,
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"messages": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    maxAgentReplySegments,
					"description": "按顺序发送给买家的 1-5 条简短消息",
					"items": map[string]any{
						"type":      "string",
						"minLength": 1,
						"maxLength": maxAgentSegmentRunes,
					},
				},
			},
			"required": []string{"messages"},
		},
	}}
}

func skipReplyTool() openai.Tool {
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{
		Name:        skipReplyToolName,
		Description: "本次不需要回复买家时调用，例如对方只发表情、感谢，或内容与交易无关。",
		Strict:      true,
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"reason": map[string]any{"type": "string", "description": "跳过回复的简要原因"},
			},
			"required": []string{"reason"},
		},
	}}
}

// buildAgentSystemPrompt 解析工具模式提示词：助手自定义 → 系统设置 → 内置模板。
// 价格安全规则和工具调用约定由代码追加，避免配置错误绕过。
func (a *AIReplierImpl) buildAgentSystemPrompt(ctx context.Context, profile *db.AIProfile, itemTitle string, itemPrice float64, itemDesc string, bargainCount int) string {
	template := strings.TrimSpace(profile.AgentPrompt)
	if template == "" {
		stored, err := a.store.Settings.Get(ctx, "ai_agent_prompt")
		if err != nil {
			a.logger.Warn("读取工具模式提示词失败，使用内置模板", "profile", profile.ID, "error_type", fmt.Sprintf("%T", err))
		} else {
			template = strings.TrimSpace(stored)
		}
	}
	if template == "" {
		template = defaultAgentPromptTemplate
	}
	prompt := strings.NewReplacer(
		"{{item_title}}", itemTitle,
		"{{item_price}}", fmt.Sprintf("%.2f", itemPrice),
		"{{item_description}}", itemDesc,
		"{item_title}", itemTitle,
		"{item_price}", fmt.Sprintf("%.2f", itemPrice),
		"{item_description}", itemDesc,
	).Replace(template)
	prompt += `

工具调用约定：
- 需要回复买家时调用 send_reply，把内容拆成 1-5 条自然简短的文本消息。
- 本次无需回复时调用 skip_reply，并简要说明原因。
- 不要直接输出回复正文；只有工具调用才会发送给买家。`
	if profile.BargainStrategyEnabled {
		prompt += priceSafetyRules(itemPrice, profile.MaxDiscountPercent, profile.MaxDiscountAmount, profile.MaxBargainRounds, bargainCount)
		prompt += "\n请自行判断买家是否在议价；只有确实需要议价时才考虑调用改价工具。"
	} else {
		prompt += "\n\n当前未启用议价策略：不得降价，也不得调用改价工具。"
	}
	return prompt
}

// replyWithAgent 执行工具调用循环，直到模型给出 send_reply/skip_reply 或轮数耗尽。
func (a *AIReplierImpl) replyWithAgent(ctx context.Context, m ChatMessage, profile *db.AIProfile, aiCfg *globalAIConfig, itemTitle string, itemPrice float64, itemDesc string, history []db.AIConversationMessage, bargainCount int, systemPrompt string, trace *aiTurnTrace, options agentRunOptions) (*ReplyResult, error) {
	client, _, err := a.newAICompletionClient(profile.ID, aiCfg)
	if err != nil {
		return nil, err
	}
	messages := buildChatMessages(systemPrompt, history, m.Text)
	tools := []openai.Tool{sendReplyTool(), skipReplyTool()}
	toolEnabled := profile.BargainStrategyEnabled && (a.orderPriceAdjuster != nil || options.simulation != nil)
	if toolEnabled {
		tools = append(tools, adjustOrderPriceTool())
	}
	// 整轮预算独立于单次 HTTP 超时，避免多轮工具调用无限拖延。
	budgetCtx, cancel := context.WithTimeout(ctx, agentReplyTotalBudget)
	defer cancel()
	maxRounds := db.NormalizeAIMaxToolRounds(profile.MaxToolRounds)
	state := &agentTurnState{}
	rounds := 0
	for rounds < maxRounds {
		rounds++
		request := openai.ChatCompletionRequest{
			Model:       aiCfg.Model,
			Messages:    messages,
			Temperature: 0.7,
			Tools:       tools,
			ToolChoice:  "auto",
		}
		request.ChatTemplateKwargs = map[string]any{"thinking": map[string]string{"type": aiCfg.ThinkingMode}}
		requestStarted := time.Now()
		resp, err := client.CreateChatCompletion(budgetCtx, request)
		if err != nil {
			a.logger.Warn("AI诊断：工具模式 LLM 调用失败", "profile", profile.ID, "chat_id", m.ChatID, "round", rounds, "duration", time.Since(requestStarted).Round(time.Millisecond), "failure_class", classifyAIRequestError(err, budgetCtx), "error_type", fmt.Sprintf("%T", err), "error_summary", safeAIErrorSummary(err))
			return nil, fmt.Errorf("AI 调用失败: %w", err)
		}
		if len(resp.Choices) == 0 {
			a.logger.Warn("AI诊断：工具模式 LLM 返回空 choices", "profile", profile.ID, "chat_id", m.ChatID, "round", rounds)
			return nil, nil
		}
		message := resp.Choices[0].Message
		trace.observe(resp, message)
		a.logger.Info("AI诊断：工具模式 LLM 调用完成", "profile", profile.ID, "chat_id", m.ChatID, "round", rounds, "duration", time.Since(requestStarted).Round(time.Millisecond), "tool_calls", len(message.ToolCalls), "content_len", len([]rune(message.Content)))
		if len(message.ToolCalls) == 0 {
			// 兼容不支持工具调用的服务商：直接文本按一段回复处理。
			if content := strings.TrimSpace(message.Content); content != "" {
				if utf8.RuneCountInString(content) > maxAgentSegmentRunes {
					a.logger.Warn("AI诊断：工具模式直出文本过长，已截断", "profile", profile.ID, "chat_id", m.ChatID, "content_len", len([]rune(content)))
					content = string([]rune(content)[:maxAgentSegmentRunes])
				}
				state.segments = append(state.segments, content)
			}
			break
		}
		messages = append(messages, message)
		stop := false
		for _, call := range message.ToolCalls {
			toolStarted := time.Now()
			result, callErr := a.executeAgentTool(budgetCtx, call, profile, itemPrice, m, state, options)
			if callErr != nil {
				a.logger.Warn("AI诊断：工具执行失败", "profile", profile.ID, "chat_id", m.ChatID, "round", rounds, "tool", call.Function.Name, "error_type", fmt.Sprintf("%T", callErr))
				result = "工具执行失败：" + callErr.Error()
			}
			if trace != nil {
				trace.toolCalls = append(trace.toolCalls, db.AIToolCallLog{Round: rounds, ToolName: call.Function.Name, Arguments: call.Function.Arguments, Result: result, Success: callErr == nil, DurationMs: time.Since(toolStarted).Milliseconds()})
			}
			messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleTool, Name: call.Function.Name, ToolCallID: call.ID, Content: result})
			if callErr == nil && (call.Function.Name == sendReplyToolName || call.Function.Name == skipReplyToolName) {
				stop = true
			}
		}
		if stop {
			break
		}
	}
	if !state.skip && len(state.segments) == 0 {
		a.logger.Info("AI诊断：工具模式未产生回复", "profile", profile.ID, "chat_id", m.ChatID, "rounds", rounds)
		return nil, nil
	}
	assistantContent := strings.Join(state.segments, "\n")
	assistantIntent := "reply"
	if state.skip {
		assistantContent = "[未回复]"
		assistantIntent = "skip_reply"
	}
	if options.simulation == nil && m.ChatID != "" && m.ItemID != "" {
		intent := "chat"
		if profile.BargainStrategyEnabled {
			intent = "bargain"
		}
		if err := a.store.AIReply.AddProfileConversationExchange(ctx, profile.ID, a.cookieID, m.ChatID, m.SenderUserID, m.ItemID,
			db.AIConversationMessage{Role: "user", Content: m.Text, Intent: intent, BargainCount: bargainCount},
			db.AIConversationMessage{Role: "assistant", Content: assistantContent, Intent: assistantIntent, BargainCount: bargainCount},
		); err != nil {
			return nil, fmt.Errorf("保存 AI 对话失败: %w", err)
		}
	}
	if state.skip {
		a.logger.Info("AI 工具模式选择跳过回复", "profile", profile.ID, "chat", m.ChatID, "reason_len", len([]rune(state.skipReason)))
		return &ReplyResult{Skip: true, SkipReason: state.skipReason}, nil
	}
	a.logger.Info("AI诊断：工具模式生成回复", "profile", profile.ID, "chat_id", m.ChatID, "segments", len(state.segments), "rounds", rounds)
	return &ReplyResult{Texts: state.segments}, nil
}

// executeAgentTool 执行单个工具调用；返回给模型的结果文本。
func (a *AIReplierImpl) executeAgentTool(ctx context.Context, call openai.ToolCall, profile *db.AIProfile, itemPrice float64, m ChatMessage, state *agentTurnState, options agentRunOptions) (string, error) {
	switch call.Function.Name {
	case sendReplyToolName:
		if state.skip {
			return "", errors.New("本回合已选择跳过回复，不能再发送")
		}
		count, err := a.parseSendReply(ctx, call.Function.Arguments, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已发送 %d 条消息", count), nil
	case skipReplyToolName:
		if len(state.segments) > 0 {
			return "", errors.New("已生成回复，不能同时跳过")
		}
		var args skipReplyArgs
		_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		state.skip = true
		state.skipReason = strings.TrimSpace(args.Reason)
		return "已记录：本次不回复", nil
	case adjustOrderPriceToolName:
		if !profile.BargainStrategyEnabled {
			return "", errors.New("改价工具未启用")
		}
		if options.simulation != nil {
			// 模拟模式只做参数与价格策略校验，不调用平台改价接口。
			return a.executeSimulatedPriceTool(options.simulation, call.Function.Arguments, profile, itemPrice)
		}
		if a.orderPriceAdjuster == nil {
			return "", errors.New("改价工具未启用")
		}
		executor := &priceToolExecutor{replier: a, profile: profile, itemPrice: itemPrice, orderClient: a.orderPriceAdjuster, cookies: m.CookieStr, chatID: m.ChatID, buyerID: m.SenderUserID}
		return executor.execute(ctx, call.Function.Arguments)
	default:
		return "", fmt.Errorf("未知工具: %s", call.Function.Name)
	}
}

// parseSendReply 校验并规整 send_reply 的分段消息，同时应用违禁词替换。
func (a *AIReplierImpl) parseSendReply(ctx context.Context, raw string, state *agentTurnState) (int, error) {
	var args sendReplyArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return 0, errors.New("回复参数不是有效 JSON")
	}
	if len(args.Messages) == 0 {
		return 0, errors.New("messages 不能为空")
	}
	segments := make([]string, 0, len(args.Messages))
	for _, rawSegment := range args.Messages {
		segment := strings.TrimSpace(rawSegment)
		if segment == "" {
			continue
		}
		if utf8.RuneCountInString(segment) > maxAgentSegmentRunes {
			return 0, fmt.Errorf("单条消息不能超过 %d 字，请拆分后重试", maxAgentSegmentRunes)
		}
		clean, err := a.store.AIProfiles.ApplyForbiddenWords(ctx, segment)
		if err != nil {
			return 0, fmt.Errorf("应用违禁词失败: %w", err)
		}
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}
		segments = append(segments, clean)
	}
	if len(segments) == 0 {
		return 0, errors.New("回复内容为空或已被违禁词过滤")
	}
	if len(state.segments)+len(segments) > maxAgentReplySegments {
		return 0, fmt.Errorf("一次回复最多 %d 条消息", maxAgentReplySegments)
	}
	state.segments = append(state.segments, segments...)
	return len(segments), nil
}
