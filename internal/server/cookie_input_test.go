package server

import "testing"

func TestCanonicalCurlInputParsesMatchingCredential(t *testing.T) {
	input := `curl.exe "https://example.com" -H "Cookie: unb=123; _m_h5_tk=token_1; cookie2=value"`
	got, err := canonicalCurlInput("123", &input)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "unb=123; _m_h5_tk=token_1; cookie2=value" {
		t.Fatalf("canonical cookie=%v", got)
	}
}

func TestCanonicalCurlInputRejectsMismatchedCredential(t *testing.T) {
	input := `curl.exe "https://example.com" -H "Cookie: unb=other; _m_h5_tk=token_1"`
	if _, err := canonicalCurlInput("123", &input); err == nil {
		t.Fatal("expected account mismatch")
	}
}

func TestCanonicalCurlInputRejectsRawCookie(t *testing.T) {
	input := "unb=123; _m_h5_tk=token_1"
	if _, err := canonicalCurlInput("123", &input); err == nil {
		t.Fatal("new curl field must not accept raw Cookie")
	}
}

func TestCanonicalLegacyCookieInputKeepsCompatibility(t *testing.T) {
	input := "unb=123; _m_h5_tk=token_1; cookie2=value"
	got, err := canonicalLegacyCookieInput("123", &input)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != input {
		t.Fatalf("legacy cookie=%v", got)
	}
}

func TestCanonicalLegacyCookieInputRejectsMissingIdentity(t *testing.T) {
	input := "_m_h5_tk=token_1"
	if _, err := canonicalLegacyCookieInput("123", &input); err == nil {
		t.Fatal("expected missing identity error")
	}
}

func TestCurrentCredentialAccountIDPrefersStoredUNB(t *testing.T) {
	if got := currentCredentialAccountID("acc1", "unb=123; _m_h5_tk=old_1"); got != "123" {
		t.Fatalf("account id=%q", got)
	}
	if got := currentCredentialAccountID("acc1", "cookie2=old"); got != "acc1" {
		t.Fatalf("fallback account id=%q", got)
	}
}
