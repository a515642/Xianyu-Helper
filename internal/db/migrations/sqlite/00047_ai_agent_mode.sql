-- +goose Up
-- +goose StatementBegin
-- AI 助手回复模式与工具配置：text 保持现有行为，tools 启用工具调用式回复。
ALTER TABLE ai_profiles ADD COLUMN reply_mode TEXT NOT NULL DEFAULT 'text';
ALTER TABLE ai_profiles ADD COLUMN max_tool_rounds INTEGER NOT NULL DEFAULT 8;
ALTER TABLE ai_profiles ADD COLUMN agent_prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_profiles ADD COLUMN notify_human_enabled INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose StatementBegin
-- AI 回合观测：每处理一条买家消息记录一轮，供提示词分析与灰度对比。
CREATE TABLE IF NOT EXISTS ai_turn_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ai_profile_id INTEGER NOT NULL DEFAULT 0,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    simulated INTEGER NOT NULL DEFAULT 0,
    session_id TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',
    prompt_hash TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    thinking_mode TEXT NOT NULL DEFAULT 'disabled',
    reply_mode TEXT NOT NULL DEFAULT 'text',
    outcome TEXT NOT NULL DEFAULT '',
    error_type TEXT NOT NULL DEFAULT '',
    reply_text TEXT NOT NULL DEFAULT '',
    reasoning TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ai_turn_logs_created ON ai_turn_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_ai_turn_logs_profile_created ON ai_turn_logs(ai_profile_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ai_turn_logs_conversation ON ai_turn_logs(cookie_id, chat_id, item_id, id);

CREATE TABLE IF NOT EXISTS ai_tool_calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_log_id INTEGER NOT NULL,
    round_index INTEGER NOT NULL DEFAULT 1,
    tool_name TEXT NOT NULL DEFAULT '',
    arguments TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',
    success INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ai_tool_calls_turn ON ai_tool_calls(turn_log_id);
CREATE INDEX IF NOT EXISTS idx_ai_tool_calls_name_created ON ai_tool_calls(tool_name, created_at);
-- +goose StatementEnd

-- +goose StatementBegin
-- 工具模式默认提示词，可在系统设置或单个 AI 助手中覆盖。
INSERT OR IGNORE INTO system_settings (key, value, description) VALUES
    ('ai_agent_prompt', '你是闲鱼卖家的自动回复助手，请根据商品信息与买家自然沟通。

商品信息：
- 标题：{item_title}
- 价格：{item_price} 元
- 描述：{item_description}

回复规则：
1. 需要回复买家时调用 send_reply，把内容拆成 1-5 条简短消息，像真人聊天一样自然。
2. 无需回复（例如买家只发来表情或无关内容）时调用 skip_reply，并说明原因。
3. 买家明确要求人工、涉及售后纠纷，或问题超出商品信息范围时，调用 notify_human 通知卖家。
4. 只有买家确实在议价时才考虑改价工具，不要主动降价。
5. 不要编造商品信息，不要承诺无法提供的服务。', '工具模式 AI 回复提示词，支持 {item_title}/{item_price}/{item_description} 占位符');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM system_settings WHERE key='ai_agent_prompt';
DROP TABLE IF EXISTS ai_tool_calls;
DROP TABLE IF EXISTS ai_turn_logs;
ALTER TABLE ai_profiles DROP COLUMN notify_human_enabled;
ALTER TABLE ai_profiles DROP COLUMN agent_prompt;
ALTER TABLE ai_profiles DROP COLUMN max_tool_rounds;
ALTER TABLE ai_profiles DROP COLUMN reply_mode;
-- +goose StatementEnd
