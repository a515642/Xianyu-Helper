-- +goose Up
-- +goose StatementBegin
DROP PROCEDURE IF EXISTS add_ai_profile_thinking_mode;
CREATE PROCEDURE add_ai_profile_thinking_mode()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='ai_profiles' AND COLUMN_NAME='thinking_mode'
    ) THEN
        ALTER TABLE ai_profiles ADD COLUMN thinking_mode VARCHAR(16) NOT NULL DEFAULT 'disabled';
    END IF;
END;
CALL add_ai_profile_thinking_mode();
DROP PROCEDURE IF EXISTS add_ai_profile_thinking_mode;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE ai_profiles DROP COLUMN thinking_mode;
-- +goose StatementEnd
