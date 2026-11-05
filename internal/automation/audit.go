package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

const (
	// auditReasonMaxRunes 限制单条审计原因的字符数，避免异常错误文本撑大审计表。
	auditReasonMaxRunes = 500
	// auditSummaryMaxRunes 限制单条审计摘要的字符数。
	auditSummaryMaxRunes = 200
	// auditRetention 是审计事件的保留时长，超过后由调度器清理。
	auditRetention = 90 * 24 * time.Hour
	// auditPruneInterval 是两次保留期清理之间的最短间隔，清理只访问本地数据库。
	auditPruneInterval = 24 * time.Hour
)

// auditEvent 描述一次待记录的自动化行为；字段均不得包含卡密正文、消息正文或凭证。
type auditEvent struct {
	eventType  string
	status     string
	ruleID     int64
	runID      int64
	actionType string
	summary    string
	reason     string
}

// auditRecorder 把自动化行为追加到审计表。审计是旁路记录：
// 写入失败只输出 Warn 日志，绝不影响自动化结果，也绝不阻止外部动作。
type auditRecorder struct {
	store  *db.Store
	logger *slog.Logger
}

func (c *Center) audit() auditRecorder {
	if c == nil {
		return auditRecorder{}
	}
	return auditRecorder{store: c.store, logger: c.logger}
}

// record 追加一条审计事件。即使原始上下文已取消，也使用独立的短时上下文写入终态。
func (r auditRecorder) record(ctx context.Context, task Task, event auditEvent) {
	if r.store == nil || r.store.Automation == nil || strings.TrimSpace(task.AccountID) == "" {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	source := task.Source
	if isDeferredReplay(task) {
		source = "recovery"
	}
	err := r.store.Automation.AppendAuditEvent(writeCtx, db.AutomationAuditEvent{
		CookieID: task.AccountID, OrderID: task.OrderID, ItemID: task.ItemID,
		RuleID: event.ruleID, RunID: event.runID, TriggerType: task.TriggerType,
		Source: source, EventType: event.eventType, Status: event.status,
		ActionType: event.actionType,
		Summary:    truncateAuditText(event.summary, auditSummaryMaxRunes),
		Reason:     truncateAuditText(event.reason, auditReasonMaxRunes),
		CreatedAt:  time.Now().UTC().Unix(),
	})
	if err != nil && r.logger != nil {
		r.logger.Warn("写入自动化审计事件失败", "account", task.AccountID,
			"order_id", task.OrderID, "event", event.eventType, "err", err)
	}
}

// skipped 记录一次被门禁拦截、没有执行任何外部动作的事件。
func (r auditRecorder) skipped(ctx context.Context, task Task, reason string) {
	r.record(ctx, task, auditEvent{eventType: db.AuditEventSkipped, status: db.AuditStatusSkipped, reason: reason})
}

// queued 记录一次已持久化排队的事件。
func (r auditRecorder) queued(ctx context.Context, task Task, runID, ruleID int64, reason string) {
	r.record(ctx, task, auditEvent{
		eventType: db.AuditEventQueued, status: db.AuditStatusQueued, runID: runID, ruleID: ruleID, reason: reason,
	})
}

// runStarted 记录运行取得执行权。
func (r auditRecorder) runStarted(ctx context.Context, task Task, ruleID, runID int64) {
	r.record(ctx, task, auditEvent{
		eventType: db.AuditEventRunStarted, status: db.AuditStatusRunning, ruleID: ruleID, runID: runID,
	})
}

// actionSucceeded 记录一个动作已确认完成。
func (r auditRecorder) actionSucceeded(ctx context.Context, task Task, ruleID, runID int64, action db.AutomationAction, sent int) {
	r.record(ctx, task, auditEvent{
		eventType: db.AuditEventActionSucceeded, status: db.AuditStatusSuccess,
		ruleID: ruleID, runID: runID, actionType: action.ActionType,
		summary: summarizeAction(action, sent),
	})
}

// actionFailed 记录一个动作失败或结果不确定。
func (r auditRecorder) actionFailed(ctx context.Context, task Task, ruleID, runID int64, action db.AutomationAction, status, reason string) {
	if status == "" {
		status = db.AuditStatusFailed
	}
	r.record(ctx, task, auditEvent{
		eventType: db.AuditEventActionFailed, status: status,
		ruleID: ruleID, runID: runID, actionType: action.ActionType,
		summary: summarizeAction(action, 0), reason: reason,
	})
}

// runFinished 记录运行进入终态。
func (r auditRecorder) runFinished(ctx context.Context, task Task, ruleID, runID int64, status string, sent int, reason string) {
	r.record(ctx, task, auditEvent{
		eventType: db.AuditEventRunFinished, status: status, ruleID: ruleID, runID: runID,
		summary: fmt.Sprintf("运行结束，已完成 %d 个动作", sent), reason: reason,
	})
}

// summarizeAction 生成动作的非敏感摘要，绝不读取已发送的卡密或消息正文。
func summarizeAction(action db.AutomationAction, sent int) string {
	switch action.ActionType {
	case ActionSendCard:
		count := sent
		if count <= 0 {
			count = action.DeliveryCount
		}
		if count <= 0 {
			count = 1
		}
		return fmt.Sprintf("发送卡密「%s」×%d", action.CardName, count)
	case ActionSendTemplate:
		return fmt.Sprintf("发送发货模板「%s」", action.DeliveryTemplateName)
	case ActionSendText:
		return "发送文本消息"
	case ActionConfirmShipment:
		return "确认发货"
	default:
		return action.ActionType
	}
}

// truncateAuditText 按字符数截断审计文本并去除首尾空白。
func truncateAuditText(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "…"
}

// pruneAuditEvents 删除超过保留期的审计事件，每天最多执行一次。
func (s *Scheduler) pruneAuditEvents(ctx context.Context) {
	if s == nil || s.center == nil || s.center.store == nil {
		return
	}
	now := time.Now().UTC()
	last := s.auditPrunedAt.Load()
	if last != 0 && now.Unix()-last < int64(auditPruneInterval/time.Second) {
		return
	}
	s.auditPrunedAt.Store(now.Unix())
	before := now.Add(-auditRetention).Unix()
	removed, err := s.center.store.Automation.PruneAuditEvents(ctx, before)
	if err != nil {
		s.center.logger.Warn("清理过期自动化审计事件失败", "err", err)
		return
	}
	if removed > 0 {
		s.center.logger.Info("已清理过期自动化审计事件", "count", removed)
	}
}
