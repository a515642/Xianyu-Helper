// reply.go 四级回复引擎：API → 关键词 → AI → 默认回复。
// 实现关键词回复、默认回复和 AI 回复的调度。
//
// Phase 3 实现：关键词（含商品ID优先+变量替换+空回复标记）、默认回复（指定商品优先+reply_once+变量替换）。
// API 回复（调外部 /xianyu/reply 接口）和 AI 回复（OpenAI 兼容）留接口注入。

package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// ReplyResult 回复结果。
type ReplyResult struct {
	Text      string // 文本回复（可空）
	ImageURL  string // 图片回复（可空）
	Source    string // 回复来源：API/关键词/AI/默认/商品首次回复
	Skip      bool   // true 表示匹配到空回复，不发送任何内容
	ReplyOnce bool   // 仅一次性回复使用，发送状态由 Handle 持久化
	// records 是一次性回复使用的投递记录表；为空时使用账号默认回复记录。
	records replyRecordStore
}

// replyRecordStore 是一条回复按会话去重所需的持久化投递状态。
// 默认回复和商品首次回复各使用一张记录表，但状态机语义相同。
type replyRecordStore interface {
	ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (db.DefaultReplyRecord, bool, error)
	MarkPartSent(ctx context.Context, cookieID, chatID, part string) error
	MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error
	MarkRecordSent(ctx context.Context, cookieID, chatID string) error
}

// APIReplier 外部 API 回复（优先级1）。返回 nil 表示无回复。
type APIReplier interface {
	Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error)
}

// AIReplier AI 回复（优先级3）。返回 nil 表示无回复。
type AIReplier interface {
	Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error)
}

// MessageSender 是回复服务发送文本/图片所需的最小接口。
type MessageSender interface {
	SendText(ctx context.Context, chatID, toUserID, text string) error
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error
}

// ReplyService 单账号回复服务。
type ReplyService struct {
	cookieID string
	store    *db.Store
	api      APIReplier // 可为 nil
	ai       AIReplier  // 可为 nil
	sender   MessageSender
	logger   *slog.Logger
}

// NewReplyService 构造。
func NewReplyService(cookieID string, store *db.Store, sender MessageSender,
	api APIReplier, ai AIReplier, logger *slog.Logger) *ReplyService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReplyService{
		cookieID: cookieID,
		store:    store,
		api:      api,
		ai:       ai,
		sender:   sender,
		logger:   logger.With("account", cookieID, "subsys", "reply"),
	}
}

// humanTakeoverWindow 是真人卖家最后一条消息之后的静默期：期间买家消息不再由
// AI 助手回复，避免与真人卖家在回复上形成竞态。
const humanTakeoverWindow = 5 * time.Minute

// humanTakeoverActive 判断当前是否应由真人卖家接管（AI 让位）。
// 让位窗口从"最后一条真人卖家消息"起算，真人持续回复则窗口不断顺延；
// 窗口内买家消息不回复；买家消息后 5 分钟仍无真人回复时，AI 助手恢复接管。
// 查询失败时不影响自动回复（返回 false）。
func (r *ReplyService) humanTakeoverActive(ctx context.Context, m ChatMessage) bool {
	if r.store == nil || r.store.Chats == nil || m.ChatID == "" {
		return false
	}
	sentAt, ok, err := r.store.Chats.LastSellerOutgoingAt(ctx, r.cookieID, m.ChatID)
	if err != nil {
		r.logger.Warn("读取真人卖家最近回复时间失败，按 AI 接管处理", "chat_id", m.ChatID, "err", err)
		return false
	}
	if !ok {
		return false
	}
	last := time.Unix(0, sentAt*int64(time.Millisecond))
	return time.Since(last) < humanTakeoverWindow
}

// Handle 收到一条聊天消息，先尝试商品首次回复，再按四级优先级回复。
// 由 Account 在防抖后调用。返回是否产生了回复。
func (r *ReplyService) Handle(ctx context.Context, m ChatMessage) error {
	started := time.Now()
	// 商品首次回复失败只记录日志，不得阻断买家本条消息的正常回复链路。
	r.handleItemGreeting(ctx, m)
	r.logger.Info("AI诊断：消息进入回复链", "chat_id", m.ChatID, "item_id", m.ItemID, "message_kind", classifyAIMessage(m), "text_len", len([]rune(m.Text)))
	// 真人卖家刚回复过 → AI 让位，跳过本次回复（含关键词/默认回复）。
	if r.humanTakeoverActive(ctx, m) {
		r.logger.Info("AI诊断：真人卖家接管中，跳过自动回复", "chat_id", m.ChatID, "item_id", m.ItemID, "window", humanTakeoverWindow.String())
		return nil
	}
	res := r.resolve(ctx, m)
	if res == nil || res.Skip {
		r.logger.Info("AI诊断：回复链无发送内容", "chat_id", m.ChatID, "item_id", m.ItemID, "has_result", res != nil, "skip", res != nil && res.Skip, "duration", time.Since(started).Round(time.Millisecond))
		return nil
	}
	// 发送：图片优先，文本随后。reply_once 使用持久化分段状态，失败时只重试
	// 尚未成功的部分。
	if r.sender == nil {
		r.logger.Warn("AI诊断：回复链得到内容但发送器未注入", "chat_id", m.ChatID, "item_id", m.ItemID, "source", res.Source)
		return nil
	}
	if err := r.deliver(ctx, m, res); err != nil {
		return err
	}
	r.logger.Info("AI诊断：回复链发送完成", "chat_id", m.ChatID, "item_id", m.ItemID, "source", res.Source, "text_len", len([]rune(res.Text)), "duration", time.Since(started).Round(time.Millisecond))
	return nil
}

