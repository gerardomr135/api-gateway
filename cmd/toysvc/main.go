// Command toysvc is a configurable fake backend for the gateway lab (M0).
//
// One binary, many personalities: run it several times with different flags
// to get "users", "orders-1", "orders-2", ... Each knob exists so you can
// trigger a specific gateway failure mode on demand:
//
//	-latency    -> timeouts and 504s (M4), slow-upstream degradation (M9)
//	-fail-rate  -> retries (M8), passive health checks (M7), circuit breaker (M8)
//	-unhealthy  -> active health checks and ejection (M7)
//
// Endpoints to implement:
//
//	GET /health  -> 200 {"status":"ok"}, or 503 when -unhealthy is set
//	ANY /        -> 200 JSON echo: service name, port, method, path, query,
//	                and ALL received headers (so you can see exactly what the
//	                gateway forwarded, stripped, or added)
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// options holds the parsed command-line flags.
type options struct {
	name      string
	port      int
	latency   time.Duration
	failRate  float64
	unhealthy bool
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.name, "name", "toysvc", "service name, echoed in every response")
	flag.IntVar(&o.port, "port", 9001, "port to listen on")
	flag.DurationVar(&o.latency, "latency", 0, "artificial delay before responding, e.g. 300ms")
	flag.Float64Var(&o.failRate, "fail-rate", 0, "fraction of requests (0.0-1.0) that return 500")
	flag.BoolVar(&o.unhealthy, "unhealthy", false, "make /health return 503")
	flag.Parse()
	return o
}

func main() {
	opts := parseFlags()

	// slog with a JSON handler: structured logs you can grep and, later (M10),
	// correlate by request ID. Add the service name as a default attribute so
	// every line says which instance logged it.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", opts.name)

	mux := http.NewServeMux()

	// Go 1.22+ patterns: "GET /health" matches only GET on that exact path.
	// "/" is a catch-all: it matches every path not matched by something more specific.
	mux.HandleFunc("GET /health", healthHandler(opts))
	mux.HandleFunc("/", echoHandler(opts, logger))

	addr := fmt.Sprintf(":%d", opts.port)
	logger.Info("toysvc listening", "addr", addr, "latency", opts.latency, "fail_rate", opts.failRate)

	// TODO(M4): this is a backend, so zero-value timeouts are tolerable in the
	// lab, but come back after M4 and ask yourself what a real service would set.
	srv := &http.Server{Addr: addr, Handler: mux}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// healthHandler reports liveness for the gateway's active health checks (M7).
func healthHandler(opts options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO(M0): return 503 when opts.unhealthy is true, otherwise 200.
		// Respond with a small JSON body and the right Content-Type.
		http.Error(w, "TODO(M0): implement /health", http.StatusNotImplemented)
	}
}

// echoHandler returns a JSON description of the request it received.
func echoHandler(opts options, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO(M0): implement, in this order:
		//   1. Sleep for opts.latency. Bonus: use a select on time.After and
		//      r.Context().Done() so a cancelled request stops waiting. You'll
		//      need this behavior to observe cancellation propagation in M4.
		//   2. With probability opts.failRate, respond 500 (math/rand/v2).
		//   3. Encode the echo payload with json.NewEncoder(w).Encode(...).
		//      Include r.Header as-is: it's a map[string][]string.
		//   4. Log one line: method, path, status, and X-Request-ID if present.
		_ = logger
		http.Error(w, "TODO(M0): implement echo", http.StatusNotImplemented)
	}
}
