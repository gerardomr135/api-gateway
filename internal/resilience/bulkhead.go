package resilience

import (
	"errors"
	"net/http"
)

// ErrBulkheadFull is returned when an upstream already has max in-flight requests.
var ErrBulkheadFull = errors.New("resilience: bulkhead full")

// Bulkhead limits concurrent in-flight requests to one upstream, so a slow
// service can't absorb every goroutine and connection the gateway has.
// Name comes from ship hulls: a flooded compartment doesn't sink the ship.
//
// Idiomatic Go semaphore: a buffered channel of empty structs.
//
//	sem := make(chan struct{}, max)
//	select {
//	case sem <- struct{}{}:    // acquired; defer <-sem to release
//	default:                   // full: reject immediately, don't queue
//	}
//
// Careful: when do you release? After RoundTrip returns, or after the
// response BODY has been fully read and closed by the proxy? (Hint: the
// upstream is still busy while the body streams.)
func Bulkhead(next http.RoundTripper, max int) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// TODO(M8)
		return next.RoundTrip(req)
	})
}
