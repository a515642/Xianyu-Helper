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

// AdjustPriceAPI is the seller-side pending-order price adjustment endpoint.
const AdjustPriceAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.trade.user.adjust.price/1.0/"

type adjustPricePayload struct {
	ModifyFee       int64  `json:"modifyFee"`
	NewTransportFee string `json:"newTransportFee"`
	OrderID         string `json:"orderId"`
}

// AdjustPriceResult describes the platform response without exposing credentials.
type AdjustPriceResult struct {
	Success        bool
	UpdatedCookies string
	Ret            []string
	RawData        map[string]any
}

// AdjustOrderPrice changes a pending order's total price. The amount is in
// integer cents; callers must enforce account/order authorization and policy.
func (c *ClientImpl) AdjustOrderPrice(ctx context.Context, cookiesStr, orderID string, targetPriceCents int64) (*AdjustPriceResult, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, errors.New("订单 ID 不能为空")
	}
	if targetPriceCents <= 0 {
		return nil, errors.New("改价金额必须大于 0")
	}
	adjustURL := c.AdjustPriceURL
	if adjustURL == "" {
		adjustURL = AdjustPriceAPI
	}
	requestCookies, err := c.adjustPriceRequest(ctx, cookiesStr, adjustURL, orderID, targetPriceCents)
	if err != nil {
		return nil, err
	}
	return requestCookies, nil
}

func (c *ClientImpl) adjustPriceRequest(ctx context.Context, cookiesStr, adjustURL, orderID string, targetPriceCents int64) (*AdjustPriceResult, error) {
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", adjustURL)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	payload := adjustPricePayload{ModifyFee: targetPriceCents, NewTransportFee: "0", OrderID: strings.TrimSpace(orderID)}
	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化订单改价请求: %w", err)
	}
	dataValue := string(dataBytes)
	sign := protocol.GenerateSign(timestamp, protocol.SignToken(signingCookies), dataValue)
	query := buildAdjustPriceQuery(timestamp, sign)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adjustURL+"?"+query, strings.NewReader("data="+url.QueryEscape(dataValue)))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("订单改价请求失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}
	var result struct {
		Ret  []string `json:"ret"`
		Data struct {
			Success bool `json:"success"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("解析订单改价响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	success := result.Data.Success && hasMTopSuccess(result.Ret)
	out := &AdjustPriceResult{Success: success, UpdatedCookies: updated, Ret: append([]string(nil), result.Ret...)}
	if success {
		return out, nil
	}
	return out, fmt.Errorf("改价接口返回失败: %s", strings.Join(result.Ret, "; "))
}

func buildAdjustPriceQuery(timestamp, sign string) string {
	parts := [][2]string{{"jsv", "2.7.2"}, {"appKey", protocol.SignAppKey}, {"t", timestamp}, {"sign", sign}, {"v", "1.0"}, {"type", "originaljson"}, {"accountSite", "xianyu"}, {"dataType", "json"}, {"timeout", "20000"}, {"api", "mtop.taobao.idle.trade.user.adjust.price"}, {"sessionOption", "AutoLoginOnly"}}
	var b strings.Builder
	for i, pair := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(pair[0])
		b.WriteByte('=')
		b.WriteString(pair[1])
	}
	return b.String()
}
