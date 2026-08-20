// ai.go AI 回复实现（优先级3）。调用 OpenAI 兼容 chat completions 接口。
// 使用商品信息、对话历史和确定性的价格边界生成回复。

package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
	"xianyu-go/internal/xianyu/mtop"
)

const (
	defaultAIBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultAIModel   = "qwen-plus"
)

var newAIHTTPClient = func(baseURL string) (*http.Client, error) {
	return netguard.TrustedEndpointHTTPClient(baseURL, 30*time.Second)
}

// AIReplierImpl AI 回复实现。
type AIReplierImpl struct {
	cookieID           string
	store              *db.Store
	logger             *slog.Logger
	orderPriceAdjuster interface {
		AdjustOrderPrice(context.Context, string, string, int64) (*mtop.AdjustPriceResult, error)
	}
}

// NewAIReplier 构造。
func (a *AIReplierImpl) SetOrderPriceAdjuster(adjuster interface {
	AdjustOrderPrice(context.Context, string, string, int64) (*mtop.AdjustPriceResult, error)
}) {
	a.orderPriceAdjuster = adjuster
}

func NewAIReplier(cookieID string, store *db.Store, logger *slog.Logger) *AIReplierImpl {
	if logger == nil {
		logger = slog.Default()
	}
	return &AIReplierImpl{
		cookieID: cookieID,
		store:    store,
		logger:   logger.With("account", cookieID, "subsys", "ai"),
	}
}

// Reply 实现 AIReplier 接口。
func (a *AIReplierImpl) Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error) {
	if strings.TrimSpace(m.ItemID) == "" && strings.TrimSpace(m.ChatID) != "" && a.store.Chats != nil {
		if itemID, err := a.store.Chats.SessionItemID(ctx, a.cookieID, m.ChatID); err == nil {
			m.ItemID = itemID
		}
	}
	if strings.TrimSpace(m.ItemID) == "" {
		return nil, nil
	}
	profile, err := a.store.AIProfiles.FindForItem(ctx, a.cookieID, m.ItemID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取商品 AI 绑定失败: %w", err)
	}
	aiCfg, err := a.effectiveAIConfig(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("读取 AI API 配置失败: %w", err)
	}
	if aiCfg.APIKey == "" {
		a.logger.Warn("AI 已启用但未配置 APIKey")
		return nil, nil
	}

	// 取商品信息和当前会话状态构造 system prompt。
	itemTitle, itemPrice, itemDesc := a.itemInfo(ctx, m.ItemID)
	history, bargainCount, _, err := a.conversationContext(ctx, profile.ID, m)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 对话历史失败: %w", err)
	}
	withinBargainLimit := true
	if profile.BargainStrategyEnabled {
		systemPrompt := buildSystemPrompt(profile.CustomPrompts, itemTitle, itemPrice, itemDesc, profile.MaxDiscountPercent, profile.MaxDiscountAmount, profile.MaxBargainRounds, bargainCount)
		systemPrompt += "\n请自行判断买家是否在议价；只有确实需要议价时才考虑后续改价工具。"
		return a.replyWithPrompt(ctx, m, profile, aiCfg, itemTitle, itemPrice, itemDesc, history, bargainCount, systemPrompt, withinBargainLimit)
	}
	systemPrompt := buildGeneralSystemPrompt(profile.CustomPrompts, itemTitle, itemPrice, itemDesc)
	return a.replyWithPrompt(ctx, m, profile, aiCfg, itemTitle, itemPrice, itemDesc, history, bargainCount, systemPrompt, withinBargainLimit)
}

