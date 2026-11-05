// Package aiendpoint 统一 AI 助手（OpenAI 兼容）地址的规范化与补全规则。
//
// 用户在系统设置里只需要填写域名，例如 https://api.openai.com 或
// https://dashscope.aliyuncs.com；本包负责补全 /v1 前缀，SDK 再补全
// /chat/completions，因此最终请求地址是 域名 + /v1/chat/completions。
package aiendpoint

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// NormalizeBaseURL 把用户填写的地址规范化为 SDK 需要的 base URL。
//
// 规则：
//   - 只写域名（https://api.openai.com）→ 补 /v1；
//   - 已带路径（https://host/v1、https://host/compatible-mode/v1）→ 原样保留；
//   - 末尾多余的 / 会被去掉；
//   - 空值返回错误：不提供默认地址，必须由用户显式填写。
func NormalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("请先在系统设置里填写 AI API 地址")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("AI API 地址无效: %w", err)
	}
	if parsed.Hostname() == "" {
		return "", errors.New("AI API 地址缺少主机名")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/v1"
		parsed.RawPath = ""
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	normalized := strings.TrimRight(parsed.String(), "/")
	return normalized, nil
}

// ChatCompletionsURL 返回规范化后的完整对话补全地址，用于连通性测试与日志。
func ChatCompletionsURL(raw string) (string, error) {
	base, err := NormalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	return base + "/chat/completions", nil
}
