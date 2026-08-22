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

const adjustPriceAPIName = "mtop.taobao.idle.trade.user.adjust.price"

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
		endpoint = AdjustPriceAPI
	}
	return c.adjustPriceRequest(ctx, cookiesStr, endpoint, orderID, targetPriceCents)
}

func (c *ClientImpl) adjustPriceRequest(ctx context.Context, cookiesStr, endpoint, orderID string, targetPriceCents int64) (*AdjustPriceResult, error) {
	payload := adjustPricePayload{ModifyFee: targetPriceCents, NewTransportFee: "0", OrderID: strings.TrimSpace(orderID)}
	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化订单改价请求: %w", err)
	}
	dataValue := string(dataBytes)
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", endpoint)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(timestamp, protocol.SignToken(signingCookies), dataValue)
	query := buildAdjustPriceQuery(timestamp, sign)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query, strings.NewReader("data="+url.QueryEscape(dataValue)))
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
	var decoded struct {
		Ret  []string         `json:"ret"`
		Data map[string]any   `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("解析订单改价响应失败: %w", err)
	}
	success, _ := decoded.Data["success"].(bool)
	result := &AdjustPriceResult{Success: success && hasMTopSuccess(decoded.Ret), UpdatedCookies: updated, Ret: append([]string(nil), decoded.Ret...), RawData: decoded.Data}
	if !result.Success {
		return result, fmt.Errorf("改价接口返回失败: %s", strings.Join(decoded.Ret, "; "))
	}
	return result, nil
}

func buildAdjustPriceQuery(timestamp, sign string) string {
	values := url.Values{}
	values.Set("jsv", "2.7.2")
	values.Set("appKey", protocol.SignAppKey)
	values.Set("t", timestamp)
	values.Set("sign", sign)
	values.Set("v", "1.0")
	values.Set("type", "originaljson")
	values.Set("accountSite", "xianyu")
	values.Set("dataType", "json")
	values.Set("timeout", "20000")
	values.Set("api", adjustPriceAPIName)
	values.Set("sessionOption", "AutoLoginOnly")
	return values.Encode()
}
