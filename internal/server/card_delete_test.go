package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestDeleteCardBlockedByActiveRule 被启用规则引用时删除卡种返回 409 和可读提示。
func TestDeleteCardBlockedByActiveRule(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "规则卡", Kind: db.CardKindCard, Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "acc1", ItemID: "item-1", Name: "发货规则", TriggerType: "order_paid",
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{{ActionType: "send_card", CardID: cardID, DeliveryCount: 1, Enabled: true, SortOrder: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodDelete, "/cards/"+itoa(cardID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "解除绑定") {
		t.Fatalf("delete body=%s", rec.Body.String())
	}
}
