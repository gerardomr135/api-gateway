package resilience

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned while the breaker is open (fail fast -> 503).
var ErrCircuitOpen = errors.New("resilience: circuit open")

// State is the breaker state.
//
//	          failures >= threshold
//	┌────────┐ ─────────────────────► ┌────────┐
//	│ Closed │                        │  Open  │ rejects everything (fail fast)
//	└────────┘                        └────────┘
//	     ▲                              │    ▲
//	     │ probes succeed  openFor elapsed   │ a probe fails
//	     │                              ▼    │
//	     │                         ┌──────────┐
//	     └──────────────────────── │ HalfOpen │ lets a few probe requests through
//	                               └──────────┘
type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

// String makes states readable in logs and metrics.
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreaker protects one upstream. Build it yourself; don't import a
// library for this one. The state machine IS the lesson.
type CircuitBreaker struct {
	mu sync.Mutex

	state            State
	failures         int       // consecutive failures while Closed
	openedAt         time.Time // when we entered Open
	halfOpenInFlight int       // probes currently allowed through

	FailureThreshold    int
	OpenFor             time.Duration
	HalfOpenMaxRequests int

	now func() time.Time // injectable clock for tests
}

// Allow is called BEFORE an attempt. It returns ErrCircuitOpen to fail fast.
// Note the lazy transition: Open becomes HalfOpen inside Allow once OpenFor
// has elapsed. No timer goroutine needed.
func (cb *CircuitBreaker) Allow() error {
	// TODO(M8)
	return nil
}

// Record is called AFTER an attempt with its outcome. Decide what counts as a
// failure: a connection error, sure. A 500? A 404? A client cancellation?
func (cb *CircuitBreaker) Record(err error) {
	// TODO(M8)
}
