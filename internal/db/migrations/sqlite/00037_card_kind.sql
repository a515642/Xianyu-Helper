-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
-- 卡种（cards）不再承载交付类型与内容：类型与内容全部下移到卡密条目（card_items）。
-- 需要重建 cards 表，因此临时关闭外键检查（SQLite 官方重建表流程）。
PRAGMA foreign_keys=OFF;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE card_items ADD COLUMN item_type TEXT NOT NULL DEFAULT 'data';
-- +goose StatementEnd

-- +goose StatementBegin
-- 历史 text 卡种的内容下移为一条可重复发送的文本卡密。
INSERT INTO card_items (card_id, user_id, item_type, content, status)
SELECT id, user_id, 'text', text_content, 'available'
FROM cards WHERE type = 'text' AND COALESCE(text_content, '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
-- 历史 image 卡种的内容下移为一条可重复发送的图片卡密。
INSERT INTO card_items (card_id, user_id, item_type, content, status)
SELECT id, user_id, 'image', image_url, 'available'
FROM cards WHERE type = 'image' AND COALESCE(image_url, '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE cards_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT TRUE,
    delay_seconds INTEGER DEFAULT 0,
    is_multi_spec BOOLEAN DEFAULT FALSE,
    spec_name TEXT,
    spec_value TEXT,
    user_id INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id)
);
INSERT INTO cards_new (id, name, description, enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id, created_at, updated_at)
SELECT id, name, description, enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id, created_at, updated_at FROM cards;
DROP TABLE cards;
ALTER TABLE cards_new RENAME TO cards;
CREATE INDEX IF NOT EXISTS idx_cards_user_id ON cards(user_id);
-- +goose StatementEnd

-- +goose StatementBegin
PRAGMA foreign_keys=ON;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA foreign_keys=OFF;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN type TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN api_config TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN text_content TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN image_url TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE cards SET type = 'data' WHERE EXISTS (SELECT 1 FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'data');
UPDATE cards SET type = 'text', text_content = (SELECT ci.content FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'text' ORDER BY ci.id LIMIT 1) WHERE EXISTS (SELECT 1 FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'text');
UPDATE cards SET type = 'image', image_url = (SELECT ci.content FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'image' ORDER BY ci.id LIMIT 1) WHERE EXISTS (SELECT 1 FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'image');
UPDATE cards SET type = 'data' WHERE type IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE card_items DROP COLUMN item_type;
-- +goose StatementEnd

-- +goose StatementBegin
PRAGMA foreign_keys=ON;
-- +goose StatementEnd
