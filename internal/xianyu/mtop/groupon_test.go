package mtop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFreeshippingAndRedFlowerRequests(t *testing.T) {
	var dataByAPI = map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		api := r.URL.Query().Get("api")
		dataByAPI[api] = r.Form.Get("data")
		w.Header().Set("Content-Type", "application/json")
		switch api {
		case "mtop.idle.groupon.activity.seller.freeshipping":
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
		case "mtop.taobao.idlemessage.red.flower":
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{}}`))
		default:
			t.Fatalf("unexpected api: %s", api)
		}
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client()}
	// groupon/red.flower 没有可注入 URL 字段时回落到常量；这里用 RoundTripper 不便，
	// 因此直接断言请求走的是同一套签名表单，并用可注入 URL 的副本。
	client.GrouponURL = server.URL
	client.RedFlowerURL = server.URL

	cookies := "unb=123; _m_h5_tk=token_1"
	fs, err := client.Freeshipping(context.Background(), cookies, "order-9", "888", "buyer-7")
	if err != nil || fs == nil || !fs.Success {
		t.Fatalf("freeshipping=%+v err=%v", fs, err)
	}
	rf, err := client.RequestRedFlower(context.Background(), cookies, "order-9")
	if err != nil || rf == nil || !rf.Success {
		t.Fatalf("red flower=%+v err=%v", rf, err)
	}

	var fsData map[string]any
	if err := json.Unmarshal([]byte(dataByAPI["mtop.idle.groupon.activity.seller.freeshipping"]), &fsData); err != nil {
		t.Fatalf("freeshipping data not json: %q err=%v", dataByAPI["mtop.idle.groupon.activity.seller.freeshipping"], err)
	}
	if fsData["bizOrderId"] != "order-9" || fsData["buyerId"] != "buyer-7" {
		t.Fatalf("freeshipping data=%+v", fsData)
	}
	// itemId 必须是裸数字（不是字符串），否则平台校验失败。
	if _, isString := fsData["itemId"].(string); isString {
		t.Fatalf("itemId 必须是数字: %v", fsData["itemId"])
	}
	if num, ok := fsData["itemId"].(float64); !ok || num != 888 {
		t.Fatalf("itemId=%v want 888", fsData["itemId"])
	}

	var rfData map[string]any
	if err := json.Unmarshal([]byte(dataByAPI["mtop.taobao.idlemessage.red.flower"]), &rfData); err != nil {
		t.Fatalf("red flower data not json: %q err=%v", dataByAPI["mtop.taobao.idlemessage.red.flower"], err)
	}
	if rfData["orderId"] != "order-9" || rfData["channel"] != "list" {
		t.Fatalf("red flower data=%+v", rfData)
	}
}

func TestFreeshippingIdempotentAndTerminalErrors(t *testing.T) {
	if !FreeshippingAlreadyDone([]string{"FAIL_BIZ_ORDER_ALREADY_DELIVERY::订单已发货成功"}) {
		t.Fatal("已发货成功应视为幂等成功")
	}
	if FreeshippingAlreadyDone([]string{"FAIL_BIZ_ORDER_NOT_EXIST::订单不存在"}) {
		t.Fatal("订单不存在不应视为已完成")
	}
	for _, ret := range []string{
		"FAIL_BIZ_GROUPON_ACTIVITY_ITEM_CHECK_ERROR::商品信息失效",
		"FAIL_BIZ_ITEM_NOT_FOUND::商品不存在",
		"FAIL_BIZ_ORDER_STATUS_ERROR::订单状态异常",
	} {
		if !FreeshippingTerminalFailure([]string{ret}) {
			t.Fatalf("应判定为不可恢复: %s", ret)
		}
	}
	if FreeshippingTerminalFailure([]string{"FAIL_BIZ_SYSTEM_BUSY::系统繁忙"}) {
		t.Fatal("系统繁忙应可重试")
	}
}
