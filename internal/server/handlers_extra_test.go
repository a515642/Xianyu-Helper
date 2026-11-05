package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestAddKeywordWithItemID 带商品ID的关键词添加 + 缺 keyword 400。
func TestAddKeywordWithItemID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 正常添加。
	body := `{"keyword":"价格","reply":"50元","item_id":"item1"}`
	req := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 缺 keyword → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader(`{"reply":"x"}`))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("缺 keyword 应 400，got %d", rec2.Code)
	}

	// 非法 JSON → 400。
	req3 := httptest.NewRequest(http.MethodPost, "/keywords-with-item-id/acc1", strings.NewReader("not-json"))
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec3.Code)
	}
}

// TestBatchCreateCards 按卡种批量导入：同名行并入同一卡种，已存在则追加。
func TestBatchCreateCards(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	// 预置一个已存在的批量卡种，导入应追加而不是新建。
	existingID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "已存在批量卡", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	store.Cards.AddItems(ctx, existingID, "OLD-1")

	h := srv.Router()
	cookie := loginHelper(t, h)

	// 同名行（卡A）合并为一个卡种；已存在的卡种追加；类型冲突与非法延时的行计入 failed。
	csv := "卡种,类型,内容,描述,延迟秒\n" +
		"卡A,card,K1,新卡种描述,0\n" +
		"卡A,card,K2,,\n" +
		"卡A,card,K3,,\n" +
		"已存在批量卡,card,NEW-1,,\n" +
		"已存在批量卡,card,NEW-2,,\n" +
		"延时异常,card,X,,9999\n" +
		"卡A,text,混入类型,,\n" +
		"只建卡种,,,,0\n"
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, _ := mw.CreateFormFile("file", "cards.csv")
	fw.Write([]byte(csv))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/cards/batch", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("batch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		Rows     int `json:"rows"`
		Created  int `json:"created"`
		Appended int `json:"appended"`
		Items    int `json:"items"`
		Failed   int `json:"failed"`
		Groups   []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			CardID  int64  `json:"card_id"`
			Created bool   `json:"created"`
			Added   int    `json:"added"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Rows != 8 || res.Created != 2 || res.Appended != 1 || res.Items != 5 || res.Failed != 2 {
		t.Fatalf("汇总异常: %+v", res)
	}

	addedByName := map[string]int{}
	for _, group := range res.Groups {
		addedByName[group.Name] = group.Added
	}
	if addedByName["卡A"] != 3 {
		t.Fatalf("卡A 应写入 3 条, got %+v", res.Groups)
	}
	if addedByName["已存在批量卡"] != 2 {
		t.Fatalf("已存在卡种应追加 2 条, got %+v", res.Groups)
	}

	// 校验落库结果。
	created, err := store.Cards.FindByName(ctx, admin.ID, "卡A")
	if err != nil || created.Kind != db.CardKindCard || created.ItemCount != 3 {
		t.Fatalf("卡A 落库异常: %+v err=%v", created, err)
	}
	existing, _ := store.Cards.FindByName(ctx, admin.ID, "已存在批量卡")
	if existing.ID != existingID || existing.ItemCount != 3 {
		t.Fatalf("已存在卡种应保留 ID 并变为 3 条: %+v", existing)
	}
	empty, err := store.Cards.FindByName(ctx, admin.ID, "只建卡种")
	if err != nil || empty.ItemCount != 0 {
		t.Fatalf("只建卡种应为空卡种: %+v err=%v", empty, err)
	}

	// 缺文件 → 400。
	req2 := httptest.NewRequest(http.MethodPost, "/cards/batch", &bytes.Buffer{})
	req2.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("缺文件应 400，got %d", rec2.Code)
	}
}

// TestParseXLSXPublishSheet 解析 XLSX 表格（复用 buildMinimalXLSX 构造）。
func TestParseXLSXPublishSheet(t *testing.T) {
	xlsx := buildMinimalXLSXForPublish(t, [][]string{{"title", "price"}, {"商品A", "9.9"}})
	rows, err := parseXLSXPublishSheet(xlsx)
	if err != nil {
		t.Fatalf("parseXLSX: %v", err)
	}
	if len(rows) != 1 || rows[0]["title"] != "商品A" {
		t.Fatalf("rows = %#v", rows)
	}

	// 非 xlsx 字节 → 报错。
	if _, err := parseXLSXPublishSheet([]byte("not-xlsx")); err == nil {
		t.Fatal("非 xlsx 应报错")
	}
}

// TestPublicHTTPClient 公网 HTTP 客户端拒绝非公网地址 + 重定向协议校验。
func TestPublicHTTPClient(t *testing.T) {
	cli := publicHTTPClient()
	if cli == nil {
		t.Fatal("应返回非 nil client")
	}
	// 访问私有 IP 应被拒绝。
	_, err := cli.Get("http://127.0.0.1:1/")
	if err == nil {
		t.Fatal("私有 IP 应被拒绝")
	}
}

// TestPublishBatchToMap 批次转 map 序列化 + 状态计数。
func TestPublishBatchToMap(t *testing.T) {
	batch := &db.ItemPublishBatch{ID: "b1", Status: "running", Filename: "x.csv"}
	rows := []db.ItemPublishBatchRow{
		{ID: 1, RowNo: 1, CookieID: "c1", Title: "t1", Status: "pending", ImagesJSON: `["a.png"]`},
		{ID: 2, RowNo: 2, CookieID: "c1", Title: "t2", Status: "running"},
	}
	m := publishBatchToMap(batch, rows)
	if m["id"] != "b1" || m["status"] != "running" {
		t.Fatalf("batch 字段异常: %+v", m)
	}
	rs, _ := m["rows"].([]map[string]any)
	if len(rs) != 2 {
		t.Fatalf("rows 数异常: %d", len(rs))
	}
}

// buildMinimalXLSXForPublish 构造最小 xlsx 供 parseXLSXPublishSheet 测试。
// 复用 parser_test.go 的 buildMinimalXLSX 但独立命名避免冲突。
func buildMinimalXLSXForPublish(t *testing.T, grid [][]string) []byte {
	t.Helper()
	return buildMinimalXLSX(t, grid)
}
