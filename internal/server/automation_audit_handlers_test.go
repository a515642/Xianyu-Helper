package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/db"
)

// TestAutomationAuditEventsEndpoint 验证规则日志按登录用户查询并支持过滤。
func TestAutomationAuditEventsEndpoint(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)
	ctx := context.Background()

	if err := store.Automation.AppendAuditEvent(ctx, db.AutomationAuditEvent{
		CookieID: "acc1", OrderID: "order-1", RuleID: 3, RunID: 9,
		TriggerType: "order_paid", EventType: db.AuditEventRunStarted,
		Status: db.AuditStatusRunning, CreatedAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Automation.AppendAuditEvent(ctx, db.AutomationAuditEvent{
		CookieID: "acc1", OrderID: "order-1", RuleID: 3, RunID: 9,
		TriggerType: "order_paid", EventType: db.AuditEventRunFinished,
		Status: db.AuditStatusSuccess, Summary: "运行结束，已完成 1 个动作", CreatedAt: 101,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/automation-audit-events?cookie_id=acc1&run_id=9", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("查询失败: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []db.AutomationAuditEvent `json:"items"`
		Total int                       `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || len(body.Items) != 2 || body.Items[0].EventType != db.AuditEventRunFinished {
		t.Fatalf("返回异常: %+v", body)
	}

	bad := httptest.NewRequest(http.MethodGet, "/automation-audit-events?run_id=abc", nil)
	bad.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("非法 run_id 应 400: %d", badRec.Code)
	}
}
