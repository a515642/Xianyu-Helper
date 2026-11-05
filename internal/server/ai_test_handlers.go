package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/aiendpoint"
)

const (
	aiTestPrompt           = "hi"
	aiTestMaxResponseBytes = 1 << 20
	// 连通性测试只有这么点预算：如果模型把 token 全花在思考链上，
	// content 会返回空，界面就会看不出到底通没通。
	aiTestMaxTokens = 256
)

// testAIConnection 用当前（或请求里传入的）AI 配置发一条 "hi" 做连通性测试。
// body: {base_url?, api_key?, model?}；缺省项回落到系统设置。
func (s *Server) testAIConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		v, err := s.Store.Settings.Get(r.Context(), "ai_api_url")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取AI地址失败")
			return
		}
		baseURL = v
	}
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		v, err := s.Store.Settings.Get(r.Context(), "ai_api_key")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取AI Key失败")
			return
		}
		apiKey = v
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		v, err := s.Store.Settings.Get(r.Context(), "ai_model")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取AI模型失败")
			return
		}
		model = v
	}
	if strings.TrimSpace(apiKey) == "" {
		writeErr(w, http.StatusBadRequest, "请先填写 API Key")
		return
	}
	if strings.TrimSpace(model) == "" {
		writeErr(w, http.StatusBadRequest, "请先选择模型")
		return
	}

	normalized, normErr := aiendpoint.NormalizeBaseURL(baseURL)
	if normErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": normErr.Error()})
		return
	}
	result, err := requestAIChatCompletion(r.Context(), normalized, apiKey, model, aiTestPrompt)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "endpoint": result.Endpoint})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"endpoint":   result.Endpoint,
		"model":      model,
		"reply":      result.Reply,
		"latency_ms": result.LatencyMs,
	})
}

type aiChatTestResult struct {
	Endpoint  string
	Reply     string
	LatencyMs int64
}

// requestAIChatCompletion 直接调用 OpenAI 兼容的 chat/completions 接口（发送 hi）。
// 不走 SDK，便于把非 2xx 响应体原样回传，帮助用户定位配置问题。
func requestAIChatCompletion(ctx context.Context, baseURL, apiKey, model, prompt string) (aiChatTestResult, error) {
	result := aiChatTestResult{}
	endpoint, err := aiendpoint.ChatCompletionsURL(baseURL)
	if err != nil {
		return result, err
	}
	result.Endpoint = endpoint
	payload, err := json.Marshal(map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0,
		// 深度思考模型会把预算先花在思考链上；给足预算并显式关闭思考，
		// 否则 content 常为空，测试看起来像"没回复"。
		"max_tokens": aiTestMaxTokens,
		"thinking":   map[string]string{"type": "disabled"},
	})
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client, err := newSettingsOutboundHTTPClient(endpoint)
	if err != nil {
		return result, fmt.Errorf("AI API 地址无效: %w", err)
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	result.LatencyMs = time.Since(started).Milliseconds()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, aiTestMaxResponseBytes))
	if err != nil {
		return result, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300))
	}
	var decoded struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return result, fmt.Errorf("解析响应失败: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return result, fmt.Errorf("响应中没有 choices")
	}
	choice := decoded.Choices[0]
	result.Reply = strings.TrimSpace(choice.Message.Content)
	if result.Reply == "" {
		// 内容为空时给出明确原因，避免让人误以为是"模型刚好回了 hi"。
		reasoning := strings.TrimSpace(choice.Message.ReasoningContent)
		switch {
		case choice.FinishReason == "length" && reasoning != "":
			result.Reply = fmt.Sprintf("模型只返回了思考内容、正文被截断（思考片段：%s）", truncate(reasoning, 120))
		case reasoning != "":
			result.Reply = "模型只返回了思考内容，没有正文"
		default:
			result.Reply = "模型返回了空内容"
		}
	}
	return result, nil
}
