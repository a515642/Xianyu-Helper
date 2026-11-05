package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordedAlert 记录账号事件，用于断言离线/重试/恢复通知的文案。
type recordedAlert struct {
	eventType string
	level     string
	title     string
	body      string
}

// alertRecordingHandler 在 recordingHandler 的基础上记录 OnAccountEvent。
// 独立于通用 handler，避免影响其它测试对 OnAccountAlert 的计数断言。
type alertRecordingHandler struct {
	recordingHandler
	alertMu sync.Mutex
	alerts  []recordedAlert
}

func (h *alertRecordingHandler) OnAccountEvent(_ context.Context, _, eventType, level, title, body string) {
	h.alertMu.Lock()
	defer h.alertMu.Unlock()
	h.alerts = append(h.alerts, recordedAlert{eventType: eventType, level: level, title: title, body: body})
}

func (h *alertRecordingHandler) alertsSnapshot() []recordedAlert {
	h.alertMu.Lock()
	defer h.alertMu.Unlock()
	out := make([]recordedAlert, len(h.alerts))
	copy(out, h.alerts)
	return out
}

func waitForRuntimeState(t *testing.T, acc *Account, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if acc.RuntimeStatus().State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime state=%q want %q", acc.RuntimeStatus().State, want)
}

func dialerCallCount(d *fakeDialer) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func TestRun_DialNetworkFailureRetriesInsteadOfExiting(t *testing.T) {
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	healthy := &fakeWSConn{recvBlock: true, closeCh: make(chan struct{})}
	dialer := &fakeDialer{results: []dialResult{
		{err: errors.New("dial tcp 127.0.0.1:443: connect: connection refused")},
		{conn: healthy},
	}}
	acc.wsDialer = dialer
	acc.dialRetryDelayFn = func(int) time.Duration { return time.Millisecond }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()

	waitForRuntimeState(t, acc, RuntimeOnline, 3*time.Second)
	if calls := dialerCallCount(dialer); calls < 2 {
		cancel()
		<-done
		t.Fatalf("网络拨号失败后应自动重试，dial calls=%d", calls)
	}
	if status := acc.RuntimeStatus().State; status == RuntimeAuthExpired {
		cancel()
		<-done
		t.Fatalf("网络拨号失败不应置为凭证失效: %q", status)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("取消后 Run 未退出")
	}
}

func TestRun_RegisterNetworkFailureRetriesInsteadOfExiting(t *testing.T) {
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	failed := &fakeWSConn{recvBlock: true, closeCh: make(chan struct{}), registerErr: errors.New("等待 /reg 响应失败: i/o timeout")}
	healthy := &fakeWSConn{recvBlock: true, closeCh: make(chan struct{})}
	dialer := &fakeDialer{results: []dialResult{{conn: failed}, {conn: healthy}}}
	acc.wsDialer = dialer
	acc.dialRetryDelayFn = func(int) time.Duration { return time.Millisecond }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()

	waitForRuntimeState(t, acc, RuntimeOnline, 3*time.Second)
	if calls := dialerCallCount(dialer); calls < 2 {
		cancel()
		<-done
		t.Fatalf("注册网络失败后应自动重试，dial calls=%d", calls)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("取消后 Run 未退出")
	}
}

func TestRun_TokenTimeoutNotificationSaysRetrying(t *testing.T) {
	acc, _, _, cleanup := newRunAccount(t, &fakeFailTokenMtop{err: errors.New("读取消息凭证超时: context deadline exceeded")})
	defer cleanup()
	h := &alertRecordingHandler{}
	acc.handler = h
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: &fakeWSConn{recvBlock: true, closeCh: make(chan struct{})}}}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	var alerts []recordedAlert
	for time.Now().Before(deadline) {
		alerts = h.alertsSnapshot()
		if len(alerts) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("取消后 Run 未退出")
	}
	if len(alerts) == 0 {
		t.Fatal("凭证获取超时应发送离线通知")
	}
	alert := alerts[0]
	if alert.title != "账号消息连接中断，正在自动重试" {
		t.Fatalf("title=%q want 自动重试提示", alert.title)
	}
	if strings.Contains(alert.body, "需要重新登录") {
		t.Fatalf("凭证超时不应提示需要重新登录: %q", alert.body)
	}
	if !strings.Contains(alert.body, "自动重试") {
		t.Fatalf("通知应说明会自动重试: %q", alert.body)
	}
}

func TestDialBackoffSequence(t *testing.T) {
	want := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second, 120 * time.Second, 120 * time.Second}
	for i, expected := range want {
		if got := dialBackoff(i + 1); got != expected {
			t.Fatalf("dialBackoff(%d)=%v want %v", i+1, got, expected)
		}
	}
	if got := dialBackoff(0); got != 5*time.Second {
		t.Fatalf("dialBackoff(0)=%v want 5s", got)
	}
}
