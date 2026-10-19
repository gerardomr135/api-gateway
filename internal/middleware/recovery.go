package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery converts a panic anywhere below it into a 500 and a logged stack
// trace, instead of net/http's default (log + abruptly close the connection).
//
// It must be the OUTERMOST middleware. Work out why before reading further,
// then verify with the M3 "break it" experiment.
//
// Gotcha: http.ErrAbortHandler is a deliberate panic used to abort a
// response; it should be re-panicked, not logged as an error.
func Recovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					logger.Error("panic recovered", "error", v, "stack", string(debug.Stack()))
					WriteError(w, r, http.StatusInternalServerError, CodeInternal)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
