package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

const (
	maxSimulationMessageRunes = 2000
	maxSimulationSessions     = 500
	aiSimulationSessionTTL    = 2 * time.Hour
	aiSimulationRequestBudget = 4 * time.Minute
)

// aiSimulationSessions 保存后台模拟会话（仅内存，重启即清空）。
// 模拟数据不写 ai_conversations，避免污染真实回复上下文。
type aiSimulationSessions struct {
	mu       sync.Mutex
	sessions map[string]*aiSimulationSession
}

type aiSimulationSession struct {
	history  []db.AIConversationMessage
	lastUsed time.Time
}

func (s *aiSimulationSessions) get(profileID int64, sessionID string) []db.AIConversationMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		return nil
	}
	session := s.sessions[simulationSessionKey(profileID, sessionID)]
	if session == nil {
		return nil
	}
	session.lastUsed = time.Now()
	return append([]db.AIConversationMessage(nil), session.history...)
}

func (s *aiSimulationSessions) append(profileID int64, sessionID string, messages ...db.AIConversationMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*aiSimulationSession)
	}
	now := time.Now()
	for key, session := range s.sessions {
		if now.Sub(session.lastUsed) > aiSimulationSessionTTL {
			delete(s.sessions, key)
		}
	}
	key := simulationSessionKey(profileID, sessionID)
	session := s.sessions[key]
	if session == nil {
		if len(s.sessions) >= maxSimulationSessions {
			oldestKey := ""
			var oldest time.Time
			for candidate, item := range s.sessions {
				if oldestKey == "" || item.lastUsed.Before(oldest) {
					oldestKey, oldest = candidate, item.lastUsed
				}
			}
			delete(s.sessions, oldestKey)
		}
		session = &aiSimulationSession{}
		s.sessions[key] = session
	}
	session.history = append(session.history, messages...)
	if len(session.history) > 40 {
		session.history = session.history[len(session.history)-40:]
	}
	session.lastUsed = now
}

func (s *aiSimulationSessions) reset(profileID int64, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, simulationSessionKey(profileID, sessionID))
}

func simulationSessionKey(profileID int64, sessionID string) string {
	return fmt.Sprintf("%d:%s", profileID, sessionID)
}

type aiSimulationRequest struct {
	SessionID       string  `json:"session_id"`
	Message         string  `json:"message"`
	Reset           bool    `json:"reset"`
	ItemTitle       string  `json:"item_title"`
	ItemPrice       float64 `json:"item_price"`
	ItemDescription string  `json:"item_description"`
	MockOrderID     string  `json:"mock_order_id"`
	MockOrderStatus string  `json:"mock_order_status"`
}

// simulateAIProfile 是管理员的模拟入口：以买家身份和助手对话，不触达真实平台。
func (s *Server) simulateAIProfile(w http.ResponseWriter, r *http.Request) {
	profile, ok := s.requireAIProfileOwner(w, r)
	if !ok {
		return
	}
	var req aiSimulationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeErr(w, http.StatusBadRequest, "模拟消息不能为空")
		return
	}
	if utf8.RuneCountInString(message) > maxSimulationMessageRunes {
		writeErr(w, http.StatusBadRequest, "模拟消息不能超过 2000 字")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if len(sessionID) > 64 {
		writeErr(w, http.StatusBadRequest, "会话 ID 过长")
		return
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("sim-%d", time.Now().UnixNano())
	}
	if req.Reset {
		s.simSessions.reset(profile.ID, sessionID)
	}
	history := s.simSessions.get(profile.ID, sessionID)

	replier := engine.NewAIReplier(profile.CookieID, s.Store, s.Logger)
	ctx, cancel := context.WithTimeout(r.Context(), aiSimulationRequestBudget)
	defer cancel()
	result, err := replier.Simulate(ctx, engine.AISimulationRequest{
		ProfileID:       profile.ID,
		SessionID:       sessionID,
		Message:         message,
		History:         history,
		ItemTitle:       req.ItemTitle,
		ItemPrice:       req.ItemPrice,
		ItemDescription: req.ItemDescription,
		MockOrderID:     req.MockOrderID,
		MockOrderStatus: req.MockOrderStatus,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	assistant := result.ReplyText
	if assistant == "" {
		assistant = "[未回复]"
	}
	s.simSessions.append(profile.ID, sessionID,
		db.AIConversationMessage{Role: "user", Content: message},
		db.AIConversationMessage{Role: "assistant", Content: assistant},
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"result":  result,
		"history": s.simSessions.get(profile.ID, sessionID),
	})
}
