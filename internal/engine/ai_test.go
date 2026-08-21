package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestBuildSystemPrompt 自定义 prompt 替换变量，且始终追加价格与轮次安全约束。
func TestBuildSystemPrompt(t *testing.T) {
	got := buildSystemPrompt("你是卖{item_title}的客服，价格{item_price}，保留{{unknown}}", "iPhone", 100, "手机", 0, 0, 3, 1)
	got = buildSystemPrompt("你是卖{{item_title}}的客服，价格{{item_price}}，详情{{item_description}}", "iPhone", 100, "手机", 0, 0, 3, 1)
	if !strings.Contains(got, "你是卖iPhone的客服，价格100.00，详情手机") {
		t.Fatalf("双大括号 prompt 替换: got %q", got)
	}
	got = buildSystemPrompt("你是卖{item_title}的客服，价格{item_price}，保留{{unknown}}", "iPhone", 100, "手机", 0, 0, 3, 1)
	if !strings.Contains(got, "你是卖iPhone的客服，价格100.00") {
		t.Fatalf("自定义 prompt 替换: got %q", got)
	}
	if !strings.Contains(got, "{{unknown}}") {
		t.Fatalf("未知模板变量应保持原样: got %q", got)
	}
	if !strings.Contains(got, "任一优惠上限为 0 时不得降价") || !strings.Contains(got, "当前砍价轮次 1") {
		t.Fatalf("自定义 prompt 缺少安全约束: %q", got)
	}

	// 0 必须保留为不允许优惠，不能静默改成默认值。
	got = buildSystemPrompt("", "会员卡", 9.9, "月卡", 0, 0, 3, 0)
	if !strings.Contains(got, "标题：会员卡") || !strings.Contains(got, "价格：9.90 元") {
		t.Fatalf("默认模板缺商品信息: %q", got)
	}
	if !strings.Contains(got, "最多优惠 0%") || !strings.Contains(got, "最多优惠 0 元") {
		t.Fatalf("零折扣配置被改写: %q", got)
	}

	// 显式折扣上限。
	got = buildSystemPrompt("", "会员卡", 9.9, "月卡", 20, 50, 4, 2)
	if !strings.Contains(got, "最多优惠 20%") || !strings.Contains(got, "最多优惠 50 元") {
		t.Fatalf("显式折扣上限: %q", got)
	}
}

func TestBargainStrategyDisabledUsesNormalAIReply(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, 0, "普通回复")
	id := enableTestAI(t, s, srv.URL, "no-bargain")
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_profiles SET bargain_strategy_enabled=0 WHERE id=?`, id)
	res, err := NewAIReplier("cid", s, nil).Reply(ctx, chatMsg("能便宜点吗", "item1", "chat-no-bargain"))
	if err != nil || res == nil || res.Skip || res.Text != "普通回复" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// newAIStore 构造一个带 admin + cookie 的测试 store，供 AIReplier 使用。
func newAIStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "ai.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d, db.DialectSQLite)
	ctx := context.Background()
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	s.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk;", admin.ID)
	s.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,item_price,item_description) VALUES ('cid','item1','测试商品','100','测试描述')`)
	return s, func() { d.Close() }
}

