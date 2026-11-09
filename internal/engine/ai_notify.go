// ai_notify.go AI 转人工工具：复用账号现有的通知渠道，向真人卖家求助。

package engine

import (
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
	notifyHumanToolName  = "notify_human"
	maxNotifyReasonRunes = 500
	// 同一会话 30 分钟内最多通知一次，避免买家反复触发刷屏。
	humanNotifyCooldown = 30 * time.Minute
	maxNotifyCooldowns  = 500
)

// HumanHandoffNotifier 是 AI 转人工所需的最小通知接口，由 notify.Notifier 实现。
type HumanHandoffNotifier interface {
	NotifyHumanHandoff(accountID, buyerName, buyerID, itemID, chatID, reason string)
}

type notifyHumanArgs struct {
	Reason string `json:"reason"`
}

func notifyHumanTool() openai.Tool {
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{
		Name:        notifyHumanToolName,
		Description: "买家明确要求人工、涉及售后纠纷，或问题超出商品信息范围时，通知真人卖家介入处理。",
		Strict:      true,
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"reason": map[string]any{"type": "string", "minLength": 1, "maxLength": maxNotifyReasonRunes, "description": "需要卖家介入的简要原因"},
			},
			"required": []string{"reason"},
		},
	}}
}

// SetHumanHandoffNotifier 注入账号通知渠道；未注入时转人工工具会返回失败。
func (a *AIReplierImpl) SetHumanHandoffNotifier(notifier HumanHandoffNotifier) {
	a.humanNotifier = notifier
}

// executeNotifyHuman 发送一次转人工通知，并做同会话冷却。
func (a *AIReplierImpl) executeNotifyHuman(m ChatMessage, profile *db.AIProfile, raw string, options agentRunOptions) (string, error) {
	var args notifyHumanArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", errors.New("通知参数不是有效 JSON")
	}
	reason := strings.TrimSpace(args.Reason)
	if reason == "" {
		return "", errors.New("通知原因不能为空")
	}
	if utf8.RuneCountInString(reason) > maxNotifyReasonRunes {
		return "", fmt.Errorf("通知原因不能超过 %d 字", maxNotifyReasonRunes)
	}
	if !profile.NotifyHumanEnabled {
		return "", errors.New("转人工通知未启用")
	}
	if options.simulation != nil {
		return "（模拟）已记录转人工通知：" + reason + "，未实际发送", nil
	}
	if a.humanNotifier == nil {
		return "", errors.New("通知渠道未接入")
	}
	if !a.claimHumanNotify(m.ChatID, m.SenderUserID) {
		return "最近已通知过卖家，本次不重复发送", nil
	}
	a.humanNotifier.NotifyHumanHandoff(a.cookieID, m.SenderName, m.SenderUserID, m.ItemID, m.ChatID, reason)
	a.logger.Info("AI 工具模式已通知真人卖家", "profile", profile.ID, "chat_id", m.ChatID, "reason_len", len([]rune(reason)))
	return "已通知卖家", nil
}

// claimHumanNotify 在冷却窗口内只允许首次通知。
func (a *AIReplierImpl) claimHumanNotify(chatID, buyerID string) bool {
	key := strings.TrimSpace(chatID)
	if key == "" {
		key = strings.TrimSpace(buyerID)
	}
	if key == "" {
		return true
	}
	now := time.Now()
	a.notifyMu.Lock()
	defer a.notifyMu.Unlock()
	if a.notifiedAt == nil {
		a.notifiedAt = make(map[string]time.Time)
	}
	if len(a.notifiedAt) > maxNotifyCooldowns {
		for candidate, at := range a.notifiedAt {
			if now.Sub(at) >= humanNotifyCooldown {
				delete(a.notifiedAt, candidate)
			}
		}
	}
	if last, ok := a.notifiedAt[key]; ok && now.Sub(last) < humanNotifyCooldown {
		return false
	}
	a.notifiedAt[key] = now
	return true
}
