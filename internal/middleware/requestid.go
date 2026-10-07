package middleware

import (
	"context"
	"net/http"
)

// HeaderRequestID is the header used to propagate the request ID.
const HeaderRequestID = "X-Request-ID"

// ctxKey is unexported so no other package can collide with (or forge) our
// context keys. A plain string key like "request_id" could be overwritten by
// any library that happens to use the same string.
type ctxKey int

const requestIDKey ctxKey = iota

// RequestID ensures every request carries an ID:
//   - reuse the inbound X-Request-ID if it looks valid (length/charset limits:
//     never trust client input blindly, it ends up in your logs),
//   - otherwise generate one (crypto/rand + hex, or a UUID),
//   - store it in the context, set it on the request header (so the proxy
//     forwards it upstream) and on the response header.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO(M3)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDFrom returns the request ID stored in ctx, or "" if none.
func RequestIDFrom(ctx context.Context) string {
	// TODO(M3): type-assert ctx.Value(requestIDKey) with the comma-ok form.
	return ""
}
