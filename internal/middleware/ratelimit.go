package middleware

import "net/http"

// Limiter decides whether a request identified by key may proceed.
// Implemented in internal/ratelimit: in-memory first, Redis later (M6).
// Depending on an interface here keeps the middleware ignorant of storage.
type Limiter interface {
	Allow(key string) bool
	// TODO(M6): you'll likely want to return more than a bool, e.g. how long
	// until the next token, to fill the Retry-After header.
}

// KeyFunc extracts the rate-limit key from a request: the authenticated user
// ID when present (M5), otherwise the client IP. Be careful with
// X-Forwarded-For: only trust it from proxies you control, or clients can
// rotate fake IPs and bypass the limit.
type KeyFunc func(*http.Request) string

// RateLimit rejects requests over the limit with 429 Too Many Requests and a
// Retry-After header (RFC 6585).
func RateLimit(l Limiter, key KeyFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO(M6)
			next.ServeHTTP(w, r)
		})
	}
}
