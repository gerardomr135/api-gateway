package middleware

import "net/http"

// Identity headers the gateway sets for upstreams AFTER verifying a token.
// Services trust these headers, which is exactly why the gateway must strip
// any copies a client sends (header spoofing / confused deputy).
const (
	HeaderUserID    = "X-User-ID"
	HeaderUserRoles = "X-User-Roles"
)

// KeySource resolves the key used to verify a token signature.
// Start with a static HS256 secret; later back it with a JWKS cache keyed by
// the token's "kid" header so keys can rotate without restarting (M5).
type KeySource interface {
	// TODO(M5): design the method(s). Hint: the signature of the
	// key func in github.com/golang-jwt/jwt/v5 is a good guide.
}

// Auth verifies a Bearer JWT on routes that require it (M5).
//
// Order of operations to get right:
//  1. ALWAYS strip inbound X-User-* headers, even on auth: none routes.
//  2. Extract "Authorization: Bearer <token>".
//  3. Verify signature with a PINNED algorithm, then exp/nbf/iss/aud.
//  4. On failure: 401 + "WWW-Authenticate: Bearer error=..." (RFC 6750).
//  5. On success: set X-User-* from claims and store claims in the context.
//
// The route's policy (required/optional/none) must reach this middleware.
// Options: pass it in when building a per-route chain, or put the matched
// route in the context in the router. Pick one and justify it.
func Auth(keys KeySource) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO(M5)
			next.ServeHTTP(w, r)
		})
	}
}
