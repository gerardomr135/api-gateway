// Package telemetry wires metrics and tracing (M10).
//
// Allowed dependencies here: github.com/prometheus/client_golang and
// go.opentelemetry.io/otel (+ otelhttp). Keep them inside this package so the
// rest of the gateway depends only on small interfaces you define.
//
// RED metrics per route (label by route NAME, never raw path):
//
//	gateway_requests_total{route, code}           counter
//	gateway_request_duration_seconds{route}       histogram
//	gateway_upstream_healthy{upstream, instance}  gauge (M7)
//	gateway_circuit_state{upstream}               gauge (M8)
//
// Cardinality check: labeling by "/api/users/42" creates one time series per
// user ID. Prometheus memory grows with series count. That's the outage.
package telemetry

import "net/http"

// Metrics is what the rest of the gateway sees. Define only what callers need.
type Metrics interface {
	// TODO(M10): e.g. ObserveRequest(route string, code int, seconds float64)
}

// AdminHandler returns the mux for the ADMIN listener (never the public one):
// /metrics, /healthz, /readyz, and /debug/pprof/* (M11).
func AdminHandler() http.Handler {
	mux := http.NewServeMux()
	// TODO(M10): mux.Handle("GET /metrics", promhttp.Handler())
	// TODO(M11): /healthz (liveness), /readyz (readiness, false during shutdown),
	//            and net/http/pprof handlers registered explicitly on this mux.
	//            Note: importing net/http/pprof for side effects registers on
	//            http.DefaultServeMux. Know why that's a security footgun.
	return mux
}
