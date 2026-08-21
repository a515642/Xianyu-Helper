package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// AIProfile is an account-scoped AI assistant that can be assigned to items.
type AIProfile struct {
	ID                     int64    `json:"id"`
	CookieID               string   `json:"cookie_id"`
	Name                   string   `json:"name"`
	Enabled                bool     `json:"enabled"`
	UseSystemAPI           bool     `json:"use_system_api"`
	APIKey                 string   `json:"-"`
	HasAPIKey              bool     `json:"has_api_key"`
	BaseURL                string   `json:"base_url"`
	ModelName              string   `json:"model_name"`
	ThinkingMode           string   `json:"thinking_mode"`
	BargainStrategyEnabled bool     `json:"bargain_strategy_enabled"`
	CustomPrompts          string   `json:"custom_prompts"`
	TriggerMode            string   `json:"trigger_mode"`
	MaxDiscountPercent     int      `json:"max_discount_percent"`
	MaxDiscountAmount      int      `json:"max_discount_amount"`
	MaxBargainRounds       int      `json:"max_bargain_rounds"`
	ItemIDs                []string `json:"item_ids"`
}

// AIForbiddenWord is a global deterministic post-processing rule.
type AIForbiddenWord struct {
	ID          int64  `json:"id"`
	Keyword     string `json:"keyword"`
	Replacement string `json:"replacement"`
	Enabled     bool   `json:"enabled"`
	SortOrder   int    `json:"sort_order"`
}

// AIProfiles stores product-scoped assistants and global safety replacements.
type AIProfiles struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

