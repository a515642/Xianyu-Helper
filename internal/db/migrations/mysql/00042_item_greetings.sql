-- +goose Up
-- +goose StatementBegin
-- 商品首次回复：规则、规则覆盖的商品，以及按会话去重的投递记录。
CREATE TABLE IF NOT EXISTS item_greeting_rules (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    reply_content TEXT NOT NULL,
    reply_image_url VARCHAR(2048) NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX idx_item_greeting_rules_cookie ON item_greeting_rules(cookie_id);

CREATE TABLE IF NOT EXISTS item_greeting_rule_items (
    rule_id BIGINT NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(255) NOT NULL,
    PRIMARY KEY (rule_id, item_id),
    UNIQUE KEY uk_item_greeting_cookie_item (cookie_id, item_id),
    FOREIGN KEY (rule_id) REFERENCES item_greeting_rules(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS item_greeting_records (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    cookie_id VARCHAR(255) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'sent',
    text_sent TINYINT(1) NOT NULL DEFAULT 1,
    image_sent TINYINT(1) NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL,
    lease_expires_at BIGINT NOT NULL DEFAULT 0,
    replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_item_greeting_record_session (cookie_id, chat_id),
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS item_greeting_records;
DROP TABLE IF EXISTS item_greeting_rule_items;
DROP TABLE IF EXISTS item_greeting_rules;
-- +goose StatementEnd
