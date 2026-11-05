package automation

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

func seedPaidDeliveryRule(t *testing.T, store *db.Store) (ruleID, cardID int64) {
	t.Helper()
	ctx := context.Background()
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-1','会员',0)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	cardID, err = store.Cards.Create(ctx, &db.CardFull{Name: "付款库存", Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatalf("create card: %v", err)
	}
	if _, err := store.Cards.AddItems(ctx, cardID, "SECRET-1\nSECRET-2"); err != nil {
		t.Fatalf("add card items: %v", err)
	}
	ruleID, err = store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true, SortOrder: 1},
		},
	})
	if err != nil || ruleID == 0 {
		t.Fatalf("create automation rule: id=%d err=%v", ruleID, err)
	}
	return ruleID, cardID
}

func automationRunCount(t *testing.T, store *db.Store, orderID string) int {
	t.Helper()
	var count int
	if err := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM automation_runs WHERE order_id=?`, orderID).Scan(&count); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	return count
}

func automationRunStatus(t *testing.T, store *db.Store, runID int64) (string, int) {
	t.Helper()
	var status string
	var sent int
	if err := store.DB.QueryRowContext(context.Background(), `SELECT status,sent_count FROM automation_runs WHERE id=?`, runID).Scan(&status, &sent); err != nil {
		t.Fatalf("read run: %v", err)
	}
	return status, sent
}

func TestPaidDeliverySkipsZeroAmountEvent(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	seedPaidDeliveryRule(t, store)

	sender := &testSender{}
	notifier := &recordingNotifier{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetNotifier(notifier)

	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-zero", OrderID: "order-zero", ItemID: "item-1", BuyerID: "buyer-1",
		Quantity: "1", Amount: "0.00",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("零金额订单不应发卡密: %v", sender.texts)
	}
	if count := automationRunCount(t, store, "order-zero"); count != 0 {
		t.Fatalf("零金额订单不应创建自动化运行，count=%d", count)
	}
	if messages := notifier.messages(); len(messages) != 0 {
		t.Fatalf("零金额订单不应发送人工告警: %v", messages)
	}
}

func TestPaidDeliverySkipsZeroAmountAfterDetailCompletion(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	seedPaidDeliveryRule(t, store)

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		Quantity: "1", Amount: "¥0.00", OrderStatus: "pending_ship",
	}})

	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-zero-detail", OrderID: "order-zero-detail", ItemID: "item-1", BuyerID: "buyer-1",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("详情补全后金额仍为零时不应发卡密: %v", sender.texts)
	}
	if count := automationRunCount(t, store, "order-zero-detail"); count != 0 {
		t.Fatalf("零金额订单不应创建自动化运行，count=%d", count)
	}
}

func TestPaidDeliveryAllowsPositiveAmount(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	seedPaidDeliveryRule(t, store)

	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)

	err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-paid", OrderID: "order-paid", ItemID: "item-1", BuyerID: "buyer-1",
		Quantity: "1", Amount: "9.90",
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "SECRET-1" {
		t.Fatalf("正金额订单应正常发卡，texts=%v", sender.texts)
	}
}

func TestPaidDeliveryCancelsZeroProgressRunOnZeroAmount(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ruleID, _ := seedPaidDeliveryRule(t, store)

	runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "cid", ItemID: "item-1", OrderID: "order-zero-run",
		BuyerID: "buyer-1", ChatID: "chat-zero-run", TriggerType: TriggerOrderPaid,
		TriggerKey: "zero-run-1", RawEventJSON: "{}",
	})
	if err != nil || !started {
		t.Fatalf("start run: id=%d started=%v err=%v", runID, started, err)
	}

	sender := &testSender{}
	notifier := &recordingNotifier{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetNotifier(notifier)
	err = center.HandleTask(ctx, Task{
		Source: "recovery", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-zero-run", OrderID: "order-zero-run", ItemID: "item-1", BuyerID: "buyer-1",
		Quantity: "1", Amount: "0.00",
		Raw: map[string]any{"automation_run_id": float64(runID)},
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	status, sent := automationRunStatus(t, store, runID)
	if status != "canceled" || sent != 0 {
		t.Fatalf("零进度运行应取消，status=%s sent=%d", status, sent)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("取消的零金额运行不应发卡: %v", sender.texts)
	}
	if messages := notifier.messages(); len(messages) != 0 {
		t.Fatalf("零进度运行取消不应发人工告警: %v", messages)
	}
}

func TestPaidDeliveryQuarantinesProgressRunOnZeroAmount(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ruleID, _ := seedPaidDeliveryRule(t, store)

	runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "cid", ItemID: "item-1", OrderID: "order-zero-progress",
		BuyerID: "buyer-1", ChatID: "chat-zero-progress", TriggerType: TriggerOrderPaid,
		TriggerKey: "zero-progress-1", RawEventJSON: "{}",
	})
	if err != nil || !started {
		t.Fatalf("start run: id=%d started=%v err=%v", runID, started, err)
	}
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET sent_count=1, action_cursor=1 WHERE id=?`, runID); err != nil {
		t.Fatalf("seed progress: %v", err)
	}

	sender := &testSender{}
	notifier := &recordingNotifier{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetNotifier(notifier)
	err = center.HandleTask(ctx, Task{
		Source: "recovery", AccountID: "cid", TriggerType: TriggerOrderPaid,
		ChatID: "chat-zero-progress", OrderID: "order-zero-progress", ItemID: "item-1", BuyerID: "buyer-1",
		Quantity: "1", Amount: "0.00",
		Raw: map[string]any{"automation_run_id": float64(runID)},
	})
	if err != nil {
		t.Fatalf("HandleTask: %v", err)
	}
	status, sent := automationRunStatus(t, store, runID)
	if status != "needs_review" || sent != 1 {
		t.Fatalf("已有进度运行应转人工核对，status=%s sent=%d", status, sent)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("人工核对不应继续发卡: %v", sender.texts)
	}
	messages := notifier.messages()
	if len(messages) != 1 || !strings.Contains(messages[0], "需要人工核对") {
		t.Fatalf("已有进度运行应发送人工核对通知，messages=%v", messages)
	}
}

func TestPaidAmountBlockedReason(t *testing.T) {
	withFetcher := &Center{fetcher: testFetcher{}}
	withoutFetcher := &Center{}
	cases := []struct {
		name    string
		center  *Center
		amount  string
		blocked bool
		reason  string
	}{
		{"zero", withoutFetcher, "0", true, reasonOrderAmountNotPositive},
		{"zero decimal", withoutFetcher, "0.00", true, reasonOrderAmountNotPositive},
		{"zero with symbol", withoutFetcher, "¥0.00", true, reasonOrderAmountNotPositive},
		{"invalid", withoutFetcher, "abc", true, reasonOrderAmountNotPositive},
		{"positive", withoutFetcher, "0.01", false, ""},
		{"positive grouped", withoutFetcher, "¥1,234.00", false, ""},
		{"missing without fetcher", withoutFetcher, "", false, ""},
		{"missing with fetcher", withFetcher, "", true, reasonOrderAmountMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := tc.center.paidAmountBlockedReason(tc.amount)
			if (reason != "") != tc.blocked || (tc.reason != "" && reason != tc.reason) {
				t.Fatalf("amount=%q reason=%q want blocked=%v reason=%q", tc.amount, reason, tc.blocked, tc.reason)
			}
		})
	}
}
