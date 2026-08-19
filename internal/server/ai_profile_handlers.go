package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/db"
)

const (
	maxAIProfilesPerAccount = 50
	maxAIProfileItems       = 500
	maxAIForbiddenWords     = 500
)

type aiProfileRequest struct {
	CookieID           string   `json:"cookie_id"`
	Name               string   `json:"name"`
	Enabled            bool     `json:"enabled"`
	UseSystemAPI       bool     `json:"use_system_api"`
	APIKey             *string  `json:"api_key"`
	ClearAPIKey        bool     `json:"clear_api_key"`
	BaseURL            string   `json:"base_url"`
	ModelName          string   `json:"model_name"`
	ThinkingMode       string   `json:"thinking_mode"`
	CustomPrompts      string   `json:"custom_prompts"`
	MaxDiscountPercent int      `json:"max_discount_percent"`
	MaxDiscountAmount  int      `json:"max_discount_amount"`
	MaxBargainRounds   int      `json:"max_bargain_rounds"`
	ItemIDs            []string `json:"item_ids"`
}

func (s *Server) mountAIProfiles(r chi.Router) {
	r.Get("/ai-profiles", s.listAIProfiles)
	r.Post("/ai-profiles", s.createAIProfile)
	r.Get("/ai-profiles/{id}", s.getAIProfile)
	r.Put("/ai-profiles/{id}", s.updateAIProfile)
	r.Delete("/ai-profiles/{id}", s.deleteAIProfile)
	r.Put("/ai-profiles/{id}/items", s.replaceAIProfileItems)
}

func (s *Server) mountAIForbiddenWords(r chi.Router) {
	r.Get("/ai-forbidden-words", s.listAIForbiddenWords)
	r.Put("/ai-forbidden-words", s.replaceAIForbiddenWords)
}

