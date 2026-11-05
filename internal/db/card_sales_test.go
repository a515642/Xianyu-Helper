package db

import (
	"context"
	"testing"
)

// TestCardSales_ReserveSoldAndRetention 验证预留写台账、确认售出、卡种删除后仍保留。
func TestCardSales_ReserveSoldAndRetention(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	cardID, err := s.Cards.Create(ctx, &CardFull{Name: "批量卡", Kind: CardKindCard, Enabled: true, UserID: uid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cards.AddItems(ctx, cardID, "K1\nK2"); err != nil {
		t.Fatal(err)
	}

	itemID, content, err := s.Cards.ReserveDataItem(ctx, cardID, CardItemMeta{
		OrderID: "O1", BuyerID: "B1", ItemID: "I1", CookieID: cid, RunID: 77,
	})
	if err != nil || itemID == 0 || content != "K1" {
		t.Fatalf("reserve item=%d content=%q err=%v", itemID, content, err)
	}
	page, err := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, CardID: cardID})
	if err != nil || page.Total != 1 || len(page.Records) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	record := page.Records[0]
	if record.Status != CardSaleReserved || record.Content != "K1" || record.CardName != "批量卡" ||
		record.OrderID != "O1" || record.BuyerID != "B1" || record.ItemID != "I1" ||
		record.CookieID != cid || record.RunID != 77 || record.SoldAt != 0 {
		t.Fatalf("reserved record=%+v", record)
	}

	// 状态筛选与订单筛选。
	if reserved, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, Status: CardSaleReserved}); reserved.Total != 1 {
		t.Fatalf("reserved total=%d", reserved.Total)
	}
	if sold, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, Status: CardSaleSold}); sold.Total != 0 {
		t.Fatalf("sold total=%d", sold.Total)
	}
	if byOrder, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, CookieID: cid, OrderID: "O1"}); byOrder.Total != 1 {
		t.Fatalf("order total=%d", byOrder.Total)
	}

	if err := s.Cards.MarkDataItemSold(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	page, _ = s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, CardID: cardID})
	if page.Records[0].Status != CardSaleSold || page.Records[0].SoldAt == 0 {
		t.Fatalf("sold record=%+v", page.Records[0])
	}

	// 释放第二条预留会删除对应台账，库存恢复可用。
	item2, _, err := s.Cards.ReserveDataItem(ctx, cardID, CardItemMeta{OrderID: "O2", CookieID: cid})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Cards.ReleaseDataItem(ctx, item2); err != nil {
		t.Fatal(err)
	}
	if page, _ = s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, CardID: cardID}); page.Total != 1 {
		t.Fatalf("records after release=%d", page.Total)
	}
	if item, err := s.Cards.GetItem(ctx, item2); err != nil || item.Status != CardItemAvailable {
		t.Fatalf("released item=%+v err=%v", item, err)
	}

	// 卡种删除后台账仍可查询，且保留当时的卡种名称快照。
	if err := s.Cards.Delete(ctx, cardID); err != nil {
		t.Fatal(err)
	}
	page, err = s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid, CardID: cardID})
	if err != nil || page.Total != 1 || page.Records[0].CardName != "批量卡" {
		t.Fatalf("retained page=%+v err=%v", page, err)
	}
}

// TestCardSales_ManualReturnClearsReserved 人工回库时未确认的台账应删除。
func TestCardSales_ManualReturnClearsReserved(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	cardID, _ := s.Cards.Create(ctx, &CardFull{Name: "卡", Kind: CardKindCard, Enabled: true, UserID: uid})
	if _, err := s.Cards.AddItems(ctx, cardID, "K1"); err != nil {
		t.Fatal(err)
	}
	itemID, _, err := s.Cards.ReserveDataItem(ctx, cardID, CardItemMeta{CookieID: cid})
	if err != nil {
		t.Fatal(err)
	}
	if page, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid}); page.Total != 1 {
		t.Fatalf("reserved total=%d", page.Total)
	}
	if _, err := s.Cards.SetStatus(ctx, uid, []int64{itemID}, CardItemAvailable); err != nil {
		t.Fatal(err)
	}
	if page, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid}); page.Total != 0 {
		t.Fatalf("records after manual return=%d", page.Total)
	}
}

// TestCardSales_SettleRunSales 验证按运行结算且不能跨用户。
func TestCardSales_SettleRunSales(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	if ok, err := s.Users.Create(ctx, "other", "other@e.com", "pw"); err != nil || !ok {
		t.Fatalf("create other ok=%v err=%v", ok, err)
	}
	other, _ := s.Users.GetByUsername(ctx, "other")
	cardID, _ := s.Cards.Create(ctx, &CardFull{Name: "卡", Kind: CardKindCard, Enabled: true, UserID: uid})
	if _, err := s.Cards.AddItems(ctx, cardID, "K1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Cards.ReserveDataItem(ctx, cardID, CardItemMeta{OrderID: "O1", CookieID: cid, RunID: 88}); err != nil {
		t.Fatal(err)
	}

	if n, err := s.Cards.SettleRunSales(ctx, other.ID, 88); err != nil || n != 0 {
		t.Fatalf("cross-user settle n=%d err=%v", n, err)
	}
	if n, err := s.Cards.SettleRunSales(ctx, uid, 88); err != nil || n != 1 {
		t.Fatalf("settle n=%d err=%v", n, err)
	}
	page, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: uid})
	if page.Records[0].Status != CardSaleSold {
		t.Fatalf("record after settle=%+v", page.Records[0])
	}
	if otherPage, _ := s.Cards.ListCardSales(ctx, CardSaleFilter{UserID: other.ID}); otherPage.Total != 0 {
		t.Fatalf("cross-user list total=%d", otherPage.Total)
	}
}
