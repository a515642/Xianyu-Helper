package db

import (
	"context"
	"errors"
	"testing"
)

// newItemGreetingTestStore 创建带账号的商品首次回复测试存储。
func newItemGreetingTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	store, cleanup := newTestDB(t)
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "greeting-admin", "greeting@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, err := store.Users.GetByUsername(ctx, "greeting-admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if err := store.Cookies.Save(ctx, "cid", "unb=1; cookie2=2;", user.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	return store, cleanup
}

// TestItemGreetings_CreateListFindAndConflict 验证规则与商品集合的读写及同账号商品冲突。
func TestItemGreetings_CreateListFindAndConflict(t *testing.T) {
	store, cleanup := newItemGreetingTestStore(t)
	defer cleanup()
	ctx := context.Background()

	id, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{
		CookieID: "cid", Name: "默认问候", Enabled: true,
		ReplyContent: "你好 {send_user_name}", ItemIDs: []string{"item-1", "item-2"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rules, err := store.ItemGreetings.ListByAccount(ctx, "cid")
	if err != nil || len(rules) != 1 {
		t.Fatalf("list: rules=%+v err=%v", rules, err)
	}
	if rules[0].ID != id || len(rules[0].ItemIDs) != 2 || !rules[0].Enabled {
		t.Fatalf("list 结果不完整: %+v", rules[0])
	}

	rule, err := store.ItemGreetings.FindEnabledForItem(ctx, "cid", "item-2")
	if err != nil || rule.ID != id || rule.ReplyContent != "你好 {send_user_name}" {
		t.Fatalf("find: rule=%+v err=%v", rule, err)
	}
	if _, err := store.ItemGreetings.FindEnabledForItem(ctx, "cid", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未覆盖商品应返回 ErrNotFound: %v", err)
	}

	_, err = store.ItemGreetings.Create(ctx, ItemGreetingRule{
		CookieID: "cid", Enabled: true, ReplyContent: "冲突", ItemIDs: []string{"item-2", "item-3"},
	})
	var conflict *ItemGreetingConflictError
	if !errors.As(err, &conflict) || len(conflict.ItemIDs) != 1 || conflict.ItemIDs[0] != "item-2" {
		t.Fatalf("应返回商品冲突: %v", err)
	}
}

// TestItemGreetings_UpdateAndDelete 验证更新会替换商品集合并允许复用本规则商品。
func TestItemGreetings_UpdateAndDelete(t *testing.T) {
	store, cleanup := newItemGreetingTestStore(t)
	defer cleanup()
	ctx := context.Background()

	id, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{
		CookieID: "cid", Enabled: true, ReplyContent: "旧", ItemIDs: []string{"item-1", "item-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ItemGreetings.Update(ctx, ItemGreetingRule{
		ID: id, CookieID: "cid", Name: "新", Enabled: false, ReplyContent: "新内容", ItemIDs: []string{"item-1", "item-3"},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := store.ItemGreetings.Get(ctx, id)
	if err != nil || updated.Enabled || updated.ReplyContent != "新内容" || len(updated.ItemIDs) != 2 {
		t.Fatalf("更新结果异常: %+v err=%v", updated, err)
	}
	if _, err := store.ItemGreetings.FindEnabledForItem(ctx, "cid", "item-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("停用规则不应命中: %v", err)
	}

	if err := store.ItemGreetings.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.ItemGreetings.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应不存在: %v", err)
	}
	// 删除规则后商品可被新规则复用。
	if _, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{
		CookieID: "cid", Enabled: true, ReplyContent: "复用", ItemIDs: []string{"item-3"},
	}); err != nil {
		t.Fatalf("删除后复用商品失败: %v", err)
	}
}

// TestItemGreetings_RecordStateMachine 验证按会话去重、失败可重试、成功不再重发。
func TestItemGreetings_RecordStateMachine(t *testing.T) {
	store, cleanup := newItemGreetingTestStore(t)
	defer cleanup()
	ctx := context.Background()
	g := store.ItemGreetings

	record, claimed, err := g.ClaimRecord(ctx, "cid", "chat-1", true, false)
	if err != nil || !claimed || !record.ImageSent {
		t.Fatalf("首次领取失败: record=%+v claimed=%v err=%v", record, claimed, err)
	}
	if _, claimed, _ = g.ClaimRecord(ctx, "cid", "chat-1", true, false); claimed {
		t.Fatal("pending 记录不应被重复领取")
	}
	if err := g.MarkRecordFailed(ctx, "cid", "chat-1", "network"); err != nil {
		t.Fatal(err)
	}
	record, claimed, err = g.ClaimRecord(ctx, "cid", "chat-1", true, false)
	if err != nil || !claimed {
		t.Fatalf("失败记录应允许重试: record=%+v claimed=%v err=%v", record, claimed, err)
	}
	if err := g.MarkPartSent(ctx, "cid", "chat-1", "text"); err != nil {
		t.Fatal(err)
	}
	if err := g.MarkRecordSent(ctx, "cid", "chat-1"); err != nil {
		t.Fatal(err)
	}
	if _, claimed, _ = g.ClaimRecord(ctx, "cid", "chat-1", true, false); claimed {
		t.Fatal("成功记录不应再次领取")
	}
	triggered, err := g.HasRecord(ctx, "cid", "chat-1")
	if err != nil || !triggered {
		t.Fatalf("应存在投递记录: triggered=%v err=%v", triggered, err)
	}
	if err := g.ClearRecords(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	if triggered, _ := g.HasRecord(ctx, "cid", "chat-1"); triggered {
		t.Fatal("清空后不应存在记录")
	}
}
