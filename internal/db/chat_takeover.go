package db

import (
	"context"
	"database/sql"
	"errors"
)

// LastSellerOutgoingAt 返回最近一条"真人卖家"出站消息的时间（毫秒）。
// 自动回复与自动化发送会带 ai- 前缀的 message_key，用于把它们与人工发送区分开：
// 只有人工发送才会让 AI 助手让位，避免 AI 自己的回复触发让位后停止服务。
// 没有人工发送记录时返回 (0, false, nil)。
func (s *ChatStore) LastSellerOutgoingAt(ctx context.Context, cookieID, chatID string) (int64, bool, error) {
	var sentAt int64
	err := s.DB.QueryRowContext(ctx, `SELECT sent_at FROM chat_messages
		WHERE cookie_id=? AND chat_id=? AND direction='outgoing' AND message_key NOT LIKE 'ai-%'
		ORDER BY sent_at DESC, id DESC LIMIT 1`, cookieID, chatID).Scan(&sentAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return sentAt, true, nil
}

// HasRecentOutgoingContent 判断 sinceMillis 之后是否已有内容相同的出站消息。
// 用于避免"本后台发送 + 平台回显"重复入库同一条消息。
func (s *ChatStore) HasRecentOutgoingContent(ctx context.Context, cookieID, chatID, content string, sinceMillis int64) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages
		WHERE cookie_id=? AND chat_id=? AND direction='outgoing' AND content=? AND sent_at>=?`,
		cookieID, chatID, content, sinceMillis).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
