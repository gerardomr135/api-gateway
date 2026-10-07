package upstream

import (
	"errors"
	"sync/atomic"
)

// ErrNoHealthyUpstream means every instance in the pool is down.
var ErrNoHealthyUpstream = errors.New("upstream: no healthy instance")

// Balancer picks an instance. Implementations must be safe for concurrent use
// and must skip unhealthy instances.
type Balancer interface {
	Pick(instances []*Upstream) (*Upstream, error)
}

// RoundRobin cycles through instances using an atomic counter.
//
// Edge case to think through: counter=5, 3 instances, instance 5%3 is down.
// Do you try the next one? How many times before giving up?
type RoundRobin struct {
	next atomic.Uint64
}

// Pick implements Balancer.
func (rr *RoundRobin) Pick(instances []*Upstream) (*Upstream, error) {
	// TODO(M7)
	return nil, ErrNoHealthyUpstream
}

// LeastConn picks the healthy instance with the fewest in-flight requests.
// Then read about "power of two choices" (P2C): pick 2 at random and take the
// less loaded one. Why do big proxies prefer P2C over a full scan?
type LeastConn struct{}

// Pick implements Balancer.
func (lc LeastConn) Pick(instances []*Upstream) (*Upstream, error) {
	// TODO(M7)
	return nil, ErrNoHealthyUpstream
}
