package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// AccessLog logs one structured line per request AFTER it completes:
// method, path, route name, status, bytes written, duration, request ID.
//
// Problem to solve: http.ResponseWriter has no "what status did I send?"
// method. You must wrap it (statusRecorder below) to capture that.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// The router fills this slot in once it has matched a route.
			r = r.WithContext(WithRouteHolder(r.Context()))
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"route", RouteNameFrom(r.Context()),
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", float64(time.Since(start))/1e6,
				"request_id", RequestIDFrom(r.Context()),
			)
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
	status      int
	bytes       int
	wroteHeader bool
}

// WriteHeader records the status code, then delegates. Only the first call
// counts: net/http ignores later ones, so recording them would make the log
// disagree with what the client actually received. Informational 1xx codes
// (e.g. 103 Early Hints) are not final, so they pass through unrecorded.
func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader && (code < 100 || code > 199 || code == http.StatusSwitchingProtocols) {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write counts bytes, then delegates. A Write with no prior WriteHeader sends
// an implicit 200, which status already defaults to.
func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush delegates to the underlying writer so streaming (SSE) isn't buffered
// by the wrapper. Flushing also commits the headers with an implicit 200.
func (r *statusRecorder) Flush() {
	r.wroteHeader = true
	http.NewResponseController(r.ResponseWriter).Flush()
}

// Unwrap exposes the original writer to http.ResponseController (Go 1.20+).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
