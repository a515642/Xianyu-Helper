package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FreeshippingAPI 是小刀（砍价成团）订单的卖家免拼发货端点。
const FreeshippingAPI = "https://h5api.m.goofish.com/h5/mtop.idle.groupon.activity.seller.freeshipping/1.0/"

const freeshippingAPIName = "mtop.idle.groupon.activity.seller.freeshipping"

// RequestRedFlowerAPI 是下单后向买家求小红花的端点。
const RequestRedFlowerAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.red.flower/1.0/"

const requestRedFlowerAPIName = "mtop.taobao.idlemessage.red.flower"

// Freeshipping 对小刀订单执行免拼发货。幂等：平台返回"已发货成功"同样视为成功。
func (c *ClientImpl) Freeshipping(ctx context.Context, cookiesStr, orderID, itemID, buyerID string) (*AccountTaskResult, error) {
	orderID = strings.TrimSpace(orderID)
	itemID = strings.TrimSpace(itemID)
	buyerID = strings.TrimSpace(buyerID)
	if orderID == "" || itemID == "" || buyerID == "" {
		return nil, fmt.Errorf("免拼发货缺少订单、商品或买家ID")
	}
	// 平台要求 itemId 为数字，这里按原样拼接（参考实现的 data 值即为裸数字）。
	dataVal := `{"bizOrderId":"` + orderID + `", "itemId":` + itemID + `,"buyerId":"` + buyerID + `"}`
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.GrouponURL, FreeshippingAPI),
		freeshippingAPIName, "1.0", rawJSONData(dataVal), "https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
}

// RequestRedFlower 向买家请求小红花（评价互动）。
func (c *ClientImpl) RequestRedFlower(ctx context.Context, cookiesStr, orderID string) (*AccountTaskResult, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, fmt.Errorf("求小红花缺少订单ID")
	}
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RedFlowerURL, RequestRedFlowerAPI),
		requestRedFlowerAPIName, "4.0", map[string]any{"orderId": orderID, "channel": "list"},
		"https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
}

// rawJSONData 让调用方直接提供已序列化的 data 值（如免拼接口需要裸数字字段）。
func rawJSONData(dataVal string) map[string]any {
	return map[string]any{rawJSONDataKey: dataVal}
}

const rawJSONDataKey = "__raw_json_data__"

// marshalAccountTaskData 序列化请求 data；rawJSONDataKey 表示整体已是 JSON 文本。
func marshalAccountTaskData(data map[string]any) (string, error) {
	if raw, ok := data[rawJSONDataKey]; ok {
		text, _ := raw.(string)
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("请求 data 为空")
		}
		if !json.Valid([]byte(text)) {
			return "", fmt.Errorf("请求 data 不是合法 JSON")
		}
		return text, nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
