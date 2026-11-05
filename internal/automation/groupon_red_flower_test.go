package automation

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeGrouponClient 记录免拼/求小红花调用，供测试断言。
type fakeGrouponClient struct {
	freeshippingCalls int
	redFlowerCalls    int
	lastOrder         string
	lastItem          string
	lastBuyer         string
	result            *mtop.AccountTaskResult
	err               error
}

func (f *fakeGrouponClient) Freeshipping(_ context.Context, _, orderID, itemID, buyerID string) (*mtop.AccountTaskResult, error) {
	f.freeshippingCalls++
	f.lastOrder, f.lastItem, f.lastBuyer = orderID, itemID, buyerID
	return f.result, f.err
}

func (f *fakeGrouponClient) RequestRedFlower(_ context.Context, _, orderID string) (*mtop.AccountTaskResult, error) {
	f.redFlowerCalls++
	f.lastOrder = orderID
	return f.result, f.err
}

func newGrouponStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	return newAutomationTestStore(t)
}

// TestFreeshippingRespectsAccountSwitch 免拼受账号开关控制。
func TestFreeshippingRespectsAccountSwitch(t *testing.T) {
	store, cleanup := newGrouponStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	client := &fakeGrouponClient{result: &mtop.AccountTaskResult{Success: true, Message: "SUCCESS::调用成功"}}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetGrouponRedFlowerClient(client)
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,order_status) VALUES ('o-1','cid','pending_ship')`); err != nil {
		t.Fatal(err)
	}

	// 未开启自动免拼 → 不调用平台接口。
	if err := center.FreeshippingForOrder(ctx, "cid", "o-1", "i-1", "b-1"); err != nil {
		t.Fatalf("closed switch should be a no-op: %v", err)
	}
	if client.freeshippingCalls != 0 {
		t.Fatalf("开关关闭时不应调用免拼: %d", client.freeshippingCalls)
	}

	// 开启后调用一次，并标记订单为小刀。
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{
		CookieID: "cid", AutoFreeshippingEnabled: true, RateContent: "x", PolishTime: "03:00",
	}); err != nil {
		t.Fatal(err)
	}
	if err := center.FreeshippingForOrder(ctx, "cid", "o-1", "i-1", "b-1"); err != nil {
		t.Fatalf("freeshipping: %v", err)
	}
	if client.freeshippingCalls != 1 || client.lastOrder != "o-1" || client.lastItem != "i-1" || client.lastBuyer != "b-1" {
		t.Fatalf("免拼参数异常: %+v", client)
	}
	if marked, err := store.Orders.OrderIsBargain(ctx, "o-1"); err != nil || !marked {
		t.Fatalf("订单应标记为小刀: marked=%v err=%v", marked, err)
	}

	// 幂等：再次调用不再打平台接口。
	if err := center.FreeshippingForOrder(ctx, "cid", "o-1", "i-1", "b-1"); err != nil {
		t.Fatal(err)
	}
	if client.freeshippingCalls != 1 {
		t.Fatalf("重复免拼应幂等，got %d", client.freeshippingCalls)
	}
	_ = admin
}

// TestRedFlowerTaskRequestsRecentOrders 求小红花扫描近期订单并标记已处理。
func TestRedFlowerTaskRequestsRecentOrders(t *testing.T) {
	store, cleanup := newGrouponStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{
		CookieID: "cid", AutoRedFlowerEnabled: true, RateContent: "x", PolishTime: "03:00",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,order_status,created_at) VALUES (?,?,?,?)`,
		"ro-1", "cid", "completed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,order_status,created_at) VALUES (?,?,?,?)`,
		"ro-2", "cid", "completed", time.Now().UTC().AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}

	client := &fakeGrouponClient{result: &mtop.AccountTaskResult{Success: true, Message: "SUCCESS::调用成功"}}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetGrouponRedFlowerClient(client)

	summary, err := center.RunRedFlower(ctx, "cid")
	if err != nil {
		t.Fatalf("RunRedFlower: %v", err)
	}
	// 只有近 10 天内的订单会被求花。
	if summary.Found != 1 || summary.Success != 1 || client.redFlowerCalls != 1 || client.lastOrder != "ro-1" {
		t.Fatalf("summary=%+v calls=%d last=%q", summary, client.redFlowerCalls, client.lastOrder)
	}
	var flag int
	if err := store.DB.QueryRowContext(ctx, `SELECT is_red_flower FROM orders WHERE order_id='ro-1'`).Scan(&flag); err != nil || flag != 1 {
		t.Fatalf("订单应标记已求小红花: flag=%d err=%v", flag, err)
	}
	// 第二次扫描不再重复求花。
	client.redFlowerCalls = 0
	if _, err := center.RunRedFlower(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	if client.redFlowerCalls != 0 {
		t.Fatalf("已求花的订单不应重复请求: %d", client.redFlowerCalls)
	}
}
