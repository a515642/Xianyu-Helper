package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 卡密条目状态。
const (
	CardItemAvailable = "available"
	CardItemUsed      = "used"
	CardItemVoid      = "void"
)

// ValidCardItemStatus 判断状态值是否合法。
func ValidCardItemStatus(status string) bool {
	switch status {
	case CardItemAvailable, CardItemUsed, CardItemVoid:
		return true
	default:
		return false
	}
}

// CardItem 是 card_items 表的一行：单条卡密内容及其状态与售出归属。
// 卡密内容一律是文本（图片卡种里存的就是图片 URL）。
type CardItem struct {
	ID        int64  `json:"id"`
	CardID    int64  `json:"card_id"`
	CardName  string `json:"card_name,omitempty"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	BuyerID   string `json:"buyer_id"`
	BuyerName string `json:"buyer_name"`
	OrderID   string `json:"order_id"`
	SpecName  string `json:"spec_name"`
	SpecValue string `json:"spec_value"`
	Quantity  int    `json:"quantity"`
	SoldAt    string `json:"sold_at"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CardStock 是某个卡种的库存统计。
type CardStock struct {
	CardID    int64 `json:"card_id"`
	Total     int64 `json:"total"`
	Available int64 `json:"available"`
	Used      int64 `json:"used"`
	Void      int64 `json:"void"`
}

// 时间戳列直接取原值，由 formatDBTime 归一化成字符串：
// Postgres/MySQL 驱动返回 time.Time，SQLite 返回字符串，COALESCE(...,”) 在
// Postgres 上会因类型不匹配报 SQLSTATE 22007。
const cardItemColumns = `ci.id, ci.card_id, ci.content, ci.status,
	COALESCE(ci.buyer_id,''), COALESCE(ci.buyer_name,''), COALESCE(ci.order_id,''),
	COALESCE(ci.spec_name,''), COALESCE(ci.spec_value,''), COALESCE(ci.quantity,1),
	ci.sold_at, ci.created_at, ci.updated_at`

// formatDBTime 把驱动返回的时间值归一化成 "2006-01-02 15:04:05" 字符串。
func formatDBTime(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// CardItemFilter 是卡密列表（逐条）的分页查询条件。
type CardItemFilter struct {
	UserID    int64
	CardID    int64
	Status    string
	Keyword   string
	BuyerID   string
	BuyerName string
	OrderID   string
	Limit     int
	Offset    int
}

// buildCardItemWhere 组装卡密列表/删除/导出共用的过滤条件。
func buildCardItemWhere(f CardItemFilter) (string, []any) {
	where := []string{"ci.user_id=?"}
	args := []any{f.UserID}
	if f.CardID > 0 {
		where = append(where, "ci.card_id=?")
		args = append(args, f.CardID)
	}
	if f.Status != "" {
		where = append(where, "ci.status=?")
		args = append(args, f.Status)
	}
	if keyword := strings.TrimSpace(f.Keyword); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		where = append(where, `(LOWER(ci.content) LIKE ? OR LOWER(COALESCE(ci.buyer_id,'')) LIKE ?
			OR LOWER(COALESCE(ci.buyer_name,'')) LIKE ? OR LOWER(COALESCE(ci.order_id,'')) LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	if buyerID := strings.TrimSpace(f.BuyerID); buyerID != "" {
		where = append(where, "LOWER(COALESCE(ci.buyer_id,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(buyerID)+"%")
	}
	if buyerName := strings.TrimSpace(f.BuyerName); buyerName != "" {
		where = append(where, "LOWER(COALESCE(ci.buyer_name,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(buyerName)+"%")
	}
	if orderID := strings.TrimSpace(f.OrderID); orderID != "" {
		where = append(where, "LOWER(COALESCE(ci.order_id,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(orderID)+"%")
	}
	return strings.Join(where, " AND "), args
}

// ListItems 按用户隔离分页查询卡密条目，并带出所属卡种名称。
func (c *Cards) ListItems(ctx context.Context, f CardItemFilter) ([]CardItem, int, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	whereSQL, args := buildCardItemWhere(f)

	var total int
	if err := c.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM card_items ci WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := c.DB.QueryContext(ctx,
		`SELECT `+cardItemColumns+`, COALESCE(c.name,'')
		 FROM card_items ci LEFT JOIN cards c ON c.id = ci.card_id
		 WHERE `+whereSQL+`
		 ORDER BY ci.id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]CardItem, 0)
	for rows.Next() {
		item, err := scanCardItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// ListItemsByIDs 按 ID 取该用户的一批卡密（保留传入顺序），用于批量复制/导出。
func (c *Cards) ListItemsByIDs(ctx context.Context, userID int64, itemIDs []int64) ([]CardItem, error) {
	ids := uniquePositiveIDs(itemIDs)
	if len(ids) == 0 {
		return []CardItem{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, userID)
	rows, err := c.DB.QueryContext(ctx,
		`SELECT `+cardItemColumns+`, COALESCE(c.name,'')
		 FROM card_items ci LEFT JOIN cards c ON c.id = ci.card_id
		 WHERE ci.id IN (`+placeholders+`) AND ci.user_id=?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[int64]CardItem, len(ids))
	for rows.Next() {
		item, err := scanCardItem(rows)
		if err != nil {
			return nil, err
		}
		byID[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]CardItem, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// GetItem 取单条卡密（带卡种名称）。
func (c *Cards) GetItem(ctx context.Context, itemID int64) (*CardItem, error) {
	row := c.DB.QueryRowContext(ctx,
		`SELECT `+cardItemColumns+`, COALESCE(c.name,'')
		 FROM card_items ci LEFT JOIN cards c ON c.id = ci.card_id WHERE ci.id=?`, itemID)
	item, err := scanCardItem(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

// rowScanner 是 *sql.Row 与 *sql.Rows 的公共扫描接口。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCardItem(scanner rowScanner) (CardItem, error) {
	var it CardItem
	var cardName sql.NullString
	var soldAt, createdAt, updatedAt any
	if err := scanner.Scan(&it.ID, &it.CardID, &it.Content, &it.Status,
		&it.BuyerID, &it.BuyerName, &it.OrderID, &it.SpecName, &it.SpecValue, &it.Quantity,
		&soldAt, &createdAt, &updatedAt, &cardName); err != nil {
		return it, err
	}
	it.CardName = cardName.String
	it.SoldAt = formatDBTime(soldAt)
	it.CreatedAt = formatDBTime(createdAt)
	it.UpdatedAt = formatDBTime(updatedAt)
	return it, nil
}

// AddItems 往卡种添加卡密内容。卡密类型（card）按行每条一张；
// 固定文本/图片是固定内容，整段作为一条（发货只取第一条）。
func (c *Cards) AddItems(ctx context.Context, cardID int64, content string) (int, error) {
	var userID int64
	var kind string
	if err := c.DB.QueryRowContext(ctx,
		`SELECT COALESCE(user_id,1), COALESCE(kind,'card') FROM cards WHERE id=?`, cardID).Scan(&userID, &kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	var lines []string
	if kind == CardKindCard {
		lines = splitLines(content)
	} else if trimmed := strings.TrimSpace(content); trimmed != "" {
		lines = []string{trimmed}
	}
	if len(lines) == 0 {
		return 0, errors.New("卡密内容不能为空")
	}
	added := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, err := c.DB.ExecContext(ctx,
			`INSERT INTO card_items (card_id,user_id,content,status) VALUES (?,?,?,'available')`,
			cardID, userID, line); err != nil {
			return added, err
		}
		added++
	}
	if added == 0 {
		return 0, errors.New("卡密内容不能为空")
	}
	return added, nil
}

// FirstItemContent 返回卡种里第一条卡密内容（固定文本/图片发货只用这一条）。
func (c *Cards) FirstItemContent(ctx context.Context, cardID int64) (string, error) {
	var content string
	err := c.DB.QueryRowContext(ctx,
		`SELECT content FROM card_items WHERE card_id=? ORDER BY id LIMIT 1`, cardID).Scan(&content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return content, nil
}

// Stock 统计某卡种的库存。cardID<=0 时统计该用户全部卡种。
func (c *Cards) Stock(ctx context.Context, userID, cardID int64) (CardStock, error) {
	var stock CardStock
	stock.CardID = cardID
	where := "user_id=?"
	args := []any{userID}
	if cardID > 0 {
		where += " AND card_id=?"
		args = append(args, cardID)
	}
	err := c.DB.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status='available' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN status='used' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN status='void' THEN 1 ELSE 0 END),0)
		 FROM card_items WHERE `+where, args...).Scan(&stock.Total, &stock.Available, &stock.Used, &stock.Void)
	if err != nil {
		return stock, err
	}
	return stock, nil
}

// StockByGroup 返回该用户每个卡种的库存统计，供列表页批量展示。
func (c *Cards) StockByGroup(ctx context.Context, userID int64) (map[int64]CardStock, error) {
	rows, err := c.DB.QueryContext(ctx,
		`SELECT card_id, COUNT(*),
		        COALESCE(SUM(CASE WHEN status='available' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN status='used' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN status='void' THEN 1 ELSE 0 END),0)
		 FROM card_items WHERE user_id=? GROUP BY card_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]CardStock)
	for rows.Next() {
		var stock CardStock
		if err := rows.Scan(&stock.CardID, &stock.Total, &stock.Available, &stock.Used, &stock.Void); err != nil {
			return nil, err
		}
		out[stock.CardID] = stock
	}
	return out, rows.Err()
}

// SetStatus 出库（available）/回库（used）/作废（void）批量改变卡密状态。
// 置回 available 时清空售出归属；置为 used 时若无订单信息则仅标记状态。
func (c *Cards) SetStatus(ctx context.Context, userID int64, itemIDs []int64, status string) (int, error) {
	if !ValidCardItemStatus(status) {
		return 0, fmt.Errorf("非法卡密状态: %s", status)
	}
	ids := uniquePositiveIDs(itemIDs)
	if len(ids) == 0 {
		return 0, errors.New("请选择要操作的卡密")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+2)
	setClause := "status=?,updated_at=CURRENT_TIMESTAMP"
	args = append(args, status)
	if status == CardItemAvailable {
		setClause = "status=?,buyer_id=NULL,buyer_name=NULL,order_id=NULL,spec_name=NULL,spec_value=NULL,quantity=1,sold_at=NULL,updated_at=CURRENT_TIMESTAMP"
	}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, userID)
	res, err := c.DB.ExecContext(ctx,
		`UPDATE card_items SET `+setClause+` WHERE id IN (`+placeholders+`) AND user_id=?`, args...)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// DeleteItems 删除卡密条目（仅本人）。
func (c *Cards) DeleteItems(ctx context.Context, userID int64, itemIDs []int64) (int, error) {
	ids := uniquePositiveIDs(itemIDs)
	if len(ids) == 0 {
		return 0, errors.New("请选择要删除的卡密")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, userID)
	res, err := c.DB.ExecContext(ctx,
		`DELETE FROM card_items WHERE id IN (`+placeholders+`) AND user_id=?`, args...)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// SetItemContent 修改单条卡密内容（仅本人）。
func (c *Cards) SetItemContent(ctx context.Context, userID, itemID int64, content string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("卡密内容不能为空")
	}
	res, err := c.DB.ExecContext(ctx,
		`UPDATE card_items SET content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		content, itemID, userID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountItemsByCard 统计某卡种的条目数。
func (c *Cards) CountItemsByCard(ctx context.Context, cardID int64) (int, error) {
	var n int
	if err := c.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_items WHERE card_id=?`, cardID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
