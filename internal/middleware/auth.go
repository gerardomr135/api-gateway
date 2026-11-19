package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

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
	Key(token *jwt.Token) (any, error)
}

type StaticKey []byte // a []byte with a name, so it can have methods

func (k StaticKey) Key(*jwt.Token) (any, error) {
	return []byte(k), nil // ignore the token, always return the secret
}

type AuthDependencies struct {
	Keys      KeySource
	Algorithm string
	Issuer    string
	Audience  string
	Leeway    time.Duration
}

type UserClaims struct {
	jwt.RegisteredClaims
	Roles []string `json:"roles"`
}

// Auth verifies a Bearer JWT according to the route's policy (M5). It is wrapped
// per route (see main.go) because the policy is only known after routing.
//
//   - none:     pass through untouched (identity headers were already stripped
//     by StripIdentityHeaders).
//   - optional: no Authorization header => anonymous, pass through. A header
//     that is present MUST be valid, otherwise 401: a bad token is
//     usually a client bug and hiding it as anonymous makes it harder to find.
//   - required: a valid token or 401.
//
// On success X-User-* are set from the claims. Failures answer 401 with a
// RFC 6750 WWW-Authenticate header.
func Auth(a AuthDependencies, policy string) Middleware {
	jwtParser := jwt.NewParser(
		jwt.WithValidMethods([]string{a.Algorithm}),
		jwt.WithLeeway(a.Leeway),
		jwt.WithIssuer(a.Issuer),
		jwt.WithAudience(a.Audience),
		jwt.WithExpirationRequired(),
	)

	return func(next http.Handler) http.Handler {
		if policy == "none" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				if policy == "optional" {
					next.ServeHTTP(w, r)
					return
				}
				// RFC 6750: no credentials => challenge without an error code.
				WriteErrorWithHeaders(w, r, http.StatusUnauthorized, CodeUnauthorized,
					map[string]string{"WWW-Authenticate": "Bearer"})
				return
			}

			// The scheme is case-insensitive (RFC 7235) and must be a prefix.
			const prefix = "bearer "
			if len(authHeader) <= len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
				unauthorizedError(w, r, "invalid_request", "expected a Bearer token")
				return
			}
			token := strings.TrimSpace(authHeader[len(prefix):])

			claims := UserClaims{}
			if _, err := jwtParser.ParseWithClaims(token, &claims, a.Keys.Key); err != nil {
				unauthorizedError(w, r, "invalid_token", "the token is invalid or expired")
				return
			}

			subject, err := claims.GetSubject()
			if err != nil || subject == "" {
				unauthorizedError(w, r, "invalid_token", "the token has no subject")
				return
			}

			r.Header.Set(HeaderUserID, subject)
			r.Header.Set(HeaderUserRoles, strings.Join(claims.Roles, ","))

			next.ServeHTTP(w, r)
		})
	}
}
