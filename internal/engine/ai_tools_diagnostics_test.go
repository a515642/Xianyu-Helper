package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
)

type diagnosticPriceAdjuster struct {
	calls  int
	cookie string
	order  string
	cents  int64
}

func (d *diagnosticPriceAdjuster) AdjustOrderPrice(_ context.Context, cookies, orderID string, cents int64) (*mtop.AdjustPriceResult, error) {
	d.calls++
	d.cookie, d.order, d.cents = cookies, orderID, cents
	return &mtop.AdjustPriceResult{Success: true}, nil
}

func TestPriceToolExecutor_EmitsSafeDiagnosticPath(t *testing.T) {
	store, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,cookie_id,item_id,buyer_id,order_status,chat_id)
		VALUES ('order-tool-1','cid','item1','buyer1','processing','chat1')`); err != nil {
		t.Fatal(err)
	}
	profileID := enableTestAI(t, store, "http://127.0.0.1", "tool-diagnostics")
	profile, err := store.AIProfiles.Get(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	replier := NewAIReplier("cid", store, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	adjuster := &diagnosticPriceAdjuster{}
	replier.SetOrderPriceAdjuster(adjuster)
	executor := &priceToolExecutor{
		replier: replier, profile: profile, itemPrice: 100,
		orderClient: adjuster, cookies: "sensitive-cookie", chatID: "chat1", buyerID: "buyer1",
	}
	result, err := executor.execute(ctx, `{"order_id":"order-tool-1","target_price_cents":9500}`)
	if err != nil || !strings.Contains(result, "95.00") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if adjuster.calls != 1 || adjuster.cookie != "sensitive-cookie" || adjuster.order != "order-tool-1" || adjuster.cents != 9500 {
		t.Fatalf("adjuster call=%+v", adjuster)
	}
	for _, marker := range []string{
		"AI诊断：开始执行改价工具",
		"AI诊断：开始调用 MTOP 改价接口",
		"AI诊断：MTOP 改价接口成功",
		"order_id=order-tool-1",
		"target_price_cents=9500",
	} {
		if !strings.Contains(logs.String(), marker) {
			t.Fatalf("diagnostic log missing %q:\n%s", marker, logs.String())
		}
	}
	if strings.Contains(logs.String(), "sensitive-cookie") {
		t.Fatalf("diagnostic log leaked cookies:\n%s", logs.String())
	}
}

func TestPriceToolExecutor_LogsPolicyFailure(t *testing.T) {
	store, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,cookie_id,item_id,buyer_id,order_status,chat_id)
		VALUES ('order-tool-2','cid','item1','buyer1','paid','chat1')`); err != nil {
		t.Fatal(err)
	}
	profileID := enableTestAI(t, store, "http://127.0.0.1", "tool-policy")
	profile, err := store.AIProfiles.Get(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	replier := NewAIReplier("cid", store, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	executor := &priceToolExecutor{replier: replier, profile: profile, itemPrice: 100, orderClient: &diagnosticPriceAdjuster{}, chatID: "chat1", buyerID: "buyer1"}
	if _, err := executor.execute(ctx, `{"order_id":"order-tool-2","target_price_cents":8000}`); err == nil {
		t.Fatal("paid order should be rejected")
	}
	if !strings.Contains(logs.String(), "not_pending_payment") {
		t.Fatalf("missing status diagnostic:\n%s", logs.String())
	}
}
