-- +goose Up
-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'card';
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE cards SET kind = 'text' WHERE EXISTS (SELECT 1 FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'text');
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE cards SET kind = 'image' WHERE EXISTS (SELECT 1 FROM card_items ci WHERE ci.card_id = cards.id AND ci.item_type = 'image');
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE card_items DROP COLUMN item_type;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE card_items ADD COLUMN item_type VARCHAR(16) NOT NULL DEFAULT 'data';
UPDATE card_items SET item_type = (SELECT c.kind FROM cards c WHERE c.id = card_items.card_id);
ALTER TABLE cards DROP COLUMN kind;
-- +goose StatementEnd
