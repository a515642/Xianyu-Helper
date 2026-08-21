package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

const adjustPriceAPIName = "mtop.taobao.idle.trade.user.adjust.price"
const adjustPriceDefaultURL = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.trade.user.adjust.price/1.0/"

type adjustPricePayload struct {
	ModifyFee       int64  `json:"modifyFee"`
	NewTransportFee string `json:"newTransportFee"`
	OrderID         string `json:"orderId"`
}

type AdjustPriceResult struct {
	Success        bool
	UpdatedCookies string
	Ret            []string
}

// AdjustOrderPrice changes a pending order's total price in integer cents.
// The caller must enforce account/order authorization and business policy.
func (c *ClientImpl) AdjustOrderPrice(ctx context.Context, cookiesStr, orderID string, targetPriceCents int64) (*AdjustPriceResult, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, errors.New("订单 ID 不能为空")
	}
	if targetPriceCents <= 0 {
		return nil, errors.New("改价金额必须大于 0")
	}
	endpoint := c.AdjustPriceURL
	if endpoint == "" {
		endpoint = adjustPriceDefaultURL
	}
	current := cookiesStr
	payload, err := json.Marshal(adjustPricePayload{ModifyFee: targetPriceCents, NewTransportFee: "0", OrderID: strings.TrimSpace(orderID)})
	if err != nil {
		return nil, fmt.Errorf("序列化改价请求: %w", err)
	}
	signingCookies, requestCookies := mtopRequestCookies(ctx, current, "https://www.goofish.com/", endpoint)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(timestamp, protocol.SignToken(signingCookies), string(payload))
	query := [][2]string{{"jsv", "2.7.2"}, {"appKey", protocol.SignAppKey}, {"t", timestamp}, {"sign", sign}, {"v", "1.0"}, {"type", "originaljson"}, {"accountSite", "xianyu"}, {"dataType", "json"}, {"timeout", "20000"}, {"api", adjustPriceAPIName}, {"sessionOption", "AutoLoginOnly"}}
	values := url.Values{}
	for _, pair := range query {
		values.Set(pair[0], pair[1])
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+values.Encode(), strings.NewReader("data="+url.QueryEscape(string(payload))))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("订单改价请求失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, current, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Ret  []string `json:"ret"`
		Data struct {
			Success bool `json:"success"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("解析订单改价响应失败: %w", err)
	}
	ok := decoded.Data.Success && hasMTopSuccess(decoded.Ret)
	result := &AdjustPriceResult{Success: ok, UpdatedCookies: updated, Ret: append([]string(nil), decoded.Ret...)}
	if !ok {
		return result, fmt.Errorf("改价接口返回失败: %s", strings.Join(decoded.Ret, "; "))
	}
	return result, nil
}
