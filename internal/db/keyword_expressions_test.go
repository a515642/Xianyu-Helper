package db

import (
	"context"
	"reflect"
	"testing"
)

// TestKeywordExpressionsPersistence 验证表达式集合与匹配模式可持久化，并兼容旧单关键词。
func TestKeywordExpressionsPersistence(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "user1", "u1@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create user: %v, %v", ok, err)
	}
	user, _ := store.Users.GetByUsername(ctx, "user1")
	if err := store.Cookies.Save(ctx, "acc1", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}

	id, err := store.Keywords.AddRow(ctx, KeywordRow{
		CookieID:    "acc1",
		Reply:       "已收到订单",
		Type:        "text",
		Expressions: []string{" 订单\\d+ ", "发货", "订单\\d+"},
		MatchType:   "regexp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("AddRow 应返回 id")
	}

	rows, err := store.Keywords.AllRows(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].Keyword != "订单\\d+" || rows[0].MatchType != "regexp" {
		t.Fatalf("首表达式/匹配模式异常: %#v", rows[0])
	}
	if want := []string{"订单\\d+", "发货"}; !reflect.DeepEqual(rows[0].Expressions, want) {
		t.Fatalf("expressions = %#v want %#v", rows[0].Expressions, want)
	}

	loaded, err := store.Keywords.AllWithType(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].MatchType != "regexp" {
		t.Fatalf("AllWithType = %#v", loaded)
	}
	if want := []string{"订单\\d+", "发货"}; !reflect.DeepEqual(loaded[0].Expressions, want) {
		t.Fatalf("AllWithType expressions = %#v want %#v", loaded[0].Expressions, want)
	}

	// 历史 Add 只写 keyword，读取时回退为单表达式包含匹配。
	if _, err := store.Keywords.Add(ctx, "acc1", "发票", "可开发票", "", "text", ""); err != nil {
		t.Fatal(err)
	}
	rows, _ = store.Keywords.AllRows(ctx, "acc1")
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	legacy := rows[1]
	if legacy.MatchType != "contains" {
		t.Fatalf("历史规则应回退 contains: %#v", legacy)
	}
	if want := []string{"发票"}; !reflect.DeepEqual(legacy.Expressions, want) {
		t.Fatalf("历史规则 expressions = %#v want %#v", legacy.Expressions, want)
	}

	// 更新为包含模式并规整表达式列表。
	legacy.Expressions = []string{"发票", "收据", "发票", ""}
	legacy.MatchType = ""
	if err := store.Keywords.UpdateByID(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	rows, _ = store.Keywords.AllRows(ctx, "acc1")
	if rows[1].MatchType != "contains" || rows[1].Keyword != "发票" {
		t.Fatalf("更新后异常: %#v", rows[1])
	}
	if want := []string{"发票", "收据"}; !reflect.DeepEqual(rows[1].Expressions, want) {
		t.Fatalf("更新后 expressions = %#v want %#v", rows[1].Expressions, want)
	}
}
