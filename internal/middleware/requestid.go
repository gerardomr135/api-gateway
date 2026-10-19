package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
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
			id := r.Header.Get(HeaderRequestID)

			if id == "" {
				id = generateRequestID()
			}

			if !isValidRequestID(id) {
				id = generateRequestID()
			}

			// Set on the request so the proxy forwards it upstream, and on
			// the response so the client can quote it when reporting a problem.
			r.Header.Set(HeaderRequestID, id)
			w.Header().Set(HeaderRequestID, id)

			ctx := context.WithValue(r.Context(), requestIDKey, id)
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDFrom returns the request ID stored in ctx, or "" if none.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

func generateRequestID() string {
	return uuid.New().String()
}

func isValidRequestID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}
