package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yourname/api-gateway/internal/middleware"
)

// The pattern for every proxy test in this repo:
//
//	upstream := httptest.NewServer(handler that records what it received)
//	gateway  := httptest.NewServer(New(upstreamURL, NewTransport(...), logger))
//	resp     := http.Get(gateway.URL + "/some/path")
//	assert on what the UPSTREAM saw and what the CLIENT got.
//
// Asserting on what the upstream saw is the whole point: a gateway is
// defined by the request it forwards, not just the response it returns.
//
// M4 tip (Go 1.27): httptest.NewTestServer runs a server over an in-memory
// fake network made for testing/synctest. With it, a test that says "upstream
// takes 5s, route timeout is 1s, expect a 504" runs instantly and
// deterministically instead of actually sleeping.
func TestProxyForwardsHeaders(t *testing.T) {
	// recordingUpstream remembers the request it received, which is what the
	// assertions below are about.
	received := make(chan *http.Request, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Clone(context.Background())
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	gw := gatewayFor(t, upstream, NewTransport(DefaultTransportOptions()), 5*time.Second)

	// send makes one request through the gateway and returns what the upstream saw.
	send := func(t *testing.T, target string, hdr http.Header) *http.Request {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, gw.URL+target, nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			req.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", resp.StatusCode)
		}
		select {
		case r := <-received:
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("upstream never received the request")
			return nil
		}
	}

	t.Run("X-Forwarded-For carries the client IP", func(t *testing.T) {
		got := send(t, "/", nil)
		if xff := got.Header.Get("X-Forwarded-For"); xff != "127.0.0.1" {
			t.Fatalf("X-Forwarded-For = %q, want %q", xff, "127.0.0.1")
		}
	})

	t.Run("client-supplied X-Forwarded-For is not trusted", func(t *testing.T) {
		// The gateway is the edge: a client can claim any address, so the value
		// it sends must not reach the upstream as if it were the real client IP.
		got := send(t, "/", http.Header{"X-Forwarded-For": {"203.0.113.7"}})
		xff := got.Header.Get("X-Forwarded-For")
		if strings.Contains(xff, "203.0.113.7") {
			t.Fatalf("X-Forwarded-For = %q: spoofed client value reached the upstream", xff)
		}
		if xff != "127.0.0.1" {
			t.Fatalf("X-Forwarded-For = %q, want the real peer %q", xff, "127.0.0.1")
		}
	})

	t.Run("hop-by-hop header named in Connection is dropped", func(t *testing.T) {
		got := send(t, "/", http.Header{
			"Connection": {"X-Secret"},
			"X-Secret":   {"1"},
			"X-Keep":     {"yes"},
		})
		if v := got.Header.Get("X-Secret"); v != "" {
			t.Fatalf("X-Secret = %q reached the upstream, want it removed", v)
		}
		if v := got.Header.Get("X-Keep"); v != "yes" {
			t.Fatalf("X-Keep = %q, want an ordinary header to pass through", v)
		}
	})

	t.Run("path and query arrive intact", func(t *testing.T) {
		// RequestURI is the raw request line, so it also proves %2F is not decoded.
		const want = "/a/b%2Fc?x=1&y=two%20words"
		got := send(t, want, nil)
		if got.RequestURI != want {
			t.Fatalf("RequestURI = %q, want %q", got.RequestURI, want)
		}
	})

	t.Run("upstream down returns 502 JSON", func(t *testing.T) {
		dead := httptest.NewServer(http.NotFoundHandler())
		dead.Close() // nothing listens on this address any more
		gwDown := gatewayFor(t, dead, NewTransport(DefaultTransportOptions()), 5*time.Second)

		resp, err := http.Get(gwDown.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error != middleware.CodeBadGateway {
			t.Fatalf("body error = %q (decode err %v), want %q", body.Error, err, middleware.CodeBadGateway)
		}
	})
}

// timeoutErr is a net.Error whose Timeout() is true, like a dial or
// ResponseHeaderTimeout failure coming out of the Transport.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestErrorHandlerMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"deadline exceeded", context.DeadlineExceeded, http.StatusGatewayTimeout, middleware.CodeTimeout},
		{"wrapped deadline exceeded", fmt.Errorf("round trip: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, middleware.CodeTimeout},
		{"transport timeout (net.Error)", timeoutErr{}, http.StatusGatewayTimeout, middleware.CodeTimeout},
		{"client canceled", context.Canceled, http.StatusBadGateway, middleware.CodeBadGateway},
		{"connection refused", errors.New("dial tcp: connection refused"), http.StatusBadGateway, middleware.CodeBadGateway},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			errorHandler(logger)(rec, httptest.NewRequest(http.MethodGet, "/", nil), tt.err)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if body.Error != tt.wantCode {
				t.Fatalf("error code = %q, want %q", body.Error, tt.wantCode)
			}
		})
	}
}

