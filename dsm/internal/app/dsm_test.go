package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureDSMAuth(t *testing.T) *dsmAuthenticator {
	t.Helper()
	script := filepath.Join(t.TempDir(), "authenticate.cgi")
	source := `#!/bin/sh
[ "$REMOTE_ADDR" = 192.0.2.10 ] || exit 1
[ "$SERVER_ADDR" = 192.0.2.20 ] || exit 1
[ "$SERVER_PORT" = 5001 ] || exit 1
[ "$HTTPS" = on ] || exit 1
[ "$HTTP_X_SYNO_TOKEN" = dsm-test-token ] || exit 1
[ "$QUERY_STRING" = SynoToken=dsm-test-token ] || exit 1
case "$HTTP_COOKIE" in id=valid) printf vlad ;; id=ordinary) printf guest ;; id=invalid-output) printf 'vlad\nadmin' ;; esac
`
	if err := os.WriteFile(script, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	return &dsmAuthenticator{cgiPath: script, isAdmin: func(name string) bool { return name == "vlad" }}
}

func dsmRequest(h http.Handler, method, path, body, cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://nas.example:5001"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Real-IP", "192.0.2.10")
	r.Header.Set("X-UPS-Server-Addr", "192.0.2.20")
	r.Header.Set("X-SYNO-TOKEN", "dsm-test-token")
	r.Header.Set("X-UPS-Request", "1")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDSMSSOAndAdministratorBoundary(t *testing.T) {
	s, _, token := testServer(t)
	s.dsmAuth = fixtureDSMAuth(t)
	h := s.DSMHandler()
	for _, path := range []string{"settings", "status", "logs", "usb", "diagnostics", "session"} {
		if w := dsmRequest(h, "GET", "/api/"+path, "", "", ""); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, w.Code)
		}
		if w := dsmRequest(h, "GET", "/api/"+path, "", "id=ordinary", ""); w.Code != 403 {
			t.Fatalf("ordinary user %s: %d", path, w.Code)
		}
		if w := dsmRequest(h, "GET", "/api/"+path, "", "id=valid", ""); w.Code != 200 {
			t.Fatalf("administrator %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	session := dsmRequest(h, "GET", "/api/session", "", "id=valid", "")
	if !strings.Contains(session.Body.String(), `"username":"vlad"`) || len(session.Result().Cookies()) != 0 {
		t.Fatal("SSO created a separate login session")
	}
	if w := dsmRequest(h, "POST", "/api/login", fmt.Sprintf(`{"token":%q}`, token), "", "https://nas.example:5001"); w.Code != 404 {
		t.Fatal("standalone login must not authorize DSM")
	}
	if w := dsmRequest(h, "POST", "/api/settings", `{}`, "id=valid", "https://evil.example"); w.Code != 403 {
		t.Fatal("SSO bypassed Origin validation")
	}
	r := httptest.NewRequest("GET", "https://nas.example:5001/api/settings", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("standalone token bypassed DSM SSO")
	}
	r.Header.Set("Cookie", "id=valid")
	r.Header.Set("X-Real-IP", "192.0.2.10")
	r.Header.Set("X-UPS-Server-Addr", "192.0.2.20")
	r.Header.Set("X-SYNO-TOKEN", "dsm-test-token")
	r.RemoteAddr = "192.0.2.10:1234"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("network request accepted by private DSM handler")
	}
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Proto", "http")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("insecure proxy accepted")
	}
	if w := dsmRequest(h, "GET", "/api/settings", "", "id=invalid-output", ""); w.Code != 401 {
		t.Fatal("invalid CGI output accepted")
	}
}

func TestDSMSSOCacheExpiresAndFailsClosed(t *testing.T) {
	a := fixtureDSMAuth(t)
	r := httptest.NewRequest("GET", "https://nas.example:5001/api/settings", nil)
	r.Header.Set("Cookie", "id=valid")
	r.Header.Set("X-Real-IP", "192.0.2.10")
	r.Header.Set("X-UPS-Server-Addr", "192.0.2.20")
	r.Header.Set("X-SYNO-TOKEN", "dsm-test-token")
	if name, err := a.authenticate(r); err != nil || name != "vlad" {
		t.Fatal(name, err)
	}
	if err := os.Remove(a.cgiPath); err != nil {
		t.Fatal(err)
	}
	if _, err := a.authenticate(r); err != nil {
		t.Fatal("validated cache not used", err)
	}
	a.validUntil = time.Now().Add(-time.Second)
	if _, err := a.authenticate(r); err == nil {
		t.Fatal("expired cache authorized after CGI failure")
	}
	r.Header.Set("Cookie", "id=other")
	if _, err := a.authenticate(r); err == nil {
		t.Fatal("changed session used old authorization")
	}
}
