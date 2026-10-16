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
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
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
		if opts.unhealthy {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unhealthy"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// echoHandler returns a JSON description of the request it received.
func echoHandler(opts options, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK

		defer func() {
			logger.Info(
				"request handled",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"request_id", r.Header.Get("X-Request-ID"),
			)
		}()

		// Use a timer instead of time.After so we can stop it if the request is cancelled.
		// Explicit timers give you better control over timer resources.
		timer := time.NewTimer(opts.latency)
		defer timer.Stop()

		// 1. Wait for the configured latency.
		select {
		case <-timer.C:
			// continue

		case <-r.Context().Done():
			status = http.StatusRequestTimeout
			http.Error(w, "request cancelled", status)
			return
		}

		// 2. Fail randomly according to failRate.
		if rand.Float64() < opts.failRate {
			status = http.StatusInternalServerError
			http.Error(w, "request failed", status)
			return
		}

		// 3. Return the request headers as JSON.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.Header)

		// 4. Logging happens through defer.
	}
}
