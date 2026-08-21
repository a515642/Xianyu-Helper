package adapter

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

type diagnosticAccounts struct {
	sender automation.MessageSender
	ok     bool
}

func (a diagnosticAccounts) GetInstance(string) (automation.MessageSender, bool) {
	return a.sender, a.ok
}

type diagnosticSender struct {
	injected []engine.ChatMessage
}

func (s *diagnosticSender) SendText(context.Context, string, string, string) error { return nil }
func (s *diagnosticSender) SendImage(context.Context, string, string, string, int64) error {
	return nil
}
func (s *diagnosticSender) UpdateCookie(string) {}
func (s *diagnosticSender) InjectAIMessage(_ context.Context, message engine.ChatMessage) error {
	s.injected = append(s.injected, message)
	return nil
}

func diagnosticLogger(buffer *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestHandleOrderCreatedForAI_EmitsDiagnosticPath(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','item-1','商品')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,cookie_id,item_id,buyer_id,order_status,chat_id)
		VALUES ('order-created-1','cid','item-1','buyer-1','processing','chat-1')`); err != nil {
		t.Fatal(err)
	}
	profileID, err := store.AIProfiles.Create(ctx, db.AIProfile{
		CookieID: "cid", Name: "bargain", Enabled: true, BargainStrategyEnabled: true,
		MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AIProfiles.ReplaceItems(ctx, profileID, "cid", []string{"item-1"}); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	sender := &diagnosticSender{}
	a := New(store, nil, diagnosticLogger(&logs))
	a.SetAccounts(diagnosticAccounts{sender: sender, ok: true})
	if err := a.HandleSystemEvent(ctx, automation.Task{
		AccountID: "cid", CookieStr: "not-logged", TriggerType: automation.TriggerOrderCreated,
		OrderID: "order-created-1", ChatID: "chat-1", ItemID: "item-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.injected) != 1 {
		t.Fatalf("injected messages=%d, want 1", len(sender.injected))
	}
	message := sender.injected[0]
	if message.MessageID != "order-created:order-created-1" || message.ItemID != "item-1" || message.ChatID != "chat-1" || !strings.Contains(message.Text, "我已经拍下") || !strings.Contains(message.Text, "订单号是 order-created-1") {
		t.Fatalf("unexpected injected message=%+v", message)
	}
	for _, marker := range []string{
		"AI诊断：订单创建事件开始检查 AI 改价注入",
		"AI诊断：开始调用 InjectAIMessage",
		"AI诊断：InjectAIMessage 完成",
		"order_id=order-created-1",
		"chat_id=chat-1",
		"profile_id=",
	} {
		if !strings.Contains(logs.String(), marker) {
			t.Fatalf("diagnostic log missing %q:\n%s", marker, logs.String())
		}
	}
	if strings.Contains(logs.String(), "not-logged") || strings.Contains(logs.String(), "我已经拍下") {
		t.Fatalf("diagnostic log leaked credential or full synthetic text:\n%s", logs.String())
	}
}

func TestHandleOrderCreatedForAI_MaterializesEarlyEventFacts(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','item-1','商品')`); err != nil {
		t.Fatal(err)
	}
	profileID, err := store.AIProfiles.Create(ctx, db.AIProfile{CookieID: "cid", Name: "bargain", Enabled: true, BargainStrategyEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AIProfiles.ReplaceItems(ctx, profileID, "cid", []string{"item-1"}); err != nil {
		t.Fatal(err)
	}
	sender := &diagnosticSender{}
	a := New(store, nil, diagnosticLogger(&bytes.Buffer{}))
	a.SetAccounts(diagnosticAccounts{sender: sender, ok: true})
	if err := a.HandleSystemEvent(ctx, automation.Task{AccountID: "cid", TriggerType: automation.TriggerOrderCreated, OrderID: "order-early", ChatID: "chat-1", ItemID: "item-1", BuyerID: "buyer-1"}); err != nil {
		t.Fatal(err)
	}
	order, err := store.Orders.Get(ctx, "order-early")
	if err != nil || order.CookieID != "cid" || order.ChatID != "chat-1" || order.ItemID != "item-1" || order.BuyerID != "buyer-1" || db.NormalizeOrderStatus(order.OrderStatus) != "processing" {
		t.Fatalf("materialized order=%+v err=%v", order, err)
	}
	if len(sender.injected) != 1 {
		t.Fatalf("injected messages=%d, want 1", len(sender.injected))
	}
}

func TestHandleOrderCreatedForAI_SkipsNonPendingOrderWithDiagnostic(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','item-1','商品')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,cookie_id,item_id,buyer_id,order_status,chat_id)
		VALUES ('order-paid-1','cid','item-1','buyer-1','paid','chat-1')`); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	sender := &diagnosticSender{}
	a := New(store, nil, diagnosticLogger(&logs))
	a.SetAccounts(diagnosticAccounts{sender: sender, ok: true})
	if err := a.HandleSystemEvent(ctx, automation.Task{
		AccountID: "cid", TriggerType: automation.TriggerOrderCreated,
		OrderID: "order-paid-1", ChatID: "chat-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.injected) != 0 {
		t.Fatalf("paid order should not inject AI message: %+v", sender.injected)
	}
	if !strings.Contains(logs.String(), "not_pending_payment") {
		t.Fatalf("missing non-pending diagnostic:\n%s", logs.String())
	}
}
