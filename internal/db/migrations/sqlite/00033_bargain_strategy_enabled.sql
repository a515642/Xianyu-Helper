-- +goose Up
-- +goose StatementBegin
ALTER TABLE ai_profiles ADD COLUMN bargain_strategy_enabled INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE ai_profiles DROP COLUMN bargain_strategy_enabled;
-- +goose StatementEnd
