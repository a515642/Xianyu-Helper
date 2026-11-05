package engine

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestItemGreeting_SendsOncePerChat 验证商品首次回复按会话去重，并在新会话再次触发。
func TestItemGreeting_SendsOncePerChat(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.ItemGreetings.Create(ctx, db.ItemGreetingRule{
		CookieID: "cid", Enabled: true, ReplyContent: "你好 {send_user_name}，这件商品在的",
		ItemIDs: []string{"item-greet"},
	}); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	r := NewReplyService("cid", s, sender, nil, nil, nil)

	if err := r.Handle(ctx, chatMsg("在吗", "item-greet", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Handle(ctx, chatMsg("还在吗", "item-greet", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Handle(ctx, chatMsg("在吗", "item-greet", "chat-2")); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 2 {
		t.Fatalf("每个会话只应发送一次首次回复: %+v", sender.texts)
	}
	if sender.texts[0].text != "你好 买家，这件商品在的" || sender.texts[1].chatID != "chat-2" {
		t.Fatalf("首次回复内容或会话异常: %+v", sender.texts)
	}
}

// TestItemGreeting_SuppressesAccountReplyOnce 验证商品级首次回复优先于账号级“仅回复一次”的默认回复。
func TestItemGreeting_SuppressesAccountReplyOnce(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.ItemGreetings.Create(ctx, db.ItemGreetingRule{
		CookieID: "cid", Enabled: true, ReplyContent: "商品首次回复", ItemIDs: []string{"item-greet"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'账号默认回复',1)`); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	r := NewReplyService("cid", s, sender, nil, nil, nil)
	if err := r.Handle(ctx, chatMsg("在吗", "item-greet", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 || sender.texts[0].text != "商品首次回复" {
		t.Fatalf("应只发送商品首次回复: %+v", sender.texts)
	}
}

// greetingFailOnceSender 首次发送文本失败，用于模拟首次回复失败但正常回复仍可继续。
type greetingFailOnceSender struct {
	recordingSender
	failed bool
}

func (s *greetingFailOnceSender) SendText(ctx context.Context, chatID, toUserID, text string) error {
	if !s.failed {
		s.failed = true
		return errors.New("greeting send failed")
	}
	return s.recordingSender.SendText(ctx, chatID, toUserID, text)
}

// TestItemGreeting_FailureDoesNotBlockNormalReply 验证首次回复发送失败不影响本条消息的正常回复。
func TestItemGreeting_FailureDoesNotBlockNormalReply(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.ItemGreetings.Create(ctx, db.ItemGreetingRule{
		CookieID: "cid", Enabled: true, ReplyContent: "商品首次回复", ItemIDs: []string{"item-greet"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'账号默认回复',0)`); err != nil {
		t.Fatal(err)
	}
	sender := &greetingFailOnceSender{}
	r := NewReplyService("cid", s, sender, nil, nil, nil)
	if err := r.Handle(ctx, chatMsg("在吗", "item-greet", "chat-1")); err != nil {
		t.Fatalf("正常回复不应被首次回复失败阻断: %v", err)
	}
	if len(sender.texts) != 1 || sender.texts[0].text != "账号默认回复" {
		t.Fatalf("应继续发送账号默认回复: %+v", sender.texts)
	}
}

// TestItemGreeting_DisabledRuleIgnored 验证停用规则不会触发首次回复。
func TestItemGreeting_DisabledRuleIgnored(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := s.ItemGreetings.Create(ctx, db.ItemGreetingRule{
		CookieID: "cid", Enabled: false, ReplyContent: "不应发送", ItemIDs: []string{"item-greet"},
	}); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	r := NewReplyService("cid", s, sender, nil, nil, nil)
	if err := r.Handle(ctx, chatMsg("在吗", "item-greet", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("停用规则不应发送: %+v", sender.texts)
	}
}
