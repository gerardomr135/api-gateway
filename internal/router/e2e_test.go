package router_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/yourname/api-gateway/internal/config"
	"github.com/yourname/api-gateway/internal/proxy"
	"github.com/yourname/api-gateway/internal/router"
)

// TestGatewayEndToEnd wires the real pieces together (router + reverse proxy +
// shared transport) in front of two fake upstreams and sends requests through
// them. Unlike the stub-handler tests in router_test.go, this checks what the
// upstream actually receives.
func TestGatewayEndToEnd(t *testing.T) {
	// Each fake upstream echoes who it is and what it saw.
	fakeUpstream := func(name string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Upstream", name)
			w.Header().Set("X-Seen-Path", r.URL.Path)
			w.Header().Set("X-Seen-URI", r.RequestURI)
			w.Header().Set("X-Seen-Query", r.URL.RawQuery)
			w.Header().Set("X-Seen-Forwarded-For", r.Header.Get("X-Forwarded-For"))
			io.WriteString(w, name)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	users := fakeUpstream("users")
	products := fakeUpstream("products")

	// A closed server gives a guaranteed connection-refused upstream.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	routes := []config.Route{
		{Name: "users", Prefix: "/api/users", Methods: []string{"GET", "POST"}, StripPrefix: true},
		{Name: "products", Prefix: "/api/products", Methods: []string{"GET"}, StripPrefix: false},
		{Name: "broken", Prefix: "/api/broken", Methods: []string{"GET"}, StripPrefix: true},
	}
	targets := map[string]string{
		"users":    users.URL,
		"products": products.URL,
		"broken":   deadURL,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	transport := proxy.NewTransport(proxy.TransportOptions{})
	handlers := make(map[string]http.Handler, len(routes))
	for _, r := range routes {
		target, err := url.Parse(targets[r.Name])
		if err != nil {
			t.Fatal(err)
		}
		handlers[r.Name] = proxy.New(target, transport, logger)
	}

	rt, err := router.New(routes, handlers)
	if err != nil {
		t.Fatalf("router.New() failed: %v", err)
	}
	gateway := httptest.NewServer(rt)
	t.Cleanup(gateway.Close)

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantUpstream       string // "" when no upstream should be hit
		wantSeenPath       string
		wantSeenURI        string // raw request line path; "" = don't check
		wantSeenQuery      string
		wantAllow          string
	}{
		{name: "strip prefix", method: "GET", path: "/api/users/42", wantStatus: 200, wantUpstream: "users", wantSeenPath: "/42"},
		{name: "strip prefix exact", method: "GET", path: "/api/users", wantStatus: 200, wantUpstream: "users", wantSeenPath: "/"},
		{name: "query string survives", method: "GET", path: "/api/users/42?expand=orders", wantStatus: 200, wantUpstream: "users", wantSeenPath: "/42", wantSeenQuery: "expand=orders"},
		{name: "no strip keeps full path", method: "GET", path: "/api/products/7", wantStatus: 200, wantUpstream: "products", wantSeenPath: "/api/products/7"},
		{name: "routes to the right upstream", method: "POST", path: "/api/users/1", wantStatus: 200, wantUpstream: "users", wantSeenPath: "/1"},
		{name: "not found", method: "GET", path: "/health", wantStatus: 404},
		{name: "segment boundary is not a match", method: "GET", path: "/api/usersx", wantStatus: 404},
		{name: "method not allowed", method: "DELETE", path: "/api/users/1", wantStatus: 405, wantAllow: "GET, POST"},
		{name: "encoded slash survives strip", method: "GET", path: "/api/users/a%2Fb", wantStatus: 200, wantUpstream: "users", wantSeenPath: "/a/b", wantSeenURI: "/a%2Fb"},
		{name: "encoded slash survives without strip", method: "GET", path: "/api/products/a%2Fb", wantStatus: 200, wantUpstream: "products", wantSeenPath: "/api/products/a/b", wantSeenURI: "/api/products/a%2Fb"},
		{name: "dot-dot segment rejected", method: "GET", path: "/api/products/../users/1", wantStatus: 400},
		{name: "encoded dot-dot rejected", method: "GET", path: "/api/products/%2e%2e/users/1", wantStatus: 400},
		{name: "encoded prefix does not match", method: "GET", path: "/api/%75sers/1", wantStatus: 404},
		{name: "upstream down", method: "GET", path: "/api/broken/1", wantStatus: 502},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, gateway.URL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := gateway.Client().Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if got := resp.Header.Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if got := resp.Header.Get("X-Upstream"); got != tt.wantUpstream {
				t.Errorf("served by %q, want %q", got, tt.wantUpstream)
			}
			if tt.wantUpstream == "" {
				return
			}
			if got := resp.Header.Get("X-Seen-Path"); got != tt.wantSeenPath {
				t.Errorf("upstream saw path %q, want %q", got, tt.wantSeenPath)
			}
			if got := resp.Header.Get("X-Seen-Query"); got != tt.wantSeenQuery {
				t.Errorf("upstream saw query %q, want %q", got, tt.wantSeenQuery)
			}
			if tt.wantSeenURI != "" {
				if got := resp.Header.Get("X-Seen-URI"); got != tt.wantSeenURI {
					t.Errorf("upstream saw request URI %q, want %q", got, tt.wantSeenURI)
				}
			}
			// SetXForwarded in proxy.New should tell the upstream who the client was.
			if resp.Header.Get("X-Seen-Forwarded-For") == "" {
				t.Error("upstream saw no X-Forwarded-For header")
			}
			body, _ := io.ReadAll(resp.Body)
			if string(body) != tt.wantUpstream {
				t.Errorf("body = %q, want %q", body, tt.wantUpstream)
			}
		})
	}
}