// deliver 发送一条已解析的回复；一次性回复先原子领取会话记录，再按分段状态投递。
func (r *ReplyService) deliver(ctx context.Context, m ChatMessage, res *ReplyResult) error {
	records := res.records
	if records == nil {
		records = r.store.DefaultReps
	}
	record := db.DefaultReplyRecord{}
	if res.ReplyOnce && m.ChatID != "" {
		var claimed bool
		var err error
		record, claimed, err = records.ClaimRecord(ctx, r.cookieID, m.ChatID, res.Text != "", res.ImageURL != "")
		if err != nil {
			return fmt.Errorf("领取%s回复发送任务: %w", res.Source, err)
		}
		if !claimed {
			return nil
		}
	}
	// 自动回复必须标记来源：真人手动发送才会触发 AI 让位。
	ctx = WithAutoSend(ctx)
	if res.ImageURL != "" && !record.ImageSent {
		if err := r.sender.SendImage(ctx, m.ChatID, m.SenderUserID, res.ImageURL, 0); err != nil {
			r.logger.Error("发送回复图片失败", "err", err)
			r.markReplyFailure(ctx, res, m, err)
			return err
		}
		if res.ReplyOnce && m.ChatID != "" {
			if err := records.MarkPartSent(ctx, r.cookieID, m.ChatID, "image"); err != nil {
				r.markReplyFailure(ctx, res, m, err)
				return err
			}
		}
	}
	if res.Text != "" && !record.TextSent {
		if err := r.sender.SendText(ctx, m.ChatID, m.SenderUserID, res.Text); err != nil {
			r.logger.Error("发送回复文本失败", "err", err)
			r.markReplyFailure(ctx, res, m, err)
			return err
		}
		if res.ReplyOnce && m.ChatID != "" {
			if err := records.MarkPartSent(ctx, r.cookieID, m.ChatID, "text"); err != nil {
				r.markReplyFailure(ctx, res, m, err)
				return err
			}
		}
	}
	if res.ReplyOnce && m.ChatID != "" {
		if err := records.MarkRecordSent(ctx, r.cookieID, m.ChatID); err != nil {
			r.markReplyFailure(ctx, res, m, err)
			return err
		}
	}
	return nil
}

// handleItemGreeting 在会话首条命中商品规则的消息上发送一次商品首次回复。
// 规则、记录读取或发送失败都只记录日志；一次性记录保证每个会话最多触发一次。
func (r *ReplyService) handleItemGreeting(ctx context.Context, m ChatMessage) {
	if m.ItemID == "" || m.ChatID == "" || r.sender == nil || r.store == nil || r.store.ItemGreetings == nil {
		return
	}
	rule, err := r.store.ItemGreetings.FindEnabledForItem(ctx, r.cookieID, m.ItemID)
	if errors.Is(err, db.ErrNotFound) {
		return
	}
	if err != nil {
		r.logger.Error("读取商品首次回复规则失败", "err", err)
		return
	}
	res := &ReplyResult{Source: "商品首次回复", ReplyOnce: true, records: r.store.ItemGreetings}
	if strings.TrimSpace(rule.ReplyContent) != "" {
		res.Text = formatReplyWithItem(rule.ReplyContent, m)
	}
	if strings.TrimSpace(rule.ReplyImageURL) != "" {
		res.ImageURL = rule.ReplyImageURL
	}
	if res.Text == "" && res.ImageURL == "" {
		return
	}
	if deliverErr := r.deliver(ctx, m, res); deliverErr != nil {
		r.logger.Error("商品首次回复发送失败", "err", deliverErr)
	}
}

func (r *ReplyService) markReplyFailure(ctx context.Context, res *ReplyResult, m ChatMessage, sendErr error) {
	if res.ReplyOnce && m.ChatID != "" {
		records := res.records
		if records == nil {
			records = r.store.DefaultReps
		}
		_ = records.MarkRecordFailed(ctx, r.cookieID, m.ChatID, sendErr.Error())
	}
}

// resolve 按优先级确定回复内容（不发送）。
func (r *ReplyService) resolve(ctx context.Context, m ChatMessage) *ReplyResult {
	// 优先级1：API 回复。
	if r.api != nil {
		if res, err := r.api.Reply(ctx, m); err != nil {
			r.logger.Error("API 回复失败", "err", err)
		} else if res != nil {
			res.Source = "API"
			return res
		}
	}

	// 优先级2：关键词匹配。
	if res := r.keywordReply(ctx, m); res != nil {
		return res
	}

	// 优先级3：AI 回复。
	if r.ai != nil {
		if res, err := r.ai.Reply(ctx, m); err != nil {
			r.logger.Error("AI 回复失败", "error_type", fmt.Sprintf("%T", err), "error_summary", safeAIErrorSummary(err))
		} else if res != nil {
			res.Source = "AI"
			return res
		}
	}

	// 优先级4：默认回复。
	return r.defaultReply(ctx, m)
}

