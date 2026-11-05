package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

const (
	TaskFreeshipping = "auto_freeshipping"
	TaskRedFlower    = "auto_red_flower"

	freeshippingMaxAttempts   = 4
	freeshippingRetryGap      = 500 * time.Millisecond
	freeshippingNetworkRetry  = 2
	redFlowerWindowDays       = 10
	redFlowerRequestGap       = 500 * time.Millisecond
	redFlowerMaxOrdersPerScan = 200
)

// GrouponRedFlowerClient 是小刀免拼与求小红花所需的最小 mtop 契约。
type GrouponRedFlowerClient interface {
	Freeshipping(ctx context.Context, cookiesStr, orderID, itemID, buyerID string) (*mtop.AccountTaskResult, error)
	RequestRedFlower(ctx context.Context, cookiesStr, orderID string) (*mtop.AccountTaskResult, error)
}

// FreeshippingForOrder 对自动触发的小刀订单执行免拼发货。
// 需要账号开启自动免拼；成功（含"已发货成功"）返回 nil。
func (c *Center) FreeshippingForOrder(ctx context.Context, accountID, orderID, itemID, buyerID string) error {
	settings, err := c.store.AccountTasks.Get(ctx, accountID)
	if err != nil {
		return err
	}
	if !settings.AutoFreeshippingEnabled {
		return nil
	}
	return c.dispatchFreeshipping(ctx, accountID, orderID, itemID, buyerID, false)
}

// FreeshippingOrder 是人工触发的免拼发货：绕过账号自动免拼开关，但仍保留幂等与安全校验。
func (c *Center) FreeshippingOrder(ctx context.Context, accountID, orderID, itemID, buyerID string) error {
	return c.dispatchFreeshipping(ctx, accountID, orderID, itemID, buyerID, true)
}

func (c *Center) dispatchFreeshipping(ctx context.Context, accountID, orderID, itemID, buyerID string, manual bool) error {
	if c.grouponRedFlower == nil {
		return fmt.Errorf("免拼客户端未初始化")
	}
	allowed, err := c.accountAutomationAllowed(ctx, accountID)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("账号已停用或暂停，跳过免拼发货")
	}
	if blocked, err := c.accountTaskSessionBlocked(ctx, accountID); err != nil || blocked {
		return fmt.Errorf("账号 Session 已失效，跳过免拼发货")
	}
	// 幂等：同一个订单只要打过标记就不再重复调用平台接口（人工补发同样受此约束）。
	if marked, err := c.store.Orders.OrderIsBargain(ctx, orderID); err == nil && marked {
		if !manual {
			return nil
		}
	}
	cookies, err := c.store.Cookies.GetValue(ctx, accountID)
	if err != nil {
		return err
	}
	if err := c.store.Orders.MarkOrderBargain(ctx, orderID); err != nil {
		c.logger.Warn("标记小刀订单失败", "order_id", orderID, "err", err)
	}
	if err := c.runFreeshipping(ctx, accountID, cookies, orderID, itemID, buyerID); err != nil {
		if mtop.IsSessionExpiredErr(err) {
			return c.recoverAccountTaskSession(ctx, accountID, err)
		}
		return err
	}
	return nil
}

