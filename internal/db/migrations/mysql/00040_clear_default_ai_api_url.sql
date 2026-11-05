-- +goose Up
-- +goose StatementBegin
-- 系统不再提供默认 AI 地址。清掉 00003 写入的默认值，避免设置页继续预填
-- 用户从未选择的地址（该迁移已执行过的库不会重跑 00003）。
UPDATE system_settings SET value='' WHERE `key`='ai_api_url'
  AND value='https://dashscope.aliyuncs.com/compatible-mode/v1';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 不回填默认地址：回滚只表示"不再清空"，空值本身是合法状态。
SELECT 1;
-- +goose StatementEnd