func (a *AIProfiles) List(ctx context.Context, cookieID string) ([]AIProfile, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT id,cookie_id,name,enabled,use_system_api,COALESCE(api_key,''),COALESCE(base_url,''),COALESCE(model_name,''),COALESCE(thinking_mode,'disabled'),bargain_strategy_enabled,COALESCE(custom_prompts,''),COALESCE(trigger_mode,'all_text'),max_discount_percent,max_discount_amount,max_bargain_rounds FROM ai_profiles WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIProfile
	for rows.Next() {
		profile, err := a.scanProfile(rows)
		if err != nil {
			return nil, err
		}
		profile.ItemIDs, err = a.itemIDs(ctx, profile.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (a *AIProfiles) Get(ctx context.Context, id int64) (*AIProfile, error) {
	row := a.DB.QueryRowContext(ctx, `SELECT id,cookie_id,name,enabled,use_system_api,COALESCE(api_key,''),COALESCE(base_url,''),COALESCE(model_name,''),COALESCE(thinking_mode,'disabled'),bargain_strategy_enabled,COALESCE(custom_prompts,''),COALESCE(trigger_mode,'all_text'),max_discount_percent,max_discount_amount,max_bargain_rounds FROM ai_profiles WHERE id=?`, id)
	profile, err := a.scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	profile.ItemIDs, err = a.itemIDs(ctx, profile.ID)
	return &profile, err
}

func (a *AIProfiles) FindForItem(ctx context.Context, cookieID, itemID string) (*AIProfile, error) {
	row := a.DB.QueryRowContext(ctx, `SELECT p.id,p.cookie_id,p.name,p.enabled,p.use_system_api,COALESCE(p.api_key,''),COALESCE(p.base_url,''),COALESCE(p.model_name,''),COALESCE(p.thinking_mode,'disabled'),p.bargain_strategy_enabled,COALESCE(p.custom_prompts,''),COALESCE(p.trigger_mode,'all_text'),p.max_discount_percent,p.max_discount_amount,p.max_bargain_rounds FROM ai_profiles p JOIN ai_profile_items i ON i.ai_profile_id=p.id WHERE i.cookie_id=? AND i.item_id=? AND p.enabled=1`, cookieID, itemID)
	profile, err := a.scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	profile.ItemIDs = []string{itemID}
	return &profile, nil
}

type profileScanner interface{ Scan(...any) error }

func (a *AIProfiles) scanProfile(scanner profileScanner) (AIProfile, error) {
	var profile AIProfile
	var enabled, useSystem, bargainEnabled int
	var encryptedKey string
	err := scanner.Scan(&profile.ID, &profile.CookieID, &profile.Name, &enabled, &useSystem, &encryptedKey, &profile.BaseURL, &profile.ModelName, &profile.ThinkingMode, &bargainEnabled, &profile.CustomPrompts, &profile.TriggerMode, &profile.MaxDiscountPercent, &profile.MaxDiscountAmount, &profile.MaxBargainRounds)
	if err != nil {
		return AIProfile{}, err
	}
	profile.Enabled = enabled != 0
	profile.UseSystemAPI = useSystem != 0
	profile.BargainStrategyEnabled = bargainEnabled != 0
	profile.APIKey, err = a.codec.decrypt("ai-profile-api-key", strconv.FormatInt(profile.ID, 10), encryptedKey)
	if err != nil {
		return AIProfile{}, err
	}
	profile.ThinkingMode = normalizeThinkingMode(profile.ThinkingMode)
	profile.HasAPIKey = strings.TrimSpace(profile.APIKey) != ""
	return profile, nil
}

func (a *AIProfiles) Create(ctx context.Context, profile AIProfile) (int64, error) {
	return insertReturningID(ctx, a.DB, a.Dialect, `INSERT INTO ai_profiles (cookie_id,name,enabled,use_system_api,api_key,base_url,model_name,thinking_mode,bargain_strategy_enabled,custom_prompts,trigger_mode,max_discount_percent,max_discount_amount,max_bargain_rounds) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, profile.CookieID, profile.Name, boolInt(profile.Enabled), boolInt(profile.UseSystemAPI), "", profile.BaseURL, profile.ModelName, normalizeThinkingMode(profile.ThinkingMode), boolInt(profile.BargainStrategyEnabled), profile.CustomPrompts, firstNonEmptyDB(profile.TriggerMode, "all_text"), profile.MaxDiscountPercent, profile.MaxDiscountAmount, profile.MaxBargainRounds)
}

func (a *AIProfiles) Update(ctx context.Context, profile AIProfile, apiKey *string, clearAPIKey bool) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE ai_profiles SET name=?,enabled=?,use_system_api=?,base_url=?,model_name=?,thinking_mode=?,bargain_strategy_enabled=?,custom_prompts=?,trigger_mode=?,max_discount_percent=?,max_discount_amount=?,max_bargain_rounds=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND cookie_id=?`, profile.Name, boolInt(profile.Enabled), boolInt(profile.UseSystemAPI), profile.BaseURL, profile.ModelName, normalizeThinkingMode(profile.ThinkingMode), boolInt(profile.BargainStrategyEnabled), profile.CustomPrompts, firstNonEmptyDB(profile.TriggerMode, "all_text"), profile.MaxDiscountPercent, profile.MaxDiscountAmount, profile.MaxBargainRounds, profile.ID, profile.CookieID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if clearAPIKey || apiKey != nil {
		value := ""
		if apiKey != nil && !clearAPIKey {
			value = *apiKey
		}
		encrypted, err := a.codec.encrypt("ai-profile-api-key", strconv.FormatInt(profile.ID, 10), value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ai_profiles SET api_key=? WHERE id=? AND cookie_id=?`, encrypted, profile.ID, profile.CookieID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *AIProfiles) Delete(ctx context.Context, id int64, cookieID string) error {
	res, err := a.DB.ExecContext(ctx, `DELETE FROM ai_profiles WHERE id=? AND cookie_id=?`, id, cookieID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceItems atomically moves listed account items to this profile.
func (a *AIProfiles) ReplaceItems(ctx context.Context, profileID int64, cookieID string, itemIDs []string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_profiles WHERE id=? AND cookie_id=?)`, profileID, cookieID).Scan(&exists); err != nil || !exists {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	for _, itemID := range itemIDs {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM item_info WHERE cookie_id=? AND item_id=?)`, cookieID, itemID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("商品 %s 不属于当前账号", itemID)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_profile_items WHERE ai_profile_id=?`, profileID); err != nil {
		return err
	}
	for _, itemID := range itemIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM ai_profile_items WHERE cookie_id=? AND item_id=?`, cookieID, itemID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_profile_items (ai_profile_id,cookie_id,item_id) VALUES (?,?,?)`, profileID, cookieID, itemID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *AIProfiles) itemIDs(ctx context.Context, profileID int64) ([]string, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT item_id FROM ai_profile_items WHERE ai_profile_id=? ORDER BY item_id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (a *AIProfiles) ListForbiddenWords(ctx context.Context) ([]AIForbiddenWord, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT id,keyword,replacement,enabled,sort_order FROM ai_forbidden_words ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIForbiddenWord
	for rows.Next() {
		var r AIForbiddenWord
		var enabled int
		if err := rows.Scan(&r.ID, &r.Keyword, &r.Replacement, &enabled, &r.SortOrder); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (a *AIProfiles) ReplaceForbiddenWords(ctx context.Context, rules []AIForbiddenWord) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_forbidden_words`); err != nil {
		return err
	}
	for i, r := range rules {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_forbidden_words (keyword,replacement,enabled,sort_order) VALUES (?,?,?,?)`, r.Keyword, r.Replacement, boolInt(r.Enabled), i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *AIProfiles) ApplyForbiddenWords(ctx context.Context, value string) (string, error) {
	rules, err := a.ListForbiddenWords(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range rules {
		if r.Enabled && r.Keyword != "" {
			value = strings.ReplaceAll(value, r.Keyword, r.Replacement)
		}
	}
	return value, nil
}

func normalizeThinkingMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "enabled") {
		return "enabled"
	}
	return "disabled"
}

func firstNonEmptyDB(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
