package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("token=" + CSRFToken(r)))
})

func TestCSRFIssuesTokenOnSafeRequest(t *testing.T) {
	h := CSRF(false)(ok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	res := rec.Result()
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("csrf cookie not set")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie not hardened: %+v", cookie)
	}
	if rec.Body.String() != "token="+cookie.Value {
		t.Fatalf("token not exposed to handler: %q", rec.Body.String())
	}

	// An existing cookie is reused.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if len(rec.Result().Cookies()) != 0 || rec.Body.String() != "token="+cookie.Value {
		t.Fatal("existing token should be reused")
	}
}

func TestCSRFRejectsUnsafeWithoutToken(t *testing.T) {
	h := CSRF(false)(ok)
	token := strings.Repeat("a", 43)

	cases := []struct {
		name   string
		cookie string
		header string
		want   int
	}{
		{"no cookie no header", "", "", http.StatusForbidden},
		{"cookie only", token, "", http.StatusForbidden},
		{"header only", "", token, http.StatusForbidden},
		{"mismatch", token, strings.Repeat("b", 43), http.StatusForbidden},
		{"match", token, token, http.StatusOK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/x", strings.NewReader("{}"))
		if c.cookie != "" {
			req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: c.cookie})
		}
		if c.header != "" {
			req.Header.Set(CSRFHeaderName, c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestCSRFRejectsCrossSiteBrowserRequest(t *testing.T) {
	h := CSRF(false)(ok)
	token := strings.Repeat("a", 43)
	req := httptest.NewRequest(http.MethodDelete, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	req.Header.Set(CSRFHeaderName, token)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site request allowed: %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	h := rec.Header()
	if !strings.Contains(h.Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("CSP missing")
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatal("security headers missing")
	}
}

func TestRecover(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(buf.String(), "boom") {
		t.Fatalf("panic not recovered: %d %s", rec.Code, buf.String())
	}
}

func TestChainOrder(t *testing.T) {
	var order []string
	mw := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	Chain(ok, mw("a"), mw("b")).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Join(order, "") != "ab" {
		t.Fatalf("wrong order %v", order)
	}
}
