package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// mountCardsReal 卡种与卡密条目（真实实现）。发货规则已统一到 automation_rules。
func (s *Server) mountCardsReal(r chi.Router) {
	// 卡种管理。
	r.Get("/cards", s.listCards)
	r.Post("/cards", s.createCard)
	r.Post("/cards/batch", s.batchCreateCards)
	r.Post("/cards/{card_id}/items", s.addCardItems)
	r.Get("/cards/{card_id}/details", s.getCard)
	r.Get("/cards/{card_id}/stock", s.cardStock)
	// 售出台账（卡种删除后仍保留）。
	r.Get("/card-sales", s.listCardSales)
	r.Get("/cards/{card_id}/sales/export", s.exportCardSales)
	// 卡密条目（逐条状态管理）。静态段需先于 {card_id} 注册，避免被当作组 ID。
	r.Get("/cards/items", s.listCardItems)
	r.Post("/cards/{card_id}/items/export", s.exportCardItems)
	r.Put("/cards/items", s.bulkUpdateCardItems)
	r.Delete("/cards/items", s.deleteCardItems)
	r.Get("/cards/items/{item_id}", s.getCardItem)
	r.Put("/cards/items/{item_id}", s.updateCardItem)
	r.Delete("/cards/items/{item_id}", s.deleteCardItem)
	r.Get("/cards/{card_id}", s.getCard)
	r.Put("/cards/{card_id}", s.updateCard)
	r.Delete("/cards/{card_id}", s.deleteCard)
}

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cards, err := s.Store.Cards.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		s.Logger.Error("查询卡密失败", "user_id", sess.UserID, "err", err)
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if cards == nil {
		s.Logger.Debug("用户没有卡密，返回空列表", "user_id", sess.UserID)
		cards = []db.CardFull{}
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	cf, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, cf)
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cf.UserID = sess.UserID
	id, err := s.Store.Cards.Create(r.Context(), cf)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	cf.ID = id
	cf.UserID = existing.UserID
	if err := s.Store.Cards.Update(r.Context(), cf); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	if _, ok := s.requireCardOwner(w, r, id); !ok {
		return
	}
	if err := s.Store.Cards.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func decodeCard(r *http.Request) (*db.CardFull, error) {
	var req struct {
		Name         string `json:"name"`
		Kind         string `json:"kind"`
		Description  string `json:"description"`
		Enabled      bool   `json:"enabled"`
		DelaySeconds int    `json:"delay_seconds"`
		IsMultiSpec  bool   `json:"is_multi_spec"`
		SpecName     string `json:"spec_name"`
		SpecValue    string `json:"spec_value"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, errStr("卡种名称不能为空")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = db.CardKindCard
	}
	if !db.ValidCardKind(kind) {
		return nil, errStr("卡密类型必须为 card、text 或 image")
	}
	if req.DelaySeconds < 0 || req.DelaySeconds > 3600 {
		return nil, errStr("延时发货必须在 0 到 3600 秒之间")
	}
	return &db.CardFull{
		Name: strings.TrimSpace(req.Name), Kind: kind, Description: req.Description,
		Enabled: req.Enabled, DelaySeconds: req.DelaySeconds, IsMultiSpec: req.IsMultiSpec,
		SpecName: req.SpecName, SpecValue: req.SpecValue,
	}, nil
}

// itemOwnedByUser 校验 cookieID 归属当前用户且其下存在 itemID 商品。
// 由自动化规则校验复用（原 deliveryRuleItemOwned，发货规则删除后改名）。
func (s *Server) itemOwnedByUser(r *http.Request, userID int64, cookieID, itemID string) bool {
	if itemID == "" {
		return true
	}
	all, err := s.Store.Cookies.AllForUser(r.Context(), userID)
	if err != nil || cookieID == "" {
		return false
	}
	if _, ok := all[cookieID]; !ok {
		return false
	}
	_, err = s.Store.Items.Get(r.Context(), cookieID, itemID)
	return err == nil
}

func errStr(s string) error { return &simpleError{s} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
