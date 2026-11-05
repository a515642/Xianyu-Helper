-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS card_items (
    id BIGSERIAL PRIMARY KEY,
    card_id BIGINT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL DEFAULT 1 REFERENCES users(id),
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
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_card_items_group_status ON card_items(card_id, status, id);
CREATE INDEX IF NOT EXISTS idx_card_items_user_status ON card_items(user_id, status, id DESC);
CREATE INDEX IF NOT EXISTS idx_card_items_order ON card_items(order_id);
CREATE INDEX IF NOT EXISTS idx_card_items_buyer ON card_items(buyer_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- 按换行切分历史 data_content，逐行拆入 card_items。
INSERT INTO card_items (card_id, user_id, content, status)
SELECT c.id, c.user_id, btrim(replace(line, E'\r', '')), 'available'
FROM cards c
CROSS JOIN LATERAL unnest(string_to_array(c.data_content, E'\n')) AS line
WHERE c.type = 'data' AND coalesce(c.data_content, '') <> ''
  AND btrim(replace(line, E'\r', '')) <> '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards DROP COLUMN data_content;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN data_content TEXT;
DROP TABLE IF EXISTS card_items;
-- +goose StatementEnd
