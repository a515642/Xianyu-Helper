package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTestAIConnectionNormalizesDomainOnly 只填域名时测试连通性必须补全 /v1。
func TestTestAIConnectionNormalizesDomainOnly(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi there"}}]}`))
	}))
	defer upstream.Close()

	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 只填域名，不带 /v1。
	if err := store.Settings.Set(ctx, "ai_api_url", upstream.URL); err != nil {
		t.Fatal(err)
	}
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatal(err)
	}
	if err := store.Settings.Set(ctx, "ai_model", "test-model"); err != nil {
		t.Fatal(err)
	}

	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPost, "/ai-test", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("只填域名应补全为 /v1/chat/completions, got %q", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("model=%v", gotBody["model"])
	}
	messages, _ := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages=%v", gotBody["messages"])
	}
	if first, _ := messages[0].(map[string]any); first["content"] != aiTestPrompt {
		t.Fatalf("应发送 %q 探测消息, got %v", aiTestPrompt, messages[0])
	}
	var res map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["reply"] != "hi there" || res["endpoint"] != upstream.URL+"/v1/chat/completions" {
		t.Fatalf("response=%+v", res)
	}
}

// TestRequestAIChatCompletionExplainsEmptyContent 空正文时必须说明原因，
// 不能把空回复伪装成正常结果（否则用户会以为"模型刚好也回了 hi"）。
func TestRequestAIChatCompletionExplainsEmptyContent(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantSubstr string
	}{
		{
			name:       "thinking_only_truncated",
			body:       `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":"用户在打招呼，我该回一句你好"}}]}`,
			wantSubstr: "思考",
		},
		{
			name:       "empty_content",
			body:       `{"choices":[{"finish_reason":"stop","message":{"content":"   "}}]}`,
			wantSubstr: "空内容",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			result, err := requestAIChatCompletion(context.Background(), upstream.URL, "sk", "m", "hi")
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if result.Reply == "" || !strings.Contains(result.Reply, tc.wantSubstr) {
				t.Fatalf("空正文应给出原因(%q), got %q", tc.wantSubstr, result.Reply)
			}
		})
	}
}

// TestRequestAIChatCompletionPayload 探测请求只发一条 hi，且显式关闭思考。
func TestRequestAIChatCompletionPayload(t *testing.T) {
	var body map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"hello"}}]}`))
	}))
	defer upstream.Close()
	if _, err := requestAIChatCompletion(context.Background(), upstream.URL, "sk", "m", "hi"); err != nil {
		t.Fatal(err)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("只应发送一条消息: %v", body["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "hi" {
		t.Fatalf("探测消息应为 user/hi: %v", first)
	}
	thinking, _ := body["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("应显式关闭思考: %v", body["thinking"])
	}
	if tokens, _ := body["max_tokens"].(float64); tokens < 128 {
		t.Fatalf("预算过小会让思考模型正文为空: %v", body["max_tokens"])
	}
}

// TestTestAIConnectionRejectsInvalidURL 非法地址应返回 400 而不是 500。
func TestTestAIConnectionRejectsInvalidURL(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	_ = store.Settings.Set(ctx, "ai_api_key", "sk-test")
	_ = store.Settings.Set(ctx, "ai_model", "m")

	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPost, "/ai-test", strings.NewReader(`{"base_url":"https://"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法地址应 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}
