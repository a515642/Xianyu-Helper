package adapter

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/chat"
	"xianyu-go/internal/engine"
)

// TestHandleOutgoingChatMessageSeparatesHumanFromAuto 验证真人发送与 AI/自动发送
// 在落库时被区分：只有真人发送会被计为"真人卖家回复"，用于 AI 让位。
func TestHandleOutgoingChatMessageSeparatesHumanFromAuto(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()

	a := New(store, nil, nil)
	a.SetChatService(chat.New(store))

	// 人类卖家手动发送（无关联待发消息时直接入库，落为人类发送）。
	if err := a.HandleOutgoingChatMessage(ctx, engine.OutgoingChatMessage{
		AccountID: "cid", ChatID: "chat-1", BuyerID: "buyer-1", Text: "老板在的",
	}); err != nil {
		t.Fatalf("human send: %v", err)
	}
	if at, ok, err := store.Chats.LastSellerOutgoingAt(ctx, "cid", "chat-1"); err != nil || !ok || at == 0 {
		t.Fatalf("真人发送应被记录: at=%d ok=%v err=%v", at, ok, err)
	}

	// AI 助手回复（Auto=true）。
	if err := a.HandleOutgoingChatMessage(ctx, engine.OutgoingChatMessage{
		AccountID: "cid", ChatID: "chat-2", BuyerID: "buyer-2", Text: "AI回复", Auto: true,
	}); err != nil {
		t.Fatalf("auto send: %v", err)
	}
	if _, ok, err := store.Chats.LastSellerOutgoingAt(ctx, "cid", "chat-2"); err != nil || ok {
		t.Fatalf("AI 发送不应计为真人回复: ok=%v err=%v", ok, err)
	}
}

// TestAdapterChatEchoFeedsTakeover 账号自身回显经 Adapter 落库后能触发让位。
func TestAdapterChatEchoFeedsTakeover(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()

	a := New(store, nil, nil)
	a.SetChatService(chat.New(store))

	if err := a.RecordChatEcho(ctx, engine.ChatEcho{
		AccountID: "cid", ChatID: "chat-echo", Text: "这是真人卖家在官方客户端的回复",
		MessageID: "m-echo", MessageType: "text", SentAt: time.Now().UTC().UnixMilli(),
	}); err != nil {
		t.Fatalf("RecordChatEcho: %v", err)
	}
	at, ok, err := store.Chats.LastSellerOutgoingAt(ctx, "cid", "chat-echo")
	if err != nil || !ok {
		t.Fatalf("回显应让 AI 让位: ok=%v err=%v", ok, err)
	}
	if time.Since(time.Unix(0, at*int64(time.Millisecond))) > 5*time.Minute {
		t.Fatalf("回显时间应在让位窗口内: at=%d", at)
	}
}
