package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestAITurnLogsEndpoint 观测列表按助手过滤，并带上工具调用明细。
func TestAITurnLogsEndpoint(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	h := srv.Router()
	session := loginHelper(t, h)
	create := httptest.NewRequest(http.MethodPost, "/ai-profiles", strings.NewReader(`{"cookie_id":"acc1","name":"观测助手","enabled":true,"use_system_api":true,"reply_mode":"tools","max_bargain_rounds":3}`))
	create.AddCookie(session)
	created := httptest.NewRecorder()
	h.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var profile map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	profileID := int64(profile["id"].(float64))

	if _, err := store.AILogs.Insert(ctx, db.AITurnLog{
		AIProfileID: profileID, CookieID: "acc1", ChatID: "chat-real", Outcome: "sent", ReplyText: "真实回复", ReplyMode: "tools",
	}, []db.AIToolCallLog{{Round: 1, ToolName: "send_reply", Arguments: `{"messages":["真实回复"]}`, Result: "已记录回复", Success: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AILogs.Insert(ctx, db.AITurnLog{
		AIProfileID: profileID, CookieID: "acc1", ChatID: "chat-sim", Simulated: true, SessionID: "sim-1", Outcome: "sent", ReplyText: "模拟回复",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AILogs.Insert(ctx, db.AITurnLog{
		AIProfileID: profileID + 1, CookieID: "acc1", ChatID: "other-assistant", Outcome: "sent", ReplyText: "别的助手",
	}, nil); err != nil {
		t.Fatal(err)
	}

	fetch := func(query string) (int, db.AITurnLogPage) {
		req := httptest.NewRequest(http.MethodGet, "/ai-profiles/"+strconv.FormatInt(profileID, 10)+"/turn-logs"+query, nil)
		req.AddCookie(session)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var page db.AITurnLogPage
		_ = json.Unmarshal(rec.Body.Bytes(), &page)
		return rec.Code, page
	}

	code, page := fetch("?simulated=real")
	if code != http.StatusOK || page.Total != 1 || len(page.Logs) != 1 {
		t.Fatalf("real code=%d page=%+v", code, page)
	}
	if page.Logs[0].ChatID != "chat-real" || page.Logs[0].Simulated || page.Logs[0].CreatedAt == "" {
		t.Fatalf("real log = %+v", page.Logs[0])
	}
	if len(page.Logs[0].ToolCalls) != 1 || page.Logs[0].ToolCalls[0].ToolName != "send_reply" || !page.Logs[0].ToolCalls[0].Success {
		t.Fatalf("tool calls = %+v", page.Logs[0].ToolCalls)
	}

	code, page = fetch("?simulated=all&page_size=10")
	if code != http.StatusOK || page.Total != 2 || len(page.Logs) != 2 {
		t.Fatalf("all code=%d page=%+v", code, page)
	}
	if page.PageSize != 10 || !page.Logs[0].Simulated || page.Logs[0].SessionID != "sim-1" {
		t.Fatalf("all page = %+v", page)
	}

	code, _ = fetch("?simulated=bogus")
	if code != http.StatusBadRequest {
		t.Fatalf("invalid filter status=%d", code)
	}
}
