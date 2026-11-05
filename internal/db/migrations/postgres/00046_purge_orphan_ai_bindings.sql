-- +goose Up
-- +goose StatementBegin
-- 商品逻辑删除后 AI 助手绑定此前未清理，留下幽灵商品；这里清理历史残留。
DELETE FROM ai_profile_items
 WHERE EXISTS (
   SELECT 1 FROM item_info i
    WHERE i.cookie_id=ai_profile_items.cookie_id
      AND i.item_id=ai_profile_items.item_id
      AND i.deleted_at IS NOT NULL
 );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 数据修复不可逆：绑定一旦清理不再恢复。
SELECT 1;
-- +goose StatementEnd
