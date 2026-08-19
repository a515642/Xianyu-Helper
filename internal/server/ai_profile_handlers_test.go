package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAIProfileCRUDAndGlobalForbiddenWords(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','item-1','测试商品')`); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	session := loginHelper(t, h)

	create := httptest.NewRequest(http.MethodPost, "/ai-profiles", strings.NewReader(`{
		"cookie_id":"acc1","name":"客服一号","enabled":true,"use_system_api":true,
		"max_discount_percent":10,"max_discount_amount":20,"max_bargain_rounds":3,
		"custom_prompts":"友好回复","item_ids":["item-1"]}`))
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
	if profile["name"] != "客服一号" || profile["api_key"] != nil {
		t.Fatalf("profile leaked or malformed: %+v", profile)
	}

	list := httptest.NewRequest(http.MethodGet, "/ai-profiles?cookie_id=acc1", nil)
	list.AddCookie(session)
	listed := httptest.NewRecorder()
	h.ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "item-1") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}

	rules := httptest.NewRequest(http.MethodPut, "/ai-forbidden-words", strings.NewReader(`{"rules":[{"keyword":"微信","replacement":"站内","enabled":true}]}`))
	rules.AddCookie(session)
	rulesRec := httptest.NewRecorder()
	h.ServeHTTP(rulesRec, rules)
	if rulesRec.Code != http.StatusOK {
		t.Fatalf("rules status=%d body=%s", rulesRec.Code, rulesRec.Body.String())
	}
	got, err := store.AIProfiles.ApplyForbiddenWords(ctx, "加微信")
	if err != nil || got != "加站内" {
		t.Fatalf("replacement=%q err=%v", got, err)
	}
}

func TestAIProfileRejectsOtherUsersAccount(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	_, _ = store.Users.Create(ctx, "other", "other@example.com", "pw")
	other, _ := store.Users.GetByUsername(ctx, "other")
	if err := store.Cookies.CreateOwned(ctx, "other-account", "unb=2", other.ID); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	session := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPost, "/ai-profiles", strings.NewReader(`{"cookie_id":"other-account","name":"越权","enabled":true,"use_system_api":true,"max_bargain_rounds":3}`))
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
