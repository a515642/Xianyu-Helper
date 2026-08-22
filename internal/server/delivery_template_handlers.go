package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

type deliveryTemplateMessageRequest struct {
	Content string `json:"content"`
}
type deliveryTemplateRequest struct {
	Name     string                           `json:"name"`
	Enabled  *bool                            `json:"enabled"`
	Messages []deliveryTemplateMessageRequest `json:"messages"`
}

func (s *Server) listDeliveryTemplates(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	items, err := s.Store.DeliveryTemplates.ListForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplatesJSON(items))
}

func (s *Server) getDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "template_id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效发货模板ID")
		return
	}
	item, err := s.Store.DeliveryTemplates.GetForUser(r.Context(), sess.UserID, id)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "发货模板不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplateJSON(*item))
}

func (s *Server) createDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	input, err := decodeDeliveryTemplateRequest(r, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.Store.DeliveryTemplates.Create(r.Context(), input)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "template_id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效发货模板ID")
		return
	}
	input, err := decodeDeliveryTemplateRequest(r, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.DeliveryTemplates.Update(r.Context(), sess.UserID, id, input); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "发货模板不存在")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "template_id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效发货模板ID")
		return
	}
	if err := s.Store.DeliveryTemplates.Delete(r.Context(), sess.UserID, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "发货模板不存在")
			return
		}
		if errors.Is(err, db.ErrDeliveryTemplateReferenced) {
			writeErr(w, http.StatusConflict, "发货模板仍被自动化规则引用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func decodeDeliveryTemplateRequest(r *http.Request, userID int64) (db.DeliveryTemplateInput, error) {
	var req deliveryTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		return db.DeliveryTemplateInput{}, errors.New("请求格式错误")
	}
	messages := make([]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		messages = append(messages, message.Content)
	}
	if _, err := deliverytemplate.Parse(messages); err != nil {
		return db.DeliveryTemplateInput{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return db.DeliveryTemplateInput{UserID: userID, Name: strings.TrimSpace(req.Name), Enabled: enabled, Messages: messages}, nil
}

func deliveryTemplatesJSON(items []db.DeliveryTemplate) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, deliveryTemplateJSON(item))
	}
	return out
}

func deliveryTemplateJSON(item db.DeliveryTemplate) map[string]any {
	messages := make([]map[string]any, 0, len(item.Messages))
	for _, message := range item.Messages {
		messages = append(messages, map[string]any{"id": message.ID, "sort_order": message.SortOrder, "content": message.Content})
	}
	return map[string]any{"id": item.ID, "name": item.Name, "enabled": item.Enabled, "messages": messages, "keys": item.Keys, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt}
}
