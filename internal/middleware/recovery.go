package middleware

import (
	"log/slog"
	"net/http"
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
			// TODO(M3): defer func() { if v := recover(); v != nil { ... } }()
			// Use runtime/debug.Stack() for the trace.
			// Question: if the handler already wrote headers before
			// panicking, can you still send a 500?
			next.ServeHTTP(w, r)
		})
	}
}
