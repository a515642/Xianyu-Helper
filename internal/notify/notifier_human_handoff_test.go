package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNotifyHumanHandoff 转人工通知带上买家、商品、会话和原因，并标记为转人工类型。
func TestNotifyHumanHandoff(t *testing.T) {
	s, cleanup := newNotifyStoreBare(t)
	defer cleanup()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	addWebhookChannel(t, s, "cid", "转人工渠道", srv.URL)
	New("cid", s, nil).NotifyHumanHandoff("cid", "张三", "buyer-1", "item-1", "chat-1", "买家要求人工处理售后")

	if gotBody == "" {
		t.Fatal("应发送转人工通知")
	}
	for _, want := range []string{"转人工通知", "张三", "buyer-1", "item-1", "chat-1", "买家要求人工处理售后"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("通知缺少 %q: %s", want, gotBody)
		}
	}
}