func (a *AIReplierImpl) replyWithPrompt(ctx context.Context, m ChatMessage, profile *db.AIProfile, aiCfg *globalAIConfig, itemTitle string, itemPrice float64, itemDesc string, history []db.AIConversationMessage, bargainCount int, systemPrompt string, withinBargainLimit bool) (*ReplyResult, error) {
	// 调 OpenAI 兼容接口。
	clientCfg := openai.DefaultConfig(aiCfg.APIKey)
	if aiCfg.BaseURL != "" {
		clientCfg.BaseURL = aiCfg.BaseURL
	}
	var err error
	clientCfg.HTTPClient, err = newAIHTTPClient(clientCfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("AI API 地址无效: %w", err)
	}
	client := openai.NewClientWithConfig(clientCfg)

	messages := []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleSystem, Content: systemPrompt}}
	for _, message := range history {
		role := openai.ChatMessageRoleUser
		if message.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: truncateAIContent(message.Content)})
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: m.Text})

	aiCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request := openai.ChatCompletionRequest{Model: aiCfg.Model, Messages: messages, Temperature: 0.7}
	if profile.BargainStrategyEnabled && a.orderPriceAdjuster != nil {
		request.Tools = []openai.Tool{adjustOrderPriceTool()}
	}
	// DashScope and compatible providers accept this extension for reasoning mode.
	request.ChatTemplateKwargs = map[string]any{"thinking": map[string]string{"type": aiCfg.ThinkingMode}}
	resp, err := client.CreateChatCompletion(aiCtx, request)
	if err != nil {
		return nil, fmt.Errorf("AI 调用失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, nil
	}
	message := resp.Choices[0].Message
	if len(message.ToolCalls) > 0 && profile.BargainStrategyEnabled && a.orderPriceAdjuster != nil {
		for _, call := range message.ToolCalls {
			if call.Function.Name != adjustOrderPriceToolName {
				continue
			}
			executor := &priceToolExecutor{replier: a, profile: profile, itemPrice: itemPrice, orderClient: a.orderPriceAdjuster, cookies: m.CookieStr, chatID: m.ChatID, buyerID: m.SenderUserID}
			toolResult, toolErr := executor.execute(aiCtx, call.Function.Arguments)
			if toolErr != nil {
				toolResult = "改价未执行：" + toolErr.Error()
			}
			messages = append(messages, message, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleTool, Name: call.Function.Name, ToolCallID: call.ID, Content: toolResult})
		}
		request.Messages = messages
		resp, err = client.CreateChatCompletion(aiCtx, request)
		if err != nil {
			return nil, fmt.Errorf("AI 工具结果调用失败: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, nil
		}
		message = resp.Choices[0].Message
	}
	reply := strings.TrimSpace(message.Content)
	if reply == "" {
		return nil, nil
	}
	silentExit := strings.Contains(reply, "<exit>")
	if !silentExit {
		reply, err = a.store.AIProfiles.ApplyForbiddenWords(ctx, reply)
		if err != nil {
			return nil, fmt.Errorf("应用 AI 违禁词失败: %w", err)
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			a.logger.Warn("AI 回复经违禁词替换后为空，降级到下一回复级别", "profile", profile.ID)
			return nil, nil
		}
	}
	if m.ChatID != "" && m.ItemID != "" {
		intent := "chat"
		if profile.BargainStrategyEnabled {
			intent = "bargain"
		}
		assistantIntent := "reply"
		if silentExit {
			assistantIntent = "silent_exit"
		}
		if err := a.store.AIReply.AddProfileConversationExchange(ctx, profile.ID, a.cookieID, m.ChatID, m.SenderUserID, m.ItemID,
			db.AIConversationMessage{Role: "user", Content: m.Text, Intent: intent, BargainCount: bargainCount},
			db.AIConversationMessage{Role: "assistant", Content: reply, Intent: assistantIntent, BargainCount: bargainCount},
		); err != nil {
			return nil, fmt.Errorf("保存 AI 对话失败: %w", err)
		}
	}
	if silentExit {
		a.logger.Info("AI 选择静默处理消息", "profile", profile.ID, "chat", m.ChatID)
		return &ReplyResult{Skip: true}, nil
	}
	return &ReplyResult{Text: reply}, nil
}

func (a *AIReplierImpl) conversationContext(ctx context.Context, profileID int64, m ChatMessage) ([]db.AIConversationMessage, int, bool, error) {
	if m.ChatID == "" || m.ItemID == "" {
		return nil, 0, false, nil
	}
	history, err := a.store.AIReply.ProfileConversationHistory(ctx, profileID, a.cookieID, m.ChatID, m.ItemID, 10)
	if err != nil {
		return nil, 0, false, err
	}
	return history, 0, false, nil
}

type globalAIConfig struct {
	APIKey       string
	BaseURL      string
	Model        string
	ThinkingMode string
}

func (a *AIReplierImpl) effectiveAIConfig(ctx context.Context, profile *db.AIProfile) (*globalAIConfig, error) {
	global, err := a.globalAIConfig(ctx)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.UseSystemAPI {
		return global, nil
	}
	if strings.TrimSpace(profile.APIKey) != "" {
		global.APIKey = strings.TrimSpace(profile.APIKey)
	}
	if strings.TrimSpace(profile.BaseURL) != "" {
		global.BaseURL = strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
	}
	if strings.TrimSpace(profile.ModelName) != "" {
		global.Model = strings.TrimSpace(profile.ModelName)
	}
	global.ThinkingMode = normalizeThinkingMode(profile.ThinkingMode)
	return global, nil
}

