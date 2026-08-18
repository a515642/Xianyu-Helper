// Package curlparser 解析 curl 命令并提取闲鱼登录 Cookie。
package curlparser

import (
	"errors"
	"fmt"
	"strings"
)

// ParsedCurl 表示从 curl 命令中解析出的登录凭证。
type ParsedCurl struct {
	AccountID string            `json:"account_id"` // unb 值，作为账号唯一标识
	Cookies   map[string]string `json:"cookies"`    // 解析后的 cookie map
	RawCookie string            `json:"raw_cookie"` // 原始 cookie 字符串
}

// requiredCookieFields 是闲鱼登录必须包含的 cookie 字段。
var requiredCookieFields = []string{"unb", "_m_h5_tk"}

// Parse 解析 curl 命令并提取 Cookie。
// 支持 Windows（^ 换行）和 Unix（\ 换行）格式。
func Parse(curlCommand string) (*ParsedCurl, error) {
	if strings.TrimSpace(curlCommand) == "" {
		return nil, errors.New("curl 命令不能为空")
	}

	// 规范化：移除换行符和续行符
	normalized := normalizeCurlCommand(curlCommand)

	// 提取 Cookie header
	cookieValue, err := extractCookieHeader(normalized)
	if err != nil {
		return nil, err
	}

	// 解析 cookie 字符串为 map
	cookies := parseCookieString(cookieValue)
	if len(cookies) == 0 {
		return nil, errors.New("Cookie 为空或格式错误")
	}

	// 验证必要字段
	for _, field := range requiredCookieFields {
		if strings.TrimSpace(cookies[field]) == "" {
			return nil, fmt.Errorf("Cookie 缺少必要字段: %s", field)
		}
	}

	accountID := strings.TrimSpace(cookies["unb"])
	if accountID == "" {
		return nil, errors.New("unb 字段为空，无法确定账号 ID")
	}

	return &ParsedCurl{
		AccountID: accountID,
		Cookies:   cookies,
		RawCookie: cookieValue,
	}, nil
}

// normalizeCurlCommand 规范化 curl 命令，移除 Windows/Unix 续行符。
// Windows 下从开发者工具复制的命令通常包含 ^"、^&、^% 等转义，
// 这里仅还原命令文本，不执行任何 shell 命令。
func normalizeCurlCommand(s string) string {
	var normalized strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '^' {
			if i+1 >= len(s) {
				continue
			}
			if s[i+1] == '\r' {
				i++
				if i+1 < len(s) && s[i+1] == '\n' {
					i++
				}
				normalized.WriteByte(' ')
				continue
			}
			if s[i+1] == '\n' {
				i++
				normalized.WriteByte(' ')
				continue
			}
			normalized.WriteByte(s[i+1])
			i++
			continue
		}
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == '\r' {
				i++
				if i+1 < len(s) && s[i+1] == '\n' {
					i++
				}
				normalized.WriteByte(' ')
				continue
			}
			if s[i+1] == '\n' {
				i++
				normalized.WriteByte(' ')
				continue
			}
		}
		normalized.WriteByte(s[i])
	}

	// 续行符之外的换行只是参数间分隔；引号内的空格必须保留。
	normalizedCommand := normalized.String()
	var compact strings.Builder
	inQuote := byte(0)
	spacePending := false
	for i := 0; i < len(normalizedCommand); i++ {
		ch := normalizedCommand[i]
		if inQuote == 0 && (ch == '"' || ch == '\'') {
			if spacePending && compact.Len() > 0 {
				compact.WriteByte(' ')
			}
			inQuote = ch
			spacePending = false
			compact.WriteByte(ch)
			continue
		}
		if inQuote != 0 && ch == inQuote {
			inQuote = 0
			spacePending = false
			compact.WriteByte(ch)
			continue
		}
		if inQuote == 0 && (ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n') {
			spacePending = true
			continue
		}
		if spacePending && compact.Len() > 0 {
			compact.WriteByte(' ')
		}
		spacePending = false
		compact.WriteByte(ch)
	}
	return strings.TrimSpace(compact.String())
}

// tokenizeCurlCommand 将命令分为参数。只做文本解析，绝不调用 shell 或 curl。
func tokenizeCurlCommand(command string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if token.Len() > 0 {
			tokens = append(tokens, token.String())
			token.Reset()
		}
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			token.WriteByte(ch)
			escaped = false
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			if ch == '\\' && quote == '"' && i+1 < len(command) {
				escaped = true
				continue
			}
			token.WriteByte(ch)
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case ' ', '\t', '\r', '\n':
			flush()
		case '\\':
			escaped = true
		default:
			token.WriteByte(ch)
		}
	}
	if escaped {
		token.WriteByte('\\')
	}
	if quote != 0 {
		return nil, errors.New("curl 命令包含未闭合引号")
	}
	flush()
	return tokens, nil
}

// extractCookieHeader 从 -H/--header 或 -b/--cookie 参数中提取 Cookie。
func extractCookieHeader(curlCommand string) (string, error) {
	tokens, err := tokenizeCurlCommand(curlCommand)
	if err != nil {
		return "", err
	}
	var values []string
	for i := 0; i < len(tokens); i++ {
		option := strings.ToLower(tokens[i])
		if option != "-h" && option != "--header" && option != "-b" && option != "--cookie" {
			continue
		}
		if i+1 >= len(tokens) {
			continue
		}
		value := tokens[i+1]
		i++
		if option == "-b" || option == "--cookie" {
			if strings.HasPrefix(value, "@") {
				continue // 不读取本地文件，避免导入行为产生副作用。
			}
			values = append(values, value)
			continue
		}
		name, headerValue, ok := strings.Cut(value, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "cookie") {
			values = append(values, strings.TrimSpace(headerValue))
		}
	}
	if len(values) == 0 {
		return "", errors.New("未找到 Cookie header，请确保 curl 命令包含 -H \"Cookie: ...\"")
	}
	return strings.Join(values, "; "), nil
}

// parseCookieString 将 "k1=v1; k2=v2" 形式的 cookie 字符串解析为 map。
func parseCookieString(cookieStr string) map[string]string {
	cookies := make(map[string]string)
	if cookieStr == "" {
		return cookies
	}

	for _, part := range strings.Split(cookieStr, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" {
			cookies[key] = value
		}
	}

	return cookies
}
