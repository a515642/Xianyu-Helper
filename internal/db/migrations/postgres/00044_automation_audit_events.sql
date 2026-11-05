-- +goose Up
CREATE TABLE IF NOT EXISTS automation_audit_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    order_id VARCHAR(255) NOT NULL DEFAULT '',
    item_id VARCHAR(255) NOT NULL DEFAULT '',
    rule_id BIGINT NOT NULL DEFAULT 0,
    run_id BIGINT NOT NULL DEFAULT 0,
    trigger_type VARCHAR(64) NOT NULL DEFAULT '',
    source VARCHAR(64) NOT NULL DEFAULT '',
    event_type VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    action_type VARCHAR(64) NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_automation_audit_user_time ON automation_audit_events(user_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_automation_audit_run ON automation_audit_events(run_id);
CREATE INDEX IF NOT EXISTS idx_automation_audit_order ON automation_audit_events(cookie_id, order_id);

-- +goose Down
DROP TABLE IF EXISTS automation_audit_events;
