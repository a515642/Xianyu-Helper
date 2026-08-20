package mtop

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdjustOrderPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.trade.user.adjust.price" {
			t.Fatalf("api=%s", r.URL.Query().Get("api"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "orderId") || !strings.Contains(string(body), "1234") || !strings.Contains(string(body), "modifyFee") || !strings.Contains(string(body), "9900") {
			t.Fatalf("body=%s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"success":true}}`))
	}))
	defer server.Close()
	client := NewClient()
	client.AdjustPriceURL = server.URL
	result, err := client.AdjustOrderPrice(context.Background(), "unb=1; _m_h5_tk=tk_1", "1234", 9900)
	if err != nil || result == nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAdjustOrderPriceRejectsInvalidInput(t *testing.T) {
	client := NewClient()
	if _, err := client.AdjustOrderPrice(context.Background(), "", "", 100); err == nil {
		t.Fatal("expected missing order error")
	}
	if _, err := client.AdjustOrderPrice(context.Background(), "", "123", 0); err == nil {
		t.Fatal("expected invalid price error")
	}
}
