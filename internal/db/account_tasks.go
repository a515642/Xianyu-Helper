package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type AccountTaskSettings struct {
	CookieID                string `json:"account_id"`
	AutoRateEnabled         bool   `json:"auto_rate_enabled"`
	RateContent             string `json:"rate_content"`
	AutoPolishEnabled       bool   `json:"auto_polish_enabled"`
	PolishTime              string `json:"polish_time"`
	AutoFreeshippingEnabled bool   `json:"auto_freeshipping_enabled"`
	AutoRedFlowerEnabled    bool   `json:"auto_red_flower_enabled"`
	LastRateScanAt          int64  `json:"last_rate_scan_at"`
	LastPolishDate          string `json:"last_polish_date"`
	LastPolishAt            int64  `json:"last_polish_at"`
	LastRedFlowerScanAt     int64  `json:"last_red_flower_scan_at"`
}

type AccountTaskRun struct {
	ID           int64  `json:"id"`
	RunKey       string `json:"run_key"`
	CookieID     string `json:"account_id"`
	TaskType     string `json:"task_type"`
	TargetID     string `json:"target_id"`
	RunDate      string `json:"run_date"`
	Status       string `json:"status"`
	SuccessCount int    `json:"success_count"`
	FailedCount  int    `json:"failed_count"`
	ErrorMessage string `json:"error_message"`
	NextRetryAt  int64  `json:"next_retry_at"`
	StartedAt    int64  `json:"started_at"`
	FinishedAt   int64  `json:"finished_at"`
}

type AccountTaskStore struct {
	DB      *sql.DB
	Dialect Dialect
}

func defaultAccountTaskSettings(cookieID string) AccountTaskSettings {
	return AccountTaskSettings{CookieID: cookieID, RateContent: "不错的买家，交易愉快", PolishTime: "03:00"}
}

