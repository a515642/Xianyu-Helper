package server

import (
	"net/http"
	"strconv"
	"strings"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// listAutomationAuditEvents 查询当前用户的自动化行为审计事件。
func (s *Server) listAutomationAuditEvents(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	filter := db.AutomationAuditFilter{
		UserID:      sess.UserID,
		CookieID:    strings.TrimSpace(r.URL.Query().Get("cookie_id")),
		OrderID:     strings.TrimSpace(r.URL.Query().Get("order_id")),
		TriggerType: strings.TrimSpace(r.URL.Query().Get("trigger_type")),
		EventType:   strings.TrimSpace(r.URL.Query().Get("event_type")),
		Status:      strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:       atoiDefault(r.URL.Query().Get("limit"), 50),
		Offset:      atoiDefault(r.URL.Query().Get("offset"), 0),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("run_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			writeErr(w, http.StatusBadRequest, "无效运行ID")
			return
		}
		filter.RunID = id
	}
	if filter.CookieID != "" {
		if _, ok := s.requireCookieOwner(w, r, filter.CookieID); !ok {
			return
		}
	}
	events, total, err := s.Store.Automation.ListAuditEvents(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化规则日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  events,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}
