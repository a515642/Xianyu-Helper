package db

import (
	"context"
	"errors"
	"strings"
	"time"
)

// 卡密售出台账状态。
const (
	// CardSaleReserved 表示卡密已从库存预留，但发送结果尚未确认。
	CardSaleReserved = "reserved"
	// CardSaleSold 表示卡密已确认发给买家。
	CardSaleSold = "sold"
)

// ValidCardSaleStatus 判断台账状态是否合法。
func ValidCardSaleStatus(status string) bool {
	return status == CardSaleReserved || status == CardSaleSold
}

// CardSaleRecord 是 card_sale_records 的一行：一次卡密发出的快照。
// 卡种或卡密条目被删除后仍保留，Content 为当时发给买家的卡密正文。
type CardSaleRecord struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	CardID     int64  `json:"card_id"`
	CardItemID int64  `json:"card_item_id"`
	CardName   string `json:"card_name"`
	CardKind   string `json:"card_kind"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	RunID      int64  `json:"run_id"`
	CookieID   string `json:"cookie_id"`
	OrderID    string `json:"order_id"`
	ItemID     string `json:"item_id"`
	BuyerID    string `json:"buyer_id"`
	CreatedAt  int64  `json:"created_at"`
	SoldAt     int64  `json:"sold_at"`
}

// CardSaleFilter 是售出台账的分页查询条件。
type CardSaleFilter struct {
	UserID   int64
	CardID   int64
	Status   string
	Search   string
	CookieID string
	OrderID  string
	Limit    int
	Offset   int
}

// CardSalePage 是一页售出台账及符合条件的总数。
type CardSalePage struct {
	Records []CardSaleRecord `json:"records"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
}

const cardSaleColumns = `id,user_id,card_id,card_item_id,card_name,card_kind,content,status,run_id,
	COALESCE(cookie_id,''),COALESCE(order_id,''),COALESCE(item_id,''),COALESCE(buyer_id,''),created_at,sold_at`

// insertCardSale 在 exec 所在连接或事务内写入一条台账记录并返回主键。
func insertCardSale(ctx context.Context, exec DBTX, dialect Dialect, record CardSaleRecord) (int64, error) {
	return insertReturningID(ctx, exec, dialect,
		`INSERT INTO card_sale_records (user_id,card_id,card_item_id,card_name,card_kind,content,status,run_id,cookie_id,order_id,item_id,buyer_id,created_at,sold_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.UserID, record.CardID, record.CardItemID, record.CardName, record.CardKind, record.Content,
		record.Status, record.RunID, nullable(record.CookieID), nullable(record.OrderID),
		nullable(record.ItemID), nullable(record.BuyerID), record.CreatedAt, record.SoldAt)
}

// MarkDataItemSold 把某条卡密对应的待确认台账确认为已售；没有待确认记录时静默成功。
func (c *Cards) MarkDataItemSold(ctx context.Context, itemID int64) error {
	if itemID <= 0 {
		return nil
	}
	_, err := c.DB.ExecContext(ctx,
		`UPDATE card_sale_records SET status=?,sold_at=? WHERE card_item_id=? AND status=?`,
		CardSaleSold, time.Now().UTC().Unix(), itemID, CardSaleReserved)
	return err
}

// SettleRunSales 把某次自动化运行遗留的待确认台账确认为已售。
// 人工确认“已执行”后调用；用户与运行都必须匹配，避免跨用户结算。
func (c *Cards) SettleRunSales(ctx context.Context, userID, runID int64) (int64, error) {
	if userID <= 0 || runID <= 0 {
		return 0, nil
	}
	res, err := c.DB.ExecContext(ctx,
		`UPDATE card_sale_records SET status=?,sold_at=? WHERE user_id=? AND run_id=? AND status=?`,
		CardSaleSold, time.Now().UTC().Unix(), userID, runID, CardSaleReserved)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// ListCardSales 按用户隔离分页查询售出台账，按最近一次预留倒序。
func (c *Cards) ListCardSales(ctx context.Context, f CardSaleFilter) (CardSalePage, error) {
	status := strings.TrimSpace(f.Status)
	if status != "" && !ValidCardSaleStatus(status) {
		return CardSalePage{}, errors.New("非法售出台账状态")
	}
	where, args := buildCardSaleWhere(f, status)
	var page CardSalePage
	if err := c.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM card_sale_records WHERE `+where, args...).Scan(&page.Total); err != nil {
		return CardSalePage{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	page.Limit, page.Offset = limit, offset
	rows, err := c.DB.QueryContext(ctx,
		`SELECT `+cardSaleColumns+` FROM card_sale_records WHERE `+where+`
		 ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return CardSalePage{}, err
	}
	defer rows.Close()
	page.Records = make([]CardSaleRecord, 0)
	for rows.Next() {
		record, scanErr := scanCardSale(rows.Scan)
		if scanErr != nil {
			return CardSalePage{}, scanErr
		}
		page.Records = append(page.Records, record)
	}
	return page, rows.Err()
}

// AllCardSales 返回某卡种的全部台账（导出用），按售出先后排列。
func (c *Cards) AllCardSales(ctx context.Context, userID, cardID int64) ([]CardSaleRecord, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT `+cardSaleColumns+` FROM card_sale_records WHERE user_id=? AND card_id=? ORDER BY id`,
		userID, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CardSaleRecord, 0)
	for rows.Next() {
		record, scanErr := scanCardSale(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func scanCardSale(scan func(dest ...any) error) (CardSaleRecord, error) {
	var record CardSaleRecord
	err := scan(&record.ID, &record.UserID, &record.CardID, &record.CardItemID,
		&record.CardName, &record.CardKind, &record.Content, &record.Status, &record.RunID,
		&record.CookieID, &record.OrderID, &record.ItemID, &record.BuyerID,
		&record.CreatedAt, &record.SoldAt)
	return record, err
}

// buildCardSaleWhere 把查询条件转换为 WHERE 片段和参数，搜索词中的 LIKE 通配符会被转义。
func buildCardSaleWhere(f CardSaleFilter, status string) (string, []any) {
	clauses := []string{"user_id=?"}
	args := []any{f.UserID}
	if f.CardID > 0 {
		clauses = append(clauses, "card_id=?")
		args = append(args, f.CardID)
	}
	if status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, status)
	}
	if cookieID := strings.TrimSpace(f.CookieID); cookieID != "" {
		clauses = append(clauses, "cookie_id=?")
		args = append(args, cookieID)
	}
	if orderID := strings.TrimSpace(f.OrderID); orderID != "" {
		clauses = append(clauses, "order_id=?")
		args = append(args, orderID)
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		pattern := "%" + strings.ToLower(strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(search)) + "%"
		clauses = append(clauses,
			`(LOWER(COALESCE(order_id,''))=? OR LOWER(COALESCE(buyer_id,''))=? OR LOWER(card_name) LIKE ? ESCAPE '!' OR LOWER(content) LIKE ? ESCAPE '!')`)
		args = append(args, strings.ToLower(search), strings.ToLower(search), pattern, pattern)
	}
	return strings.Join(clauses, " AND "), args
}
