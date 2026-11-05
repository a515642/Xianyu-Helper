-- +goose Up
-- +goose StatementBegin
-- 卡密售出台账：预留时写入 reserved，确认发送成功后改为 sold。
-- 卡种和卡密条目删除后台账仍保留快照，便于按订单追溯已发出的卡密。
CREATE TABLE IF NOT EXISTS card_sale_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    card_id INTEGER NOT NULL,
    card_item_id INTEGER NOT NULL DEFAULT 0,
    card_name TEXT NOT NULL DEFAULT '',
    card_kind TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'reserved',
    run_id INTEGER NOT NULL DEFAULT 0,
    cookie_id TEXT,
    order_id TEXT,
    item_id TEXT,
    buyer_id TEXT,
    created_at INTEGER NOT NULL,
    sold_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_card_sale_user_created ON card_sale_records(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_card_sale_card ON card_sale_records(card_id, id);
CREATE INDEX IF NOT EXISTS idx_card_sale_order ON card_sale_records(cookie_id, order_id);
CREATE INDEX IF NOT EXISTS idx_card_sale_run ON card_sale_records(run_id, status);
CREATE INDEX IF NOT EXISTS idx_card_sale_item ON card_sale_records(card_item_id, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS card_sale_records;
-- +goose StatementEnd
