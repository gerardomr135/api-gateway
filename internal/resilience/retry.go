// Package resilience contains retry, circuit breaker and bulkhead (M8).
//
// All three are implemented as http.RoundTripper wrappers: they sit between
// the ReverseProxy and the shared Transport, so they see every upstream
// attempt individually.
//
//	ReverseProxy ─► Bulkhead ─► Retry ─► CircuitBreaker ─► Transport ─► network
//
// That order is a PROPOSAL, not the answer. Part of M8 is deciding the order
// and defending it. (Should a retry consume a new bulkhead slot? Should the
// breaker count each attempt or each logical request?)
package resilience

import (
	"net/http"
	"time"
)

// RoundTripperFunc adapts a function to http.RoundTripper, like
// http.HandlerFunc does for handlers. Handy for wrappers and test fakes.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// RetryPolicy configures Retry.
type RetryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

// Retry wraps next with retries for SAFE situations only:
//   - idempotent methods (GET, HEAD, OPTIONS, PUT, DELETE) or a request
//     carrying an Idempotency-Key,
//   - connection errors and 502/503/504, never 4xx,
//   - only while req.Context() still has time left.
//
// Bodies are streams: once the first attempt reads req.Body it's gone.
// Look at req.GetBody to replay it, and decide what to do when it's nil.
//
// Backoff: exponential with FULL jitter, sleep = rand(0, min(max, base*2^n)).
// Sleep with select on time.After and ctx.Done(), never a bare time.Sleep.
func Retry(next http.RoundTripper, p RetryPolicy) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// TODO(M8). Gotcha: before retrying after a 5xx response, drain and
		// close the previous resp.Body, or you leak the connection.
		return next.RoundTrip(req)
	})
}
