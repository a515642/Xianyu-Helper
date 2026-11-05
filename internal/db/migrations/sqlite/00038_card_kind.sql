-- +goose Up
-- +goose StatementBegin
-- 卡密类型上移为卡种属性（kind）：card=批量卡密（每条一次性消耗）、
-- text=固定文本、image=固定图片（后两者可重复发送）。卡密条目只保留文本内容，
-- 图片卡种存的也是图片 URL。历史数据按已有卡密类型回填。
ALTER TABLE cards ADD COLUMN kind TEXT NOT NULL DEFAULT 'card';
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
ALTER TABLE card_items ADD COLUMN item_type TEXT NOT NULL DEFAULT 'data';
UPDATE card_items SET item_type = (SELECT c.kind FROM cards c WHERE c.id = card_items.card_id);
ALTER TABLE cards DROP COLUMN kind;
-- +goose StatementEnd
