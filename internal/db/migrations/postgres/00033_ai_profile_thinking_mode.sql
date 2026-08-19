-- +goose Up
-- +goose StatementBegin
ALTER TABLE ai_profiles ADD COLUMN IF NOT EXISTS thinking_mode TEXT NOT NULL DEFAULT 'disabled';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE ai_profiles DROP COLUMN IF EXISTS thinking_mode;
-- +goose StatementEnd
