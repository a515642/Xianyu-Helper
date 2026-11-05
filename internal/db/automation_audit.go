package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// 自动化审计事件类型。
const (
	AuditEventSkipped         = "skipped"
	AuditEventQueued          = "queued"
	AuditEventRunStarted      = "run_started"
	AuditEventActionSucceeded = "action_succeeded"
	AuditEventActionFailed    = "action_failed"
	AuditEventRunFinished     = "run_finished"
)

// 自动化审计状态。
const (
	AuditStatusSkipped     = "skipped"
	AuditStatusQueued      = "queued"
	AuditStatusRunning     = "running"
	AuditStatusSuccess     = "success"
	AuditStatusFailed      = "failed"
	AuditStatusNeedsReview = "needs_review"
	AuditStatusCanceled    = "canceled"
)

// AutomationAuditEvent 是一条只追加的自动化行为审计事件。
// 它只保存摘要与原因，绝不包含卡密正文、消息正文或凭证。
type AutomationAuditEvent struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"-"`
	CookieID    string `json:"cookie_id"`
	OrderID     string `json:"order_id"`
	ItemID      string `json:"item_id"`
	RuleID      int64  `json:"rule_id"`
	RunID       int64  `json:"run_id"`
	TriggerType string `json:"trigger_type"`
	Source      string `json:"source"`
	EventType   string `json:"event_type"`
	Status      string `json:"status"`
	ActionType  string `json:"action_type"`
	Summary     string `json:"summary"`
	Reason      string `json:"reason"`
	CreatedAt   int64  `json:"created_at"`
}

// AutomationAuditFilter 是审计事件的查询条件；UserID 必须由调用方按登录用户填充。
type AutomationAuditFilter struct {
	UserID      int64
	CookieID    string
	OrderID     string
	TriggerType string
	EventType   string
	Status      string
	RunID       int64
	Limit       int
	Offset      int
}

// AppendAuditEvent 追加一条审计事件，事件归属账号的拥有者。
// 账号不存在时静默丢弃，避免审计旁路影响自动化主流程。
func (a *AutomationRules) AppendAuditEvent(ctx context.Context, e AutomationAuditEvent) error {
	var userID int64
	err := a.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, e.CookieID).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	_, err = a.DB.ExecContext(ctx,
		`INSERT INTO automation_audit_events
		 (user_id,cookie_id,order_id,item_id,rule_id,run_id,trigger_type,source,event_type,status,action_type,summary,reason,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		userID, e.CookieID, e.OrderID, e.ItemID, e.RuleID, e.RunID, e.TriggerType, e.Source,
		e.EventType, e.Status, e.ActionType, e.Summary, e.Reason, e.CreatedAt)
	return err
}

// ListAuditEvents 按用户隔离查询审计事件，返回当前页和总数。
func (a *AutomationRules) ListAuditEvents(ctx context.Context, f AutomationAuditFilter) ([]AutomationAuditEvent, int, error) {
	where := []string{"user_id=?"}
	args := []any{f.UserID}
	add := func(clause string, value any) {
		where = append(where, clause)
		args = append(args, value)
	}
	if value := strings.TrimSpace(f.CookieID); value != "" {
		add("cookie_id=?", value)
	}
	if value := strings.TrimSpace(f.OrderID); value != "" {
		add("order_id=?", value)
	}
	if value := strings.TrimSpace(f.TriggerType); value != "" {
		add("trigger_type=?", value)
	}
	if value := strings.TrimSpace(f.EventType); value != "" {
		add("event_type=?", value)
	}
	if value := strings.TrimSpace(f.Status); value != "" {
		add("status=?", value)
	}
	if f.RunID > 0 {
		add("run_id=?", f.RunID)
	}
	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := a.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM automation_audit_events`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := a.DB.QueryContext(ctx,
		`SELECT id,cookie_id,order_id,item_id,rule_id,run_id,trigger_type,source,event_type,status,
		        action_type,summary,reason,created_at
		   FROM automation_audit_events`+whereSQL+` ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AutomationAuditEvent, 0, limit)
	for rows.Next() {
		var e AutomationAuditEvent
		e.UserID = f.UserID
		if err := rows.Scan(&e.ID, &e.CookieID, &e.OrderID, &e.ItemID, &e.RuleID, &e.RunID,
			&e.TriggerType, &e.Source, &e.EventType, &e.Status, &e.ActionType,
			&e.Summary, &e.Reason, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// PruneAuditEvents 删除早于 before（Unix 秒）的审计事件。
func (a *AutomationRules) PruneAuditEvents(ctx context.Context, before int64) (int64, error) {
	res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_audit_events WHERE created_at<?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
