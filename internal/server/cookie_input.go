package server

import (
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/curlparser"
)

func currentCredentialAccountID(accountID, currentCookie string) string {
	if unb := strings.TrimSpace(parseLegacyCookieInput(currentCookie)["unb"]); unb != "" {
		return unb
	}
	return strings.TrimSpace(accountID)
}

func canonicalCurlInput(expectedAccountID string, input *string) (*string, error) {
	if input == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*input)
	if value == "" {
		return nil, errors.New("curl 命令不能为空")
	}
	parsed, err := curlparser.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("解析 curl 命令失败: %w", err)
	}
	if parsed.AccountID != expectedAccountID {
		return nil, fmt.Errorf("curl 中的 unb=%s 与当前账号不匹配", parsed.AccountID)
	}
	return &parsed.RawCookie, nil
}

func canonicalLegacyCookieInput(expectedAccountID string, input *string) (*string, error) {
	if input == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*input)
	if value == "" {
		return nil, errors.New("Cookie 不能为空")
	}
	legacyID := strings.TrimSpace(parseLegacyCookieInput(value)["unb"])
	if legacyID == "" {
		return nil, errors.New("Cookie 缺少 unb")
	}
	if legacyID != expectedAccountID {
		return nil, fmt.Errorf("Cookie 中的 unb=%s 与当前账号不匹配", legacyID)
	}
	return &value, nil
}

func parseLegacyCookieInput(value string) map[string]string {
	cookies := make(map[string]string)
	for _, part := range strings.Split(value, ";") {
		key, item, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(key) != "" {
			cookies[strings.TrimSpace(key)] = strings.TrimSpace(item)
		}
	}
	return cookies
}
