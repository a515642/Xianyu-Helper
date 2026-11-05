package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// listCardItems 分页查询卡密条目，支持按卡密组、状态、买家/订单/卡密关键字过滤。
func (s *Server) listCardItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	query := r.URL.Query()
	page := atoiDefault(query.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := atoiDefault(query.Get("page_size"), 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	status := strings.TrimSpace(query.Get("status"))
	if status != "" && !db.ValidCardItemStatus(status) {
		writeErr(w, http.StatusBadRequest, "非法卡密状态")
		return
	}
	cardID := int64(atoiDefault(query.Get("card_id"), 0))
	filter := db.CardItemFilter{
		UserID:    sess.UserID,
		CardID:    cardID,
		Status:    status,
		Keyword:   query.Get("keyword"),
		BuyerID:   query.Get("buyer_id"),
		BuyerName: query.Get("buyer_name"),
		OrderID:   query.Get("order_id"),
		Limit:     pageSize,
		Offset:    (page - 1) * pageSize,
	}
	items, total, err := s.Store.Cards.ListItems(r.Context(), filter)
	if err != nil {
		s.Logger.Error("查询卡密条目失败", "user_id", sess.UserID, "err", err)
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages,
	})
}

// addCardItems 往卡种添加卡密。body: {content}。
// 批量卡密按行每条一张；固定文本/图片整段一条。
func (s *Server) addCardItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡种ID")
		return
	}
	cf, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeErr(w, http.StatusBadRequest, "卡密内容不能为空")
		return
	}
	added, err := s.Store.Cards.AddItems(r.Context(), id, req.Content)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	stock, err := s.Store.Cards.Stock(r.Context(), sess.UserID, cf.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计库存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "added": added, "stock": stock})
}

// getCardItem 取单条卡密。
func (s *Server) getCardItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.requireCardItemOwner(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// updateCardItem 修改单条卡密（状态 / 内容 / 售出归属）。出库清空归属，回库保留内容。
func (s *Server) updateCardItem(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if _, ok := s.requireCardItemOwner(w, r); !ok {
		return
	}
	var req struct {
		Content *string `json:"content"`
		Status  *string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	itemID, _ := strconv.ParseInt(chi.URLParam(r, "item_id"), 10, 64)
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !db.ValidCardItemStatus(status) {
			writeErr(w, http.StatusBadRequest, "非法卡密状态")
			return
		}
		if _, err := s.Store.Cards.SetStatus(r.Context(), sess.UserID, []int64{itemID}, status); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新状态失败")
			return
		}
	}
	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			writeErr(w, http.StatusBadRequest, "卡密内容不能为空")
			return
		}
		if err := s.Store.Cards.SetItemContent(r.Context(), sess.UserID, itemID, content); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新内容失败")
			return
		}
	}
	item, err := s.Store.Cards.GetItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取卡密失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// deleteCardItem 删除单条卡密。
func (s *Server) deleteCardItem(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if _, ok := s.requireCardItemOwner(w, r); !ok {
		return
	}
	itemID, _ := strconv.ParseInt(chi.URLParam(r, "item_id"), 10, 64)
	n, err := s.Store.Cards.DeleteItems(r.Context(), sess.UserID, []int64{itemID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "卡密不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "deleted": n})
}

// deleteCardItems 批量删除卡密。IDs 通过 ids 查询参数或 JSON 体传入。
func (s *Server) deleteCardItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	ids, err := parseBulkIDs(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.Store.Cards.DeleteItems(r.Context(), sess.UserID, ids)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "deleted": n})
}

// bulkUpdateCardItems 批量出库/回库/作废。status=available 回库，status=used 出库，status=void 作废。
func (s *Server) bulkUpdateCardItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	var req struct {
		IDs        []int64 `json:"ids"`
		Status     string  `json:"status"`
		IDsCSV     string  `json:"ids_csv"`
		StatusName string  `json:"status_name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	ids := req.IDs
	if len(ids) == 0 && strings.TrimSpace(req.IDsCSV) != "" {
		ids, _ = parseIDList(req.IDsCSV)
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = strings.TrimSpace(req.StatusName)
	}
	if !db.ValidCardItemStatus(status) {
		writeErr(w, http.StatusBadRequest, "非法卡密状态，须为 available/used/void")
		return
	}
	if len(ids) == 0 {
		writeErr(w, http.StatusBadRequest, "请选择要操作的卡密")
		return
	}
	n, err := s.Store.Cards.SetStatus(r.Context(), sess.UserID, ids, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "批量更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "updated": n})
}

// cardStock 返回指定卡密组的库存统计。
func (s *Server) cardStock(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡密组ID")
		return
	}
	if _, ok := s.requireCardOwner(w, r, id); !ok {
		return
	}
	stock, err := s.Store.Cards.Stock(r.Context(), sess.UserID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计库存失败")
		return
	}
	writeJSON(w, http.StatusOK, stock)
}

// requireCardItemOwner 校验路径 item_id 存在且归属当前用户。
func (s *Server) requireCardItemOwner(w http.ResponseWriter, r *http.Request) (*db.CardItem, bool) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return nil, false
	}
	itemID, err := strconv.ParseInt(chi.URLParam(r, "item_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡密ID")
		return nil, false
	}
	item, err := s.Store.Cards.GetItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "卡密不存在")
		return nil, false
	}
	var ownerID int64
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT user_id FROM card_items WHERE id=?`, itemID).Scan(&ownerID); err != nil || ownerID != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权操作该卡密")
		return nil, false
	}
	return item, true
}

func parseBulkIDs(r *http.Request) ([]int64, error) {
	if raw := strings.TrimSpace(r.URL.Query().Get("ids")); raw != "" {
		return parseIDList(raw)
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return nil, errStr("请求格式错误")
	}
	if len(req.IDs) == 0 {
		return nil, errStr("请选择要操作的卡密")
	}
	return req.IDs, nil
}

func parseIDList(raw string) ([]int64, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, errStr("卡密ID格式错误")
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, errStr("请选择要操作的卡密")
	}
	return out, nil
}
