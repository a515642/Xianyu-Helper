package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/automation"
)

// echoRecordingHandler 记录 ChatEcho。
type echoRecordingHandler struct {
	echoes []ChatEcho
}

func (h *echoRecordingHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }
func (h *echoRecordingHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}
func (h *echoRecordingHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }
func (h *echoRecordingHandler) OnAccountAlert(context.Context, string, string, string, string) {
}
func (h *echoRecordingHandler) RecordChatEcho(_ context.Context, echo ChatEcho) error {
	h.echoes = append(h.echoes, echo)
	return nil
}

func selfEchoEnvelope(chatID, text, messageID, sendTime string) map[string]any {
	return map[string]any{
		"1": map[string]any{
			"2": chatID,
			"3": messageID,
			"10": map[string]any{
				"senderUserId":    "me-001",
				"reminderContent": text,
				"sendTime":        sendTime,
				"contentType":     "1",
			},
		},
	}
}

// TestSelfEchoIsRecordedButNotAutoReplied 自身回显只记录、不进入自动回复。
func TestSelfEchoIsRecordedButNotAutoReplied(t *testing.T) {
	handler := &echoRecordingHandler{}
	account := New(Config{CookieID: "account-1", CookieStr: "unb=me-001", Handler: handler})

	account.handleMessageContext(context.Background(), selfEchoEnvelope("chat-1", "老板在的，稍等", "m-1", "1700000000000"))

	if len(handler.echoes) != 1 {
		t.Fatalf("应记录 1 条回显，got %+v", handler.echoes)
	}
	got := handler.echoes[0]
	if got.ChatID != "chat-1" || got.Text != "老板在的，稍等" || got.MessageID != "m-1" || got.SentAt != 1700000000000 {
		t.Fatalf("回显字段异常: %+v", got)
	}
	// 回显的发送者是本账号，不能把卖家自己写成买家。
	if got.BuyerID != "" {
		t.Fatalf("回显不应把卖家写成买家: buyer_id=%q", got.BuyerID)
	}
}

// TestSelfEchoIgnoresAiPrefixedUserMessage AI/自动化发送不应触发真人让位。
func TestSelfEchoIgnoresAiPrefixedUserMessage(t *testing.T) {
	handler := &echoRecordingHandler{}
	account := New(Config{CookieID: "account-1", CookieStr: "unb=me-001", Handler: handler})

	account.handleMessageContext(context.Background(), selfEchoEnvelope("chat-1", "AI回复", "ai-abc", "1700000000000"))
	if len(handler.echoes) != 0 {
		t.Fatalf("ai- 前缀的发送不应记录为真人回复: %+v", handler.echoes)
	}
}

// TestSelfEchoIgnoresBusinessCards 交易卡片不属于真人聊天内容。
func TestSelfEchoIgnoresBusinessCards(t *testing.T) {
	handler := &echoRecordingHandler{}
	account := New(Config{CookieID: "account-1", CookieStr: "unb=me-001", Handler: handler})

	envelope := selfEchoEnvelope("chat-1", "卖家已发货", "m-card", "1700000000000")
	envelope["1"].(map[string]any)["10"].(map[string]any)["contentType"] = "26"
	account.handleMessageContext(context.Background(), envelope)
	if len(handler.echoes) != 0 {
		t.Fatalf("交易卡片不应记录 chat echo: %+v", handler.echoes)
	}
}
