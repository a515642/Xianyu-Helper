package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestAISimulationEndpoint 模拟对话复用真实助手配置，维护服务端内存历史，可重置。
func TestAISimulationEndpoint(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','item-1','测试商品')`); err != nil {
		t.Fatal(err)
	}
	requests := 0
	var lastMessages []any
	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastMessages, _ = body["messages"].([]any)
		requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant",
			"tool_calls": []any{map[string]any{
				"id":       "call-1",
				"type":     "function",
				"function": map[string]any{"name": "send_reply", "arguments": `{"messages":["你好","在的"]}`},
			}},
		}}}})
	}))
	t.Cleanup(aiSrv.Close)

	h := srv.Router()
	session := loginHelper(t, h)
	createBody := `{"cookie_id":"acc1","name":"模拟客服","enabled":true,"use_system_api":false,
		"api_key":"sk-test","base_url":"` + aiSrv.URL + `","model_name":"test-model",
		"reply_mode":"tools","max_bargain_rounds":3,"item_ids":["item-1"]}`
	create := httptest.NewRequest(http.MethodPost, "/ai-profiles", strings.NewReader(createBody))
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
	id := int64(profile["id"].(float64))

	simulate := func(body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/ai-profiles/"+strconv.FormatInt(id, 10)+"/simulate", strings.NewReader(body))
		req.AddCookie(session)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var decoded map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
		return rec.Code, decoded
	}

	code, first := simulate(`{"session_id":"sess-1","message":"在吗"}`)
	if code != http.StatusOK {
		t.Fatalf("first status=%d body=%v", code, first)
	}
	result, _ := first["result"].(map[string]any)
	segments, _ := result["segments"].([]any)
	if len(segments) != 2 || result["reply_text"] != "你好\n在的" {
		t.Fatalf("result=%+v", result)
	}
	if history, _ := first["history"].([]any); len(history) != 2 {
		t.Fatalf("history=%+v", first["history"])
	}

	code, second := simulate(`{"session_id":"sess-1","message":"便宜点"}`)
	if code != http.StatusOK {
		t.Fatalf("second status=%d body=%v", code, second)
	}
	if history, _ := second["history"].([]any); len(history) != 4 {
		t.Fatalf("expected 4 history entries, got %+v", second["history"])
	}
	// 第二次请求的上下文里应包含第一轮的买家消息和 AI 回复。
	var sawUser, sawAssistant bool
	for _, raw := range lastMessages {
		message, _ := raw.(map[string]any)
		if message["content"] == "在吗" {
			sawUser = true
		}
		if message["content"] == "你好\n在的" {
			sawAssistant = true
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("simulation history missing: %+v", lastMessages)
	}

	code, third := simulate(`{"session_id":"sess-1","message":"重新开始","reset":true}`)
	if code != http.StatusOK {
		t.Fatalf("reset status=%d body=%v", code, third)
	}
	if history, _ := third["history"].([]any); len(history) != 2 {
		t.Fatalf("reset history=%+v", third["history"])
	}
	if requests != 3 {
		t.Fatalf("LLM requests=%d want 3", requests)
	}

	var conversations int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_conversations`).Scan(&conversations); err != nil {
		t.Fatal(err)
	}
	if conversations != 0 {
		t.Fatalf("模拟不应写入真实对话，got %d", conversations)
	}
}
