package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// newTakeoverStore 建一个带聊天的测试库。
func newTakeoverStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "takeover.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d, db.DialectSQLite)
	s.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(context.Background(), "admin")
	s.Cookies.Save(context.Background(), "cid", "unb=123;", admin.ID)
	return s, func() { d.Close() }
}

func seedOutgoing(t *testing.T, s *db.Store, chatID, key, text string, sentAt int64) {
	t.Helper()
	session := db.ChatSession{CookieID: "cid", ChatID: chatID, BuyerID: "buyer1", BuyerName: "买家"}
	message := db.ChatMessage{MessageKey: key, Direction: "outgoing", SenderID: "cid",
		SenderName: "我", MessageType: "text", Content: text, Status: "sent", SentAt: sentAt}
	if _, _, err := s.Chats.SaveMessage(context.Background(), session, message, false); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
}

// TestLastSellerOutgoingAtIgnoresAutoSends 只有真人发送计入让位时间。
func TestLastSellerOutgoingAtIgnoresAutoSends(t *testing.T) {
	s, cleanup := newTakeoverStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().UnixMilli()

	if _, ok, err := s.Chats.LastSellerOutgoingAt(ctx, "cid", "chat1"); err != nil || ok {
		t.Fatalf("空会话应无记录: ok=%v err=%v", ok, err)
	}
	// AI 自身的回复不能触发让位。
	seedOutgoing(t, s, "chat1", "ai-1", "AI回复", now)
	if _, ok, err := s.Chats.LastSellerOutgoingAt(ctx, "cid", "chat1"); err != nil || ok {
		t.Fatalf("ai- 前缀不应计为真人回复: ok=%v err=%v", ok, err)
	}
	// 手动发送计入。
	seedOutgoing(t, s, "chat1", "local-1", "老板在的", now)
	got, ok, err := s.Chats.LastSellerOutgoingAt(ctx, "cid", "chat1")
	if err != nil || !ok || got != now {
		t.Fatalf("真人回复应被识别: got=%d ok=%v err=%v", got, ok, err)
	}
}

// TestHumanTakeoverSuppressesAutoReply 真人卖家回复后 AI 让位。
func TestHumanTakeoverSuppressesAutoReply(t *testing.T) {
	s, cleanup := newTakeoverStore(t)
	defer cleanup()
	ctx := context.Background()
	r := NewReplyService("cid", s, nil, nil, nil, nil)
	now := time.Now().UTC().UnixMilli()

	// 无真人回复 → 不让位。
	if r.humanTakeoverActive(ctx, chatMsg("在吗", "item1", "chat-none")) {
		t.Fatal("无真人回复时不应让位")
	}
	// 最近一条是 3 分钟前的真人回复 → 让位（未满 5 分钟）。
	seedOutgoing(t, s, "chat-recent", "local-1", "在的", now-3*60*1000)
	if !r.humanTakeoverActive(ctx, chatMsg("在吗", "item1", "chat-recent")) {
		t.Fatal("真人刚回复后应让位")
	}
	// 真人回复已超过 5 分钟 → 恢复 AI 接管。
	seedOutgoing(t, s, "chat-stale", "local-2", "稍等", now-6*60*1000)
	if r.humanTakeoverActive(ctx, chatMsg("在吗", "item1", "chat-stale")) {
		t.Fatal("超过让位窗口后应恢复 AI 接管")
	}
	// 真人最后一条在 6 分钟前，但 AI 自己 1 分钟前刚回复过 → 仍应恢复 AI 接管。
	seedOutgoing(t, s, "chat-stale", "ai-9", "AI回复", now-60*1000)
	if r.humanTakeoverActive(ctx, chatMsg("在吗", "item1", "chat-stale")) {
		t.Fatal("AI 自身回复不应延长让位窗口")
	}
	// 真人 6 分钟前回复过，之后 1 分钟前又手动回复一次 → 仍在窗口内。
	seedOutgoing(t, s, "chat-followup", "local-3", "稍等", now-6*60*1000)
	seedOutgoing(t, s, "chat-followup", "local-4", "来了", now-60*1000)
	if !r.humanTakeoverActive(ctx, chatMsg("在吗", "item1", "chat-followup")) {
		t.Fatal("真人持续回复应以最后一条顺延窗口")
	}
}

// TestReplyHandleSkipsDuringTakeover 让位期间 Handle 不产生回复。
func TestReplyHandleSkipsDuringTakeover(t *testing.T) {
	s, cleanup := newTakeoverStore(t)
	defer cleanup()
	ctx := context.Background()
	// 让关键词一定命中，确保"没回复"是因为让位而不是没内容。
	s.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','在的','text')`)
	seedOutgoing(t, s, "chat1", "local-1", "在的", time.Now().UTC().UnixMilli())

	sender := &countingSender{}
	r := NewReplyService("cid", s, sender, nil, nil, nil)
	if err := r.Handle(ctx, chatMsg("老板在吗", "item1", "chat1")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sender.texts != 0 {
		t.Fatalf("让位期间不应自动回复，got %d", sender.texts)
	}

	// 另一个会话：真人回复已超过让位窗口，应恢复正常回复。
	seedOutgoing(t, s, "chat2", "local-2", "稍等", time.Now().Add(-6*time.Minute).UTC().UnixMilli())
	if err := r.Handle(ctx, chatMsg("老板在吗", "item1", "chat2")); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sender.texts != 1 {
		t.Fatalf("让位结束后应恢复回复，got %d", sender.texts)
	}
}

type countingSender struct {
	texts int
}

func (s *countingSender) SendText(context.Context, string, string, string) error {
	s.texts++
	return nil
}
func (s *countingSender) SendImage(context.Context, string, string, string, int64) error { return nil }
