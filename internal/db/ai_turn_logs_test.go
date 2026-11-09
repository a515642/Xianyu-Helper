package db

import (
	"context"
	"testing"
	"time"
)

// TestAITurnLogStore_InsertAndDeleteBefore 验证观测主表与工具明细原子写入，
// 以及按保留期删除时明细先于主表清理。
func TestAITurnLogStore_InsertAndDeleteBefore(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	turn := AITurnLog{
		AIProfileID:      7,
		CookieID:         "acc1",
		ChatID:           "chat1",
		ItemID:           "item1",
		UserID:           "buyer1",
		Simulated:        true,
		SessionID:        "sim-1",
		SystemPrompt:     "你是客服",
		PromptHash:       "hash",
		Model:            "test-model",
		ThinkingMode:     "enabled",
		ReplyMode:        "tools",
		Outcome:          "sent",
		ReplyText:        "你好",
		Reasoning:        "思考过程",
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		DurationMs:       1234,
	}
	calls := []AIToolCallLog{
		{Round: 1, ToolName: "send_reply", Arguments: `{"messages":["你好"]}`, Result: `{"ok":true}`, Success: true, DurationMs: 20},
		{Round: 2, ToolName: "notify_human", Arguments: `{}`, Result: `{"ok":true}`, Success: true, DurationMs: 5},
	}
	id, err := s.AILogs.Insert(ctx, turn, calls)
	if err != nil || id <= 0 {
		t.Fatalf("Insert: id=%d err=%v", id, err)
	}

	var got AITurnLog
	var simulated int
	if err := s.DB.QueryRowContext(ctx, `SELECT ai_profile_id,cookie_id,chat_id,item_id,user_id,simulated,session_id,system_prompt,prompt_hash,model,thinking_mode,reply_mode,outcome,reply_text,reasoning,prompt_tokens,completion_tokens,total_tokens,duration_ms FROM ai_turn_logs WHERE id=?`, id).Scan(
		&got.AIProfileID, &got.CookieID, &got.ChatID, &got.ItemID, &got.UserID, &simulated, &got.SessionID, &got.SystemPrompt, &got.PromptHash, &got.Model, &got.ThinkingMode, &got.ReplyMode, &got.Outcome, &got.ReplyText, &got.Reasoning, &got.PromptTokens, &got.CompletionTokens, &got.TotalTokens, &got.DurationMs); err != nil {
		t.Fatalf("query turn: %v", err)
	}
	if got.AIProfileID != 7 || got.ChatID != "chat1" || got.ReplyMode != "tools" || got.Outcome != "sent" || got.ReplyText != "你好" || got.TotalTokens != 15 || simulated != 1 {
		t.Fatalf("turn = %+v simulated=%d", got, simulated)
	}
	var callCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_tool_calls WHERE turn_log_id=?`, id).Scan(&callCount); err != nil || callCount != 2 {
		t.Fatalf("tool calls = %d err=%v", callCount, err)
	}

	// 再写一轮并把它调整到保留期之前；第一轮保持最新。
	oldID, err := s.AILogs.Insert(ctx, AITurnLog{CookieID: "acc1", ChatID: "old-chat", Outcome: "skipped"}, []AIToolCallLog{{ToolName: "skip_reply", Success: true}})
	if err != nil {
		t.Fatalf("Insert old: %v", err)
	}
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	if _, err := s.DB.ExecContext(ctx, `UPDATE ai_turn_logs SET created_at=? WHERE id=?`, oldTime, oldID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	deleted, err := s.AILogs.DeleteBefore(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteBefore: deleted=%d err=%v", deleted, err)
	}
	var remaining int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_turn_logs`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("remaining turns = %d err=%v", remaining, err)
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_tool_calls WHERE turn_log_id=?`, oldID).Scan(&callCount); err != nil || callCount != 0 {
		t.Fatalf("old tool calls = %d err=%v", callCount, err)
	}
}
