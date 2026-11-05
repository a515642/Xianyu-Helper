package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ItemGreetingRule 对应 item_greeting_rules，并携带规则覆盖的商品集合。
type ItemGreetingRule struct {
	// ID 是规则主键，新建前为 0。
	ID int64
	// CookieID 是规则所属账号。
	CookieID string
	// Name 是规则备注名。
	Name string
	// Enabled 表示规则是否参与首次回复匹配。
	Enabled bool
	// ReplyContent 是首次回复文字，可含 {send_user_name} 等变量。
	ReplyContent string
	// ReplyImageURL 是首次回复图片地址，可为空。
	ReplyImageURL string
	// ItemIDs 是规则覆盖的商品标识，同一账号内不会与其他规则重复。
	ItemIDs []string
}

// ItemGreetingConflictError 表示部分商品已经被同账号的其他首次回复规则占用。
type ItemGreetingConflictError struct {
	// ItemIDs 是发生冲突的商品标识，按查询顺序排列。
	ItemIDs []string
}

// Error 返回不含敏感信息的冲突说明。
func (e *ItemGreetingConflictError) Error() string {
	return "商品已被其他首次回复规则使用: " + strings.Join(e.ItemIDs, ",")
}

// ItemGreetings 商品首次回复规则及其按会话去重的投递记录。
type ItemGreetings struct {
	DB      *sql.DB
	Dialect Dialect
}

// ListByAccount 返回某账号的全部规则，按创建顺序排列。
func (g *ItemGreetings) ListByAccount(ctx context.Context, cookieID string) ([]ItemGreetingRule, error) {
	return g.list(ctx, `WHERE r.cookie_id=?`, cookieID)
}

// ListForUser 返回某用户名下所有账号的规则。
func (g *ItemGreetings) ListForUser(ctx context.Context, userID int64) ([]ItemGreetingRule, error) {
	return g.list(ctx, `JOIN cookies c ON c.id=r.cookie_id WHERE c.user_id=?`, userID)
}

// list 按给定的 JOIN/WHERE 片段读取规则，再一次性补齐商品集合。
// condition 只能是本包内的常量片段，参数通过 arg 绑定。
func (g *ItemGreetings) list(ctx context.Context, condition string, arg any) ([]ItemGreetingRule, error) {
	rows, err := g.DB.QueryContext(ctx,
		`SELECT r.id, r.cookie_id, r.name, r.enabled, r.reply_content, r.reply_image_url
		 FROM item_greeting_rules r `+condition+` ORDER BY r.id ASC`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := make([]ItemGreetingRule, 0)
	byID := make(map[int64]int)
	for rows.Next() {
		var rule ItemGreetingRule
		var enabled int
		if scanErr := rows.Scan(&rule.ID, &rule.CookieID, &rule.Name, &enabled, &rule.ReplyContent, &rule.ReplyImageURL); scanErr != nil {
			return nil, scanErr
		}
		rule.Enabled = enabled != 0
		rule.ItemIDs = []string{}
		byID[rule.ID] = len(rules)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return rules, nil
	}
	itemRows, err := g.DB.QueryContext(ctx,
		`SELECT i.rule_id, i.item_id FROM item_greeting_rule_items i
		 JOIN item_greeting_rules r ON r.id=i.rule_id `+condition+` ORDER BY i.rule_id ASC, i.item_id ASC`, arg)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var ruleID int64
		var itemID string
		if scanErr := itemRows.Scan(&ruleID, &itemID); scanErr != nil {
			return nil, scanErr
		}
		if index, ok := byID[ruleID]; ok {
			rules[index].ItemIDs = append(rules[index].ItemIDs, itemID)
		}
	}
	return rules, itemRows.Err()
}

