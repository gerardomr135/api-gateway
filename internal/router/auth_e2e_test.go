package router_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/yourname/api-gateway/internal/config"
	"github.com/yourname/api-gateway/internal/middleware"
	"github.com/yourname/api-gateway/internal/proxy"
	"github.com/yourname/api-gateway/internal/router"
)

// TestGatewayAuthEndToEnd wires the same chain as cmd/gateway/main.go (global
// layers around the router, Auth wrapped per route) in front of a fake
// upstream, and checks what the upstream actually receives for each policy.
func TestGatewayAuthEndToEnd(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Echo the identity headers the upstream saw.
		w.Header().Set("X-Seen-User-ID", r.Header.Get("X-User-ID"))
		w.Header().Set("X-Seen-User-Roles", r.Header.Get("X-User-Roles"))
		io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)

	routes := []config.Route{
		{Name: "secure", Prefix: "/api/secure", Methods: []string{"GET"}, StripPrefix: true, Auth: "required", Timeout: 5 * time.Second},
		{Name: "mixed", Prefix: "/api/mixed", Methods: []string{"GET"}, StripPrefix: true, Auth: "optional", Timeout: 5 * time.Second},
		{Name: "open", Prefix: "/api/open", Methods: []string{"GET"}, StripPrefix: true, Auth: "none", Timeout: 5 * time.Second},
	}
	deps := middleware.AuthDependencies{
		Keys: middleware.StaticKey("secret"), Algorithm: "HS256", Issuer: "iss", Audience: "aud", Leeway: time.Second,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	target, _ := url.Parse(upstream.URL)
	p := proxy.New(target, proxy.NewTransport(proxy.DefaultTransportOptions()), logger)
	handlers := map[string]http.Handler{}
	for _, r := range routes {
		mws := []middleware.Middleware{middleware.Timeout(r.Timeout)}
		if r.Auth != "none" {
			mws = append(mws, middleware.Auth(deps, r.Auth))
		}
		handlers[r.Name] = middleware.Chain(p, mws...)
	}
	rt, err := router.New(routes, handlers)
	if err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(middleware.Chain(rt,
		middleware.Recovery(logger),
		middleware.RequestID(),
		middleware.StripIdentityHeaders(),
		middleware.AccessLog(logger),
	))
	t.Cleanup(gateway.Close)

	token := func(key string, exp time.Time, aud string) string {
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.UserClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "alice", Issuer: "iss", Audience: jwt.ClaimStrings{aud}, ExpiresAt: jwt.NewNumericDate(exp),
			},
			Roles: []string{"admin", "dev"},
		}).SignedString([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	valid := token("secret", time.Now().Add(time.Hour), "aud")

	forged := map[string]string{"X-User-ID": "mallory", "X-User-Roles": "superuser"}
	tests := []struct {
		name, path, auth string
		forge            bool
		wantStatus       int
		wantHit          bool
		wantID, wantRole string
	}{
		{name: "required valid token", path: "/api/secure/x", auth: "Bearer " + valid, wantStatus: 200, wantHit: true, wantID: "alice", wantRole: "admin,dev"},
		{name: "required valid token overwrites forged identity", path: "/api/secure/x", auth: "Bearer " + valid, forge: true, wantStatus: 200, wantHit: true, wantID: "alice", wantRole: "admin,dev"},
		{name: "required no token with forged identity", path: "/api/secure/x", forge: true, wantStatus: 401},
		{name: "required wrong key", path: "/api/secure/x", auth: "Bearer " + token("other", time.Now().Add(time.Hour), "aud"), wantStatus: 401},
		{name: "required expired", path: "/api/secure/x", auth: "Bearer " + token("secret", time.Now().Add(-time.Hour), "aud"), wantStatus: 401},
		{name: "required wrong audience", path: "/api/secure/x", auth: "Bearer " + token("secret", time.Now().Add(time.Hour), "other"), wantStatus: 401},
		{name: "optional anonymous strips forged identity", path: "/api/mixed/x", forge: true, wantStatus: 200, wantHit: true},
		{name: "optional valid token", path: "/api/mixed/x", auth: "Bearer " + valid, wantStatus: 200, wantHit: true, wantID: "alice", wantRole: "admin,dev"},
		{name: "optional invalid token is 401", path: "/api/mixed/x", auth: "Bearer garbage", wantStatus: 401},
		{name: "none strips forged identity", path: "/api/open/x", forge: true, wantStatus: 200, wantHit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := hits.Load()
			req, _ := http.NewRequest("GET", gateway.URL+tt.path, nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			if tt.forge {
				for k, v := range forged {
					req.Header.Set(k, v)
				}
			}
			resp, err := gateway.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if hit := hits.Load() > before; hit != tt.wantHit {
				t.Errorf("upstream hit = %v, want %v", hit, tt.wantHit)
			}
			if got := resp.Header.Get("X-Seen-User-ID"); got != tt.wantID {
				t.Errorf("upstream saw X-User-ID %q, want %q", got, tt.wantID)
			}
			if got := resp.Header.Get("X-Seen-User-Roles"); got != tt.wantRole {
				t.Errorf("upstream saw X-User-Roles %q, want %q", got, tt.wantRole)
			}
			if tt.wantStatus == 401 {
				if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
					t.Errorf("WWW-Authenticate = %q", resp.Header.Get("WWW-Authenticate"))
				}
				var body struct{ Error, RequestID string }
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error != "unauthorized" {
					t.Errorf("body = %+v, err = %v", body, err)
				}
				if resp.Header.Get("X-Request-ID") == "" {
					t.Error("401 response has no request id")
				}
			}
		})
	}
}
