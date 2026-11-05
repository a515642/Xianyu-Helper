package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestKeywordExpressionCRUD 验证关键词多表达式/正则的保存、列表和校验。
func TestKeywordExpressionCRUD(t *testing.T) {
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

	created := do(http.MethodPost, "/keywords-with-item-id/acc1",
		`{"keyword":"","expressions":["订单\\d+","发货"],"match_type":"regexp","reply":"已收到","type":"text"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", created.Code, created.Body.String())
	}
	var createdBody map[string]any
	json.Unmarshal(created.Body.Bytes(), &createdBody)
	id := int64(createdBody["id"].(float64))

	list := do(http.MethodGet, "/keywords-with-type/acc1", "")
	if list.Code != http.StatusOK {
		t.Fatalf("列表失败: %d", list.Code)
	}
	var rows []map[string]any
	json.Unmarshal(list.Body.Bytes(), &rows)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0]["match_type"] != "regexp" || rows[0]["keyword"] != "订单\\d+" {
		t.Fatalf("保存结果异常: %+v", rows[0])
	}
	expressions, _ := rows[0]["expressions"].([]any)
	if len(expressions) != 2 || expressions[1] != "发货" {
		t.Fatalf("expressions = %+v", rows[0]["expressions"])
	}

	invalid := do(http.MethodPost, "/keywords-with-item-id/acc1",
		`{"expressions":["["],"match_type":"regexp","reply":"x","type":"text"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("非法正则应 400: %d %s", invalid.Code, invalid.Body.String())
	}

	updated := do(http.MethodPut, "/keywords-with-type/acc1/"+strconv.FormatInt(id, 10),
		`{"expressions":["发票","收据"],"match_type":"contains","reply":"可以开票","type":"text"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("更新失败: %d %s", updated.Code, updated.Body.String())
	}
	list = do(http.MethodGet, "/keywords-with-type/acc1", "")
	json.Unmarshal(list.Body.Bytes(), &rows)
	if rows[0]["match_type"] != "contains" || rows[0]["keyword"] != "发票" {
		t.Fatalf("更新结果异常: %+v", rows[0])
	}
}
