package db

import (
	"context"
	"database/sql"
	"errors"
)

// MarkOrderBargain 标记订单为小刀（砍价成团）订单。
func (o *Orders) MarkOrderBargain(ctx context.Context, orderID string) error {
	_, err := o.DB.ExecContext(ctx,
		`UPDATE orders SET is_bargain=1,updated_at=CURRENT_TIMESTAMP WHERE order_id=?`, orderID)
	return err
}

// MarkOrderRedFlowerRequested 标记订单已经求过小红花。
func (o *Orders) MarkOrderRedFlowerRequested(ctx context.Context, orderID string) error {
	_, err := o.DB.ExecContext(ctx,
		`UPDATE orders SET is_red_flower=1,updated_at=CURRENT_TIMESTAMP WHERE order_id=?`, orderID)
	return err
}

// OrderIsBargain 返回订单是否已被标记为小刀订单。订单行不存在时按未标记处理。
func (o *Orders) OrderIsBargain(ctx context.Context, orderID string) (bool, error) {
	var flag int
	err := o.DB.QueryRowContext(ctx,
		`SELECT COALESCE(is_bargain,0) FROM orders WHERE order_id=?`, orderID).Scan(&flag)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return flag != 0, nil
}