func (s *AccountTaskStore) Get(ctx context.Context, cookieID string) (AccountTaskSettings, error) {
	result := defaultAccountTaskSettings(cookieID)
	var rateEnabled, polishEnabled int
	var freeshippingEnabled, redFlowerEnabled int
	err := s.DB.QueryRowContext(ctx, `SELECT auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,
		auto_freeshipping_enabled,auto_red_flower_enabled,
		last_rate_scan_at,last_polish_date,last_polish_at,last_red_flower_scan_at
		FROM account_task_settings WHERE cookie_id=?`, cookieID).Scan(
		&rateEnabled, &result.RateContent, &polishEnabled, &result.PolishTime,
		&freeshippingEnabled, &redFlowerEnabled, &result.LastRateScanAt,
		&result.LastPolishDate, &result.LastPolishAt, &result.LastRedFlowerScanAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	result.AutoRateEnabled = rateEnabled != 0
	result.AutoPolishEnabled = polishEnabled != 0
	result.AutoFreeshippingEnabled = freeshippingEnabled != 0
	result.AutoRedFlowerEnabled = redFlowerEnabled != 0
	return result, err
}

func (s *AccountTaskStore) Upsert(ctx context.Context, settings AccountTaskSettings) error {
	settings.RateContent = strings.TrimSpace(settings.RateContent)
	if settings.RateContent == "" {
		settings.RateContent = "不错的买家，交易愉快"
	}
	if settings.PolishTime == "" {
		settings.PolishTime = "03:00"
	}
	now := time.Now().UTC().Unix()
	query := `INSERT INTO account_task_settings
		(cookie_id,auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,auto_freeshipping_enabled,auto_red_flower_enabled,last_rate_scan_at,last_polish_date,last_polish_at,last_red_flower_scan_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectUpsert(s.Dialect, []string{"cookie_id"}, map[string]string{
		"auto_rate_enabled": "EXCLUDED.auto_rate_enabled", "rate_content": "EXCLUDED.rate_content",
		"auto_polish_enabled": "EXCLUDED.auto_polish_enabled", "polish_time": "EXCLUDED.polish_time",
		"auto_freeshipping_enabled": "EXCLUDED.auto_freeshipping_enabled",
		"auto_red_flower_enabled":   "EXCLUDED.auto_red_flower_enabled",
		"updated_at":                "EXCLUDED.updated_at",
	})
	_, err := s.DB.ExecContext(ctx, query, settings.CookieID, boolInt(settings.AutoRateEnabled), settings.RateContent,
		boolInt(settings.AutoPolishEnabled), settings.PolishTime, boolInt(settings.AutoFreeshippingEnabled),
		boolInt(settings.AutoRedFlowerEnabled), settings.LastRateScanAt, settings.LastPolishDate,
		settings.LastPolishAt, settings.LastRedFlowerScanAt, now, now)
	return err
}

func (s *AccountTaskStore) Enabled(ctx context.Context) ([]AccountTaskSettings, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.cookie_id,s.auto_rate_enabled,s.rate_content,s.auto_polish_enabled,s.polish_time,
		s.auto_freeshipping_enabled,s.auto_red_flower_enabled,
		s.last_rate_scan_at,s.last_polish_date,s.last_polish_at,s.last_red_flower_scan_at
		FROM account_task_settings s JOIN cookies c ON c.id=s.cookie_id
		WHERE s.auto_rate_enabled=1 OR s.auto_polish_enabled=1
		   OR s.auto_freeshipping_enabled=1 OR s.auto_red_flower_enabled=1 ORDER BY s.cookie_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AccountTaskSettings
	for rows.Next() {
		var row AccountTaskSettings
		var rate, polish, freeshipping, redFlower int
		if err := rows.Scan(&row.CookieID, &rate, &row.RateContent, &polish, &row.PolishTime,
			&freeshipping, &redFlower, &row.LastRateScanAt, &row.LastPolishDate, &row.LastPolishAt,
			&row.LastRedFlowerScanAt); err != nil {
			return nil, err
		}
		row.AutoRateEnabled, row.AutoPolishEnabled = rate != 0, polish != 0
		row.AutoFreeshippingEnabled, row.AutoRedFlowerEnabled = freeshipping != 0, redFlower != 0
		result = append(result, row)
	}
	return result, rows.Err()
}

// ClaimRun creates a run or atomically reclaims a due failed run.
func (s *AccountTaskStore) ClaimRun(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, false)
}

// ClaimRunImmediately creates a run or immediately reclaims a failed run. It is
// intended for an explicit user retry; scheduled workers should keep using
// ClaimRun so repeated platform failures still honor their retry delay.
func (s *AccountTaskStore) ClaimRunImmediately(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, true)
}

func (s *AccountTaskStore) claimRun(ctx context.Context, run AccountTaskRun, now int64, immediate bool) (bool, error) {
	retryCondition := "next_retry_at<=?"
	args := []any{now, run.RunKey, now}
	if immediate {
		retryCondition = "1=1"
		args = args[:2]
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status='running',started_at=?,finished_at=0,error_message=''
		WHERE run_key=? AND status='failed' AND `+retryCondition, args...)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	query := dialectInsertIgnorePrefix(s.Dialect) + ` INTO account_task_runs
		(run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,error_message,next_retry_at,started_at,finished_at)
		VALUES(?,?,?,?,?,'running',0,0,'',0,?,0)` + dialectInsertIgnore(s.Dialect, []string{"run_key"})
	res, err = s.DB.ExecContext(ctx, query, run.RunKey, run.CookieID, run.TaskType, run.TargetID, run.RunDate, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *AccountTaskStore) FinishRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status=?,success_count=?,failed_count=?,error_message=?,next_retry_at=?,finished_at=? WHERE run_key=?`,
		status, success, failed, message, nextRetryAt, time.Now().UTC().Unix(), runKey)
	return err
}

func (s *AccountTaskStore) MarkRateScan(ctx context.Context, cookieID string, at int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_rate_scan_at=?,updated_at=? WHERE cookie_id=?`, at, at, cookieID)
	return err
}

func (s *AccountTaskStore) MarkPolished(ctx context.Context, cookieID, date string, at int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_polish_date=?,last_polish_at=?,updated_at=? WHERE cookie_id=?`, date, at, at, cookieID)
	return err
}

// MarkRedFlowerScan 记录最近一次求小红花扫描时间。
func (s *AccountTaskStore) MarkRedFlowerScan(ctx context.Context, cookieID string, at int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_red_flower_scan_at=?,updated_at=? WHERE cookie_id=?`, at, at, cookieID)
	return err
}

// RedFlowerCandidates 返回该账号近期需要求小红花的订单号（下单时间在 sinceUnix（秒）之后）。
func (s *AccountTaskStore) RedFlowerCandidates(ctx context.Context, cookieID string, sinceUnix int64, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT order_id FROM orders
		WHERE cookie_id=? AND is_red_flower=0 AND deleted_at IS NULL
		  AND order_status NOT IN ('unknown','cancelled','refunding')
		  AND created_at >= ?
		ORDER BY created_at ASC LIMIT ?`,
		cookieID, time.Unix(sinceUnix, 0).UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			return nil, err
		}
		out = append(out, orderID)
	}
	return out, rows.Err()
}

func (s *AccountTaskStore) RecentRuns(ctx context.Context, cookieID string, limit int) ([]AccountTaskRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,
		error_message,next_retry_at,started_at,finished_at FROM account_task_runs WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AccountTaskRun
	for rows.Next() {
		var row AccountTaskRun
		if err := rows.Scan(&row.ID, &row.RunKey, &row.CookieID, &row.TaskType, &row.TargetID, &row.RunDate,
			&row.Status, &row.SuccessCount, &row.FailedCount, &row.ErrorMessage, &row.NextRetryAt,
			&row.StartedAt, &row.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
