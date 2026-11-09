package engine

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestBuildAgentSystemPrompt 提示词优先级：助手覆盖 → 系统设置 → 内置模板；安全规则始终追加。
func TestBuildAgentSystemPrompt(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	replier := NewAIReplier("cid", s, nil)

	// 都未配置 → 内置模板。
	got := replier.buildAgentSystemPrompt(ctx, &db.AIProfile{}, "iPhone", 100, "手机", 0)
	if !strings.Contains(got, "你是闲鱼卖家的自动回复助手") || !strings.Contains(got, "工具调用约定") {
		t.Fatalf("默认模板缺失: %q", got)
	}

	// 系统设置生效。
	if err := s.Settings.Set(ctx, "ai_agent_prompt", "全局模板 {item_title}"); err != nil {
		t.Fatal(err)
	}
	got = replier.buildAgentSystemPrompt(ctx, &db.AIProfile{}, "iPhone", 100, "手机", 0)
	if !strings.Contains(got, "全局模板 iPhone") || !strings.Contains(got, "工具调用约定") {
		t.Fatalf("系统设置未生效: %q", got)
	}

	// 助手自定义优先于系统设置，并替换价格占位符。
	profile := &db.AIProfile{AgentPrompt: "助手模板 {item_price}", BargainStrategyEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 3}
	got = replier.buildAgentSystemPrompt(ctx, profile, "iPhone", 100, "手机", 1)
	if !strings.Contains(got, "助手模板 100.00") || strings.Contains(got, "全局模板") {
		t.Fatalf("助手提示词优先级错误: %q", got)
	}
	if !strings.Contains(got, "不可覆盖的价格安全规则") || !strings.Contains(got, "当前砍价轮次 1") {
		t.Fatalf("议价安全规则缺失: %q", got)
	}

	// 未启用议价时明确禁止降价。
	got = replier.buildAgentSystemPrompt(ctx, &db.AIProfile{AgentPrompt: "x"}, "iPhone", 100, "手机", 0)
	if !strings.Contains(got, "当前未启用议价策略") {
		t.Fatalf("未启用议价约束缺失: %q", got)
	}
}

// TestParseSendReplyLimits 校验分段数量、单段长度和违禁词替换。
func TestParseSendReplyLimits(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	replier := NewAIReplier("cid", s, nil)

	state := &agentTurnState{}
	if _, err := replier.parseSendReply(ctx, `{"messages":["一","二","三","四","五","六"]}`, state); err == nil {
		t.Fatal("超过 5 段应被拒绝")
	}
	if len(state.segments) != 0 {
		t.Fatalf("拒绝后不应写入分段: %+v", state.segments)
	}

	long := strings.Repeat("长", maxAgentSegmentRunes+1)
	if _, err := replier.parseSendReply(ctx, `{"messages":["`+long+`"]}`, state); err == nil {
		t.Fatal("超过单段长度应被拒绝")
	}

	if _, err := replier.parseSendReply(ctx, `{"messages":["","  你好  "]}`, state); err != nil {
		t.Fatal(err)
	}
	if len(state.segments) != 1 || state.segments[0] != "你好" {
		t.Fatalf("空段应被跳过: %+v", state.segments)
	}

	if err := s.AIProfiles.ReplaceForbiddenWords(ctx, []db.AIForbiddenWord{{Keyword: "微信", Replacement: "站内", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := replier.parseSendReply(ctx, `{"messages":["加微信聊"]}`, state); err != nil {
		t.Fatal(err)
	}
	if state.segments[len(state.segments)-1] != "加站内聊" {
		t.Fatalf("违禁词未替换: %+v", state.segments)
	}

	// 多次调用累计超过 5 段也要拒绝。
	state2 := &agentTurnState{segments: []string{"1", "2", "3", "4"}}
	if _, err := replier.parseSendReply(ctx, `{"messages":["5","6"]}`, state2); err == nil {
		t.Fatal("累计超过 5 段应被拒绝")
	}
}
