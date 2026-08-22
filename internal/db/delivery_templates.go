package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"xianyu-go/internal/deliverytemplate"
)

var ErrDeliveryTemplateReferenced = errors.New("发货模板仍被自动化规则引用")

type DeliveryTemplate struct {
	ID        int64
	UserID    int64
	Name      string
	Enabled   bool
	DeletedAt string
	CreatedAt string
	UpdatedAt string
	Messages  []DeliveryTemplateMessage
	Keys      []string
}

type DeliveryTemplateMessage struct {
	ID         int64
	TemplateID int64
	SortOrder  int
	Content    string
}

type DeliveryTemplateInput struct {
	UserID   int64
	Name     string
	Enabled  bool
	Messages []string
}

// DeliveryTemplateBinding maps an opaque template key to one card group.
type DeliveryTemplateBinding struct {
	VariableKey   string
	CardID        int64
	CardName      string
	DeliveryCount int
}

type DeliveryTemplateStore struct {
	DB      *sql.DB
	Dialect Dialect
}

func (d *DeliveryTemplateStore) ListForUser(ctx context.Context, userID int64) ([]DeliveryTemplate, error) {
	rows, err := d.DB.QueryContext(ctx, `SELECT id,user_id,name,enabled,created_at,updated_at
		FROM delivery_templates WHERE user_id=? AND deleted_at IS NULL ORDER BY updated_at DESC,id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryTemplate{}
	for rows.Next() {
		var item DeliveryTemplate
		var enabled int
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		if err := d.loadMessages(ctx, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DeliveryTemplateStore) GetForUser(ctx context.Context, userID, id int64) (*DeliveryTemplate, error) {
	var item DeliveryTemplate
	var enabled int
	err := d.DB.QueryRowContext(ctx, `SELECT id,user_id,name,enabled,created_at,updated_at
		FROM delivery_templates WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, userID).
		Scan(&item.ID, &item.UserID, &item.Name, &enabled, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Enabled = enabled != 0
	if err := d.loadMessages(ctx, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (d *DeliveryTemplateStore) loadMessages(ctx context.Context, item *DeliveryTemplate) error {
	rows, err := d.DB.QueryContext(ctx, `SELECT id,template_id,sort_order,content FROM delivery_template_messages WHERE template_id=? ORDER BY sort_order ASC,id ASC`, item.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	item.Messages = []DeliveryTemplateMessage{}
	var contents []string
	for rows.Next() {
		var message DeliveryTemplateMessage
		if err := rows.Scan(&message.ID, &message.TemplateID, &message.SortOrder, &message.Content); err != nil {
			return err
		}
		item.Messages = append(item.Messages, message)
		contents = append(contents, message.Content)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	parsed, err := deliverytemplate.Parse(contents)
	if err == nil {
		item.Keys = parsed.Keys
	}
	return nil
}

func (d *DeliveryTemplateStore) Create(ctx context.Context, in DeliveryTemplateInput) (int64, error) {
	parsed, err := deliverytemplate.Parse(in.Messages)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return 0, errors.New("发货模板名称不能为空")
	}
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertReturningID(ctx, tx, d.Dialect, `INSERT INTO delivery_templates (user_id,name,enabled) VALUES (?,?,?)`, in.UserID, strings.TrimSpace(in.Name), boolToInt(in.Enabled))
	if err != nil {
		return 0, err
	}
	if err := insertTemplateMessages(ctx, tx, id, parsed.Messages); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DeliveryTemplateStore) Update(ctx context.Context, userID, id int64, in DeliveryTemplateInput) error {
	parsed, err := deliverytemplate.Parse(in.Messages)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("发货模板名称不能为空")
	}
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldKeysJSON string
	_ = oldKeysJSON
	res, err := tx.ExecContext(ctx, `UPDATE delivery_templates SET name=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND deleted_at IS NULL`, strings.TrimSpace(in.Name), boolToInt(in.Enabled), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM delivery_template_messages WHERE template_id=?`, id); err != nil {
		return err
	}
	if err := insertTemplateMessages(ctx, tx, id, parsed.Messages); err != nil {
		return err
	}
	return tx.Commit()
}

func insertTemplateMessages(ctx context.Context, tx *sql.Tx, templateID int64, messages []string) error {
	for index, content := range messages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_template_messages (template_id,sort_order,content) VALUES (?,?,?)`, templateID, index+1, content); err != nil {
			return err
		}
	}
	return nil
}

func (d *DeliveryTemplateStore) Delete(ctx context.Context, userID, id int64) error {
	var refs int
	if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rule_actions a JOIN automation_rules r ON r.id=a.rule_id WHERE a.delivery_template_id=?`, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrDeliveryTemplateReferenced
	}
	res, err := d.DB.ExecContext(ctx, `UPDATE delivery_templates SET deleted_at=CURRENT_TIMESTAMP,enabled=0,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=? AND deleted_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
