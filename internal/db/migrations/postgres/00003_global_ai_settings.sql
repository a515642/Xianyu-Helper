-- +goose Up
INSERT INTO system_settings ("key", value, description) VALUES
    ('ai_api_url', '', 'AI OpenAI兼容API地址（只填域名，系统自动补全 /v1/chat/completions）'),
    ('ai_api_key', '', 'AI API Key'),
    ('ai_model', '', 'AI模型名称（必填）')
ON CONFLICT ("key") DO NOTHING;

-- +goose Down
DELETE FROM system_settings WHERE "key" IN ('ai_api_url', 'ai_api_key', 'ai_model');
