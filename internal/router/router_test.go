package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourname/api-gateway/internal/config"
)

// These cases encode the matching rules from the package doc. Write the
// implementation until they all pass, then add your own edge cases
// (trailing slashes? double slashes? URL-encoded segments like %2F?).
func TestMatch(t *testing.T) {
	routes := []config.Route{
		{Name: "api", Prefix: "/api", Methods: []string{"GET"}},
		{Name: "user", Prefix: "/api/user", Methods: []string{"GET"}},
		{Name: "users", Prefix: "/api/users", Methods: []string{"GET", "POST"}, StripPrefix: true},
	}
	handlers := map[string]http.Handler{
		"api":   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		"user":  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		"users": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}
	rt, err := New(routes, handlers)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	tests := []struct {
		name         string
		method, path string
		wantRoute    string // "" when an error is expected
		wantUpPath   string
		wantErr      error
	}{
		{"exact prefix", "GET", "/api/users", "users", "/", nil},
		{"longest prefix wins", "GET", "/api/users/42", "users", "/42", nil},
		{"segment boundary", "GET", "/api/username", "api", "/api/username", nil},
		{"shorter route", "GET", "/api/user/7", "user", "/api/user/7", nil},
		{"no match", "GET", "/health", "", "", ErrNotFound},
		{"wrong method", "DELETE", "/api/users/1", "", "", ErrMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := rt.Match(tt.method, tt.path)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if m.Route.Name != tt.wantRoute || m.UpstreamPath != tt.wantUpPath {
				t.Fatalf("got (%s, %s), want (%s, %s)", m.Route.Name, m.UpstreamPath, tt.wantRoute, tt.wantUpPath)
			}
		})
	}
}

func TestServeHTTP(t *testing.T) {
	routes := []config.Route{
		{Name: "api", Prefix: "/api", Methods: []string{"GET"}},
		{Name: "users", Prefix: "/api/users", Methods: []string{"GET", "POST"}, StripPrefix: true},
	}

	// Each fake handler records which route served the request and the path it saw.
	var gotRoute, gotPath string
	record := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotRoute, gotPath = name, r.URL.Path
			w.WriteHeader(http.StatusOK)
		})
	}
	rt, err := New(routes, map[string]http.Handler{"api": record("api"), "users": record("users")})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantAllow          string // "" when no Allow header is expected
		wantRoute          string // "" when no handler should run
		wantPath           string
	}{
		{"not found", "GET", "/health", http.StatusNotFound, "", "", ""},
		{"method not allowed", "DELETE", "/api/users/1", http.StatusMethodNotAllowed, "GET, POST", "", ""},
		{"strip prefix", "GET", "/api/users/42", http.StatusOK, "", "users", "/42"},
		{"strip prefix exact", "GET", "/api/users", http.StatusOK, "", "users", "/"},
		{"no strip keeps path", "GET", "/api/other", http.StatusOK, "", "api", "/api/other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRoute, gotPath = "", ""
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			rt.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if gotRoute != tt.wantRoute || gotPath != tt.wantPath {
				t.Errorf("handler saw (%q, %q), want (%q, %q)", gotRoute, gotPath, tt.wantRoute, tt.wantPath)
			}
			// Error responses must be written exactly once (the double-write bug
			// appended a 500 message after the 404/405 one).
			if tt.wantStatus >= 400 && strings.Contains(rec.Body.String(), "internal server error") {
				t.Errorf("body has a second error written: %q", rec.Body.String())
			}
			// The caller's request must not be mutated by strip_prefix.
			if req.URL.Path != tt.path {
				t.Errorf("original request path mutated to %q", req.URL.Path)
			}
		})
	}
}