func (s *Server) listAIProfiles(w http.ResponseWriter, r *http.Request) {
	cid := strings.TrimSpace(r.URL.Query().Get("cookie_id"))
	if cid == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id")
		return
	}
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	profiles, err := s.Store.AIProfiles.List(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询 AI 助手失败")
		return
	}
	if profiles == nil {
		profiles = []db.AIProfile{}
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (s *Server) getAIProfile(w http.ResponseWriter, r *http.Request) {
	profile, ok := s.requireAIProfileOwner(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) createAIProfile(w http.ResponseWriter, r *http.Request) {
	var req aiProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if _, ok := s.requireCookieOwner(w, r, strings.TrimSpace(req.CookieID)); !ok {
		return
	}
	if err := validateAIProfileRequest(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	profiles, err := s.Store.AIProfiles.List(r.Context(), req.CookieID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询 AI 助手失败")
		return
	}
	if len(profiles) >= maxAIProfilesPerAccount {
		writeErr(w, http.StatusBadRequest, "单个账号最多创建 50 个 AI 助手")
		return
	}
	profile := profileFromRequest(0, req)
	id, err := s.Store.AIProfiles.Create(r.Context(), profile)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "创建 AI 助手失败: "+err.Error())
		return
	}
	profile.ID = id
	if err := s.Store.AIProfiles.Update(r.Context(), profile, req.APIKey, req.ClearAPIKey); err != nil {
		_ = s.Store.AIProfiles.Delete(r.Context(), id, req.CookieID)
		writeErr(w, http.StatusBadRequest, "保存 AI 配置失败: "+err.Error())
		return
	}
	if err := s.Store.AIProfiles.ReplaceItems(r.Context(), id, req.CookieID, normalizeIDs(req.ItemIDs)); err != nil {
		_ = s.Store.AIProfiles.Delete(r.Context(), id, req.CookieID)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, _ := s.Store.AIProfiles.Get(r.Context(), id)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateAIProfile(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.requireAIProfileOwner(w, r)
	if !ok {
		return
	}
	var req aiProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.CookieID = existing.CookieID
	if err := validateAIProfileRequest(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	profile := profileFromRequest(existing.ID, req)
	if err := s.Store.AIProfiles.Update(r.Context(), profile, req.APIKey, req.ClearAPIKey); err != nil {
		writeErr(w, http.StatusBadRequest, "保存 AI 助手失败: "+err.Error())
		return
	}
	if req.ItemIDs != nil {
		if err := s.Store.AIProfiles.ReplaceItems(r.Context(), profile.ID, profile.CookieID, normalizeIDs(req.ItemIDs)); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	updated, _ := s.Store.AIProfiles.Get(r.Context(), profile.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteAIProfile(w http.ResponseWriter, r *http.Request) {
	profile, ok := s.requireAIProfileOwner(w, r)
	if !ok {
		return
	}
	if err := s.Store.AIProfiles.Delete(r.Context(), profile.ID, profile.CookieID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除 AI 助手失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) replaceAIProfileItems(w http.ResponseWriter, r *http.Request) {
	profile, ok := s.requireAIProfileOwner(w, r)
	if !ok {
		return
	}
	var req struct {
		ItemIDs []string `json:"item_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	ids := normalizeIDs(req.ItemIDs)
	if len(ids) > maxAIProfileItems {
		writeErr(w, http.StatusBadRequest, "单个 AI 最多绑定 500 个商品")
		return
	}
	if err := s.Store.AIProfiles.ReplaceItems(r.Context(), profile.ID, profile.CookieID, ids); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) requireAIProfileOwner(w http.ResponseWriter, r *http.Request) (*db.AIProfile, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "AI 助手 ID 无效")
		return nil, false
	}
	profile, err := s.Store.AIProfiles.Get(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "AI 助手不存在")
		return nil, false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询 AI 助手失败")
		return nil, false
	}
	if _, ok := s.requireCookieOwner(w, r, profile.CookieID); !ok {
		return nil, false
	}
	return profile, true
}

func validateAIProfileRequest(req aiProfileRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 100 {
		return errors.New("AI 名称不能为空且不能超过 100 个字符")
	}
	if len(req.CustomPrompts) > 20000 {
		return errors.New("提示词不能超过 20000 字节")
	}
	if len(req.BaseURL) > 2048 || len(req.ModelName) > 255 {
		return errors.New("API 地址或模型名称过长")
	}
	if req.APIKey != nil && len(*req.APIKey) > 4096 {
		return errors.New("API Key 过长")
	}
	if req.MaxDiscountPercent < 0 || req.MaxDiscountPercent > 100 {
		return errors.New("最大折扣比例必须在 0 到 100 之间")
	}
	if req.MaxDiscountAmount < 0 {
		return errors.New("最大折扣金额不能小于 0")
	}
	if req.MaxBargainRounds < 1 || req.MaxBargainRounds > 10 {
		return errors.New("最大砍价轮次必须在 1 到 10 之间")
	}
	if len(normalizeIDs(req.ItemIDs)) > maxAIProfileItems {
		return errors.New("单个 AI 最多绑定 500 个商品")
	}
	return nil
}

func profileFromRequest(id int64, req aiProfileRequest) db.AIProfile {
	return db.AIProfile{ID: id, CookieID: strings.TrimSpace(req.CookieID), Name: strings.TrimSpace(req.Name), Enabled: req.Enabled, UseSystemAPI: req.UseSystemAPI, BaseURL: strings.TrimSpace(req.BaseURL), ModelName: strings.TrimSpace(req.ModelName), ThinkingMode: req.ThinkingMode, CustomPrompts: req.CustomPrompts, TriggerMode: "all_text", MaxDiscountPercent: req.MaxDiscountPercent, MaxDiscountAmount: req.MaxDiscountAmount, MaxBargainRounds: req.MaxBargainRounds}
}
func normalizeIDs(values []string) []string {
	if values == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (s *Server) listAIForbiddenWords(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.AIProfiles.ListForbiddenWords(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询违禁词失败")
		return
	}
	if rules == nil {
		rules = []db.AIForbiddenWord{}
	}
	writeJSON(w, http.StatusOK, rules)
}
func (s *Server) replaceAIForbiddenWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []db.AIForbiddenWord `json:"rules"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Rules) > maxAIForbiddenWords {
		writeErr(w, http.StatusBadRequest, "违禁词最多 500 条")
		return
	}
	seen := map[string]struct{}{}
	for i := range req.Rules {
		req.Rules[i].Keyword = strings.TrimSpace(req.Rules[i].Keyword)
		if req.Rules[i].Keyword == "" || utf8.RuneCountInString(req.Rules[i].Keyword) > 200 {
			writeErr(w, http.StatusBadRequest, "违禁词不能为空且不能超过 200 个字符")
			return
		}
		if utf8.RuneCountInString(req.Rules[i].Replacement) > 500 {
			writeErr(w, http.StatusBadRequest, "替换词不能超过 500 个字符")
			return
		}
		if _, ok := seen[req.Rules[i].Keyword]; ok {
			writeErr(w, http.StatusBadRequest, "违禁词不能重复")
			return
		}
		seen[req.Rules[i].Keyword] = struct{}{}
	}
	if err := s.Store.AIProfiles.ReplaceForbiddenWords(r.Context(), req.Rules); err != nil {
		writeErr(w, http.StatusBadRequest, "保存违禁词失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
