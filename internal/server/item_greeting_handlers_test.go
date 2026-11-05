package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestItemGreetingLifecycle 验证商品首次回复规则的创建、列表、冲突、更新与删除。
func TestItemGreetingLifecycle(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	created := do(http.MethodPost, "/api/reply-rules/greetings",
		`{"cookie_id":"acc1","name":"默认问候","enabled":true,"reply_content":"你好 {send_user_name}","item_ids":["item-1","item-2"]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("创建失败: %d %s", created.Code, created.Body.String())
	}
	var rule map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	id := int64(rule["id"].(float64))

	list := do(http.MethodGet, "/api/reply-rules/greetings?cookie_id=acc1", "")
	if list.Code != http.StatusOK {
		t.Fatalf("列表失败: %d", list.Code)
	}
	var rules []map[string]any
	json.Unmarshal(list.Body.Bytes(), &rules)
	if len(rules) != 1 || len(rules[0]["item_ids"].([]any)) != 2 {
		t.Fatalf("列表异常: %+v", rules)
	}

	conflict := do(http.MethodPost, "/api/reply-rules/greetings",
		`{"cookie_id":"acc1","enabled":true,"reply_content":"冲突","item_ids":["item-2"]}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("商品冲突应 409: %d %s", conflict.Code, conflict.Body.String())
	}

	updated := do(http.MethodPut, "/api/reply-rules/greetings/"+strconv.FormatInt(id, 10),
		`{"name":"改名","enabled":false,"reply_content":"新内容","item_ids":["item-3"]}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("更新失败: %d %s", updated.Code, updated.Body.String())
	}

	bad := do(http.MethodPost, "/api/reply-rules/greetings",
		`{"cookie_id":"acc1","enabled":true,"reply_content":"","item_ids":["item-9"]}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("空内容应 400: %d", bad.Code)
	}

	cleared := do(http.MethodPost, "/api/reply-rules/greetings/records/acc1/clear", "")
	if cleared.Code != http.StatusOK {
		t.Fatalf("清空记录失败: %d", cleared.Code)
	}

	deleted := do(http.MethodDelete, "/api/reply-rules/greetings/"+strconv.FormatInt(id, 10), "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("删除失败: %d", deleted.Code)
	}
	missing := do(http.MethodDelete, "/api/reply-rules/greetings/"+strconv.FormatInt(id, 10), "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("重复删除应 404: %d", missing.Code)
	}
}
