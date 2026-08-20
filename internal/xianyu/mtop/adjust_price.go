package mtop

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const AdjustPriceAPI = "mtop.taobao.idle.trade.user.adjust.price"

// AdjustPriceResult describes the platform response without exposing raw credentials.
type AdjustPriceResult struct {
	Success        bool
	UpdatedCookies string
	Ret            []string
	RawData        map[string]any
}

// AdjustOrderPrice changes the price of an existing order. TargetPriceCents is
// expressed in integer cents; the caller remains responsible for business
// authorization and confirmation policy.
// AdjustOrderPrice 修改指定订单价格。调用方必须在更高层完成账号归属、订单状态和人工/LLM 工具授权校验。
func (c *ClientImpl) AdjustOrderPrice(ctx context.Context, cookiesStr, orderID string, targetPriceCents int64) (*AdjustPriceResult, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, errors.New("订单 ID 不能为空")
	}
	if targetPriceCents <= 0 {
		return nil, errors.New("改价金额必须大于 0")
	}
	data := map[string]any{
		"orderId":     strings.TrimSpace(orderID),
		"targetPrice": strconv.FormatInt(targetPriceCents, 10),
	}
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr,
		firstNonEmptyURL(c.AdjustPriceURL, "https://h5api.m.goofish.com/h5/"+AdjustPriceAPI+"/1.0/"),
		AdjustPriceAPI, "1.0", data, "https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	ret := append([]string(nil), decoded.Ret...)
	success := false
	for _, value := range ret {
		if strings.Contains(value, "SUCCESS") {
			success = true
			break
		}
	}
	if !success {
		return &AdjustPriceResult{Success: false, UpdatedCookies: updated, Ret: ret, RawData: decoded.Data}, fmt.Errorf("改价接口返回失败: %s", strings.Join(ret, "; "))
	}
	return &AdjustPriceResult{Success: true, UpdatedCookies: updated, Ret: ret, RawData: decoded.Data}, nil
}