// Get 读取单条规则；不存在返回 ErrNotFound。
func (g *ItemGreetings) Get(ctx context.Context, id int64) (*ItemGreetingRule, error) {
	rules, err := g.list(ctx, `WHERE r.id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, ErrNotFound
	}
	return &rules[0], nil
}

// Create 在同一事务中写入规则与商品集合，返回新规则主键。
func (g *ItemGreetings) Create(ctx context.Context, rule ItemGreetingRule) (int64, error) {
	tx, err := g.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := g.ensureItemsFree(ctx, tx, rule.CookieID, rule.ItemIDs, 0); err != nil {
		return 0, err
	}
	id, err := insertReturningID(ctx, tx, g.Dialect,
		`INSERT INTO item_greeting_rules (cookie_id, name, enabled, reply_content, reply_image_url)
		 VALUES (?,?,?,?,?)`,
		rule.CookieID, rule.Name, boolToInt(rule.Enabled), rule.ReplyContent, rule.ReplyImageURL)
	if err != nil {
		return 0, err
	}
	if err := g.insertItems(ctx, tx, id, rule.CookieID, rule.ItemIDs); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Update 覆盖规则内容与商品集合；规则不存在返回 ErrNotFound。
func (g *ItemGreetings) Update(ctx context.Context, rule ItemGreetingRule) error {
	tx, err := g.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := g.ensureItemsFree(ctx, tx, rule.CookieID, rule.ItemIDs, rule.ID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE item_greeting_rules SET name=?, enabled=?, reply_content=?, reply_image_url=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND cookie_id=?`,
		rule.Name, boolToInt(rule.Enabled), rule.ReplyContent, rule.ReplyImageURL, rule.ID, rule.CookieID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		// 部分驱动在值未变化时返回 0，需再确认规则确实存在。
		if _, getErr := g.Get(ctx, rule.ID); getErr != nil {
			return getErr
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_greeting_rule_items WHERE rule_id=?`, rule.ID); err != nil {
		return err
	}
	if err := g.insertItems(ctx, tx, rule.ID, rule.CookieID, rule.ItemIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete 删除规则及其商品集合；已存在的投递记录按账号保留，避免买家被重复打扰。
func (g *ItemGreetings) Delete(ctx context.Context, id int64) error {
	tx, err := g.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_greeting_rule_items WHERE rule_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_greeting_rules WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ensureItemsFree 检查商品是否被同账号其他规则占用，exceptRuleID 为正在更新的规则。
func (g *ItemGreetings) ensureItemsFree(ctx context.Context, tx *sql.Tx, cookieID string, itemIDs []string, exceptRuleID int64) error {
	var conflicts []string
	for _, itemID := range itemIDs {
		var existing string
		err := tx.QueryRowContext(ctx,
			`SELECT item_id FROM item_greeting_rule_items WHERE cookie_id=? AND item_id=? AND rule_id<>?`,
			cookieID, itemID, exceptRuleID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		conflicts = append(conflicts, existing)
	}
	if len(conflicts) > 0 {
		return &ItemGreetingConflictError{ItemIDs: conflicts}
	}
	return nil
}

// insertItems 写入规则的商品集合。
func (g *ItemGreetings) insertItems(ctx context.Context, tx *sql.Tx, ruleID int64, cookieID string, itemIDs []string) error {
	for _, itemID := range itemIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_greeting_rule_items (rule_id, cookie_id, item_id) VALUES (?,?,?)`,
			ruleID, cookieID, itemID); err != nil {
			return fmt.Errorf("写入首次回复商品: %w", err)
		}
	}
	return nil
}

