package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/db"
)

type recordingHumanNotifier struct {
	calls int
	last  []string
}

func (r *recordingHumanNotifier) NotifyHumanHandoff(accountID, buyerName, buyerID, itemID, chatID, reason string) {
	r.calls++
	r.last = []string{accountID, buyerName, buyerID, itemID, chatID, reason}
}

// TestNotifyHumanToolThroughAgent 工具模式可在回复前调用 notify_human。
func TestNotifyHumanToolThroughAgent(t *testing.T) {
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
			_ = json.NewEncoder(w).Encode(toolCallResponse(notifyHumanToolName, `{"reason":"买家要求人工处理售后"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(toolCallResponse(sendReplyToolName, `{"messages":["已帮您转给卖家"]}`))
	}))
	t.Cleanup(srv.Close)
	profileID := enableTestAI(t, s, srv.URL, "notify-ai")
	if _, err := s.DB.ExecContext(ctx, `UPDATE ai_profiles SET reply_mode='tools', notify_human_enabled=1 WHERE id=?`, profileID); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingHumanNotifier{}
	replier := NewAIReplier("cid", s, nil)
	replier.SetHumanHandoffNotifier(notifier)

	res, err := replier.Reply(ctx, chatMsg("我要找人工", "item1", "chat-notify"))
	if err != nil || res == nil || len(res.Texts) != 1 || res.Texts[0] != "已帮您转给卖家" {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier calls=%d want 1", notifier.calls)
	}
	if notifier.last[0] != "cid" || notifier.last[3] != "item1" || notifier.last[4] != "chat-notify" || notifier.last[5] != "买家要求人工处理售后" {
		t.Fatalf("notify fields=%v", notifier.last)
	}
	tools, _ := bodies[0]["tools"].([]any)
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		if tool, ok := raw.(map[string]any); ok {
			if fn, ok := tool["function"].(map[string]any); ok {
				names = append(names, fn["name"].(string))
			}
		}
	}
	if !strings.Contains(strings.Join(names, ","), notifyHumanToolName) {
		t.Fatalf("notify tool missing: %v", names)
	}
}

// TestNotifyHumanCooldown 同一会话 30 分钟内只通知一次。
func TestNotifyHumanCooldown(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	profileID := enableTestAI(t, s, "https://example.invalid/v1", "notify-cooldown")
	if _, err := s.DB.ExecContext(ctx, `UPDATE ai_profiles SET notify_human_enabled=1 WHERE id=?`, profileID); err != nil {
		t.Fatal(err)
	}
	profile, err := s.AIProfiles.Get(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingHumanNotifier{}
	replier := NewAIReplier("cid", s, nil)
	replier.SetHumanHandoffNotifier(notifier)
	m := chatMsg("我要人工", "item1", "chat-cooldown")
	call := openai.ToolCall{Function: openai.FunctionCall{Name: notifyHumanToolName, Arguments: `{"reason":"需要人工"}`}}

	first, err := replier.executeAgentTool(ctx, call, profile, 100, m, &agentTurnState{}, agentRunOptions{})
	if err != nil || first != "已通知卖家" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := replier.executeAgentTool(ctx, call, profile, 100, m, &agentTurnState{}, agentRunOptions{})
	if err != nil || !strings.Contains(second, "最近已通知") {
		t.Fatalf("second=%q err=%v", second, err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notifier calls=%d want 1", notifier.calls)
	}
}

// TestNotifyHumanDisabledAndSimulation 关闭时拒绝；模拟时只记录不发送。
func TestNotifyHumanDisabledAndSimulation(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	replier := NewAIReplier("cid", s, nil)
	notifier := &recordingHumanNotifier{}
	replier.SetHumanHandoffNotifier(notifier)
	call := openai.ToolCall{Function: openai.FunctionCall{Name: notifyHumanToolName, Arguments: `{"reason":"售后问题"}`}}
	m := chatMsg("要人工", "item1", "chat-disabled")

	if _, err := replier.executeAgentTool(ctx, call, &db.AIProfile{}, 100, m, &agentTurnState{}, agentRunOptions{}); err == nil {
		t.Fatal("未启用时调用转人工应失败")
	}

	simulated, err := replier.executeAgentTool(ctx, call, &db.AIProfile{NotifyHumanEnabled: true}, 100, m, &agentTurnState{}, agentRunOptions{simulation: &aiSimulationOrder{OrderID: "order-1", Status: "processing"}})
	if err != nil || !strings.Contains(simulated, "（模拟）") {
		t.Fatalf("simulated=%q err=%v", simulated, err)
	}
	if notifier.calls != 0 {
		t.Fatalf("模拟不应真实通知，calls=%d", notifier.calls)
	}
}