func (a *AIReplierImpl) globalAIConfig(ctx context.Context) (*globalAIConfig, error) {
	apiKey, err := a.store.Settings.Get(ctx, "ai_api_key")
	if err != nil {
		return nil, err
	}
	baseURL, err := a.store.Settings.Get(ctx, "ai_api_url")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL, err = a.store.Settings.Get(ctx, "ai_base_url")
		if err != nil {
			return nil, err
		}
	}
	model, err := a.store.Settings.Get(ctx, "ai_model")
	if err != nil {
		return nil, err
	}
	thinkingMode, err := a.store.Settings.Get(ctx, "ai_thinking_mode")
	if err != nil {
		return nil, err
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultAIBaseURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultAIModel
	}
	if strings.EqualFold(strings.TrimSpace(thinkingMode), "enabled") {
		thinkingMode = "enabled"
	} else {
		thinkingMode = "disabled"
	}
	return &globalAIConfig{
		APIKey:       strings.TrimSpace(apiKey),
		BaseURL:      baseURL,
		Model:        model,
		ThinkingMode: thinkingMode,
	}, nil
}

// itemInfo 取商品标题/价格/描述。
func (a *AIReplierImpl) itemInfo(ctx context.Context, itemID string) (title string, price float64, desc string) {
	it, err := a.store.Items.Get(ctx, a.cookieID, itemID)
	if err != nil || it == nil {
		return "商品信息获取失败", 0, "暂无商品描述"
	}
	title = it.ItemTitle
	if title == "" {
		title = "未知商品"
	}
	price = parsePrice(it.ItemPrice)
	desc = it.ItemDescription
	if desc == "" {
		desc = it.ItemDetail
	}
	if desc == "" {
		desc = "暂无商品描述"
	}
	return
}

// buildSystemPrompt 构造 system 提示词。
// 自定义 prompt 只替换业务文案，价格和轮次安全约束始终由后端追加。
func normalizeThinkingMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "enabled") {
		return "enabled"
	}
	return "disabled"
}

func buildSystemPrompt(customPrompts, itemTitle string, itemPrice float64, itemDesc string, maxDiscountPercent, maxDiscountAmount, maxBargainRounds, bargainCount int) string {
	var base string
	if strings.TrimSpace(customPrompts) != "" {
		base = strings.NewReplacer(
			"{{item_title}}", itemTitle,
			"{{item_price}}", fmt.Sprintf("%.2f", itemPrice),
			"{{item_description}}", itemDesc,
			"{item_title}", itemTitle,
			"{item_price}", fmt.Sprintf("%.2f", itemPrice),
			"{item_description}", itemDesc,
		).Replace(customPrompts)
	} else {
		base = fmt.Sprintf(`你是闲鱼卖家的自动回复助手。请根据商品信息友好地回复买家。

商品信息：
- 标题：%s
- 价格：%.2f 元
- 描述：%s

要求：
1. 语气友好自然，像真人卖家
2. 回答简洁，不要过长
3. 不要编造商品没有的功能
4. 直接回复内容，不要加引号或解释`, itemTitle, itemPrice, itemDesc)
	}
	return base + fmt.Sprintf(`

不可覆盖的价格安全规则：
- 原价 %.2f 元；最多优惠 %d%%，且最多优惠 %d 元；两个上限必须同时满足。
- 任一优惠上限为 0 时不得降价。
- 当前砍价轮次 %d，最多允许 %d 轮。
- 回复报价必须带“元”，不得给出低于允许最低价的价格。`, itemPrice, maxDiscountPercent, maxDiscountAmount, bargainCount, maxBargainRounds)
}

func buildGeneralSystemPrompt(customPrompts, itemTitle string, itemPrice float64, itemDesc string) string {
	if strings.TrimSpace(customPrompts) != "" {
		return strings.NewReplacer("{{item_title}}", itemTitle, "{{item_price}}", fmt.Sprintf("%.2f", itemPrice), "{{item_description}}", itemDesc, "{item_title}", itemTitle, "{item_price}", fmt.Sprintf("%.2f", itemPrice), "{item_description}", itemDesc).Replace(customPrompts)
	}
	return fmt.Sprintf("你是闲鱼卖家的自动回复助手，请根据商品信息自然回复买家。商品名称：%s；价格：%.2f 元；详情：%s。不要编造信息；若无需回复，请输出 <exit>。", itemTitle, itemPrice, itemDesc)
}

var priceRe = regexp.MustCompile(`[^\d.]`)

// minimumAllowedPrice is retained for future tool-level policy validation; the AI
// response path no longer rewrites model text based on extracted currency values.
func minimumAllowedPrice(price float64, maxDiscountPercent, maxDiscountAmount int, allowDiscount bool) float64 {
	if price <= 0 {
		return 0
	}
	if !allowDiscount || maxDiscountPercent <= 0 || maxDiscountAmount <= 0 {
		return price
	}
	byPercent := price * (1 - float64(maxDiscountPercent)/100)
	byAmount := price - float64(maxDiscountAmount)
	return math.Max(0, math.Max(byPercent, byAmount))
}

func truncateAIContent(content string) string {
	const maxRunes = 2000
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
}

// parsePrice 移除非数字字符后转换为 float。
func parsePrice(s string) float64 {
	cleaned := priceRe.ReplaceAllString(s, "")
	if cleaned == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(cleaned, 64)
	return f
}
