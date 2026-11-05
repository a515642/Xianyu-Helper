package db

import (
	"context"
	"errors"
	"testing"
)

// TestCards_DeleteBlocksActiveRuleAndDetachesDeletedRule 验证被启用规则引用的卡种
// 拒绝删除，规则删除后释放引用即可删除卡种。
func TestCards_DeleteBlocksActiveRuleAndDetachesDeletedRule(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	cardID, err := s.Cards.Create(ctx, &CardFull{Name: "规则卡", Kind: CardKindCard, Enabled: true, UserID: uid})
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := s.Automation.Create(ctx, AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: "item-1", Name: "发货规则", TriggerType: "order_paid",
		Enabled: true, Priority: 100,
		Actions: []AutomationActionInput{{ActionType: "send_card", CardID: cardID, DeliveryCount: 1, Enabled: true, SortOrder: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Cards.Delete(ctx, cardID); !errors.Is(err, ErrCardInUse) {
		t.Fatalf("active rule delete err=%v", err)
	}
	if _, err := s.Cards.Get(ctx, cardID); err != nil {
		t.Fatalf("card should remain: err=%v", err)
	}

	if err := s.Automation.Delete(ctx, uid, ruleID); err != nil {
		t.Fatal(err)
	}
	if err := s.Cards.Delete(ctx, cardID); err != nil {
		t.Fatalf("delete after rule removal err=%v", err)
	}
	if _, err := s.Cards.Get(ctx, cardID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("card should be deleted: err=%v", err)
	}
	var leftover int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rule_actions WHERE card_id=?`, cardID).Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	if leftover != 0 {
		t.Fatalf("deleted rule still references card: %d", leftover)
	}
}
