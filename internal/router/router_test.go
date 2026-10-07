package router

import (
	"errors"
	"testing"

	"github.com/yourname/api-gateway/internal/config"
)

// These cases encode the matching rules from the package doc. Write the
// implementation until they all pass, then add your own edge cases
// (trailing slashes? double slashes? URL-encoded segments like %2F?).
func TestMatch(t *testing.T) {
	t.Skip("TODO(M2): remove this Skip when you start implementing Match")

	routes := []config.Route{
		{Name: "api", Prefix: "/api", Methods: []string{"GET"}},
		{Name: "user", Prefix: "/api/user", Methods: []string{"GET"}},
		{Name: "users", Prefix: "/api/users", Methods: []string{"GET", "POST"}, StripPrefix: true},
	}
	rt := New(routes)

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
