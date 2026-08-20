package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

const adjustOrderPriceToolName = "adjust_order_price"

type priceToolArgs struct {
	OrderID          string `json:"order_id"`
	TargetPriceCents int64  `json:"target_price_cents"`
}

func adjustOrderPriceTool() openai.Tool {
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{
		Name:        adjustOrderPriceToolName,
		Description: "将当前买家对应的待付款订单改为指定总价，金额单位为分。只有确实需要议价且满足价格策略时调用。",
		Strict:      true,
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"order_id":           map[string]any{"type": "string", "description": "待付款订单 ID"},
				"target_price_cents": map[string]any{"type": "integer", "minimum": 1, "description": "目标总价，单位为分"},
			},
			"required": []string{"order_id", "target_price_cents"},
		},
	}}
}

type priceToolExecutor struct {
	replier     *AIReplierImpl
	profile     *db.AIProfile
	itemPrice   float64
	orderClient interface {
		AdjustOrderPrice(context.Context, string, string, int64) (*mtop.AdjustPriceResult, error)
	}
	cookies string
	chatID  string
	buyerID string
}

func (e *priceToolExecutor) execute(ctx context.Context, raw string) (string, error) {
	var args priceToolArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", errors.New("改价参数不是有效 JSON")
	}
	args.OrderID = strings.TrimSpace(args.OrderID)
	if args.OrderID == "" || args.TargetPriceCents <= 0 {
		return "", errors.New("订单 ID 和目标金额必须有效")
	}
	order, err := e.replier.store.Orders.Get(ctx, args.OrderID)
	if err != nil {
		return "", fmt.Errorf("读取订单失败: %w", err)
	}
	if order.CookieID != e.replier.cookieID || (order.ChatID != "" && order.ChatID != e.chatID) || (order.BuyerID != "" && order.BuyerID != e.buyerID) {
		return "", errors.New("订单不属于当前账号或会话")
	}
	status := db.NormalizeOrderStatus(order.OrderStatus)
	if status != "processing" {
		return "", fmt.Errorf("订单状态 %s 不允许改价，仅待付款订单可改价", status)
	}
	itemPrice := e.itemPrice
	if itemPrice <= 0 {
		return "", errors.New("商品原价无效")
	}
	maxDiscount := math.Max(itemPrice*float64(e.profile.MaxDiscountPercent)/100, float64(e.profile.MaxDiscountAmount))
	minimum := itemPrice - maxDiscount
	if e.profile.MaxDiscountPercent <= 0 || e.profile.MaxDiscountAmount <= 0 {
		minimum = itemPrice
	}
	if float64(args.TargetPriceCents)/100+0.0001 < minimum || float64(args.TargetPriceCents)/100 > itemPrice+0.0001 {
		return "", fmt.Errorf("目标价格必须在 %.2f 到 %.2f 元之间", minimum, itemPrice)
	}
	result, err := e.orderClient.AdjustOrderPrice(ctx, e.cookies, args.OrderID, args.TargetPriceCents)
	if err != nil {
		return "", err
	}
	if result == nil || !result.Success {
		return "", errors.New("平台未确认改价成功")
	}
	return fmt.Sprintf("订单 %s 已改价为 %.2f 元", args.OrderID, float64(args.TargetPriceCents)/100), nil
}
