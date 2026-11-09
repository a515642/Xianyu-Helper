-- +goose Up
-- +goose StatementBegin
-- AI 助手回复模式与工具配置：text 保持现有行为，tools 启用工具调用式回复。
ALTER TABLE ai_profiles ADD COLUMN reply_mode VARCHAR(16) NOT NULL DEFAULT 'text';
ALTER TABLE ai_profiles ADD COLUMN max_tool_rounds INT NOT NULL DEFAULT 8;
ALTER TABLE ai_profiles ADD COLUMN agent_prompt MEDIUMTEXT NULL;
ALTER TABLE ai_profiles ADD COLUMN notify_human_enabled TINYINT(1) NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose StatementBegin
-- AI 回合观测：每处理一条买家消息记录一轮，供提示词分析与灰度对比。
CREATE TABLE IF NOT EXISTS ai_turn_logs (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    ai_profile_id BIGINT NOT NULL DEFAULT 0,
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL DEFAULT '',
    item_id VARCHAR(191) NOT NULL DEFAULT '',
    user_id VARCHAR(191) NOT NULL DEFAULT '',
    simulated TINYINT(1) NOT NULL DEFAULT 0,
    session_id VARCHAR(64) NOT NULL DEFAULT '',
    system_prompt MEDIUMTEXT NOT NULL,
    prompt_hash VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(191) NOT NULL DEFAULT '',
    thinking_mode VARCHAR(16) NOT NULL DEFAULT 'disabled',
    reply_mode VARCHAR(16) NOT NULL DEFAULT 'text',
    outcome VARCHAR(16) NOT NULL DEFAULT '',
    error_type VARCHAR(64) NOT NULL DEFAULT '',
    reply_text MEDIUMTEXT NOT NULL,
    reasoning MEDIUMTEXT NOT NULL,
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_ai_turn_logs_created (created_at),
    KEY idx_ai_turn_logs_profile_created (ai_profile_id, created_at),
    KEY idx_ai_turn_logs_conversation (cookie_id, chat_id, item_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS ai_tool_calls (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    turn_log_id BIGINT NOT NULL,
    round_index INT NOT NULL DEFAULT 1,
    tool_name VARCHAR(64) NOT NULL DEFAULT '',
    arguments MEDIUMTEXT NOT NULL,
    result MEDIUMTEXT NOT NULL,
    success TINYINT(1) NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    KEY idx_ai_tool_calls_turn (turn_log_id),
    KEY idx_ai_tool_calls_name_created (tool_name, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose StatementEnd

-- +goose StatementBegin
-- 工具模式默认提示词，可在系统设置或单个 AI 助手中覆盖。
INSERT IGNORE INTO system_settings (`key`, value, description) VALUES
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
DELETE FROM system_settings WHERE `key`='ai_agent_prompt';
DROP TABLE IF EXISTS ai_tool_calls;
DROP TABLE IF EXISTS ai_turn_logs;
ALTER TABLE ai_profiles DROP COLUMN notify_human_enabled;
ALTER TABLE ai_profiles DROP COLUMN agent_prompt;
ALTER TABLE ai_profiles DROP COLUMN max_tool_rounds;
ALTER TABLE ai_profiles DROP COLUMN reply_mode;
-- +goose StatementEnd
