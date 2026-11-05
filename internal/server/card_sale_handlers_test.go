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

// TestCardSaleListAndExport 售出台账分页、筛选与 CSV 导出。
func TestCardSaleListAndExport(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "台账卡组", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cards.AddItems(ctx, cardID, "C-1\nC-2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Cards.ReserveDataItem(ctx, cardID, db.CardItemMeta{
		OrderID: "O-1", BuyerID: "B-1", ItemID: "I-1", CookieID: "acc1", RunID: 5,
	}); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := request("/card-sales?card_id=" + itoa(cardID))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Records   []db.CardSaleRecord `json:"records"`
		Total     int                 `json:"total"`
		TotalPage int                 `json:"total_pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Records) != 1 || page.TotalPage != 1 {
		t.Fatalf("page=%+v", page)
	}
	record := page.Records[0]
	if record.Status != db.CardSaleReserved || record.Content != "C-1" || record.OrderID != "O-1" ||
		record.BuyerID != "B-1" || record.CookieID != "acc1" || record.RunID != 5 {
		t.Fatalf("record=%+v", record)
	}

	rec = request("/card-sales?status=sold")
	if rec.Code != http.StatusOK {
		t.Fatalf("sold filter status=%d", rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Total != 0 {
		t.Fatalf("sold total=%d", page.Total)
	}

	if rec = request("/card-sales?status=bogus"); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d", rec.Code)
	}

	rec = request("/cards/" + itoa(cardID) + "/sales/export")
	if rec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type=%s", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("export should start with UTF-8 BOM")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "C-1") || !strings.Contains(body, "待核对") || !strings.Contains(body, "O-1") {
		t.Fatalf("export body=%s", body)
	}
}
