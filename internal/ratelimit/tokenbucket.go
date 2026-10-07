// Package ratelimit implements rate-limiting algorithms (M6).
//
// Token bucket in one picture:
//
//	       refill: rate tokens/sec
//	              │
//	        ┌─────▼─────┐
//	        │ ● ● ● ●   │  capacity = burst
//	        └─────┬─────┘
//	              │ each request takes 1 token
//	              ▼
//	token available? ── yes ──► allow
//	       │
//	       no ──► reject (429)
//
// Lazy refill: don't run a ticker goroutine per bucket. On each call,
// compute how many tokens accrued since `last` and add them (capped).
package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket is a single bucket. Safe for concurrent use.
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64   // current tokens (fractional: refill is continuous)
	capacity float64   // max tokens = allowed burst
	rate     float64   // tokens added per second
	last     time.Time // last refill time

	// now is injectable so tests can control time instead of sleeping.
	// Flaky sleep-based tests are a classic rate-limiter trap.
	now func() time.Time
}

// NewTokenBucket returns a full bucket.
func NewTokenBucket(ratePerSec float64, burst int) *TokenBucket {
	// TODO(M6): initialize all fields; start full (tokens = capacity).
	return &TokenBucket{now: time.Now}
}

// Allow reports whether one token could be taken right now.
func (b *TokenBucket) Allow() bool {
	// TODO(M6):
	//   lock
	//   elapsed := now - last; tokens = min(capacity, tokens + elapsed*rate); last = now
	//   if tokens >= 1 { tokens--; return true }
	//   return false
	return true
}

// Keyed holds one bucket per client key (user ID or IP).
//
// Two problems to solve:
//  1. Concurrency: many goroutines create/read buckets. sync.Mutex + map,
//     or sync.Map? Measure both with a benchmark before deciding.
//  2. Memory: a bucket per IP grows forever. Evict buckets idle for longer
//     than some TTL (a background sweep, or check on access).
type Keyed struct {
	// TODO(M6)
}

// Allow satisfies middleware.Limiter.
func (k *Keyed) Allow(key string) bool {
	// TODO(M6)
	return true
}
