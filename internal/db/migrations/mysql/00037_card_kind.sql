-- +goose Up
-- +goose StatementBegin
-- 卡种（cards）不再承载交付类型与内容：类型与内容全部下移到卡密条目（card_items）。
ALTER TABLE card_items ADD COLUMN item_type VARCHAR(16) NOT NULL DEFAULT 'data';
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO card_items (card_id, user_id, item_type, content, status)
SELECT id, user_id, 'text', text_content, 'available'
FROM cards WHERE type = 'text' AND COALESCE(text_content, '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO card_items (card_id, user_id, item_type, content, status)
SELECT id, user_id, 'image', image_url, 'available'
FROM cards WHERE type = 'image' AND COALESCE(image_url, '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards
    DROP COLUMN type,
    DROP COLUMN api_config,
    DROP COLUMN text_content,
    DROP COLUMN image_url;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cards
    ADD COLUMN type VARCHAR(32) NULL,
    ADD COLUMN api_config TEXT NULL,
    ADD COLUMN text_content TEXT NULL,
    ADD COLUMN image_url TEXT NULL;
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
