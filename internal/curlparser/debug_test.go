package curlparser

import (
	"fmt"
	"testing"
)

func TestDebug(t *testing.T) {
	curl := `curl.exe "https://example.com" ^
  -H "Cookie: unb=123456; _m_h5_tk=abc_def; cookie2=xyz" ^
  -H "User-Agent: Mozilla/5.0"`
	
	normalized := normalizeCurlCommand(curl)
	fmt.Printf("Normalized: %q\n", normalized)
	
	cookie, err := extractCookieHeader(normalized)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Cookie: %q\n", cookie)
	
	cookies := parseCookieString(cookie)
	fmt.Printf("Cookies: %v\n", cookies)
}
