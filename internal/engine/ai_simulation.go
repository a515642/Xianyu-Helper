// ai_simulation.go AI 助手的后台模拟对话。
// 管理员扮演买家，与真实提示词、真实模型配置走同一条回复链；
// 发送、改价、对话持久化全部走 dry-run，只留下 simulated 观测数据。

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

const (
	simulationChatItemID   = "simulation-item"
	simulationBuyerUserID  = "simulation-buyer"
	simulationTitle        = "模拟商品"
	simulationDescription  = "模拟商品描述"
	simulationDefaultPrice = 100.0
	// 模拟请求体同样限制历史长度，避免后台会话无限增长。
	maxSimulationHistory = 20
)

// aiSimulationOrder 描述模拟环境里的待付款订单；真实改价绝不会执行。
type aiSimulationOrder struct {
	OrderID string
	Status  string
}

// AISimulationRequest 是一次模拟对话的输入。History 由调用方维护（内存会话）。
type AISimulationRequest struct {
	ProfileID       int64
	SessionID       string
	Message         string
	History         []db.AIConversationMessage
	ItemTitle       string
	ItemPrice       float64
	ItemDescription string
	MockOrderID     string
	MockOrderStatus string
}

// AISimulationResult 是模拟回复，包含便于提示词分析的观测信息。
type AISimulationResult struct {
	SessionID        string             `json:"session_id"`
	ReplyText        string             `json:"reply_text"`
	Segments         []string           `json:"segments"`
	Skip             bool               `json:"skip"`
	SkipReason       string             `json:"skip_reason"`
	Reasoning        string             `json:"reasoning"`
	SystemPrompt     string             `json:"system_prompt"`
	PromptTokens     int                `json:"prompt_tokens"`
	CompletionTokens int                `json:"completion_tokens"`
	TotalTokens      int                `json:"total_tokens"`
	DurationMs       int64              `json:"duration_ms"`
	ToolCalls        []db.AIToolCallLog `json:"tool_calls"`
}

// Simulate 在不触达平台的前提下生成一次回复。
// 需要管理员提供的只是模拟商品与订单参数，AI 配置完全复用助手本身的设置。
func (a *AIReplierImpl) Simulate(ctx context.Context, req AISimulationRequest) (*AISimulationResult, error) {
	started := time.Now()
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return nil, errors.New("模拟消息不能为空")
	}
	if req.ProfileID <= 0 {
		return nil, errors.New("AI 助手 ID 无效")
	}
	profile, err := a.store.AIProfiles.Get(ctx, req.ProfileID)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 助手失败: %w", err)
	}
	if profile.CookieID != a.cookieID {
		return nil, errors.New("AI 助手不属于当前账号")
	}
	aiCfg, err := a.effectiveAIConfig(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("读取 AI API 配置失败: %w", err)
	}
	if strings.TrimSpace(aiCfg.APIKey) == "" {
		return nil, errors.New("该 AI 助手尚未配置 API Key")
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("sim-%d", time.Now().UnixNano())
	}
	itemTitle := strings.TrimSpace(req.ItemTitle)
	if itemTitle == "" {
		itemTitle = simulationTitle
	}
	itemDesc := strings.TrimSpace(req.ItemDescription)
	if itemDesc == "" {
		itemDesc = simulationDescription
	}
	itemPrice := req.ItemPrice
	if itemPrice <= 0 {
		itemPrice = simulationDefaultPrice
	}
	var mockOrder *aiSimulationOrder
	if orderID := strings.TrimSpace(req.MockOrderID); orderID != "" {
		status := strings.TrimSpace(req.MockOrderStatus)
		if status == "" {
			status = "processing"
		}
		mockOrder = &aiSimulationOrder{OrderID: orderID, Status: db.NormalizeOrderStatus(status)}
	}

	history := req.History
	if len(history) > maxSimulationHistory {
		history = history[len(history)-maxSimulationHistory:]
	}
	m := ChatMessage{
		AccountID:    a.cookieID,
		ChatID:       "",
		SenderUserID: simulationBuyerUserID,
		Text:         message,
		MessageID:    "simulation:" + sessionID,
		ItemID:       simulationChatItemID,
	}

	var systemPrompt string
	if profile.ReplyMode == "tools" {
		systemPrompt = a.buildAgentSystemPrompt(ctx, profile, itemTitle, itemPrice, itemDesc, 0)
	} else if profile.BargainStrategyEnabled {
		systemPrompt = buildSystemPrompt(profile.CustomPrompts, itemTitle, itemPrice, itemDesc, profile.MaxDiscountPercent, profile.MaxDiscountAmount, profile.MaxBargainRounds, 0)
	} else {
		systemPrompt = buildGeneralSystemPrompt(profile.CustomPrompts, itemTitle, itemPrice, itemDesc)
	}
	systemPrompt += simulationContextBlock(profile, mockOrder)

	trace := newAITurnTrace(started, a, m, profile, aiCfg, systemPrompt)
	trace.simulated = true
	trace.sessionID = sessionID
	trace.itemID = simulationChatItemID

	options := agentRunOptions{simulation: mockOrder}
	var result *ReplyResult
	var replyErr error
	if profile.ReplyMode == "tools" {
		result, replyErr = a.replyWithAgent(ctx, m, profile, aiCfg, itemTitle, itemPrice, itemDesc, history, 0, systemPrompt, trace, options)
	} else {
		result, replyErr = a.replyWithPrompt(ctx, m, profile, aiCfg, itemTitle, itemPrice, itemDesc, history, 0, systemPrompt, true, trace, options)
	}
	a.recordAITurn(trace, result, replyErr)
	if replyErr != nil {
		return nil, replyErr
	}

	out := &AISimulationResult{
		SessionID:        sessionID,
		Reasoning:        trace.reasoning,
		SystemPrompt:     systemPrompt,
		PromptTokens:     trace.promptTokens,
		CompletionTokens: trace.completionTok,
		TotalTokens:      trace.totalTokens,
		DurationMs:       time.Since(started).Milliseconds(),
		ToolCalls:        trace.toolCalls,
	}
	if result != nil {
		out.Skip = result.Skip
		out.SkipReason = result.SkipReason
		out.Segments = append([]string(nil), result.textSegments()...)
		out.ReplyText = result.TextContent()
	}
	a.logger.Info("AI诊断：模拟回复完成", "profile", profile.ID, "session_id", sessionID, "duration", out.DurationMs, "segments", len(out.Segments), "skip", out.Skip, "tool_calls", len(out.ToolCalls))
	return out, nil
}

