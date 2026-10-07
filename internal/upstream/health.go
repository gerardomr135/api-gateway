package upstream

import (
	"context"
	"net/http"
	"time"
)

// HealthChecker actively probes every instance of a pool (M7).
type HealthChecker struct {
	Pool           *Pool
	Path           string        // e.g. /health
	Interval       time.Duration // how often to probe
	UnhealthyAfter int           // consecutive failures before ejecting
	HealthyAfter   int           // consecutive successes before re-admitting

	// Client must have a SHORT timeout, much shorter than Interval.
	// A probe that hangs is a failed probe.
	Client *http.Client
}

// Run probes until ctx is cancelled. Start it with `go hc.Run(ctx)` and stop
// it by cancelling ctx during shutdown (M11). Never leak this goroutine.
func (hc *HealthChecker) Run(ctx context.Context) {
	// TODO(M7):
	//   ticker := time.NewTicker(hc.Interval); defer ticker.Stop()
	//   for { select { case <-ctx.Done(): return; case <-ticker.C: probe all } }
	// Should all instances be probed in parallel or sequentially? What happens
	// with sequential probes when one instance takes the full timeout?
}

// probe checks one instance and updates its state.
func (hc *HealthChecker) probe(ctx context.Context, u *Upstream) {
	// TODO(M7): build the request with http.NewRequestWithContext, treat
	// 2xx as success, anything else (or an error) as failure, apply thresholds,
	// and log every state TRANSITION (not every probe: that's noise).
}