func (c *Center) runFreeshipping(ctx context.Context, accountID, cookies, orderID, itemID, buyerID string) error {
	current := cookies
	networkFailures := 0
	var lastErr error
	for attempt := 0; attempt < freeshippingMaxAttempts; attempt++ {
		result, err := c.grouponRedFlower.Freeshipping(ctx, current, orderID, itemID, buyerID)
		if err != nil {
			// 会话/风控错误直接上报，由上层触发续期。
			if mtop.IsSessionExpiredErr(err) || mtop.IsRiskVerificationErr(err) {
				return err
			}
			networkFailures++
			lastErr = err
			if networkFailures > freeshippingNetworkRetry {
				return err
			}
			if sleepErr := sleepWithContext(ctx, freeshippingRetryGap); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		if result != nil && result.UpdatedCookies != "" {
			current = c.persistTaskCookies(ctx, accountID, current, result.UpdatedCookies)
		}
		ret := []string{}
		if result != nil && result.Message != "" {
			ret = append(ret, result.Message)
		}
		if result != nil && result.Success {
			return nil
		}
		if mtop.FreeshippingAlreadyDone(ret) {
			return nil
		}
		if mtop.FreeshippingTerminalFailure(ret) {
			return fmt.Errorf("免拼发货失败（不可重试）: %s", strings.Join(ret, "; "))
		}
		lastErr = fmt.Errorf("免拼发货失败: %s", strings.Join(ret, "; "))
		if sleepErr := sleepWithContext(ctx, freeshippingRetryGap); sleepErr != nil {
			return sleepErr
		}
	}
	if lastErr == nil {
		lastErr = errors.New("免拼发货失败")
	}
	return lastErr
}

// RunRedFlower 对单个账号执行一次求小红花扫描。
func (c *Center) RunRedFlower(ctx context.Context, accountID string) (AccountTaskSummary, error) {
	summary := AccountTaskSummary{TaskType: TaskRedFlower}
	if c.grouponRedFlower == nil {
		return summary, fmt.Errorf("求小红花客户端未初始化")
	}
	settings, err := c.store.AccountTasks.Get(ctx, accountID)
	if err != nil {
		return summary, err
	}
	return c.runRedFlower(ctx, settings)
}

func (c *Center) runRedFlower(ctx context.Context, settings db.AccountTaskSettings) (AccountTaskSummary, error) {
	summary := AccountTaskSummary{TaskType: TaskRedFlower}
	if c.grouponRedFlower == nil {
		return summary, fmt.Errorf("求小红花客户端未初始化")
	}
	since := time.Now().UTC().AddDate(0, 0, -redFlowerWindowDays).Unix()
	orders, err := c.store.AccountTasks.RedFlowerCandidates(ctx, settings.CookieID, since, redFlowerMaxOrdersPerScan)
	if err != nil {
		return summary, err
	}
	summary.Found = len(orders)
	if len(orders) == 0 {
		_ = c.store.AccountTasks.MarkRedFlowerScan(ctx, settings.CookieID, time.Now().UTC().Unix())
		return summary, nil
	}
	cookies, err := c.store.Cookies.GetValue(ctx, settings.CookieID)
	if err != nil {
		return summary, err
	}
	current := cookies
	for _, orderID := range orders {
		runKey := "redflower:" + settings.CookieID + ":" + orderID
		claimed, claimErr := c.store.AccountTasks.ClaimRun(ctx, db.AccountTaskRun{RunKey: runKey,
			CookieID: settings.CookieID, TaskType: TaskRedFlower, TargetID: orderID}, time.Now().UTC().Unix())
		if claimErr != nil {
			return summary, claimErr
		}
		if !claimed {
			summary.Skipped++
			continue
		}
		result, requestErr := c.grouponRedFlower.RequestRedFlower(ctx, current, orderID)
		if requestErr != nil || result == nil || !result.Success {
			message := errorString(requestErr)
			if result != nil && result.Message != "" {
				message = result.Message
			}
			_ = c.store.AccountTasks.FinishRun(ctx, runKey, "failed", 0, 1, message, time.Now().UTC().Add(10*time.Minute).Unix())
			summary.Failed++
			if isPlatformBlockErr(requestErr) {
				return summary, requestErr
			}
			continue
		}
		if result.UpdatedCookies != "" {
			current = c.persistTaskCookies(ctx, settings.CookieID, current, result.UpdatedCookies)
		}
		if err := c.store.Orders.MarkOrderRedFlowerRequested(ctx, orderID); err != nil {
			c.logger.Warn("标记订单已求小红花失败", "order_id", orderID, "err", err)
		}
		_ = c.store.AccountTasks.FinishRun(ctx, runKey, "success", 1, 0, "", 0)
		summary.Success++
		if err := sleepWithContext(ctx, redFlowerRequestGap); err != nil {
			return summary, err
		}
	}
	_ = c.store.AccountTasks.MarkRedFlowerScan(ctx, settings.CookieID, time.Now().UTC().Unix())
	return summary, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