// simulationContextBlock 告诉模型这是模拟环境；改价工具只接受模拟订单号。
func simulationContextBlock(profile *db.AIProfile, order *aiSimulationOrder) string {
	block := "\n\n当前为模拟环境：你正在后台与管理员扮演的买家对话，回复不会真实发送，改价也不会真实执行。"
	if profile.ReplyMode != "tools" {
		return block + "\n当前回复模式为纯文本，不提供改价工具。"
	}
	if order == nil || order.OrderID == "" {
		return block + "\n当前没有关联的待付款订单，无法改价。"
	}
	return block + fmt.Sprintf("\n买家对应的待付款订单号是 %s，订单状态 %s；如需改价只能使用该订单号。", order.OrderID, order.Status)
}

// executeSimulatedPriceTool 只做参数与价格策略校验，不调用平台改价接口。
func (a *AIReplierImpl) executeSimulatedPriceTool(order *aiSimulationOrder, raw string, profile *db.AIProfile, itemPrice float64) (string, error) {
	var args priceToolArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", errors.New("改价参数不是有效 JSON")
	}
	args.OrderID = strings.TrimSpace(args.OrderID)
	if args.OrderID == "" || args.TargetPriceCents <= 0 {
		return "", errors.New("订单 ID 和目标金额必须有效")
	}
	if order == nil || order.OrderID == "" {
		return "", errors.New("模拟环境没有关联的待付款订单")
	}
	if args.OrderID != order.OrderID {
		return "", errors.New("模拟订单号与当前会话不一致")
	}
	status := db.NormalizeOrderStatus(order.Status)
	if status != "processing" {
		return "", fmt.Errorf("订单状态 %s 不允许改价，仅待付款订单可改价", status)
	}
	if itemPrice <= 0 {
		return "", errors.New("商品原价无效")
	}
	minimum := minimumAllowedPrice(itemPrice, profile.MaxDiscountPercent, profile.MaxDiscountAmount, true)
	if float64(args.TargetPriceCents)/100+0.0001 < minimum || float64(args.TargetPriceCents)/100 > itemPrice+0.0001 {
		return "", fmt.Errorf("目标价格必须在 %.2f 到 %.2f 元之间", minimum, itemPrice)
	}
	return fmt.Sprintf("（模拟）订单 %s 将改价为 %.2f 元，未实际调用平台接口", args.OrderID, float64(args.TargetPriceCents)/100), nil
}
