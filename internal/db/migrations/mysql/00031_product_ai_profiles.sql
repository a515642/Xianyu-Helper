-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS ai_profiles (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(191) NOT NULL,
    name VARCHAR(191) NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 1,
    use_system_api TINYINT NOT NULL DEFAULT 1,
    api_key TEXT NOT NULL,
    base_url TEXT NOT NULL,
    model_name VARCHAR(191) NOT NULL DEFAULT '',
    thinking_mode VARCHAR(16) NOT NULL DEFAULT 'disabled',
    custom_prompts TEXT NOT NULL,
    trigger_mode VARCHAR(32) NOT NULL DEFAULT 'all_text',
    max_discount_percent INT NOT NULL DEFAULT 10,
    max_discount_amount INT NOT NULL DEFAULT 100,
    max_bargain_rounds INT NOT NULL DEFAULT 3,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_ai_profiles_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE KEY uq_ai_profiles_cookie_name (cookie_id, name),
    KEY idx_ai_profiles_cookie (cookie_id, enabled)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ai_profile_items (
    ai_profile_id BIGINT NOT NULL,
    cookie_id VARCHAR(191) NOT NULL,
    item_id VARCHAR(191) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (ai_profile_id, item_id),
    UNIQUE KEY uq_ai_profile_items_cookie_item (cookie_id, item_id),
    KEY idx_ai_profile_items_lookup (cookie_id, item_id),
    CONSTRAINT fk_ai_profile_items_profile FOREIGN KEY (ai_profile_id) REFERENCES ai_profiles(id) ON DELETE CASCADE,
    CONSTRAINT fk_ai_profile_items_cookie FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ai_forbidden_words (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    keyword VARCHAR(191) NOT NULL,
    replacement TEXT NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 1,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_ai_forbidden_words_keyword (keyword)
) ENGINE=InnoDB;

ALTER TABLE ai_conversations ADD COLUMN ai_profile_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_ai_conversations_profile_context
    ON ai_conversations(ai_profile_id, cookie_id, chat_id, item_id, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_ai_conversations_profile_context ON ai_conversations;
ALTER TABLE ai_conversations DROP COLUMN ai_profile_id;
DROP TABLE IF EXISTS ai_forbidden_words;
DROP TABLE IF EXISTS ai_profile_items;
DROP TABLE IF EXISTS ai_profiles;
-- +goose StatementEnd
