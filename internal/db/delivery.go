package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ItemInfo 对应 item_info 表。
type ItemInfo struct {
	ID                    int64
	CookieID              string
	ItemID                string
	ItemTitle             string
	ItemDescription       string
	ItemCategory          string
	ItemPrice             string
	ItemDetail            string
	IsMultiSpec           bool
	MultiQuantityDelivery bool
}

// Items 商品信息操作。
type Items struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取某账号下某商品信息。不存在返回 ErrNotFound。
func (i *Items) Get(ctx context.Context, cookieID, itemID string) (*ItemInfo, error) {
	var it ItemInfo
	var isMulti, multiQty int
	var title, desc, cat, price, detail sql.NullString
	err := i.DB.QueryRowContext(ctx,
		`SELECT id, cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail,
		        is_multi_spec, multi_quantity_delivery
		 FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(
		&it.ID, &it.CookieID, &it.ItemID, &title, &desc, &cat,
		&price, &detail, &isMulti, &multiQty)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	it.ItemTitle = title.String
	it.ItemDescription = desc.String
	it.ItemCategory = cat.String
	it.ItemPrice = price.String
	it.ItemDetail = detail.String
	it.IsMultiSpec = isMulti != 0
	it.MultiQuantityDelivery = multiQty != 0
	return &it, nil
}

// IsMultiSpec 是否多规格商品。
func (i *Items) IsMultiSpec(ctx context.Context, cookieID, itemID string) bool {
	var v int
	err := i.DB.QueryRowContext(ctx, `SELECT is_multi_spec FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(&v)
	if err != nil {
		return false
	}
	return v != 0
}

// MultiQuantityDelivery 是否开启多数量发货。
func (i *Items) MultiQuantityDelivery(ctx context.Context, cookieID, itemID string) bool {
	var v int
	err := i.DB.QueryRowContext(ctx, `SELECT multi_quantity_delivery FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(&v)
	if err != nil {
		return false
	}
	return v != 0
}

// Cards 卡券操作。库存以 card_items 逐条记录维护，不再使用卡片级多行文本。
type Cards struct {
	DB      *sql.DB
	Dialect Dialect
}

// CardItemMeta 描述一次发货预留要写回的售出归属。
type CardItemMeta struct {
	BuyerID   string
	BuyerName string
	OrderID   string
	SpecName  string
	SpecValue string
	Quantity  int
}

// ReserveDataItem 原子预留一条可用卡密（data 类型），返回条目 ID 和内容。
// 发送确定失败时应调用 ReleaseDataItem 归还；发送结果不确定时保留已用状态供人工核对。
func (c *Cards) ReserveDataItem(ctx context.Context, cardID int64, meta CardItemMeta) (int64, string, error) {
	quantity := meta.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	for attempts := 0; attempts < 20; attempts++ {
		var itemID int64
		var content string
		err := c.DB.QueryRowContext(ctx,
			`SELECT id, content FROM card_items WHERE card_id=? AND status='available' ORDER BY id LIMIT 1`,
			cardID).Scan(&itemID, &content)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, "", ErrCardStockEmpty
			}
			return 0, "", err
		}
		res, err := c.DB.ExecContext(ctx,
			`UPDATE card_items SET status='used',buyer_id=?,buyer_name=?,order_id=?,spec_name=?,spec_value=?,
			        quantity=?,sold_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
			 WHERE id=? AND status='available'`,
			nullable(meta.BuyerID), nullable(meta.BuyerName), nullable(meta.OrderID),
			nullable(meta.SpecName), nullable(meta.SpecValue), quantity, itemID)
		if err != nil {
			return 0, "", err
		}
		if affected, err := res.RowsAffected(); err == nil && affected == 1 {
			return itemID, content, nil
		}
	}
	return 0, "", errors.New("卡密库存并发修改过于频繁，请稍后重试")
}

// ReleaseDataItem 把发送失败的预留卡密恢复为可用并清空售出归属。
func (c *Cards) ReleaseDataItem(ctx context.Context, itemID int64) error {
	if itemID <= 0 {
		return errors.New("恢复卡密缺少条目ID")
	}
	res, err := c.DB.ExecContext(ctx,
		`UPDATE card_items SET status='available',buyer_id=NULL,buyer_name=NULL,order_id=NULL,
		        spec_name=NULL,spec_value=NULL,quantity=1,sold_at=NULL,updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND status='used'`, itemID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return errors.New("卡密条目不可归还或已被修改")
	}
	return nil
}

// splitLines / joinLines 统一处理多行库存内容。
func splitLines(s string) []string {
	var out []string
	cur := []rune{}
	for _, r := range s {
		switch r {
		case '\n':
			out = append(out, string(cur))
			cur = cur[:0]
		case '\r':
			out = append(out, string(cur))
			cur = cur[:0]
			// 跳过 \r，处理 \r\n
		default:
			cur = append(cur, r)
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	// 过滤空行。
	res := out[:0]
	for _, l := range out {
		if strings.TrimSpace(l) != "" {
			res = append(res, l)
		}
	}
	return res
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}
