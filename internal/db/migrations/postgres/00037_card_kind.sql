-- +goose Up
-- +goose StatementBegin
-- 卡种（cards）不再承载交付类型与内容：类型与内容全部下移到卡密条目（card_items）。
ALTER TABLE card_items ADD COLUMN item_type TEXT NOT NULL DEFAULT 'data';
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO card_items (card_id, user_id, item_type, content, status)
SELECT id, user_id, 'text', text_content, 'available'
FROM cards WHERE type = 'text' AND coalesce(text_content, '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO card_items (card_id, user_id, item_type, content, status)
SELECT id, user_id, 'image', image_url, 'available'
FROM cards WHERE type = 'image' AND coalesce(image_url, '') <> '';
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
    ADD COLUMN type TEXT,
    ADD COLUMN api_config TEXT,
    ADD COLUMN text_content TEXT,
    ADD COLUMN image_url TEXT;
ALTER TABLE card_items DROP COLUMN item_type;
-- +goose StatementEnd
