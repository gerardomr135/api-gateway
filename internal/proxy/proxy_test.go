package proxy

import (
	"testing"
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
func TestProxyForwardsHeaders(t *testing.T) {
	t.Skip("TODO(M1): remove this Skip and implement")

	// TODO(M1): cases to cover
	//   - X-Forwarded-For contains the client IP (and appends, not replaces)
	//   - a hop-by-hop header (e.g. "Connection: X-Secret" + "X-Secret: 1")
	//     does NOT reach the upstream
	//   - path and query string arrive intact
	//   - upstream down -> client gets 502 JSON
}
