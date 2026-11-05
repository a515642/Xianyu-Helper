-- +goose Up
-- +goose StatementBegin
-- 小刀（砍价成团）自动免拼开关。orders.is_bargain 已存在于初始 schema，这里不重复添加。
ALTER TABLE account_task_settings ADD COLUMN auto_freeshipping_enabled TINYINT(1) NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
-- 求小红花：订单标记 + 账号开关 + 上次扫描时间。
ALTER TABLE orders ADD COLUMN is_red_flower TINYINT(1) NOT NULL DEFAULT 0;
ALTER TABLE account_task_settings ADD COLUMN auto_red_flower_enabled TINYINT(1) NOT NULL DEFAULT 0;
ALTER TABLE account_task_settings ADD COLUMN last_red_flower_scan_at BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE account_task_settings DROP COLUMN last_red_flower_scan_at;
ALTER TABLE account_task_settings DROP COLUMN auto_red_flower_enabled;
ALTER TABLE orders DROP COLUMN is_red_flower;
ALTER TABLE account_task_settings DROP COLUMN auto_freeshipping_enabled;
-- +goose StatementEnd
