package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// addKeywordRule 写入一条带表达式集合的关键词规则。
func addKeywordRule(t *testing.T, s *db.Store, expressions []string, matchType, reply string) {
	t.Helper()
	if _, err := s.Keywords.AddRow(context.Background(), db.KeywordRow{
		CookieID: "cid", Reply: reply, Type: "text",
		Expressions: expressions, MatchType: matchType,
	}); err != nil {
		t.Fatalf("AddRow: %v", err)
	}
}

// TestKeywordReply_Regexp 正则模式命中，且默认不区分大小写。
func TestKeywordReply_Regexp(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	addKeywordRule(t, s, []string{`order\s*\d+`}, "regexp", "收到订单号")

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(context.Background(), chatMsg("ORDER 12345 什么时候发", "item1", "chat1"))
	if res == nil || res.Text != "收到订单号" {
		t.Fatalf("应命中正则关键词，got %+v", res)
	}
}

// TestKeywordReply_MultipleExpressions 多表达式任意一个命中即可回复。
func TestKeywordReply_MultipleExpressions(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	addKeywordRule(t, s, []string{"发票", "收据"}, "contains", "可以开票")

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(context.Background(), chatMsg("能开收据吗", "item1", "chat1"))
	if res == nil || res.Text != "可以开票" {
		t.Fatalf("应命中第二个表达式，got %+v", res)
	}
}

// TestKeywordReply_InvalidRegexpSkipped 非法正则只跳过自身，不影响同规则其他表达式。
func TestKeywordReply_InvalidRegexpSkipped(t *testing.T) {
	s, cleanup := newReplyStore(t)
	defer cleanup()
	addKeywordRule(t, s, []string{"[", "退款"}, "regexp", "可以退款")

	r := NewReplyService("cid", s, nil, nil, nil, nil)
	res := r.resolve(context.Background(), chatMsg("我要退款", "item1", "chat1"))
	if res == nil || res.Text != "可以退款" {
		t.Fatalf("非法正则后仍应命中，got %+v", res)
	}
}
