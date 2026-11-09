package db

import (
	"context"
	"database/sql"
	"time"
)

// AITurnLog 是一次 AI 处理（一条买家消息）的观测记录。
// 用于提示词效果分析、工具调用排查和灰度对比，不参与对话上下文。
type AITurnLog struct {
	ID               int64  `json:"id"`
	AIProfileID      int64  `json:"ai_profile_id"`
	CookieID         string `json:"cookie_id"`
	ChatID           string `json:"chat_id"`
	ItemID           string `json:"item_id"`
	UserID           string `json:"user_id"`
	Simulated        bool   `json:"simulated"`
	SessionID        string `json:"session_id"`
	SystemPrompt     string `json:"system_prompt"`
	PromptHash       string `json:"prompt_hash"`
	Model            string `json:"model"`
	ThinkingMode     string `json:"thinking_mode"`
	ReplyMode        string `json:"reply_mode"`
	Outcome          string `json:"outcome"`
	ErrorType        string `json:"error_type"`
	ReplyText        string `json:"reply_text"`
	Reasoning        string `json:"reasoning"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	DurationMs       int64  `json:"duration_ms"`
	CreatedAt        string `json:"created_at"`
}

// AIToolCallLog 是一次工具调用的明细，挂在 AITurnLog 下。
type AIToolCallLog struct {
	ID         int64  `json:"id"`
	TurnLogID  int64  `json:"turn_log_id"`
	Round      int    `json:"round"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
	Result     string `json:"result"`
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	CreatedAt  string `json:"created_at"`
}

// AITurnLogStore 读写 AI 回合观测数据。
type AITurnLogStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// Insert 原子写入一轮观测及其工具调用明细，返回观测记录 ID。
func (s *AITurnLogStore) Insert(ctx context.Context, turn AITurnLog, calls []AIToolCallLog) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertReturningID(ctx, tx, s.Dialect, `INSERT INTO ai_turn_logs
		(ai_profile_id,cookie_id,chat_id,item_id,user_id,simulated,session_id,system_prompt,prompt_hash,model,thinking_mode,reply_mode,outcome,error_type,reply_text,reasoning,prompt_tokens,completion_tokens,total_tokens,duration_ms)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		turn.AIProfileID, turn.CookieID, turn.ChatID, turn.ItemID, turn.UserID, boolInt(turn.Simulated), turn.SessionID,
		turn.SystemPrompt, turn.PromptHash, turn.Model, turn.ThinkingMode, NormalizeAIReplyMode(turn.ReplyMode),
		turn.Outcome, turn.ErrorType, turn.ReplyText, turn.Reasoning, turn.PromptTokens, turn.CompletionTokens,
		turn.TotalTokens, turn.DurationMs)
	if err != nil {
		return 0, err
	}
	for _, call := range calls {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_tool_calls
			(turn_log_id,round_index,tool_name,arguments,result,success,duration_ms)
			VALUES (?,?,?,?,?,?,?)`,
			id, call.Round, call.ToolName, call.Arguments, call.Result, boolInt(call.Success), call.DurationMs); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// DeleteBefore 删除 cutoff 之前的观测数据，明细先于主表删除以兼容没有外键的方言。
func (s *AITurnLogStore) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM ai_tool_calls WHERE turn_log_id IN (SELECT id FROM ai_turn_logs WHERE created_at < ?)`, cutoff); err != nil {
		return 0, err
	}
	result, err := s.DB.ExecContext(ctx, `DELETE FROM ai_turn_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
