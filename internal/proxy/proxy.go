// Package proxy builds the reverse proxies that forward requests upstream (M1).
//
// httputil.ReverseProxy already does the hard parts: streaming bodies,
// removing hop-by-hop headers, copying trailers, handling 101 Upgrade.
// Your job is to configure it correctly and to understand what it does for you.
//
// Read the source once: `go doc -src net/http/httputil.ReverseProxy.ServeHTTP`.
// Find where it removes hop-by-hop headers and where it calls Rewrite.
package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// New returns a reverse proxy that forwards to target using transport.
//
// Pass the transport in (dependency injection) instead of creating one here:
// all proxies should share ONE tuned Transport so they share one connection
// pool (M4). It also lets tests inject a fake RoundTripper.
func New(target *url.URL, transport http.RoundTripper, logger *slog.Logger) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: transport,

		// Rewrite (Go 1.20+) replaces Director. It receives both the inbound
		// request (pr.In, read-only) and the outbound one (pr.Out).
		// Look up why Director was considered unsafe: it runs AFTER
		// hop-by-hop headers are removed, so a client could smuggle headers
		// listed in "Connection: ..." past it.
		Rewrite: func(pr *httputil.ProxyRequest) {
			// TODO(M1):
			//   - pr.SetURL(target)        sets scheme/host/path joining
			//   - pr.SetXForwarded()       sets X-Forwarded-For/Host/Proto
			//   - decide about pr.Out.Host: keep client's Host or use target's?
			// TODO(M3): copy the request ID onto pr.Out.Header.
			// TODO(M5): strip inbound X-User-* headers, set them from verified claims.
		},

		ErrorHandler: errorHandler(logger),

		// TODO(M3/M10): ModifyResponse is where you could add response headers
		// (e.g. X-Request-ID) or record upstream status for metrics.
	}
}

// errorHandler is called when the upstream round trip fails (connection
// refused, timeout, ...). The default writes a bare 502 with no body.
func errorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		// TODO(M1): log the error and respond 502 with a JSON body like
		//   {"error":"bad_gateway","request_id":"..."}
		// TODO(M4): distinguish with errors.Is:
		//   context.DeadlineExceeded -> 504 Gateway Timeout
		//   context.Canceled         -> client went away; log it, status barely matters
		//   anything else            -> 502 Bad Gateway
		logger.Error("upstream error", "err", err, "path", r.URL.Path)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}
