package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
)

const (
	aiTurnLogWriteTimeout = 5 * time.Second
	aiTurnLogRetention    = 30 * 24 * time.Hour
	aiTurnLogCleanupEvery = 24 * time.Hour
)

// aiTurnTrace 收集一次 AI 处理的观测信息；写日志失败不影响回复。
type aiTurnTrace struct {
	started       time.Time
	profileID     int64
	cookieID      string
	chatID        string
	itemID        string
	userID        string
	simulated     bool
	sessionID     string
	systemPrompt  string
	model         string
	thinkingMode  string
	replyMode     string
	reasoning     string
	promptTokens  int
	completionTok int
	totalTokens   int
	toolCalls     []db.AIToolCallLog
}

func newAITurnTrace(started time.Time, a *AIReplierImpl, m ChatMessage, profile *db.AIProfile, aiCfg *globalAIConfig, systemPrompt string) *aiTurnTrace {
	return &aiTurnTrace{
		started:      started,
		profileID:    profile.ID,
		cookieID:     a.cookieID,
		chatID:       m.ChatID,
		itemID:       m.ItemID,
		userID:       m.SenderUserID,
		systemPrompt: systemPrompt,
		model:        aiCfg.Model,
		thinkingMode: aiCfg.ThinkingMode,
		replyMode:    profile.ReplyMode,
	}
}

// observe 记录一次 LLM 响应中的思考内容和 token 用量。
func (t *aiTurnTrace) observe(resp openai.ChatCompletionResponse, message openai.ChatCompletionMessage) {
	if t == nil {
		return
	}
	if reasoning := strings.TrimSpace(message.ReasoningContent); reasoning != "" {
		if t.reasoning != "" {
			t.reasoning += "\n\n"
		}
		t.reasoning += reasoning
	}
	if resp.Usage.TotalTokens > 0 || resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		t.promptTokens = resp.Usage.PromptTokens
		t.completionTok = resp.Usage.CompletionTokens
		t.totalTokens = resp.Usage.TotalTokens
	}
}

// recordAITurn 异步落库一轮观测；任何错误都只记录日志，不改变回复结果。
func (a *AIReplierImpl) recordAITurn(trace *aiTurnTrace, result *ReplyResult, replyErr error) {
	if trace == nil || a == nil || a.store == nil || a.store.AILogs == nil {
		return
	}
	entry := db.AITurnLog{
		AIProfileID:      trace.profileID,
		CookieID:         trace.cookieID,
		ChatID:           trace.chatID,
		ItemID:           trace.itemID,
		UserID:           trace.userID,
		Simulated:        trace.simulated,
		SessionID:        trace.sessionID,
		SystemPrompt:     trace.systemPrompt,
		PromptHash:       hashAIPrompt(trace.systemPrompt),
		Model:            trace.model,
		ThinkingMode:     trace.thinkingMode,
		ReplyMode:        trace.replyMode,
		Reasoning:        trace.reasoning,
		PromptTokens:     trace.promptTokens,
		CompletionTokens: trace.completionTok,
		TotalTokens:      trace.totalTokens,
		DurationMs:       time.Since(trace.started).Milliseconds(),
	}
	switch {
	case replyErr != nil:
		entry.Outcome = "error"
		entry.ErrorType = aiTurnErrorType(replyErr)
	case result == nil:
		entry.Outcome = "empty"
	case result.Skip:
		entry.Outcome = "skipped"
	case strings.TrimSpace(result.Text) != "":
		entry.Outcome = "sent"
		entry.ReplyText = result.Text
	default:
		entry.Outcome = "empty"
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiTurnLogWriteTimeout)
	defer cancel()
	if _, err := a.store.AILogs.Insert(ctx, entry, trace.toolCalls); err != nil {
		a.logger.Warn("AI诊断：写入回合观测失败", "profile", trace.profileID, "chat_id", trace.chatID, "error_type", fmt.Sprintf("%T", err))
	}
}

func aiTurnErrorType(err error) string {
	if err == nil {
		return ""
	}
	return classifyAIRequestError(err, context.Background())
}

func hashAIPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", sum)
}

// StartAITurnLogRetention 启动观测数据清理：启动时清理一次，之后按天清理 30 天前的数据。
func StartAITurnLogRetention(ctx context.Context, store *db.Store, logger *slog.Logger) {
	if store == nil || store.AILogs == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	go func() {
		clean := func() {
			cleanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			deleted, err := store.AILogs.DeleteBefore(cleanCtx, time.Now().Add(-aiTurnLogRetention))
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					logger.Warn("清理过期 AI 观测数据失败", "err", err)
				}
				return
			}
			if deleted > 0 {
				logger.Info("已清理过期 AI 观测数据", "deleted", deleted)
			}
		}
		clean()
		ticker := time.NewTicker(aiTurnLogCleanupEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				clean()
			}
		}
	}()
}
