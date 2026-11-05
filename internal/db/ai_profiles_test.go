package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAIProfilesBindItemsEncryptKeyAndReplaceForbiddenWords(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "ai-profile-test-key")
	d, _, err := Open(context.Background(), filepath.Join(t.TempDir(), "ai-profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := NewStore(d, DialectSQLite)
	ctx := context.Background()
	_, _ = store.Users.Create(ctx, "owner", "owner@example.com", "pw")
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	if err := store.Cookies.CreateOwned(ctx, "acc", "unb=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = d.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc','i1','Item 1'),('acc','i2','Item 2')`)
	id, err := store.AIProfiles.Create(ctx, AIProfile{CookieID: "acc", Name: "客服", Enabled: true, UseSystemAPI: false, MaxBargainRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	key := "secret"
	profile := AIProfile{ID: id, CookieID: "acc", Name: "客服", Enabled: true, UseSystemAPI: false, BaseURL: "https://example.com/v1", ModelName: "m", MaxBargainRounds: 3}
	if err := store.AIProfiles.Update(ctx, profile, &key, false); err != nil {
		t.Fatal(err)
	}
	if err := store.AIProfiles.ReplaceItems(ctx, id, "acc", []string{"i1", "i2"}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.AIProfiles.FindForItem(ctx, "acc", "i1")
	if err != nil || stored.APIKey != "secret" || len(stored.ItemIDs) != 1 {
		t.Fatalf("profile=%+v err=%v", stored, err)
	}
	var raw string
	if err := d.QueryRowContext(ctx, `SELECT api_key FROM ai_profiles WHERE id=?`, id).Scan(&raw); err != nil || raw == "secret" {
		t.Fatalf("key not encrypted: %q err=%v", raw, err)
	}
	if err := store.AIProfiles.ReplaceForbiddenWords(ctx, []AIForbiddenWord{{Keyword: "微信", Replacement: "站内沟通", Enabled: true}, {Keyword: "违规", Replacement: "", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.AIProfiles.ApplyForbiddenWords(ctx, "加微信违规")
	if err != nil || got != "加站内沟通" {
		t.Fatalf("replacement=%q err=%v", got, err)
	}
}

func TestAIProfilesMoveSingleItemBinding(t *testing.T) {
	d, _, err := Open(context.Background(), filepath.Join(t.TempDir(), "move.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d, DialectSQLite)
	ctx := context.Background()
	_, _ = s.Users.Create(ctx, "u", "u@e.com", "pw")
	u, _ := s.Users.GetByUsername(ctx, "u")
	_ = s.Cookies.CreateOwned(ctx, "a", "unb=1", u.ID)
	_, _ = d.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('a','i','I')`)
	one, _ := s.AIProfiles.Create(ctx, AIProfile{CookieID: "a", Name: "one", Enabled: true, UseSystemAPI: true, MaxBargainRounds: 3})
	two, _ := s.AIProfiles.Create(ctx, AIProfile{CookieID: "a", Name: "two", Enabled: true, UseSystemAPI: true, MaxBargainRounds: 3})
	_ = s.AIProfiles.ReplaceItems(ctx, one, "a", []string{"i"})
	if err := s.AIProfiles.ReplaceItems(ctx, two, "a", []string{"i"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.AIProfiles.FindForItem(ctx, "a", "i")
	if err != nil || p.ID != two {
		t.Fatalf("profile=%+v err=%v", p, err)
	}
}

// TestAIProfilesDropBindingWhenItemDeleted 商品删除后 AI 助手不应残留幽灵绑定。
func TestAIProfilesDropBindingWhenItemDeleted(t *testing.T) {
	d, _, err := Open(context.Background(), filepath.Join(t.TempDir(), "ai-item-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d, DialectSQLite)
	ctx := context.Background()
	_, _ = s.Users.Create(ctx, "u", "u@e.com", "pw")
	u, _ := s.Users.GetByUsername(ctx, "u")
	if err := s.Cookies.CreateOwned(ctx, "a", "unb=1", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('a','i1','I1'),('a','i2','I2')`); err != nil {
		t.Fatal(err)
	}
	id, err := s.AIProfiles.Create(ctx, AIProfile{CookieID: "a", Name: "客服", Enabled: true, UseSystemAPI: true, MaxBargainRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AIProfiles.ReplaceItems(ctx, id, "a", []string{"i1", "i2"}); err != nil {
		t.Fatal(err)
	}

	if err := s.Items.Delete(ctx, "a", "i1"); err != nil {
		t.Fatal(err)
	}
	profile, err := s.AIProfiles.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.ItemIDs) != 1 || profile.ItemIDs[0] != "i2" {
		t.Fatalf("item_ids after delete=%v", profile.ItemIDs)
	}
	if _, err := s.AIProfiles.FindForItem(ctx, "a", "i1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted item still bound: err=%v", err)
	}
}

// TestAIProfilesDropBindingWhenItemRemovedBySync 远端下架商品后同步应清理 AI 绑定。
func TestAIProfilesDropBindingWhenItemRemovedBySync(t *testing.T) {
	d, _, err := Open(context.Background(), filepath.Join(t.TempDir(), "ai-item-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d, DialectSQLite)
	ctx := context.Background()
	_, _ = s.Users.Create(ctx, "u", "u@e.com", "pw")
	u, _ := s.Users.GetByUsername(ctx, "u")
	if err := s.Cookies.CreateOwned(ctx, "a", "unb=1", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Items.SyncFromRemote(ctx, "a", []ItemInfoRow{{ItemID: "i1", ItemTitle: "I1"}, {ItemID: "i2", ItemTitle: "I2"}}); err != nil {
		t.Fatal(err)
	}
	id, err := s.AIProfiles.Create(ctx, AIProfile{CookieID: "a", Name: "客服", Enabled: true, UseSystemAPI: true, MaxBargainRounds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AIProfiles.ReplaceItems(ctx, id, "a", []string{"i1", "i2"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Items.SyncFromRemote(ctx, "a", []ItemInfoRow{{ItemID: "i2", ItemTitle: "I2"}}); err != nil {
		t.Fatal(err)
	}
	profile, err := s.AIProfiles.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.ItemIDs) != 1 || profile.ItemIDs[0] != "i2" {
		t.Fatalf("item_ids after sync=%v", profile.ItemIDs)
	}
}
