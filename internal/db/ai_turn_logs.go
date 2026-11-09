package db

import (
	"context"
	"database/sql"
	"strings"
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
	// ToolCalls 仅在查询结果中附带，写入时由 Insert 单独处理。
	ToolCalls []AIToolCallLog `json:"tool_calls"`
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

// AITurnLogFilter 是观测列表的查询条件；Simulated 为 nil 时不过滤真实/模拟。
type AITurnLogFilter struct {
	CookieID  string
	ProfileID int64
	Simulated *bool
	Outcome   string
	Page      int
	PageSize  int
}

// AITurnLogPage 是一页观测记录，附带每轮的工具调用明细。
type AITurnLogPage struct {
	Logs     []AITurnLog `json:"logs"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// List 按时间倒序分页查询观测记录，供提示词分析与灰度对比。
func (s *AITurnLogStore) List(ctx context.Context, filter AITurnLogFilter) (AITurnLogPage, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	where := ` WHERE cookie_id=?`
	args := []any{filter.CookieID}
	if filter.ProfileID > 0 {
		where += ` AND ai_profile_id=?`
		args = append(args, filter.ProfileID)
	}
	if filter.Simulated != nil {
		where += ` AND simulated=?`
		args = append(args, boolInt(*filter.Simulated))
	}
	if filter.Outcome != "" {
		where += ` AND outcome=?`
		args = append(args, filter.Outcome)
	}

	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_turn_logs`+where, args...).Scan(&total); err != nil {
		return AITurnLogPage{}, err
	}
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := s.DB.QueryContext(ctx, `SELECT id,ai_profile_id,cookie_id,chat_id,item_id,user_id,simulated,session_id,
		system_prompt,prompt_hash,model,thinking_mode,reply_mode,outcome,error_type,reply_text,reasoning,
		prompt_tokens,completion_tokens,total_tokens,duration_ms,created_at
		FROM ai_turn_logs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return AITurnLogPage{}, err
	}
	defer rows.Close()
	logs := []AITurnLog{}
	for rows.Next() {
		log, err := scanAITurnLog(rows)
		if err != nil {
			return AITurnLogPage{}, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return AITurnLogPage{}, err
	}
	if len(logs) > 0 {
		if err := s.loadToolCalls(ctx, logs); err != nil {
			return AITurnLogPage{}, err
		}
	}
	return AITurnLogPage{Logs: logs, Total: total, Page: page, PageSize: pageSize}, nil
}

func scanAITurnLog(scanner rowScanner) (AITurnLog, error) {
	var log AITurnLog
	var simulated int
	var createdAt any
	err := scanner.Scan(&log.ID, &log.AIProfileID, &log.CookieID, &log.ChatID, &log.ItemID, &log.UserID,
		&simulated, &log.SessionID, &log.SystemPrompt, &log.PromptHash, &log.Model, &log.ThinkingMode,
		&log.ReplyMode, &log.Outcome, &log.ErrorType, &log.ReplyText, &log.Reasoning,
		&log.PromptTokens, &log.CompletionTokens, &log.TotalTokens, &log.DurationMs, &createdAt)
	if err != nil {
		return log, err
	}
	log.Simulated = simulated != 0
	log.CreatedAt = formatDBTime(createdAt)
	log.ToolCalls = []AIToolCallLog{}
	return log, nil
}

func (s *AITurnLogStore) loadToolCalls(ctx context.Context, logs []AITurnLog) error {
	index := make(map[int64]int, len(logs))
	placeholders := make([]string, 0, len(logs))
	args := make([]any, 0, len(logs))
	for i := range logs {
		index[logs[i].ID] = i
		placeholders = append(placeholders, "?")
		args = append(args, logs[i].ID)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,turn_log_id,round_index,tool_name,arguments,result,success,duration_ms,created_at
		FROM ai_tool_calls WHERE turn_log_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY turn_log_id DESC, round_index ASC, id ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var call AIToolCallLog
		var success int
		var createdAt any
		if err := rows.Scan(&call.ID, &call.TurnLogID, &call.Round, &call.ToolName, &call.Arguments, &call.Result, &success, &call.DurationMs, &createdAt); err != nil {
			return err
		}
		call.Success = success != 0
		call.CreatedAt = formatDBTime(createdAt)
		if i, ok := index[call.TurnLogID]; ok {
			logs[i].ToolCalls = append(logs[i].ToolCalls, call)
		}
	}
	return rows.Err()
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
