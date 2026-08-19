-- +goose Up
-- +goose StatementBegin
INSERT INTO system_settings (`key`, value, description) VALUES ('ai_thinking_mode','disabled','AI 思考模式：enabled/disabled') ON CONFLICT (`key`) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM system_settings WHERE `key`='ai_thinking_mode';
-- +goose StatementEnd
