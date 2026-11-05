package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestIsPlatformBlockErr 验证风控、会话失效和普通错误的阻断分类，nil 与普通错误不得触发阻断。
func TestIsPlatformBlockErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"普通错误", errors.New("temporary"), false},
		{"风控", errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR"), true},
		{"会话失效", &mtop.SessionExpiredError{API: "自动评价接口", Ret: []string{"FAIL_SYS_SESSION_EXPIRED"}}, true},
		{"包装后的风控", errors.Join(errors.New("自动化动作结果需要人工核对"), errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR")), true},
	}
	for _, tc := range cases {
		if got := isPlatformBlockErr(tc.err); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

// TestRunDeferredTasksStopsAccountAfterRiskVerification 验证同账号首个延迟任务触发风控后，
// 其余任务不再执行，保持 running 等待租约到期，而不是继续请求平台。
func TestRunDeferredTasksStopsAccountAfterRiskVerification(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "risk-stop", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "x", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}

	orderIDs := []string{"risk-1", "risk-2"}
	for _, orderID := range orderIDs {
		task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: orderID, ChatID: "chat", BuyerID: "buyer"}
		raw, _ := json.Marshal(task)
		if err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
			TaskKey: "cid:buyer_reviewed:" + orderID, CookieID: "cid", TriggerType: TriggerBuyerReviewed,
			TaskJSON: string(raw), DueAt: 0,
		}); err != nil {
			t.Fatal(err)
		}
	}

	sender := &testSender{err: errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR")}
	center := New(store, testSenderProvider{sender: sender}, nil)
	(&Scheduler{center: center}).runDeferredTasks(ctx)

	statusOf := func(orderID string) string {
		var status string
		if err := store.DB.QueryRowContext(ctx,
			`SELECT status FROM automation_pending_tasks WHERE task_key=?`, "cid:buyer_reviewed:"+orderID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if got := statusOf("risk-1"); got != "pending" {
		t.Fatalf("首个任务应回到 pending 等待重试, got %s", got)
	}
	if got := statusOf("risk-2"); got != "running" {
		t.Fatalf("同账号后续任务应被跳过并保持 running, got %s", got)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("风控失败不应记录成功发送: %v", sender.texts)
	}
}

// TestRunRecoveryTasksStopsAccountAfterRiskVerification 验证失败运行恢复遇到风控后，
// 同账号的其余运行留待下一轮，不再请求平台。
func TestRunRecoveryTasksStopsAccountAfterRiskVerification(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "recovery-risk-stop", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "x", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	runIDs := make([]int64, 0, 2)
	for _, orderID := range []string{"recovery-risk-1", "recovery-risk-2"} {
		task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: orderID, ChatID: "chat", BuyerID: "buyer"}
		raw, _ := json.Marshal(task)
		runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
			RuleID: ruleID, CookieID: "cid", ItemID: "item-1", OrderID: orderID,
			BuyerID: "buyer", ChatID: "chat", TriggerType: TriggerBuyerReviewed,
			TriggerKey: "buyer_reviewed:" + orderID, RawEventJSON: string(raw),
		})
		if err != nil || !started {
			t.Fatalf("start run %s: id=%d started=%v err=%v", orderID, runID, started, err)
		}
		runIDs = append(runIDs, runID)
	}
	// 让两笔运行立即进入可恢复集合。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id IN (?,?)`, runIDs[0], runIDs[1]); err != nil {
		t.Fatal(err)
	}

	sender := &testSender{err: errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR")}
	center := New(store, testSenderProvider{sender: sender}, nil)
	(&Scheduler{center: center}).runRecoveryTasks(ctx)

	statusOf := func(runID int64) string {
		var status string
		if err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE id=?`, runID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if got := statusOf(runIDs[0]); got != "needs_review" {
		t.Fatalf("首个运行遇到风控应转人工核对, got %s", got)
	}
	if got := statusOf(runIDs[1]); got != "running" {
		t.Fatalf("同账号后续运行应被跳过并保持 running, got %s", got)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("风控失败不应记录成功发送: %v", sender.texts)
	}
}

// TestAccountTaskRateStopsOnRiskVerification 验证自动评价批次遇到风控后立即停止，
// 不再处理同批剩余订单。
func TestAccountTaskRateStopsOnRiskVerification(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	client := &fakeAccountTaskClient{
		pending: []mtop.PendingRateOrder{{TradeID: "rate-1"}, {TradeID: "rate-2"}, {TradeID: "rate-3"}},
		rateErr: errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR"),
	}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{
		CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00",
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if !mtop.IsRiskVerificationErr(err) {
		t.Fatalf("应返回风控错误: %v", err)
	}
	if client.rateCalls != 1 || summary.Failed != 1 {
		t.Fatalf("风控后应停止批次: rateCalls=%d failed=%d", client.rateCalls, summary.Failed)
	}
}

// TestPrepareTaskSkipsOrderDetailWhenPaidFactsComplete 验证付款任务本地订单事实齐全时
// 不再请求平台订单详情，缺少规格时仍会请求并补齐。
func TestPrepareTaskSkipsOrderDetailWhenPaidFactsComplete(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	calls := 0
	center := New(store, nil, nil)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{
		SpecName: "套餐", SpecValue: "30天", Quantity: "1", Amount: "9.9",
	}, calls: &calls})
	paid := func(orderID string) Task {
		return Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: orderID,
			ItemID: "item-1", BuyerID: "buyer", ChatID: "chat"}
	}

	if err := store.Orders.Upsert(ctx, "facts-full", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "item-1", BuyerID: "buyer", ChatID: "chat",
		SpecName: "套餐", SpecValue: "90天", Quantity: "2", Amount: "19.8",
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := center.prepareTask(ctx, paid("facts-full"))
	if err != nil || calls != 0 {
		t.Fatalf("事实齐全不应请求订单详情: calls=%d err=%v", calls, err)
	}
	if prepared.SpecValue != "90天" || prepared.Quantity != "2" {
		t.Fatalf("应沿用本地订单事实: %+v", prepared)
	}

	if err := store.Orders.Upsert(ctx, "facts-missing", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "item-1", BuyerID: "buyer", ChatID: "chat", Quantity: "1", Amount: "9.9",
	}); err != nil {
		t.Fatal(err)
	}
	filled, err := center.prepareTask(ctx, paid("facts-missing"))
	if err != nil || calls != 1 || filled.SpecValue != "30天" {
		t.Fatalf("缺规格应请求并补齐: calls=%d spec=%q err=%v", calls, filled.SpecValue, err)
	}
}
