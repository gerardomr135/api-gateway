package middleware

import (
	"log/slog"
	"net/http"
)

// AccessLog logs one structured line per request AFTER it completes:
// method, path, route name, status, bytes written, duration, request ID.
//
// Problem to solve: http.ResponseWriter has no "what status did I send?"
// method. You must wrap it (statusRecorder below) to capture that.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO(M3): start := time.Now(); rec := &statusRecorder{...};
			// next.ServeHTTP(rec, r); logger.Info("request", ...)
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture status and size.
//
// Embedding the interface gives us every method for free, but it HIDES the
// optional interfaces the underlying writer may implement (http.Flusher for
// streaming/SSE, http.Hijacker for WebSockets). Two ways out:
//   - implement Unwrap() so http.ResponseController can reach the original,
//   - or explicitly implement Flush/Hijack and delegate.
//
// Test SSE through your gateway in M3 to see the difference.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// WriteHeader records the status code, then delegates.
func (r *statusRecorder) WriteHeader(code int) {
	// TODO(M3): what if WriteHeader is called twice? What if it's never
	// called and the handler only calls Write? (Then the status is 200.)
	r.ResponseWriter.WriteHeader(code)
}

// Write counts bytes, then delegates.
func (r *statusRecorder) Write(b []byte) (int, error) {
	// TODO(M3)
	return r.ResponseWriter.Write(b)
}

// Unwrap exposes the original writer to http.ResponseController (Go 1.20+).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
