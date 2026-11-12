package middleware

import (
	"net/http"
	"strings"
)

// StripIdentityHeaders removes every X-User-* header the CLIENT sent.
//
// Services trust X-User-ID and friends because only the gateway is supposed to
// write them (after verifying a token). HTTP headers are plain text, so a client
// can send X-User-ID: admin itself; if we forwarded it, any route (especially
// auth: none ones, where no token is checked) would hand an attacker-chosen
// identity to the upstream. Run this on EVERY request, before anything that
// reads or sets identity, so the only X-User-* an upstream ever sees is ours.
//
// Matching is by prefix on the name, ignoring case and treating "_" as "-":
// a new X-User-Email header is covered automatically, and X_User_ID cannot slip
// past a filter that only knows the dash form, because CGI-style backends
// (WSGI, PHP) map both spellings to the same variable.
func StripIdentityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for headerName := range r.Header {
				n := strings.ToLower(strings.ReplaceAll(headerName, "_", "-"))
				if strings.HasPrefix(n, "x-user-") {
					delete(r.Header, headerName)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
