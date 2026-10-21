package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeoutSetsDeadlineAndCallsNext(t *testing.T) {
	const want = 2 * time.Second

	called := false
	var deadline time.Time
	var hasDeadline bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		deadline, hasDeadline = r.Context().Deadline()
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	start := time.Now()
	Timeout(want)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("next handler was not called")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (response must pass through)", rec.Code, http.StatusTeapot)
	}
	if !hasDeadline {
		t.Fatal("request context has no deadline")
	}
	if got := deadline.Sub(start); got < want-100*time.Millisecond || got > want+100*time.Millisecond {
		t.Fatalf("deadline is %v after start, want about %v", got, want)
	}
}

func TestTimeoutExpiresContext(t *testing.T) {
	var err error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			err = r.Context().Err()
		case <-time.After(2 * time.Second):
		}
	})

	Timeout(20*time.Millisecond)(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want DeadlineExceeded", err)
	}
}

// The parent's deadline wins when it is shorter: WithTimeout never extends one.
func TestTimeoutKeepsShorterParentDeadline(t *testing.T) {
	var deadline time.Time
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, _ = r.Context().Deadline()
	})

	parent, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(parent)

	start := time.Now()
	Timeout(time.Minute)(next).ServeHTTP(httptest.NewRecorder(), req)

	if got := deadline.Sub(start); got > time.Second {
		t.Fatalf("deadline is %v away, want the parent's ~50ms", got)
	}
}
