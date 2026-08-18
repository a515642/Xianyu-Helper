package curlparser

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		curl    string
		wantID  string
		wantErr bool
	}{
		{
			name: "Windows 格式 curl 命令",
			curl: `curl.exe ^"https://example.com^" ^
  -H ^"Cookie: unb=123456; _m_h5_tk=abc_def; cookie2=xyz^" ^
  -H ^"User-Agent: Mozilla/5.0^"`,
			wantID: "123456",
		},
		{
			name: "Unix 格式 curl 命令",
			curl: `curl "https://example.com" \
  -H 'Cookie: unb=789; _m_h5_tk=tk_value' \
  -H "User-Agent: Mozilla/5.0"`,
			wantID: "789",
		},
		{
			name:   "单行 curl 命令",
			curl:   `curl "https://example.com" -H "Cookie: unb=111; _m_h5_tk=222"`,
			wantID: "111",
		},
		{
			name:   "使用 --header",
			curl:   `curl "https://example.com" --header "Cookie: unb=abc; _m_h5_tk=def"`,
			wantID: "abc",
		},
		{
			name:   "使用 --cookie 参数",
			curl:   `curl "https://example.com" --cookie "unb=from-cookie; _m_h5_tk=token"`,
			wantID: "from-cookie",
		},
		{
			name:    "空命令",
			curl:    "",
			wantErr: true,
		},
		{
			name:    "缺少 Cookie header",
			curl:    `curl "https://example.com" -H "User-Agent: Mozilla/5.0"`,
			wantErr: true,
		},
		{
			name:    "Cookie 缺少 unb",
			curl:    `curl "https://example.com" -H "Cookie: _m_h5_tk=abc"`,
			wantErr: true,
		},
		{
			name:    "Cookie 缺少 _m_h5_tk",
			curl:    `curl "https://example.com" -H "Cookie: unb=123"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Parse(tt.curl)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.AccountID != tt.wantID {
				t.Errorf("Parse() AccountID = %v, want %v", result.AccountID, tt.wantID)
			}
		})
	}
}

func TestParseCookieString(t *testing.T) {
	got := parseCookieString("  unb = 123 ;  _m_h5_tk = abc  ")
	if got["unb"] != "123" || got["_m_h5_tk"] != "abc" {
		t.Fatalf("unexpected cookies: %#v", got)
	}
}

func TestNormalizeCurlCommand(t *testing.T) {
	got := normalizeCurlCommand("curl.exe ^\r\n  -H ^\r\n  test")
	if got != "curl.exe -H test" {
		t.Fatalf("normalizeCurlCommand() = %q", got)
	}
	got = normalizeCurlCommand("curl \\\n  -H \\\n  test")
	if got != "curl -H test" {
		t.Fatalf("normalizeCurlCommand() = %q", got)
	}
}
