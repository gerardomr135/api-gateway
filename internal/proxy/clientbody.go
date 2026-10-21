package proxy

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"
)

// clientBody wraps the request body that ReverseProxy streams from the CLIENT
// to the upstream. A read error here (the client stalled, hung up mid-upload)
// comes back out of RoundTrip looking like any other transport failure, and a
// timeout among them is a net.Error just like a dial timeout. Recording the
// error lets errorHandler tell "the client was slow" from "the upstream failed".
type clientBody struct {
	io.ReadCloser
	// err is written by the Transport's write goroutine and read by the handler
	// goroutine after RoundTrip returns, so it must be atomic.
	err atomic.Pointer[error]
}

func newClientBody(rc io.ReadCloser) *clientBody {
	return &clientBody{ReadCloser: rc}
}

func (b *clientBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	// ErrBodyReadAfterClose means the Transport already gave up (for example the
	// upstream refused the connection) and closed the body. That is not the
	// client's failure, so it must not be recorded as one.
	if err != nil && err != io.EOF && !errors.Is(err, http.ErrBodyReadAfterClose) {
		b.err.CompareAndSwap(nil, &err) // keep the first failure
	}
	return n, err
}

// Err returns the first non-EOF error seen while reading the client's body.
func (b *clientBody) Err() error {
	if p := b.err.Load(); p != nil {
		return *p
	}
	return nil
}

// clientBodyErr returns the client-side body read error recorded on r, if any.
func clientBodyErr(r *http.Request) error {
	if b, ok := r.Body.(*clientBody); ok {
		return b.Err()
	}
	return nil
}