// slowUpstream answers after delay, but gives up early if the gateway aborts
// the call, so the test does not wait for it.
func slowUpstream(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func gatewayFor(t *testing.T, upstream *httptest.Server, transport http.RoundTripper, routeTimeout time.Duration) *httptest.Server {
	t.Helper()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := httptest.NewServer(middleware.Timeout(routeTimeout)(New(target, transport, logger)))
	t.Cleanup(gw.Close)
	return gw
}

func TestRouteTimeoutReturns504(t *testing.T) {
	upstream := slowUpstream(t, 2*time.Second)
	gw := gatewayFor(t, upstream, NewTransport(DefaultTransportOptions()), 100*time.Millisecond)

	start := time.Now()
	resp, err := http.Get(gw.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", resp.StatusCode)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %v, want about the 100ms route timeout", took)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error != middleware.CodeTimeout {
		t.Fatalf("body error = %q (decode err %v), want %q", body.Error, err, middleware.CodeTimeout)
	}
}

func TestFastUpstreamWithinTimeoutReturns200(t *testing.T) {
	upstream := slowUpstream(t, 0)
	gw := gatewayFor(t, upstream, NewTransport(DefaultTransportOptions()), time.Second)

	resp, err := http.Get(gw.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// ResponseHeaderTimeout fires inside the Transport, so the error is a
// net.Error timeout and NOT context.DeadlineExceeded. It must still be a 504.
func TestResponseHeaderTimeoutReturns504(t *testing.T) {
	upstream := slowUpstream(t, 2*time.Second)
	opts := DefaultTransportOptions()
	opts.ResponseHeaderTimeout = 50 * time.Millisecond
	gw := gatewayFor(t, upstream, NewTransport(opts), 5*time.Second)

	resp, err := http.Get(gw.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", resp.StatusCode)
	}
}

// A client that hangs up must cancel the upstream call.
func TestClientCancelAbortsUpstream(t *testing.T) {
	aborted := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			close(aborted)
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(upstream.Close)
	gw := gatewayFor(t, upstream, NewTransport(DefaultTransportOptions()), 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, gw.URL, nil)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatal("expected the client request to fail after cancel")
	}

	select {
	case <-aborted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not canceled when the client went away")
	}
}

func TestNewTransportAppliesOptions(t *testing.T) {
	opts := DefaultTransportOptions()
	tr := NewTransport(opts)

	if tr.MaxIdleConnsPerHost != opts.MaxIdleConnsPerHost || tr.MaxIdleConnsPerHost <= http.DefaultMaxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d (stdlib default is %d)", tr.MaxIdleConnsPerHost, opts.MaxIdleConnsPerHost, http.DefaultMaxIdleConnsPerHost)
	}
	if tr.MaxIdleConns != opts.MaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", tr.MaxIdleConns, opts.MaxIdleConns)
	}
	if tr.ResponseHeaderTimeout != opts.ResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, opts.ResponseHeaderTimeout)
	}
	if tr.TLSHandshakeTimeout != opts.TLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", tr.TLSHandshakeTimeout, opts.TLSHandshakeTimeout)
	}
	if tr.IdleConnTimeout != opts.IdleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v", tr.IdleConnTimeout, opts.IdleConnTimeout)
	}
	if tr.DialContext == nil {
		t.Error("DialContext is nil, so there is no connect timeout")
	}
}

