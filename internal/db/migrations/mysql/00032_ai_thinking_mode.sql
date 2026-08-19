-- +goose Up
-- +goose StatementBegin
INSERT INTO system_settings (`key`, value, description) VALUES ('ai_thinking_mode','disabled','AI thinking mode: enabled/disabled') ON DUPLICATE KEY UPDATE `key`=`key`;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM system_settings WHERE `key`='ai_thinking_mode';
-- +goose StatementEnd
