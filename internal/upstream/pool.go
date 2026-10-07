// Package upstream manages backend instances: membership, health and
// selection (M7).
//
//	            ┌──────────── Pool "orders" ────────────┐
//	Balancer ──►│ orders-1 ✔  in-flight 3               │
//	  .Next()   │ orders-2 ✘  (ejected by health check) │◄── HealthChecker
//	            │ orders-3 ✔  in-flight 1               │    (goroutine)
//	            └───────────────────────────────────────┘
//
// Concurrency model: the request path READS health and in-flight counters on
// every request, and the health checker WRITES health occasionally. Atomics
// keep the hot path lock-free. Explain in NOTES.md why a mutex would also be
// correct, and what it would cost.
package upstream

import (
	"net/url"
	"sync/atomic"
)

// Upstream is one backend instance.
type Upstream struct {
	URL *url.URL

	healthy  atomic.Bool  // flipped by active (HealthChecker) and passive checks
	inFlight atomic.Int64 // used by least-connections; inc before, dec after (defer!)

	// TODO(M7): counters for consecutive successes/failures, used to apply
	// unhealthy_after / healthy_after thresholds and avoid flapping.
}

// Healthy reports whether this instance should receive traffic.
func (u *Upstream) Healthy() bool { return u.healthy.Load() }

// Pool is a named set of instances plus the strategy that picks among them.
type Pool struct {
	Name      string
	Instances []*Upstream
	Balancer  Balancer
}

// NewPool parses raw instance URLs. New instances start healthy or unhealthy?
// Both are defensible: optimistic gets traffic flowing immediately,
// pessimistic avoids sending traffic to an instance that's still booting.
func NewPool(name string, rawURLs []string, b Balancer) (*Pool, error) {
	// TODO(M7)
	return &Pool{Name: name, Balancer: b}, nil
}

// Next returns a healthy instance, or ErrNoHealthyUpstream (-> 503).
func (p *Pool) Next() (*Upstream, error) {
	// TODO(M7): delegate to p.Balancer.
	return nil, ErrNoHealthyUpstream
}
