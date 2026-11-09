package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestSimulateToolsModeDryRun 模拟对话可以走完整的工具链：
// 改价只校验不执行，回复只返回给后台，且不写入真实对话历史。
func TestSimulateToolsModeDryRun(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			_ = json.NewEncoder(w).Encode(toolCallResponse(adjustOrderPriceToolName, `{"order_id":"order-sim-1","target_price_cents":9000}`))
			return
		}
		_ = json.NewEncoder(w).Encode(toolCallResponse(sendReplyToolName, `{"messages":["可以的","给你便宜10元"]}`))
	}))
	t.Cleanup(srv.Close)
	profileID := enableTestAI(t, s, srv.URL, "sim-ai")
	if _, err := s.DB.ExecContext(ctx, `UPDATE ai_profiles SET reply_mode='tools', agent_prompt='你是模拟卖家 {item_title}' WHERE id=?`, profileID); err != nil {
		t.Fatal(err)
	}

	replier := NewAIReplier("cid", s, nil)
	// 不注入真实改价客户端：模拟模式必须自行走 dry-run。
	res, err := replier.Simulate(ctx, AISimulationRequest{
		ProfileID:       profileID,
		SessionID:       "sess-1",
		Message:         "能便宜点吗",
		MockOrderID:     "order-sim-1",
		MockOrderStatus: "processing",
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if res.SessionID != "sess-1" || len(res.Segments) != 2 || res.ReplyText != "可以的\n给你便宜10元" || res.Skip {
		t.Fatalf("result=%+v", res)
	}
	if len(res.ToolCalls) != 2 || !res.ToolCalls[0].Success || !strings.Contains(res.ToolCalls[0].Result, "（模拟）") {
		t.Fatalf("tool_calls=%+v", res.ToolCalls)
	}
	if !strings.Contains(bodies[0]["messages"].([]any)[0].(map[string]any)["content"].(string), "order-sim-1") {
		t.Fatal("模拟系统提示词应包含模拟订单号")
	}

	var conversations int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_conversations`).Scan(&conversations); err != nil {
		t.Fatal(err)
	}
	if conversations != 0 {
		t.Fatalf("模拟不应写入真实对话历史，got %d", conversations)
	}
	var simulated int
	var sessionID string
	if err := s.DB.QueryRowContext(ctx, `SELECT simulated,session_id FROM ai_turn_logs WHERE ai_profile_id=?`, profileID).Scan(&simulated, &sessionID); err != nil {
		t.Fatal(err)
	}
	if simulated != 1 || sessionID != "sess-1" {
		t.Fatalf("turn log simulated=%d session=%q", simulated, sessionID)
	}
}

// TestSimulateSkipReply 模拟模式同样支持 skip_reply，并带回原因。
func TestSimulateSkipReply(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv, _ := mockOpenAIToolServer(t, toolCallResponse(skipReplyToolName, `{"reason":"只是表情"}`))
	profileID := enableTestAI(t, s, srv.URL, "sim-skip")
	if _, err := s.DB.ExecContext(ctx, `UPDATE ai_profiles SET reply_mode='tools' WHERE id=?`, profileID); err != nil {
		t.Fatal(err)
	}
	res, err := NewAIReplier("cid", s, nil).Simulate(ctx, AISimulationRequest{ProfileID: profileID, Message: "[微笑]"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skip || res.SkipReason != "只是表情" || res.ReplyText != "" {
		t.Fatalf("result=%+v", res)
	}
}

// TestExecuteSimulatedPriceTool 模拟改价与真实执行保持同一套参数/价格策略校验。
func TestExecuteSimulatedPriceTool(t *testing.T) {
	a := NewAIReplier("cid", nil, nil)
	profile := &db.AIProfile{MaxDiscountPercent: 10, MaxDiscountAmount: 20}
	order := &aiSimulationOrder{OrderID: "order-1", Status: "processing"}

	cases := []struct {
		name    string
		order   *aiSimulationOrder
		raw     string
		wantErr string
		want    string
	}{
		{"无模拟订单", nil, `{"order_id":"order-1","target_price_cents":9000}`, "没有关联", ""},
		{"订单号不一致", order, `{"order_id":"other","target_price_cents":9000}`, "不一致", ""},
		{"非待付款", &aiSimulationOrder{OrderID: "order-1", Status: "shipped"}, `{"order_id":"order-1","target_price_cents":9000}`, "不允许改价", ""},
		{"低于最低价", order, `{"order_id":"order-1","target_price_cents":8500}`, "目标价格必须在", ""},
		{"高于原价", order, `{"order_id":"order-1","target_price_cents":10100}`, "目标价格必须在", ""},
		{"非法 JSON", order, `{`, "不是有效 JSON", ""},
		{"成功", order, `{"order_id":"order-1","target_price_cents":9000}`, "", "（模拟）订单 order-1 将改价为 90.00 元，未实际调用平台接口"},
	}
	for _, tc := range cases {
		got, err := a.executeSimulatedPriceTool(tc.order, tc.raw, profile, 100)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("%s: err=%v want %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: got=%q err=%v want %q", tc.name, got, err, tc.want)
		}
	}
}

// TestSimulateRejectsOtherAccount 模拟入口不能读取其他账号的助手配置。
func TestSimulateRejectsOtherAccount(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	profileID := enableTestAI(t, s, "https://example.invalid/v1", "other-ai")
	if _, err := NewAIReplier("other-cookie", s, nil).Simulate(ctx, AISimulationRequest{ProfileID: profileID, Message: "在吗"}); err == nil || !strings.Contains(err.Error(), "不属于当前账号") {
		t.Fatalf("err=%v", err)
	}
}
