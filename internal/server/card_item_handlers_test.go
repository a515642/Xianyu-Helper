package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestCardItemsListAndBulkStatus 卡密条目分页列表、出库/回库与查询过滤。
func TestCardItemsListAndBulkStatus(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "列表卡组", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cards.AddItems(ctx, cardID, "C-1\nC-2\nC-3"); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	request := func(method, path, body string) *httptest.ResponseRecorder {
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// 默认列表带分页字段。
	rec := request(http.MethodGet, "/cards/items?page=1&page_size=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items     []db.CardItem `json:"items"`
		Total     int           `json:"total"`
		PageSize  int           `json:"page_size"`
		TotalPage int           `json:"total_pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.PageSize != 2 || page.TotalPage != 2 {
		t.Fatalf("分页字段异常: %+v", page)
	}
	ids := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}

	// 批量出库（标记已用）。
	body, _ := json.Marshal(map[string]any{"ids": ids, "status": "used"})
	rec = request(http.MethodPut, "/cards/items", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk used status=%d body=%s", rec.Code, rec.Body.String())
	}
	var bulk struct {
		Updated int `json:"updated"`
	}
	json.Unmarshal(rec.Body.Bytes(), &bulk)
	if bulk.Updated != 2 {
		t.Fatalf("updated=%d want 2", bulk.Updated)
	}

	// 按状态查询只返回已用。
	rec = request(http.MethodGet, "/cards/items?status=used", "")
	json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Total != 2 {
		t.Fatalf("used total=%d want 2", page.Total)
	}

	// 批量回库。
	body, _ = json.Marshal(map[string]any{"ids": ids, "status": "available"})
	rec = request(http.MethodPut, "/cards/items", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk available status=%d", rec.Code)
	}
	rec = request(http.MethodGet, "/cards/items?status=available", "")
	json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Total != 3 {
		t.Fatalf("available total=%d want 3", page.Total)
	}

	// 非法状态 → 400。
	body, _ = json.Marshal(map[string]any{"ids": ids, "status": "broken"})
	rec = request(http.MethodPut, "/cards/items", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法状态应 400，got %d", rec.Code)
	}
}

// TestCardItemBuyerAndOrderFilter 买家 / 订单 / 卡密关键字查询。
func TestCardItemBuyerAndOrderFilter(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "归属卡组", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	if _, err := store.Cards.AddItems(ctx, cardID, "SECRET-A\nSECRET-B"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Cards.ReserveDataItem(ctx, cardID, db.CardItemMeta{
		BuyerID: "buyer-9", BuyerName: "张三", OrderID: "order-42",
	}); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	get := func(query string) []db.CardItem {
		req := httptest.NewRequest(http.MethodGet, "/cards/items"+query, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %s status=%d body=%s", query, rec.Code, rec.Body.String())
		}
		var page struct {
			Items []db.CardItem `json:"items"`
		}
		json.Unmarshal(rec.Body.Bytes(), &page)
		return page.Items
	}

	if got := get("?buyer_id=buyer-9"); len(got) != 1 || got[0].OrderID != "order-42" {
		t.Fatalf("按买家ID查询异常: %+v", got)
	}
	if got := get("?buyer_name=%E5%BC%A0%E4%B8%89"); len(got) != 1 {
		t.Fatalf("按买家昵称查询异常: %+v", got)
	}
	if got := get("?order_id=order-42"); len(got) != 1 {
		t.Fatalf("按订单号查询异常: %+v", got)
	}
	if got := get("?keyword=SECRET-B"); len(got) != 1 || got[0].Content != "SECRET-B" {
		t.Fatalf("按卡密关键字查询异常: %+v", got)
	}
}

// TestAddCardItemsEndpoint 往卡种添加卡密：类型校验、单条约束与库存回传。
func TestAddCardItemsEndpoint(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "添加卡密", Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	post := func(card string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/cards/"+card+"/items", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// 批量卡密：每行一条。
	rec := post(itoa(cardID), `{"content":"K1\nK2\nK3"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("add data status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		Added int          `json:"added"`
		Stock db.CardStock `json:"stock"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 || res.Stock.Available != 3 {
		t.Fatalf("added=%d stock=%+v", res.Added, res.Stock)
	}

	// 空内容 → 400。
	if rec := post(itoa(cardID), `{"content":"   "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("空内容应 400，got %d", rec.Code)
	}
	// 不存在的卡种 → 404。
	if rec := post("999999", `{"content":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在卡种应 404，got %d", rec.Code)
	}

	// 固定文本卡种：整段一条。
	textID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "文本卡种", Kind: db.CardKindText, Enabled: true, UserID: admin.ID})
	if rec := post(itoa(textID), `{"content":"固定文案"}`); rec.Code != http.StatusOK {
		t.Fatalf("text 添加 status=%d", rec.Code)
	}
}

// TestExportCardItems 导出卡密：按所选 ids 导出 txt/csv/xlsx，并支持按筛选导出。
func TestExportCardItems(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "导出卡种", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cards.AddItems(ctx, cardID, "E-1\nE-2\nE-3"); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	items, _, err := store.Cards.ListItems(ctx, db.CardItemFilter{UserID: admin.ID, CardID: cardID, Limit: 10})
	if err != nil || len(items) != 3 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	ids := []int64{items[0].ID, items[1].ID}

	export := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/cards/"+itoa(cardID)+"/items/export", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// txt：只导出所选两条，一行一条。
	txtBody, _ := json.Marshal(map[string]any{"ids": ids, "format": "txt"})
	rec := export(string(txtBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("txt status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("txt content-type=%q", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); strings.Count(got, "E-") != 2 {
		t.Fatalf("txt body=%q", got)
	}

	// csv：带 BOM 与表头。
	csvBody, _ := json.Marshal(map[string]any{"ids": ids, "format": "csv"})
	rec = export(string(csvBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv status=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "卡密内容") || !strings.Contains(rec.Body.String(), "E-") {
		t.Fatalf("csv body=%q", rec.Body.String())
	}

	// xlsx：zip 魔数 PK。
	xlsxBody, _ := json.Marshal(map[string]any{"ids": ids, "format": "xlsx"})
	rec = export(string(xlsxBody))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 || rec.Body.Bytes()[0] != 0x50 {
		t.Fatalf("xlsx status=%d len=%d", rec.Code, rec.Body.Len())
	}

	// 不传 ids → 按筛选导出全部。
	allBody, _ := json.Marshal(map[string]any{"format": "txt"})
	rec = export(string(allBody))
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), "E-") != 3 {
		t.Fatalf("all txt status=%d body=%q", rec.Code, rec.Body.String())
	}

	// 非法格式 → 400。
	if rec := export(`${B}"format":"pdf"${B}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法格式应 400，got %d", rec.Code)
	}
}

// TestCardItemsRequireAuth 卡密条目端点未登录应 401。
func TestCardItemsRequireAuth(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	req := httptest.NewRequest(http.MethodGet, "/cards/items", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}
}

// TestCardItemContentUpdate 单条内容更新与删除。
func TestCardItemContentUpdate(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "编辑卡组", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	if _, err := store.Cards.AddItems(ctx, cardID, "OLD"); err != nil {
		t.Fatal(err)
	}
	items, _, err := store.Cards.ListItems(ctx, db.CardItemFilter{UserID: admin.ID, CardID: cardID})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := bytes.NewBufferString(`{"content":"NEW"}`)
	req := httptest.NewRequest(http.MethodPut, "/cards/items/"+itoa(items[0].ID), body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("更新内容 status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.Cards.GetItem(ctx, items[0].ID)
	if got.Content != "NEW" {
		t.Fatalf("content=%q want NEW", got.Content)
	}

	// 空内容 → 400。
	req = httptest.NewRequest(http.MethodPut, "/cards/items/"+itoa(items[0].ID), bytes.NewBufferString(`{"content":"  "}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空内容应 400，got %d", rec.Code)
	}

	// 删除。
	req = httptest.NewRequest(http.MethodDelete, "/cards/items/"+itoa(items[0].ID), nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("删除 status=%d", rec.Code)
	}
	if _, err := store.Cards.GetItem(ctx, items[0].ID); err == nil {
		t.Fatal("删除后条目应不存在")
	}
}
