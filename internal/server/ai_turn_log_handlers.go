package server

import (
	"net/http"
	"strings"

	"xianyu-go/internal/db"
)

// listAITurnLogs 返回某个 AI 助手的观测记录，用于提示词分析和灰度对比。
// GET /ai-profiles/{id}/turn-logs?simulated=real|simulated|all&outcome=&page=&page_size=
func (s *Server) listAITurnLogs(w http.ResponseWriter, r *http.Request) {
	profile, ok := s.requireAIProfileOwner(w, r)
	if !ok {
		return
	}
	filter := db.AITurnLogFilter{
		CookieID:  profile.CookieID,
		ProfileID: profile.ID,
		Outcome:   strings.TrimSpace(r.URL.Query().Get("outcome")),
		Page:      atoiDefault(r.URL.Query().Get("page"), 1),
		PageSize:  atoiDefault(r.URL.Query().Get("page_size"), 20),
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("simulated"))) {
	case "", "all":
	case "1", "true", "simulated":
		simulated := true
		filter.Simulated = &simulated
	case "0", "false", "real":
		simulated := false
		filter.Simulated = &simulated
	default:
		writeErr(w, http.StatusBadRequest, "simulated 只能是 real、simulated 或 all")
		return
	}
	page, err := s.Store.AILogs.List(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询 AI 观测记录失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}
