package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate is a table-driven test skeleton. Each case is a Config and
// whether Validate should reject it. Add a case for every check you implement.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		wantMsg string // optional substring to look for in the error message
	}{
		{
			name: "route references unknown upstream",
			cfg: Config{
				Upstreams: map[string]Upstream{},
				Routes:    []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "unknown upstream",
		},
		{
			name: "duplicate prefix",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{
					{Name: "users1", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}},
					{Name: "users2", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}},
				},
			},
			wantErr: true,
			wantMsg: "duplicate prefix",
		},
		{
			name: "invalid instance URL",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001", "invalid-url"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "upstream \"users\" has instance URL \"invalid-url\" with invalid scheme \"\"\nupstream \"users\" has instance URL \"invalid-url\" with empty host",
		},
		{
			name: "prefix without leading slash",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "invalid prefix",
		},
		{
			name: "invalid method",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"INVALID"}}},
			},
			wantErr: true,
			wantMsg: "invalid method",
		},
		{
			name: "duplicate route name",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{
					{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}},
					{Name: "users", Prefix: "/api/users2", Upstream: "users", Methods: []string{"GET"}},
				},
			},
			wantErr: true,
			wantMsg: "duplicate route name",
		},
		{
			name: "no methods",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{}}},
			},
			wantErr: true,
			wantMsg: "has no methods",
		},
		{
			name: "proxy route without upstream",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "is a proxy route but has no upstream",
		},
		{
			name: "aggregate route with upstream",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Type: "aggregate"}},
			},
			wantErr: true,
			wantMsg: "is an aggregate route but has an upstream",
		},
		{
			name: "trailing slash in prefix",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users/", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: `must not end with "/"`,
		},
		{
			name: "root prefix",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "root", Prefix: "/", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: `must not end with "/"`,
		},
		{
			name: "empty prefix",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: `must start with "/"`,
		},
		{
			name: "url with bad scheme",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"ftp://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "invalid scheme",
		},
		{
			name: "url with empty host",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http:///path"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "empty host",
		},
		{
			name: "valid config",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: false,
			wantMsg: "",
		},
		{
			name: "duplicate prefix with different methods",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{
					{Name: "users1", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}},
					{Name: "users2", Prefix: "/api/users", Upstream: "users", Methods: []string{"POST"}},
				},
			},
			wantErr: true,
			wantMsg: "duplicate prefix",
		},
		{
			name: "bad instance URL with valid scheme but no host",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "empty host",
		},
		{
			name: "prefix without leading slash",
			cfg: Config{
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "api/users", Upstream: "users", Methods: []string{"GET"}}},
			},
			wantErr: true,
			wantMsg: "invalid prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("Validate() error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Run("successful load defaults", func(t *testing.T) {
		path := writeConfig(t, `
upstreams:
  users:
    instances: ["http://localhost:9001"]
routes:
  - name: users
    prefix: /api/users
    upstream: users
    methods: [GET]
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Server.Listen != ":8080" {
			t.Errorf("Listen = %q, want :8080", cfg.Server.Listen)
		}
		if cfg.Routes[0].Auth != "none" {
			t.Errorf("Auth = %q, want none", cfg.Routes[0].Auth)
		}
	})
	t.Run("failed load defaults", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("err = %v, want os.ErrNotExist", err)
		}
	})
}

func TestValidateReportsAllErrors(t *testing.T) {
	cfg := Config{
		Upstreams: map[string]Upstream{
			"users": {Instances: []string{"http://localhost:9001"}},
		},
		Routes: []Route{
			{Name: "a", Prefix: "/api/a/", Upstream: "users", Methods: []string{"GET"}},
			{Name: "b", Prefix: "api/b", Upstream: "users", Methods: []string{"GET"}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want errors")
	}
	for _, want := range []string{`must not end with "/"`, `must start with "/"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error = %q, want it to contain %q", err, want)
		}
	}
}

func TestLoadRejectsInvalidPrefix(t *testing.T) {
	path := writeConfig(t, `
upstreams:
  users:
    instances: ["http://localhost:9001"]
routes:
  - name: users
    prefix: /api/users/
    upstream: users
    methods: [GET]
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() = nil, want error for trailing-slash prefix")
	}
	if !strings.Contains(err.Error(), `must not end with "/"`) {
		t.Fatalf("Load() error = %q, want trailing-slash message", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routes.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
