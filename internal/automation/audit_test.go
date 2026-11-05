package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestAutomationAuditRecordsRunLifecycle 验证一次成功的自动发货会留下运行、动作与终态审计事件。
func TestAutomationAuditRecordsRunLifecycle(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-a','A',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,enabled,user_id) VALUES (71,'审计卡',1,?)`, admin.ID)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO card_items (card_id,user_id,content,status) VALUES (71,?,'CARD','available')`, admin.ID)
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-a", Name: "审计", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 71, DeliveryCount: 1, ConfigJSON: `{"spec_name":"套餐","spec_value":"标准"}`, Enabled: true, SortOrder: 1},
		},
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		SpecName: "套餐", SpecValue: "标准", Quantity: "1", Amount: "9.9", OrderStatus: "pending_ship",
	}})
	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-a", OrderID: "order-a", ItemID: "item-a", BuyerID: "buyer-a",
	}); err != nil {
		t.Fatalf("HandleTask: %v", err)
	}

	events, total, err := store.Automation.ListAuditEvents(ctx, db.AutomationAuditFilter{UserID: admin.ID, OrderID: "order-a"})
	if err != nil {
		t.Fatal(err)
	}
	if total < 3 {
		t.Fatalf("应至少记录 3 条审计事件，got %d: %#v", total, events)
	}
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.EventType] = true
		if event.Summary == "CARD" {
			t.Fatalf("审计事件不得包含卡密正文: %#v", event)
		}
	}
	for _, want := range []string{db.AuditEventRunStarted, db.AuditEventActionSucceeded, db.AuditEventRunFinished} {
		if !seen[want] {
			t.Fatalf("缺少审计事件 %s: %#v", want, seen)
		}
	}
}

// TestAutomationAuditRecordsSkippedWithoutRules 验证无匹配规则时留下跳过原因。
func TestAutomationAuditRecordsSkippedWithoutRules(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	center := New(store, nil, nil)
	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		OrderID: "order-none", ItemID: "item-none",
	}); err != nil {
		t.Fatal(err)
	}

	events, total, err := store.Automation.ListAuditEvents(ctx, db.AutomationAuditFilter{UserID: admin.ID, Status: db.AuditStatusSkipped})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("skipped events=%#v total=%d", events, total)
	}
	if events[0].EventType != db.AuditEventSkipped || events[0].Reason == "" {
		t.Fatalf("跳过原因缺失: %#v", events[0])
	}
}