// Proves connection reuse: the second request must get a pooled connection.
func TestTransportReusesConnections(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)
	client := &http.Client{Transport: NewTransport(DefaultTransportOptions())}

	var reused []bool
	for range 2 {
		trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = append(reused, i.Reused) }}
		req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, upstream.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		// No io.Copy(io.Discard, resp.Body) before Close: since the Transport
		// started draining small unread bodies itself, closing is enough to
		// return the connection to the pool. That only holds for bodies up to
		// 256 KiB, and the drain is asynchronous (see NOTES.md, M4).
		resp.Body.Close()
	}

	if len(reused) != 2 || reused[0] || !reused[1] {
		t.Fatalf("GotConn Reused = %v, want [false true]", reused)
	}
}

// errReader fails every Read with err, like a client connection that died.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestErrorHandlerClientBodyFailure(t *testing.T) {
	tests := []struct {
		name       string
		readErr    error // error the CLIENT body returns; nil means the body is fine
		upstream   error // error RoundTrip reports
		wantStatus int
		wantCode   string
	}{
		{"client stalled (net timeout)", timeoutErr{}, timeoutErr{}, http.StatusRequestTimeout, middleware.CodeRequestTimeout},
		{"client stalled, error wrapped by the Transport", timeoutErr{}, fmt.Errorf("write request body: %w", timeoutErr{}), http.StatusRequestTimeout, middleware.CodeRequestTimeout},
		{"client vanished mid-upload", io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, http.StatusBadRequest, middleware.CodeBadRequest},
		{"client stalled, Transport reports the canceled context", timeoutErr{}, context.Canceled, http.StatusRequestTimeout, middleware.CodeRequestTimeout},
		// The body read fine, so a timeout is still the upstream's fault.
		{"upstream timeout, healthy body", nil, timeoutErr{}, http.StatusGatewayTimeout, middleware.CodeTimeout},
		// The body failed, but the reported error is something else entirely.
		{"unrelated error after a body failure", timeoutErr{}, errors.New("dial tcp: connection refused"), http.StatusBadGateway, middleware.CodeBadGateway},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.ReadCloser = io.NopCloser(strings.NewReader("ok"))
			if tt.readErr != nil {
				body = io.NopCloser(errReader{tt.readErr})
			}
			cb := newClientBody(body)
			_, _ = cb.Read(make([]byte, 8)) // what the Transport's write loop does

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Body = cb
			rec := httptest.NewRecorder()
			errorHandler(logger)(rec, req, tt.upstream)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var got struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got.Error != tt.wantCode {
				t.Fatalf("error code = %q (decode err %v), want %q", got.Error, err, tt.wantCode)
			}
			if tt.wantStatus == http.StatusRequestTimeout && rec.Header().Get("Connection") != "close" {
				t.Fatalf("Connection = %q, want close after a 408", rec.Header().Get("Connection"))
			}
		})
	}
}

// End to end: a client that stalls mid-upload past ReadTimeout is the slow side,
// so it must get a 408, not a 504 that blames the upstream.
func TestSlowClientUploadReturns408(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := middleware.Timeout(5 * time.Second)(New(target, NewTransport(DefaultTransportOptions()), logger))
	gw := httptest.NewUnstartedServer(h)
	gw.Config.ReadTimeout = 200 * time.Millisecond
	gw.Start()
	t.Cleanup(gw.Close)

	conn, err := net.Dial("tcp", gw.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Headers and one chunk, then stall past ReadTimeout.
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n")
	time.Sleep(600 * time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no response from the gateway: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error != middleware.CodeRequestTimeout {
		t.Fatalf("body error = %q (decode err %v), want %q", body.Error, err, middleware.CodeRequestTimeout)
	}
}

// A body that the Transport closed early must not be blamed on the client.
func TestClientBodyIgnoresReadAfterClose(t *testing.T) {
	cb := newClientBody(io.NopCloser(errReader{http.ErrBodyReadAfterClose}))
	_, _ = cb.Read(make([]byte, 8))
	if err := cb.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil for ErrBodyReadAfterClose", err)
	}
}
