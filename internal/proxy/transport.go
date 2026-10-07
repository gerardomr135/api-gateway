package proxy

import "net/http"

// TransportOptions are the knobs you'll tune in M4.
type TransportOptions struct {
	// TODO(M4): add fields for, at least:
	//   DialTimeout, TLSHandshakeTimeout, ResponseHeaderTimeout,
	//   MaxIdleConns, MaxIdleConnsPerHost, IdleConnTimeout
}

// NewTransport builds the ONE shared *http.Transport for all upstreams.
//
// Why it matters: http.DefaultTransport keeps only 2 idle connections per
// host (DefaultMaxIdleConnsPerHost). Under load, every extra concurrent
// request opens a NEW TCP connection and closes it afterwards, leaving
// sockets in TIME_WAIT. You'll reproduce this in M4's "break it" step.
func NewTransport(opts TransportOptions) *http.Transport {
	// TODO(M4): start from http.DefaultTransport.(*http.Transport).Clone()
	// so you inherit sane defaults (proxy from env, HTTP/2 attempt, ...),
	// then override the fields from opts. Explain each override in a comment.
	return http.DefaultTransport.(*http.Transport).Clone()
}
