-- +goose Up
-- +goose StatementBegin
-- 系统不再提供默认 AI 模型名。清掉 00003 写入的默认值，让设置页保持为空、
-- 由用户显式选择（00003 已执行过的库不会重跑）。
UPDATE system_settings SET value='' WHERE `key`='ai_model' AND value='qwen-plus';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 不回填默认模型：回滚只表示"不再清空"，空值本身是合法状态。
SELECT 1;
-- +goose StatementEnd
