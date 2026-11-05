package db

import (
	"context"
	"testing"
)

// TestAutomationAuditEvents 验证审计事件按用户隔离、支持过滤并可按保留期清理。
func TestAutomationAuditEvents(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	if ok, err := store.Users.Create(ctx, "u1", "u1@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create u1: %v, %v", ok, err)
	}
	if ok, err := store.Users.Create(ctx, "u2", "u2@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create u2: %v, %v", ok, err)
	}
	u1, _ := store.Users.GetByUsername(ctx, "u1")
	u2, _ := store.Users.GetByUsername(ctx, "u2")
	if err := store.Cookies.Save(ctx, "acc1", "unb=1", u1.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.Save(ctx, "acc2", "unb=2", u2.ID); err != nil {
		t.Fatal(err)
	}

	appendEvent := func(cookieID, eventType, status string, runID, createdAt int64) {
		t.Helper()
		if err := store.Automation.AppendAuditEvent(ctx, AutomationAuditEvent{
			CookieID: cookieID, OrderID: "order1", RuleID: 7, RunID: runID,
			TriggerType: "order_paid", EventType: eventType, Status: status,
			ActionType: "send_card", Summary: "发送卡密「测试」×1", CreatedAt: createdAt,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	appendEvent("acc1", AuditEventRunStarted, AuditStatusRunning, 11, 1000)
	appendEvent("acc1", AuditEventActionSucceeded, AuditStatusSuccess, 11, 1001)
	appendEvent("acc1", AuditEventRunFinished, AuditStatusSuccess, 11, 1002)
	appendEvent("acc2", AuditEventSkipped, AuditStatusSkipped, 0, 1003)

	events, total, err := store.Automation.ListAuditEvents(ctx, AutomationAuditFilter{UserID: u1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(events) != 3 {
		t.Fatalf("u1 total=%d len=%d want 3", total, len(events))
	}
	if events[0].EventType != AuditEventRunFinished || events[0].UserID != u1.ID {
		t.Fatalf("应按时间倒序且归属 u1: %#v", events[0])
	}

	byRun, total, err := store.Automation.ListAuditEvents(ctx, AutomationAuditFilter{UserID: u1.ID, RunID: 11, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(byRun) != 2 {
		t.Fatalf("run filter total=%d len=%d want 3/2", total, len(byRun))
	}

	byStatus, total, err := store.Automation.ListAuditEvents(ctx, AutomationAuditFilter{UserID: u2.ID, Status: AuditStatusSkipped})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(byStatus) != 1 || byStatus[0].CookieID != "acc2" {
		t.Fatalf("status filter events=%#v total=%d", byStatus, total)
	}

	removed, err := store.Automation.PruneAuditEvents(ctx, 1002)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("pruned=%d want 2", removed)
	}
	remaining, total, _ := store.Automation.ListAuditEvents(ctx, AutomationAuditFilter{UserID: u1.ID})
	if total != 1 || len(remaining) != 1 {
		t.Fatalf("remaining total=%d len=%d", total, len(remaining))
	}
}
