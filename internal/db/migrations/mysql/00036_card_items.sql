-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS card_items (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    card_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL DEFAULT 1,
    content TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'available',
    buyer_id VARCHAR(128) NULL,
    buyer_name VARCHAR(255) NULL,
    order_id VARCHAR(128) NULL,
    spec_name VARCHAR(255) NULL,
    spec_value VARCHAR(255) NULL,
    quantity INTEGER DEFAULT 1,
    sold_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_card_items_card FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE,
    CONSTRAINT fk_card_items_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_card_items_group_status ON card_items(card_id, status, id);
CREATE INDEX idx_card_items_user_status ON card_items(user_id, status, id);
CREATE INDEX idx_card_items_order ON card_items(order_id);
CREATE INDEX idx_card_items_buyer ON card_items(buyer_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- 用 MySQL 8 递归 CTE 按换行切分历史 data_content，逐行拆入 card_items。
-- MySQL 要求 CTE 紧跟在 INSERT INTO ... 的 SELECT 之前。
INSERT INTO card_items (card_id, user_id, content, status)
WITH RECURSIVE seq AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM seq WHERE n < 1000),
numbered AS (
    SELECT c.id AS card_id, c.user_id,
           TRIM(SUBSTRING_INDEX(SUBSTRING_INDEX(c.data_content, '\n', seq.n), '\n', -1)) AS line
    FROM cards c JOIN seq ON seq.n <= 1 + (LENGTH(c.data_content) - LENGTH(REPLACE(c.data_content, '\n', '')))
    WHERE c.type = 'data' AND c.data_content IS NOT NULL AND c.data_content <> ''
)
SELECT card_id, user_id, REPLACE(line, '\r', ''), 'available'
FROM numbered WHERE REPLACE(line, '\r', '') <> '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE cards DROP COLUMN data_content;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cards ADD COLUMN data_content LONGTEXT;
DROP TABLE IF EXISTS card_items;
-- +goose StatementEnd

