package aiendpoint

import "testing"

// TestNormalizeBaseURLDomainOnly 只填域名时补全 /v1；空值不再回退默认地址。
func TestNormalizeBaseURLDomainOnly(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com", "https://api.openai.com/v1"},
		{"api.openai.com", "https://api.openai.com/v1"},
		{"https://api.openai.com/", "https://api.openai.com/v1"},
		{"https://api.deepseek.com/v1", "https://api.deepseek.com/v1"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		{"https://host/custom/prefix", "https://host/custom/prefix"},
	}
	for _, tc := range cases {
		got, err := NormalizeBaseURL(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizeBaseURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// TestNormalizeBaseURLEmptyIsError 不提供默认地址。
func TestNormalizeBaseURLEmptyIsError(t *testing.T) {
	if _, err := NormalizeBaseURL("   "); err == nil {
		t.Fatal("空地址应报错，而不是回退默认服务商")
	}
	if _, err := ChatCompletionsURL(""); err == nil {
		t.Fatal("空地址不应生成 chat/completions 地址")
	}
	got, err := ChatCompletionsURL("https://api.openai.com")
	if err != nil || got != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("ChatCompletionsURL = %q, %v", got, err)
	}
}
