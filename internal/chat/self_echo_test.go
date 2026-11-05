package chat

import (
	"context"
	"path/filepath"
	"testing"

	"xianyu-go/internal/db"
)

func newEchoStore(t *testing.T) (*Service, *db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "echo.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "cid", "unb=123;", admin.ID)
	return New(store), store, func() { d.Close() }
}

// TestRecordSelfEchoStoresHumanSend 人类卖家的发送会被记录为出站消息。
func TestRecordSelfEchoStoresHumanSend(t *testing.T) {
	svc, store, cleanup := newEchoStore(t)
	defer cleanup()
	ctx := context.Background()
	session := db.ChatSession{CookieID: "cid", ChatID: "chat-1", BuyerID: "buyer-1"}

	if _, _, err := svc.RecordSelfEcho(ctx, session, "m-1", "text", "老板在的", 1700000000000); err != nil {
		t.Fatalf("RecordSelfEcho: %v", err)
	}
	got, ok, err := store.Chats.LastSellerOutgoingAt(ctx, "cid", "chat-1")
	if err != nil || !ok || got != 1700000000000 {
		t.Fatalf("真人发送未记录: got=%d ok=%v err=%v", got, ok, err)
	}
}

// TestRecordSelfEchoDeduplicatesLocalSend 本后台发送 + 平台回显只保留一条。
func TestRecordSelfEchoDeduplicatesLocalSend(t *testing.T) {
	svc, _, cleanup := newEchoStore(t)
	defer cleanup()
	ctx := context.Background()
	session := db.ChatSession{CookieID: "cid", ChatID: "chat-1", BuyerID: "buyer-1"}

	// 模拟本后台发送：已经以 local- key 入库。
	if _, err := svc.CreateOutgoing(ctx, session, "稍等哈"); err != nil {
		t.Fatalf("CreateOutgoing: %v", err)
	}
	// 平台回显同一条内容。
	if _, inserted, err := svc.RecordSelfEcho(ctx, session, "m-1", "text", "稍等哈", 1700000000000); err != nil {
		t.Fatalf("RecordSelfEcho: %v", err)
	} else if inserted {
		t.Fatal("与本地发送重复的回显不应再次入库")
	}
}

// TestRecordSelfEchoSkipsEmpty AI/系统类空内容直接忽略。
func TestRecordSelfEchoSkipsEmpty(t *testing.T) {
	svc, _, cleanup := newEchoStore(t)
	defer cleanup()
	ctx := context.Background()
	session := db.ChatSession{CookieID: "cid", ChatID: "chat-1", BuyerID: "buyer-1"}
	if _, inserted, err := svc.RecordSelfEcho(ctx, session, "m-1", "text", "   ", 0); err != nil || inserted {
		t.Fatalf("空内容不应入库: inserted=%v err=%v", inserted, err)
	}
}
