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
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/yourname/api-gateway/internal/middleware"
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
			// TODO(M5): strip inbound X-User-* headers, set them from verified claims.
			pr.SetURL(target)
			pr.SetXForwarded()
			// The context is the source of truth for the ID; don't rely on
			// the inbound header copy surviving.
			if id := middleware.RequestIDFrom(pr.In.Context()); id != "" {
				pr.Out.Header.Set(middleware.HeaderRequestID, id)
			}
			// Track failures while reading the client's body, so a slow or
			// vanished uploader is not reported as an upstream failure.
			if pr.Out.Body != nil && pr.Out.Body != http.NoBody {
				pr.Out.Body = newClientBody(pr.Out.Body)
			}
		},

		ErrorHandler: errorHandler(logger),
		// The response X-Request-ID is set by the RequestID middleware, NOT in
		// ModifyResponse: ReverseProxy copies upstream headers onto w with
		// Add, so setting it in both places sends the header twice.
		// TODO(M10): ModifyResponse is where you could record upstream status
		// for metrics.
	}
}

// errorHandler is called when the upstream round trip fails (connection
// refused, timeout, ...). The default writes a bare 502 with no body.
func errorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		// errors.Is, not ==: the Transport wraps context errors.
		//   context.DeadlineExceeded -> 504 Gateway Timeout
		//   net.Error with Timeout() -> 504 (dial / TLS / ResponseHeaderTimeout)
		//   context.Canceled         -> client went away; 499 so the access log is honest
		//   anything else            -> 502 Bad Gateway
		reqID := middleware.RequestIDFrom(r.Context())

		// The failure may have happened reading the CLIENT's body, not talking
		// to the upstream. Check that first: a stalled upload surfaces as a
		// net.Error timeout, which would otherwise be mapped to 504 below.
		// RoundTrip does not always return the body error itself. When the
		// server's read deadline fires it also cancels r.Context(), and the
		// Transport can report that cancellation first, so accept both.
		if cerr := clientBodyErr(r); cerr != nil && (errors.Is(err, cerr) || errors.Is(err, context.Canceled)) {
			if netErr, ok := errors.AsType[net.Error](cerr); ok && netErr.Timeout() {
				logger.Warn("client upload timed out", "err", cerr, "path", r.URL.Path, middleware.HeaderRequestID, reqID)
				// The rest of the body was never read, so the connection is not
				// reusable (RFC 9110: close after a 408).
				w.Header().Set("Connection", "close")
				middleware.WriteError(w, r, http.StatusRequestTimeout, middleware.CodeRequestTimeout)
				return
			}
			logger.Warn("client body failed", "err", cerr, "path", r.URL.Path, middleware.HeaderRequestID, reqID)
			w.Header().Set("Connection", "close")
			middleware.WriteError(w, r, http.StatusBadRequest, middleware.CodeBadRequest)
			return
		}

		switch {
		case errors.Is(err, context.DeadlineExceeded):
			logger.Warn("upstream timeout", "path", r.URL.Path, middleware.HeaderRequestID, reqID)
			middleware.WriteError(w, r, http.StatusGatewayTimeout, middleware.CodeTimeout)
		case errors.Is(err, context.Canceled):
			logger.Warn("client canceled request", "path", r.URL.Path, middleware.HeaderRequestID, reqID)
			middleware.WriteError(w, r, middleware.StatusClientClosedRequest, middleware.CodeClientClosed)
		default:
			// Dial and ResponseHeaderTimeout failures come from the Transport,
			// not from our context, so they are net.Errors, not DeadlineExceeded.
			if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
				logger.Warn("upstream timeout", "err", err, "path", r.URL.Path, middleware.HeaderRequestID, reqID)
				middleware.WriteError(w, r, http.StatusGatewayTimeout, middleware.CodeTimeout)
				return
			}
			logger.Error("upstream error", "err", err, "path", r.URL.Path, middleware.HeaderRequestID, reqID)
			middleware.WriteError(w, r, http.StatusBadGateway, middleware.CodeBadGateway)
		}
	}
}
