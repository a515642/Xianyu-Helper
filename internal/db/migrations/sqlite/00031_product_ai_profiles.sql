-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS ai_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    use_system_api INTEGER NOT NULL DEFAULT 1,
    api_key TEXT NOT NULL DEFAULT '',
    base_url TEXT NOT NULL DEFAULT '',
    model_name TEXT NOT NULL DEFAULT '',
    thinking_mode TEXT NOT NULL DEFAULT 'disabled',
    custom_prompts TEXT NOT NULL DEFAULT '',
    trigger_mode TEXT NOT NULL DEFAULT 'all_text',
    max_discount_percent INTEGER NOT NULL DEFAULT 10,
    max_discount_amount INTEGER NOT NULL DEFAULT 100,
    max_bargain_rounds INTEGER NOT NULL DEFAULT 3,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE(cookie_id, name)
);
CREATE INDEX IF NOT EXISTS idx_ai_profiles_cookie ON ai_profiles(cookie_id, enabled);

CREATE TABLE IF NOT EXISTS ai_profile_items (
    ai_profile_id INTEGER NOT NULL,
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (ai_profile_id, item_id),
    UNIQUE(cookie_id, item_id),
    FOREIGN KEY (ai_profile_id) REFERENCES ai_profiles(id) ON DELETE CASCADE,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ai_profile_items_lookup ON ai_profile_items(cookie_id, item_id);

CREATE TABLE IF NOT EXISTS ai_forbidden_words (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword TEXT NOT NULL UNIQUE,
    replacement TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE ai_conversations ADD COLUMN ai_profile_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_ai_conversations_profile_context
    ON ai_conversations(ai_profile_id, cookie_id, chat_id, item_id, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_ai_conversations_profile_context;
ALTER TABLE ai_conversations DROP COLUMN ai_profile_id;
DROP TABLE IF EXISTS ai_forbidden_words;
DROP TABLE IF EXISTS ai_profile_items;
DROP TABLE IF EXISTS ai_profiles;
-- +goose StatementEnd
