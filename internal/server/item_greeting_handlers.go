package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

const (
	itemGreetingMaxNameRunes     = 100
	itemGreetingMaxContentRunes  = 2000
	itemGreetingMaxImageURLRunes = 2048
	itemGreetingMaxItemsPerRule  = 500
)

// mountItemGreetingsReal 商品首次回复端点。
func (s *Server) mountItemGreetingsReal(r chi.Router) {
	r.Get("/api/reply-rules/greetings", s.listItemGreetings)
	r.Post("/api/reply-rules/greetings", s.createItemGreeting)
	r.Put("/api/reply-rules/greetings/{id}", s.updateItemGreeting)
	r.Delete("/api/reply-rules/greetings/{id}", s.deleteItemGreeting)
	r.Post("/api/reply-rules/greetings/records/{cid}/clear", s.clearItemGreetingRecords)
}

// itemGreetingRequest 是创建或更新商品首次回复规则的请求体。
type itemGreetingRequest struct {
	CookieID      string   `json:"cookie_id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	ReplyContent  string   `json:"reply_content"`
	ReplyImageURL string   `json:"reply_image_url"`
	ItemIDs       []string `json:"item_ids"`
}

// normalizeItemGreetingRequest 规整空白、去重商品并校验必填项和长度。
func normalizeItemGreetingRequest(req itemGreetingRequest) (db.ItemGreetingRule, string) {
	rule := db.ItemGreetingRule{
		CookieID:      strings.TrimSpace(req.CookieID),
		Name:          strings.TrimSpace(req.Name),
		Enabled:       req.Enabled,
		ReplyContent:  strings.TrimSpace(req.ReplyContent),
		ReplyImageURL: strings.TrimSpace(req.ReplyImageURL),
	}
	seen := make(map[string]struct{}, len(req.ItemIDs))
	for _, raw := range req.ItemIDs {
		itemID := strings.TrimSpace(raw)
		if itemID == "" {
			continue
		}
		if _, dup := seen[itemID]; dup {
			continue
		}
		seen[itemID] = struct{}{}
		rule.ItemIDs = append(rule.ItemIDs, itemID)
	}
	switch {
	case len(rule.ItemIDs) == 0:
		return db.ItemGreetingRule{}, "至少选择一个商品"
	case len(rule.ItemIDs) > itemGreetingMaxItemsPerRule:
		return db.ItemGreetingRule{}, "单条规则覆盖的商品过多"
	case rule.ReplyContent == "" && rule.ReplyImageURL == "":
		return db.ItemGreetingRule{}, "回复文字和图片至少填写一项"
	case utf8.RuneCountInString(rule.Name) > itemGreetingMaxNameRunes:
		return db.ItemGreetingRule{}, "规则名称过长"
	case utf8.RuneCountInString(rule.ReplyContent) > itemGreetingMaxContentRunes:
		return db.ItemGreetingRule{}, "回复文字过长"
	case utf8.RuneCountInString(rule.ReplyImageURL) > itemGreetingMaxImageURLRunes:
		return db.ItemGreetingRule{}, "图片地址过长"
	}
	return rule, ""
}

// itemGreetingResponse 把数据库规则转为前端响应，保证 item_ids 不为 null。
func itemGreetingResponse(rule db.ItemGreetingRule) map[string]any {
	itemIDs := rule.ItemIDs
	if itemIDs == nil {
		itemIDs = []string{}
	}
	return map[string]any{
		"id": rule.ID, "cookie_id": rule.CookieID, "name": rule.Name, "enabled": rule.Enabled,
		"reply_content": rule.ReplyContent, "reply_image_url": rule.ReplyImageURL, "item_ids": itemIDs,
	}
}

func (s *Server) listItemGreetings(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cid := strings.TrimSpace(r.URL.Query().Get("cookie_id"))
	var rules []db.ItemGreetingRule
	var err error
	if cid == "" {
		rules, err = s.Store.ItemGreetings.ListForUser(r.Context(), sess.UserID)
	} else {
		if _, ok := s.requireCookieOwner(w, r, cid); !ok {
			return
		}
		rules, err = s.Store.ItemGreetings.ListByAccount(r.Context(), cid)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, itemGreetingResponse(rule))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createItemGreeting(w http.ResponseWriter, r *http.Request) {
	var req itemGreetingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	rule, reason := normalizeItemGreetingRequest(req)
	if reason != "" {
		writeErr(w, http.StatusBadRequest, reason)
		return
	}
	if rule.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "账号标识无效")
		return
	}
	if _, ok := s.requireCookieOwner(w, r, rule.CookieID); !ok {
		return
	}
	id, err := s.Store.ItemGreetings.Create(r.Context(), rule)
	if err != nil {
		writeItemGreetingError(w, err, "保存失败")
		return
	}
	rule.ID = id
	writeJSON(w, http.StatusCreated, itemGreetingResponse(rule))
}

func (s *Server) updateItemGreeting(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "规则标识无效")
		return
	}
	existing, err := s.Store.ItemGreetings.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "规则不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if _, ok := s.requireCookieOwner(w, r, existing.CookieID); !ok {
		return
	}
	var req itemGreetingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.CookieID) != "" && strings.TrimSpace(req.CookieID) != existing.CookieID {
		writeErr(w, http.StatusBadRequest, "不能修改规则所属账号")
		return
	}
	req.CookieID = existing.CookieID
	rule, reason := normalizeItemGreetingRequest(req)
	if reason != "" {
		writeErr(w, http.StatusBadRequest, reason)
		return
	}
	rule.ID = id
	if err := s.Store.ItemGreetings.Update(r.Context(), rule); err != nil {
		writeItemGreetingError(w, err, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, itemGreetingResponse(rule))
}

func (s *Server) deleteItemGreeting(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "规则标识无效")
		return
	}
	rule, err := s.Store.ItemGreetings.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "规则不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if _, ok := s.requireCookieOwner(w, r, rule.CookieID); !ok {
		return
	}
	if err := s.Store.ItemGreetings.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) clearItemGreetingRecords(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	if err := s.Store.ItemGreetings.ClearRecords(r.Context(), cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "清空失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// writeItemGreetingError 把商品首次回复写入错误映射为统一响应。
func writeItemGreetingError(w http.ResponseWriter, err error, fallback string) {
	var conflict *db.ItemGreetingConflictError
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "部分商品已被其他首次回复规则使用", "item_ids": conflict.ItemIDs,
		})
	case errors.Is(err, db.ErrNotFound):
		writeErr(w, http.StatusNotFound, "规则不存在")
	default:
		writeErr(w, http.StatusInternalServerError, fallback)
	}
}
