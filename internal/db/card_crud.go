package db

import (
	"context"
	"database/sql"
	"errors"
)

// 卡种类型（kind）：决定发货行为。
const (
	// CardKindCard 批量卡密：每条一次性消耗。
	CardKindCard = "card"
	// CardKindText 固定文本：可重复发送，只取第一条卡密内容。
	CardKindText = "text"
	// CardKindImage 固定图片：可重复发送，只取第一条卡密内容（图片 URL）。
	CardKindImage = "image"
)

// ValidCardKind 判断卡种类型是否合法。
func ValidCardKind(kind string) bool {
	switch kind {
	case CardKindCard, CardKindText, CardKindImage:
		return true
	default:
		return false
	}
}

// CardFull 是卡种（cards）完整信息。卡种描述"发什么、什么时候发"，
// 具体内容由 card_items 逐条维护。
type CardFull struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Description  string     `json:"description"`
	Enabled      bool       `json:"enabled"`
	DelaySeconds int        `json:"delay_seconds"`
	IsMultiSpec  bool       `json:"is_multi_spec"`
	SpecName     string     `json:"spec_name"`
	SpecValue    string     `json:"spec_value"`
	UserID       int64      `json:"user_id"`
	ItemCount    int        `json:"item_count"`
	Stock        *CardStock `json:"stock,omitempty"`
}

// Get 取单个卡种。
func (c *Cards) Get(ctx context.Context, cardID int64) (*CardFull, error) {
	var cf CardFull
	var enabled, isMultiSpec int
	var kind, specName, specValue, desc sql.NullString
	err := c.DB.QueryRowContext(ctx,
		`SELECT id, name, kind, description, enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id
		 FROM cards WHERE id=?`, cardID).Scan(
		&cf.ID, &cf.Name, &kind, &desc, &enabled, &cf.DelaySeconds, &isMultiSpec, &specName, &specValue, &cf.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	cf.Kind = kind.String
	if cf.Kind == "" {
		cf.Kind = CardKindCard
	}
	cf.Description = desc.String
	cf.Enabled = enabled != 0
	cf.IsMultiSpec = isMultiSpec != 0
	cf.SpecName = specName.String
	cf.SpecValue = specValue.String
	if stock, stockErr := c.Stock(ctx, cf.UserID, cf.ID); stockErr == nil {
		cf.Stock = &stock
		cf.ItemCount = int(stock.Total)
	}
	return &cf, nil
}

// FindByName 按名称查找该用户的卡种（批量导入据此把同名行并入同一卡种）。
// 不存在返回 ErrNotFound。
func (c *Cards) FindByName(ctx context.Context, userID int64, name string) (*CardFull, error) {
	var id int64
	err := c.DB.QueryRowContext(ctx,
		`SELECT id FROM cards WHERE user_id=? AND name=? ORDER BY id LIMIT 1`, userID, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c.Get(ctx, id)
}

// AllForUser 取某用户全部卡种，并带出每个卡种的库存统计。
func (c *Cards) AllForUser(ctx context.Context, userID int64) ([]CardFull, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT id, name, kind, description, enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id
		 FROM cards WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardFull
	for rows.Next() {
		var cf CardFull
		var enabled, isMultiSpec int
		var kind, specName, specValue, desc sql.NullString
		if err := rows.Scan(&cf.ID, &cf.Name, &kind, &desc, &enabled, &cf.DelaySeconds, &isMultiSpec,
			&specName, &specValue, &cf.UserID); err != nil {
			return nil, err
		}
		cf.Kind = kind.String
		if cf.Kind == "" {
			cf.Kind = CardKindCard
		}
		cf.Description = desc.String
		cf.Enabled = enabled != 0
		cf.IsMultiSpec = isMultiSpec != 0
		cf.SpecName = specName.String
		cf.SpecValue = specValue.String
		out = append(out, cf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if stocks, stockErr := c.StockByGroup(ctx, userID); stockErr == nil {
		for index := range out {
			if stock, ok := stocks[out[index].ID]; ok {
				s := stock
				out[index].Stock = &s
				out[index].ItemCount = int(stock.Total)
			} else {
				out[index].Stock = &CardStock{CardID: out[index].ID}
			}
		}
	}
	return out, nil
}

// Create 创建卡种，返回新 ID。
func (c *Cards) Create(ctx context.Context, cf *CardFull) (int64, error) {
	kind := cf.Kind
	if kind == "" {
		kind = CardKindCard
	}
	return insertReturningID(ctx, c.DB, c.Dialect,
		`INSERT INTO cards (name, kind, description, enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		cf.Name, kind, nullable(cf.Description), boolToInt(cf.Enabled), cf.DelaySeconds,
		boolToInt(cf.IsMultiSpec), nullable(cf.SpecName), nullable(cf.SpecValue), cf.UserID)
}

// Update 更新卡种。
func (c *Cards) Update(ctx context.Context, cf *CardFull) error {
	kind := cf.Kind
	if kind == "" {
		kind = CardKindCard
	}
	_, err := c.DB.ExecContext(ctx,
		`UPDATE cards SET name=?, kind=?, description=?, enabled=?, delay_seconds=?, is_multi_spec=?,
		    spec_name=?, spec_value=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		cf.Name, kind, nullable(cf.Description), boolToInt(cf.Enabled), cf.DelaySeconds,
		boolToInt(cf.IsMultiSpec), nullable(cf.SpecName), nullable(cf.SpecValue), cf.ID)
	return err
}

// Delete 删除卡种；其卡密条目由外键 ON DELETE CASCADE 一并删除。
func (c *Cards) Delete(ctx context.Context, cardID int64) error {
	_, err := c.DB.ExecContext(ctx, `DELETE FROM cards WHERE id=?`, cardID)
	return err
}