// keywordReply 关键词匹配。返回 nil 表示无匹配；
// 返回 Skip=true 表示匹配到空回复（不发送）。
// 移植自 get_keyword_reply：商品ID关键词优先 → 通用关键词。
func (r *ReplyService) keywordReply(ctx context.Context, m ChatMessage) *ReplyResult {
	kws, err := r.store.Keywords.AllWithType(ctx, r.cookieID)
	if err != nil || len(kws) == 0 {
		return nil
	}
	msgLower := strings.ToLower(m.Text)

	// 1. 商品ID关键词优先。
	if m.ItemID != "" {
		for _, kw := range kws {
			if kw.ItemID == m.ItemID && strings.Contains(msgLower, strings.ToLower(kw.Keyword)) {
				return r.keywordResult(kw, m)
			}
		}
	}
	// 2. 通用关键词（无 item_id）。
	for _, kw := range kws {
		if kw.ItemID == "" && strings.Contains(msgLower, strings.ToLower(kw.Keyword)) {
			return r.keywordResult(kw, m)
		}
	}
	return nil
}

func (r *ReplyService) keywordResult(kw db.Keyword, m ChatMessage) *ReplyResult {
	if kw.Type == "image" && kw.ImageURL != "" {
		return &ReplyResult{ImageURL: kw.ImageURL, Source: "关键词"}
	}
	if strings.TrimSpace(kw.Reply) == "" {
		return &ReplyResult{Skip: true, Source: "关键词"} // EMPTY_REPLY
	}
	return &ReplyResult{Text: formatReply(kw.Reply, m), Source: "关键词"}
}

// defaultReply 默认回复。移植自 get_default_reply：
// 指定商品回复优先 → 账号默认回复（reply_once 防重复 + 变量替换）。
func (r *ReplyService) defaultReply(ctx context.Context, m ChatMessage) *ReplyResult {
	// 1. 指定商品回复。
	if m.ItemID != "" {
		if ir, err := r.store.ItemReps.Get(ctx, r.cookieID, m.ItemID); err == nil && ir != nil && strings.TrimSpace(ir.ReplyContent) != "" {
			return &ReplyResult{Text: formatReplyWithItem(ir.ReplyContent, m), Source: "默认"}
		}
	}
	// 2. 账号默认回复。
	dr, err := r.store.DefaultReps.Get(ctx, r.cookieID)
	if err != nil || dr == nil || !dr.Enabled {
		return nil
	}
	// 文字和图片都为空 → 空回复标记。
	if strings.TrimSpace(dr.ReplyContent) == "" && strings.TrimSpace(dr.ReplyImageURL) == "" {
		return &ReplyResult{Skip: true, Source: "默认"}
	}
	// 商品首次回复已触发的会话不再发送账号级“仅回复一次”的默认回复，商品级覆盖账号级。
	if dr.ReplyOnce && r.itemGreetingTriggered(ctx, m) {
		return nil
	}
	res := &ReplyResult{Source: "默认", ReplyOnce: dr.ReplyOnce}
	if strings.TrimSpace(dr.ReplyContent) != "" {
		res.Text = formatReply(dr.ReplyContent, m)
	}
	if strings.TrimSpace(dr.ReplyImageURL) != "" {
		res.ImageURL = dr.ReplyImageURL
	}
	return res
}

// itemGreetingTriggered 判断当前会话是否已有商品首次回复记录；读取失败时按未触发处理，保持默认回复可用。
func (r *ReplyService) itemGreetingTriggered(ctx context.Context, m ChatMessage) bool {
	if m.ChatID == "" || r.store == nil || r.store.ItemGreetings == nil {
		return false
	}
	triggered, err := r.store.ItemGreetings.HasRecord(ctx, r.cookieID, m.ChatID)
	if err != nil {
		r.logger.Error("读取商品首次回复记录失败", "err", err)
		return false
	}
	return triggered
}

// formatReply 变量替换：{send_user_name} {send_user_id} {send_message}。
// 替换回复模板变量；若替换出错则返回原文。
func formatReply(template string, m ChatMessage) string {
	return safeFormat(template, map[string]string{
		"send_user_name": m.SenderName,
		"send_user_id":   m.SenderUserID,
		"send_message":   m.Text,
	})
}

// formatReplyWithItem 含 {item_id} 变量。
func formatReplyWithItem(template string, m ChatMessage) string {
	return safeFormat(template, map[string]string{
		"send_user_name": m.SenderName,
		"send_user_id":   m.SenderUserID,
		"send_message":   m.Text,
		"item_id":        m.ItemID,
	})
}

// safeFormat 实现命名占位符替换。
func safeFormat(template string, vars map[string]string) string {
	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}
