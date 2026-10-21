package proxy

import (
	"net"
	"net/http"
	"time"
)

// TransportOptions are the knobs you'll tune in M4.
type TransportOptions struct {
	DialTimeout           time.Duration // TCP connect to the upstream
	TLSHandshakeTimeout   time.Duration // https upstreams only
	ResponseHeaderTimeout time.Duration // request written -> response headers arrive
	MaxIdleConns          int           // idle connections across all hosts
	MaxIdleConnsPerHost   int           // idle connections kept per upstream host (stdlib default is 2)
	IdleConnTimeout       time.Duration // how long an idle connection stays pooled
}

// DefaultTransportOptions are the values the gateway runs with. Keep
// IdleConnTimeout shorter than any upstream's own idle timeout, so the gateway
// is the side that closes idle connections first.
func DefaultTransportOptions() TransportOptions {
	return TransportOptions{
		DialTimeout:           2 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       30 * time.Second,
	}
}

// NewTransport builds the ONE shared *http.Transport for all upstreams.
//
// Why it matters: http.DefaultTransport keeps only 2 idle connections per
// host (DefaultMaxIdleConnsPerHost). Under load, every extra concurrent
// request opens a NEW TCP connection and closes it afterwards, leaving
// sockets in TIME_WAIT. You'll reproduce this in M4's "break it" step.
func NewTransport(opts TransportOptions) *http.Transport {
	// Start from the default Transport's clone so we keep its sane defaults
	// (proxy from the environment, the HTTP/2 attempt, ExpectContinueTimeout),
	// then override only what a gateway must control.
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Connect timeout: a black-holed host would otherwise wait for the OS
	// default, which can be minutes. KeepAlive makes the kernel probe idle
	// connections so dead peers are noticed.
	transport.DialContext = (&net.Dialer{Timeout: opts.DialTimeout, KeepAlive: 30 * time.Second}).DialContext
	// Bounds the TLS handshake of https upstreams (new connections only).
	transport.TLSHandshakeTimeout = opts.TLSHandshakeTimeout
	// Request written -> response headers arrive. Does NOT cover the body:
	// only the route's context deadline does.
	transport.ResponseHeaderTimeout = opts.ResponseHeaderTimeout
	// Idle connections kept across all hosts.
	transport.MaxIdleConns = opts.MaxIdleConns
	// The important one. The stdlib default is 2, so under load almost every
	// connection is closed after use and re-dialed (TIME_WAIT piles up on us).
	transport.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	// How long a pooled connection may sit idle. Keep it shorter than the
	// upstreams' own idle timeout so we close first, never reuse a half-closed one.
	transport.IdleConnTimeout = opts.IdleConnTimeout

	return transport
}