func enableTestAI(t *testing.T, s *db.Store, baseURL string, profileName string) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := s.AIProfiles.Create(ctx, db.AIProfile{CookieID: "cid", Name: profileName, Enabled: true, UseSystemAPI: false, BargainStrategyEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AIProfiles.Update(ctx, db.AIProfile{ID: id, CookieID: "cid", Name: profileName, Enabled: true, UseSystemAPI: false, BaseURL: baseURL, BargainStrategyEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 1}, strPtr("sk-test"), false); err != nil {
		t.Fatal(err)
	}
	if err := s.AIProfiles.ReplaceItems(ctx, id, "cid", []string{"item1"}); err != nil {
		t.Fatal(err)
	}
	return id
}

func strPtr(value string) *string { return &value }

// TestAIReply_DisabledReturnsNil AI 未启用 / 无 APIKey 时应返回 nil,nil（降级到下一级）。
func TestAIReply_DisabledReturnsNil(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	a := NewAIReplier("cid", s, nil)

	// 无配置记录 → 未启用 → nil。
	res, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("未配置应返回 nil,nil: res=%+v err=%v", res, err)
	}

	// 配置但 ai_enabled=0 → nil。
	// No product AI profile is bound, so the new product-scoped path is disabled.
	res, err = a.Reply(ctx, chatMsg("在吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("未启用应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// TestAIReply_NoAPIKeyReturnsNil 启用 AI 但全局未配 APIKey → nil（不报错降级）。
func TestAIReply_NoAPIKeyReturnsNil(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	id, err := s.AIProfiles.Create(ctx, db.AIProfile{CookieID: "cid", Name: "test-ai", Enabled: true, UseSystemAPI: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AIProfiles.ReplaceItems(ctx, id, "cid", []string{"item1"}); err != nil {
		t.Fatal(err)
	}
	a := NewAIReplier("cid", s, nil)

	res, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("无 APIKey 应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// mockOpenAIServer 启动一个返回固定 chat completion 响应的 HTTP 服务。
// status=0 表示返回成功响应；其余为 HTTP 状态码（用于失败降级测试）。
func mockOpenAIServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			http.Error(w, "upstream error", status)
			return
		}
		resp := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAIReply_HTTPErrorDegrades AI 调用 HTTP 500 → 返回错误（上层降级到默认回复）。
func TestAIReply_HTTPErrorDegrades(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, http.StatusInternalServerError, "")

	// 启用商品 AI + 配 APIKey + 指向 mock 服务。
	id := enableTestAI(t, s, srv.URL, "error-ai")
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_profiles SET bargain_strategy_enabled=1 WHERE id=?`, id)

	a := NewAIReplier("cid", s, nil)
	res, err := a.Reply(ctx, chatMsg("还能优惠吗", "item1", "chat1"))
	if err == nil {
		t.Fatalf("HTTP 500 应返回错误，got res=%+v", res)
	}
	if res != nil {
		t.Fatalf("失败时不应返回结果: %+v", res)
	}
	history, historyErr := s.AIReply.ConversationHistory(ctx, "cid", "chat1", "item1", 10)
	if historyErr != nil || len(history) != 0 {
		t.Fatalf("上游失败不应写入半轮历史: history=%+v err=%v", history, historyErr)
	}
}

// TestAIReply_EmptyChoicesReturnsNil 成功响应但无 choices → nil（不报错）。
func TestAIReply_EmptyChoicesReturnsNil(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	t.Cleanup(srv.Close)
	id := enableTestAI(t, s, srv.URL, "test-ai")
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_profiles SET bargain_strategy_enabled=1 WHERE id=?`, id)

	a := NewAIReplier("cid", s, nil)
	res, err := a.Reply(ctx, chatMsg("可以便宜一点吗", "item1", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("空 choices 应返回 nil,nil: res=%+v err=%v", res, err)
	}
}

// TestAIReply_SuccessReturnsContent 正常调用返回 AI 文本。
func TestAIReply_SuccessReturnsContent(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, 0, "你好，在的哦")
	id := enableTestAI(t, s, srv.URL, "test-ai")
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_profiles SET bargain_strategy_enabled=1 WHERE id=?`, id)

	a := NewAIReplier("cid", s, nil)
	res, err := a.Reply(ctx, chatMsg("最低多少钱", "item1", "chat1"))
	if err != nil {
		t.Fatalf("成功调用不应报错: %v", err)
	}
	if res == nil || res.Text != "你好，在的哦" {
		t.Fatalf("应返回 AI 文本: %+v", res)
	}
}

func TestAIReply_ThinkingModeDoesNotForceFunctionToolChoice(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	var requestBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "思考模式回复"},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	id := enableTestAI(t, s, srv.URL, "thinking-tool-ai")
	_, err := s.DB.ExecContext(ctx, `UPDATE ai_profiles SET thinking_mode='enabled' WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}

	replier := NewAIReplier("cid", s, nil)
	replier.SetOrderPriceAdjuster(&diagnosticPriceAdjuster{})
	result, err := replier.Reply(ctx, chatMsg("可以便宜吗", "item1", "thinking-tool-chat"))
	if err != nil || result == nil || result.Text != "思考模式回复" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, ok := requestBody["tool_choice"]; ok {
		t.Fatalf("thinking mode must not force function tool_choice: %#v", requestBody["tool_choice"])
	}
	tools, ok := requestBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("thinking mode should retain the price tool: %#v", requestBody["tools"])
	}
}

func TestAIReply_NonBargainBoundItemAndForbiddenWords(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, 0, "可以加微信沟通")
	profileID := enableTestAI(t, s, srv.URL, "general-ai")
	if err := s.AIProfiles.ReplaceForbiddenWords(ctx, []db.AIForbiddenWord{{Keyword: "微信", Replacement: "站内", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	res, err := NewAIReplier("cid", s, nil).Reply(ctx, chatMsg("在吗，什么时候发货", "item1", "chat-general"))
	if err != nil || res == nil || res.Text != "可以加站内沟通" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	history, err := s.AIReply.ProfileConversationHistory(ctx, profileID, "cid", "chat-general", "item1", 10)
	if err != nil || len(history) != 2 || history[1].Content != res.Text {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestAIReplyExitPersistsContextWithoutSending(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, 0, "无需回复 <exit>")
	profileID := enableTestAI(t, s, srv.URL, "silent-ai")
	res, err := NewAIReplier("cid", s, nil).Reply(ctx, chatMsg("谢谢", "item1", "chat-silent"))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.Skip || res.Text != "" {
		t.Fatalf("silent result=%+v", res)
	}
	history, err := s.AIReply.ProfileConversationHistory(ctx, profileID, "cid", "chat-silent", "item1", 10)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if history[1].Content != "无需回复 <exit>" || history[1].Intent != "silent_exit" {
		t.Fatalf("assistant history=%+v", history[1])
	}
}

func TestAIReply_UnboundItemFallsThrough(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()

	res, err := NewAIReplier("cid", s, nil).Reply(ctx, chatMsg("在吗，什么时候发货", "unbound-item", "chat1"))
	if err != nil || res != nil {
		t.Fatalf("未绑定商品应交给默认回复: res=%+v err=%v", res, err)
	}
}

// TestGlobalAIConfig 默认值兜底 + 显式设置覆盖。
func TestGlobalAIConfig(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	a := NewAIReplier("cid", s, nil)

	// 全空 → 默认 BaseURL + Model。
	cfg, err := a.globalAIConfig(ctx)
	if err != nil {
		t.Fatalf("globalAIConfig: %v", err)
	}
	if cfg.BaseURL != defaultAIBaseURL || cfg.Model != defaultAIModel || cfg.APIKey != "" {
		t.Fatalf("默认值异常: %+v", cfg)
	}

	// 显式设置。
	s.Settings.Set(ctx, "ai_api_key", "sk-x")
	s.Settings.Set(ctx, "ai_api_url", "https://example.com/v1/")
	s.Settings.Set(ctx, "ai_model", "gpt-4o")
	cfg, _ = a.globalAIConfig(ctx)
	if cfg.APIKey != "sk-x" || cfg.BaseURL != "https://example.com/v1" || cfg.Model != "gpt-4o" {
		t.Fatalf("显式设置异常: %+v", cfg)
	}
}

// TestAIReplierItemInfo 商品缺失时兜底占位；存在时取真实标题/价格/描述。
func TestAIReplierItemInfo(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	a := NewAIReplier("cid", s, nil)

	// 商品不存在 → 占位。
	title, price, desc := a.itemInfo(ctx, "no-such-item")
	if title != "商品信息获取失败" || desc != "暂无商品描述" || price != 0 {
		t.Fatalf("缺失商品应兜底: title=%q price=%v desc=%q", title, price, desc)
	}

	// 更新商品。
	s.DB.ExecContext(ctx, `UPDATE item_info SET item_title='会员卡',item_price='9.90',item_description='用户编辑描述',item_detail='原始详情' WHERE cookie_id='cid' AND item_id='item1'`)
	title, price, desc = a.itemInfo(ctx, "item1")
	if title != "会员卡" || price != 9.9 || desc != "用户编辑描述" {
		t.Fatalf("真实商品: title=%q price=%v desc=%q", title, price, desc)
	}
}

func TestAIReplyTracksBargainRoundsAndBlocksUnsafePrice(t *testing.T) {
	s, cleanup := newAIStore(t)
	defer cleanup()
	ctx := context.Background()
	srv := mockOpenAIServer(t, 0, "可以，80 元成交")
	s.DB.ExecContext(ctx, `UPDATE item_info SET item_title='商品',item_price='100',item_description='描述' WHERE cookie_id='cid' AND item_id='item1'`)
	profileID := enableTestAI(t, s, srv.URL, "bargain-ai")
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_profiles SET bargain_strategy_enabled=0 WHERE id=?`, profileID)
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_profiles SET bargain_strategy_enabled=1 WHERE id=?`, profileID)

	a := NewAIReplier("cid", s, nil)
	first, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat1"))
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.Text != "可以，80 元成交" {
		t.Fatalf("开启策略时应保留模型原始文本: %+v", first)
	}
	second, err := a.Reply(ctx, chatMsg("再便宜一点", "item1", "chat1"))
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.Text != "可以，80 元成交" {
		t.Fatalf("策略开启时应由模型决定回复: %+v", second)
	}
	count, err := s.AIReply.ProfileBargainCount(ctx, profileID, "cid", "chat1", "item1")
	if err != nil || count != 0 {
		t.Fatalf("不再由正则统计 bargain count=%d err=%v", count, err)
	}
	history, err := s.AIReply.ProfileConversationHistory(ctx, profileID, "cid", "chat1", "item1", 10)
	if err != nil || len(history) != 4 {
		t.Fatalf("history len=%d err=%v", len(history), err)
	}
}
