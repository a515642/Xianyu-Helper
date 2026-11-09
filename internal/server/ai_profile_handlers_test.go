package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// TestAIProfileAgentModeFields 验证工具模式字段的创建、回读，以及旧版前端未提交时保留原值。
func TestAIProfileAgentModeFields(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','item-1','测试商品')`); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	session := loginHelper(t, h)

	create := httptest.NewRequest(http.MethodPost, "/ai-profiles", strings.NewReader(`{
		"cookie_id":"acc1","name":"工具客服","enabled":true,"use_system_api":true,
		"max_bargain_rounds":3,"reply_mode":"tools","max_tool_rounds":5,
		"agent_prompt":"自定义工具提示词","notify_human_enabled":false,"item_ids":["item-1"]}`))
	create.AddCookie(session)
	created := httptest.NewRecorder()
	h.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var profile struct {
		ID                 int64  `json:"id"`
		ReplyMode          string `json:"reply_mode"`
		MaxToolRounds      int    `json:"max_tool_rounds"`
		AgentPrompt        string `json:"agent_prompt"`
		NotifyHumanEnabled bool   `json:"notify_human_enabled"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID == 0 || profile.ReplyMode != "tools" || profile.MaxToolRounds != 5 || profile.AgentPrompt != "自定义工具提示词" || profile.NotifyHumanEnabled {
		t.Fatalf("created profile = %+v", profile)
	}

	// 模拟旧版前端：只提交原有字段，新增配置应保持不变。
	update := httptest.NewRequest(http.MethodPut, "/ai-profiles/"+strconv.FormatInt(profile.ID, 10), strings.NewReader(`{
		"name":"工具客服","enabled":true,"use_system_api":true,"max_bargain_rounds":3}`))
	update.AddCookie(session)
	updated := httptest.NewRecorder()
	h.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ReplyMode != "tools" || profile.MaxToolRounds != 5 || profile.AgentPrompt != "自定义工具提示词" || profile.NotifyHumanEnabled {
		t.Fatalf("旧版请求不应清空工具配置: %+v", profile)
	}

	// 超出范围的工具轮数被拒绝。
	invalid := httptest.NewRequest(http.MethodPut, "/ai-profiles/"+strconv.FormatInt(profile.ID, 10), strings.NewReader(`{
		"name":"工具客服","enabled":true,"use_system_api":true,"max_bargain_rounds":3,"max_tool_rounds":13}`))
	invalid.AddCookie(session)
	invalidRec := httptest.NewRecorder()
	h.ServeHTTP(invalidRec, invalid)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid rounds status=%d body=%s", invalidRec.Code, invalidRec.Body.String())
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
