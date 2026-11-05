-- +goose Up
-- +goose StatementBegin
-- 卡密条目表：每张卡密一行，维护可用/已用/作废状态与售出归属，替代原先"售出即从
-- data_content 删除"的库存模型。
CREATE TABLE IF NOT EXISTS card_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    card_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 1,
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'available',
    buyer_id TEXT,
    buyer_name TEXT,
    order_id TEXT,
    spec_name TEXT,
    spec_value TEXT,
    quantity INTEGER DEFAULT 1,
    sold_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_card_items_group_status ON card_items(card_id, status, id);
CREATE INDEX IF NOT EXISTS idx_card_items_user_status ON card_items(user_id, status, id DESC);
CREATE INDEX IF NOT EXISTS idx_card_items_order ON card_items(order_id);
CREATE INDEX IF NOT EXISTS idx_card_items_buyer ON card_items(buyer_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- 把历史 data_content 按行拆入 card_items（每行一张卡密，均为可用）。
-- 用递归 CTE 逐行切分，保留原始顺序（rowid 决定了历史发货顺序）。
WITH RECURSIVE split(item_id, rest, line, seq) AS (
    SELECT id,
           CASE WHEN instr(COALESCE(data_content,''), char(10)) > 0
                THEN substr(data_content, instr(data_content, char(10)) + 1)
                ELSE '' END,
           CASE WHEN instr(COALESCE(data_content,''), char(10)) > 0
                THEN substr(data_content, 1, instr(data_content, char(10)) - 1)
                ELSE COALESCE(data_content,'') END,
           1
    FROM cards
    WHERE type = 'data' AND data_content IS NOT NULL AND data_content <> ''
    UNION ALL
    SELECT item_id,
           CASE WHEN instr(rest, char(10)) > 0 THEN substr(rest, instr(rest, char(10)) + 1) ELSE '' END,
           CASE WHEN instr(rest, char(10)) > 0 THEN substr(rest, 1, instr(rest, char(10)) - 1) ELSE rest END,
           seq + 1
    FROM split
    WHERE rest <> ''
)
INSERT INTO card_items (card_id, user_id, content, status)
SELECT s.item_id, c.user_id, replace(trim(s.line), char(13), ''), 'available'
FROM split s JOIN cards c ON c.id = s.item_id
WHERE replace(trim(s.line), char(13), '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
-- 历史库存已经拆入 card_items，data_content 不再被任何代码读写。
ALTER TABLE cards DROP COLUMN data_content;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN data_content TEXT;
DROP TABLE IF EXISTS card_items;
-- +goose StatementEnd
