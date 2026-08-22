-- +goose Up
-- +goose StatementBegin
-- 商品库存型触发器必须绑定具体商品；保留历史账号级规则用于审计，但停止执行。
UPDATE automation_rules SET enabled=0, updated_at=CURRENT_TIMESTAMP
 WHERE trigger_type IN ('order_paid','buyer_reviewed') AND item_id='';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 不自动恢复历史规则，避免回滚后意外重新触发账号级发货/赠品。
-- +goose StatementEnd
