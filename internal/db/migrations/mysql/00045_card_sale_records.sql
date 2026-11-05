-- +goose Up
-- +goose StatementBegin
-- 卡密售出台账：预留时写入 reserved，确认发送成功后改为 sold。
-- 卡种和卡密条目删除后台账仍保留快照，便于按订单追溯已发出的卡密。
CREATE TABLE IF NOT EXISTS card_sale_records (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    card_id BIGINT NOT NULL,
    card_item_id BIGINT NOT NULL DEFAULT 0,
    card_name VARCHAR(255) NOT NULL DEFAULT '',
    card_kind VARCHAR(32) NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'reserved',
    run_id BIGINT NOT NULL DEFAULT 0,
    cookie_id VARCHAR(128) NULL,
    order_id VARCHAR(128) NULL,
    item_id VARCHAR(128) NULL,
    buyer_id VARCHAR(128) NULL,
    created_at BIGINT NOT NULL,
    sold_at BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_card_sale_user_created ON card_sale_records(user_id, created_at, id);
CREATE INDEX idx_card_sale_card ON card_sale_records(card_id, id);
CREATE INDEX idx_card_sale_order ON card_sale_records(cookie_id, order_id);
CREATE INDEX idx_card_sale_run ON card_sale_records(run_id, status);
CREATE INDEX idx_card_sale_item ON card_sale_records(card_item_id, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS card_sale_records;
-- +goose StatementEnd