// FindEnabledForItem 返回覆盖该商品且已启用的规则；没有则返回 ErrNotFound。
func (g *ItemGreetings) FindEnabledForItem(ctx context.Context, cookieID, itemID string) (*ItemGreetingRule, error) {
	var rule ItemGreetingRule
	var enabled int
	err := g.DB.QueryRowContext(ctx,
		`SELECT r.id, r.cookie_id, r.name, r.enabled, r.reply_content, r.reply_image_url
		 FROM item_greeting_rule_items i JOIN item_greeting_rules r ON r.id=i.rule_id
		 WHERE i.cookie_id=? AND i.item_id=? AND r.enabled<>0`, cookieID, itemID).
		Scan(&rule.ID, &rule.CookieID, &rule.Name, &enabled, &rule.ReplyContent, &rule.ReplyImageURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rule.Enabled = enabled != 0
	rule.ItemIDs = []string{itemID}
	return &rule, nil
}

// HasRecord 判断会话是否已触发过商品首次回复（任意投递状态）。
func (g *ItemGreetings) HasRecord(ctx context.Context, cookieID, chatID string) (bool, error) {
	var n int
	err := g.DB.QueryRowContext(ctx,
		`SELECT 1 FROM item_greeting_records WHERE cookie_id=? AND chat_id=? LIMIT 1`, cookieID, chatID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ClearRecords 清空账号的首次回复记录。
func (g *ItemGreetings) ClearRecords(ctx context.Context, cookieID string) error {
	_, err := g.DB.ExecContext(ctx, `DELETE FROM item_greeting_records WHERE cookie_id=?`, cookieID)
	return err
}

// ClaimRecord 原子领取一次首次回复投递。新记录初始化为 pending；失败记录允许继续
// 投递尚未成功的部分；pending/sent 记录会阻止并发重复发送。
func (g *ItemGreetings) ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (DefaultReplyRecord, bool, error) {
	now := time.Now().UTC().Unix()
	leaseExpiresAt := now + int64((5*time.Minute)/time.Second)
	query := dialectInsertIgnorePrefix(g.Dialect) + ` INTO item_greeting_records
		(cookie_id,chat_id,status,text_sent,image_sent,last_error,lease_expires_at,updated_at)
		VALUES (?,?, 'pending', ?, ?, '', ?, CURRENT_TIMESTAMP)` + dialectInsertIgnore(g.Dialect, []string{"cookie_id", "chat_id"})
	res, err := g.DB.ExecContext(ctx, query, cookieID, chatID, boolToInt(!needsText), boolToInt(!needsImage), leaseExpiresAt)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		return DefaultReplyRecord{Status: "pending", TextSent: !needsText, ImageSent: !needsImage}, true, nil
	}

	record, err := g.record(ctx, cookieID, chatID)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	if record.Status == "sent" {
		return record, false, nil
	}
	res, err = g.DB.ExecContext(ctx, `UPDATE item_greeting_records
		SET status='pending',last_error='',lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?
		  AND (status='failed' OR (status='pending' AND lease_expires_at<?))`,
		leaseExpiresAt, cookieID, chatID, now)
	if err != nil {
		return DefaultReplyRecord{}, false, err
	}
	affected, _ := res.RowsAffected()
	return record, affected > 0, nil
}

// record 查询一次首次回复的投递状态。
func (g *ItemGreetings) record(ctx context.Context, cookieID, chatID string) (DefaultReplyRecord, error) {
	var record DefaultReplyRecord
	var textSent, imageSent int
	err := g.DB.QueryRowContext(ctx, `SELECT status,text_sent,image_sent
		FROM item_greeting_records WHERE cookie_id=? AND chat_id=?`, cookieID, chatID).
		Scan(&record.Status, &textSent, &imageSent)
	record.TextSent = textSent != 0
	record.ImageSent = imageSent != 0
	return record, err
}

// MarkPartSent 标记图片或文字已经成功投递。
func (g *ItemGreetings) MarkPartSent(ctx context.Context, cookieID, chatID, part string) error {
	column := ""
	switch part {
	case "text":
		column = "text_sent"
	case "image":
		column = "image_sent"
	default:
		return errors.New("未知首次回复部分")
	}
	_, err := g.DB.ExecContext(ctx, `UPDATE item_greeting_records SET `+column+`=1,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// MarkRecordFailed 标记确定未送达的失败状态。
func (g *ItemGreetings) MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error {
	_, err := g.DB.ExecContext(ctx, `UPDATE item_greeting_records
		SET status='failed',last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND chat_id=?`, message, cookieID, chatID)
	return err
}

// MarkRecordSent 标记完整回复已经投递完成。
func (g *ItemGreetings) MarkRecordSent(ctx context.Context, cookieID, chatID string) error {
	_, err := g.DB.ExecContext(ctx, `UPDATE item_greeting_records
		SET status='sent',last_error='',lease_expires_at=0,replied_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}
