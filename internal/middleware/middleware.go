// Package middleware provides HTTP middleware: security headers, CSRF
// protection, panic recovery and request logging.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Chain applies middleware so that the first one listed is the outermost.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// ContentSecurityPolicy used for all responses. Scripts and styles may only
// come from the application itself; images may be loaded from the web
// because Markdown documents commonly embed remote images.
const ContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' https: http: data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// SecurityHeaders sets defensive response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", ContentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// Recover turns panics into 500 responses.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					logger.Error("panic", "err", v, "path", r.URL.Path, "stack", string(debug.Stack()))
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger logs one line per request.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start).Round(time.Microsecond))
		})
	}
}

// CSRF protection.
//
// Two independent defenses are combined:
//
//  1. Go's http.CrossOriginProtection rejects cross-origin unsafe requests
//     using the Sec-Fetch-Site / Origin headers sent by modern browsers.
//  2. A per-browser token (double-submit): a random token is stored in a
//     SameSite=Strict, HttpOnly cookie and embedded in the page; unsafe
//     requests must echo it in the X-CSRF-Token header (or the csrf_token
//     form field). Cross-site pages can neither read the cookie nor set the
//     custom header.

const (
	CSRFCookieName = "md_csrf"
	CSRFHeaderName = "X-CSRF-Token"
	CSRFFormField  = "csrf_token"
)

type csrfKey struct{}

// CSRFToken returns the token for the current request (for templates).
func CSRFToken(r *http.Request) string {
	tok, _ := r.Context().Value(csrfKey{}).(string)
	return tok
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions || m == http.MethodTrace
}

// CSRF returns middleware enforcing CSRF protection on unsafe methods.
func CSRF(secureCookie bool) func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	return func(next http.Handler) http.Handler {
		tokenCheck := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string
			if c, err := r.Cookie(CSRFCookieName); err == nil && len(c.Value) >= 32 && len(c.Value) <= 128 {
				token = c.Value
			}

			if isSafeMethod(r.Method) {
				if token == "" {
					token = newToken()
					http.SetCookie(w, &http.Cookie{
						Name:     CSRFCookieName,
						Value:    token,
						Path:     "/",
						HttpOnly: true,
						Secure:   secureCookie,
						SameSite: http.SameSiteStrictMode,
						MaxAge:   60 * 60 * 24 * 365,
					})
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, token)))
				return
			}

			sent := r.Header.Get(CSRFHeaderName)
			if sent == "" && r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
				sent = r.PostFormValue(CSRFFormField)
			}
			if token == "" || sent == "" || subtle.ConstantTimeCompare([]byte(token), []byte(sent)) != 1 {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"invalid or missing CSRF token; reload the page and try again"}`))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, token)))
		})
		return cop.Handler(tokenCheck)
	}
}
