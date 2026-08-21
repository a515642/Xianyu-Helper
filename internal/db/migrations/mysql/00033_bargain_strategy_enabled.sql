-- +goose Up
-- +goose StatementBegin
DROP PROCEDURE IF EXISTS add_bargain_strategy_enabled;
CREATE PROCEDURE add_bargain_strategy_enabled()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='ai_profiles' AND COLUMN_NAME='bargain_strategy_enabled'
    ) THEN
        ALTER TABLE ai_profiles ADD COLUMN bargain_strategy_enabled TINYINT NOT NULL DEFAULT 0;
    END IF;
END;
CALL add_bargain_strategy_enabled();
DROP PROCEDURE IF EXISTS add_bargain_strategy_enabled;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE ai_profiles DROP COLUMN bargain_strategy_enabled;
-- +goose StatementEnd
