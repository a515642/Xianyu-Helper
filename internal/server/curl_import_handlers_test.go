package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportCookieFromCurl(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	session := loginHelper(t, srv.Router())
	curl := `curl.exe ^"https://h5api.m.goofish.com/example^" ^
  -H ^"Cookie: unb=998877; _m_h5_tk=token_123; cookie2=cookie-value^"`
	body, err := json.Marshal(map[string]string{"curl": curl})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/cookies/import-curl", strings.NewReader(string(body)))
	request.AddCookie(session)
	recorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	value, err := store.Cookies.GetValue(context.Background(), "998877")
	if err != nil || !strings.Contains(value, "_m_h5_tk=token_123") {
		t.Fatalf("value=%q err=%v", value, err)
	}
	detail, err := store.Cookies.GetDetails(context.Background(), "998877")
	if err != nil || detail.LoginMethod != loginMethodCurl {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}

func TestImportCookieFromCurlRejectsMissingCredential(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	session := loginHelper(t, srv.Router())
	body, err := json.Marshal(map[string]string{"curl": `curl -H "Cookie: unb=1"`})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/cookies/import-curl", strings.NewReader(string(body)))
	request.AddCookie(session)
	recorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
