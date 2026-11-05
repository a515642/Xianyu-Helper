-- +goose Up
CREATE TABLE IF NOT EXISTS automation_audit_events (
    id BIGINT NOT NULL AUTO_INCREMENT,
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
    summary TEXT,
    reason TEXT,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_automation_audit_user_time (user_id, created_at, id),
    KEY idx_automation_audit_run (run_id),
    KEY idx_automation_audit_order (cookie_id, order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS automation_audit_events;
