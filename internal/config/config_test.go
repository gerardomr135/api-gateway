package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Auth: "none"}},
			},
			wantErr: false,
			wantMsg: "",
		},
		{
			name: "write_timeout above route timeout",
			cfg: Config{
				Server: Server{WriteTimeout: 15 * time.Second},
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Auth: "none", Timeout: 3 * time.Second}},
			},
			wantErr: false,
			wantMsg: "",
		},
		{
			name: "write_timeout below route timeout",
			cfg: Config{
				Server: Server{WriteTimeout: 2 * time.Second},
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Timeout: 3 * time.Second}},
			},
			wantErr: true,
			wantMsg: "must exceed the largest route",
		},
		{
			name: "write_timeout equal to route timeout",
			cfg: Config{
				Server: Server{WriteTimeout: 3 * time.Second},
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Timeout: 3 * time.Second}},
			},
			wantErr: true,
			wantMsg: "must exceed the largest route",
		},
		{
			name: "route timeout above response_header_timeout",
			cfg: Config{
				Server: Server{ResponseHeaderTimeout: 10 * time.Second},
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Timeout: 30 * time.Second}},
			},
			wantErr: true,
			wantMsg: "exceeds response_header_timeout",
		},
		{
			name: "route timeout equal to response_header_timeout",
			cfg: Config{
				Server: Server{ResponseHeaderTimeout: 3 * time.Second},
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Auth: "none", Timeout: 3 * time.Second}},
			},
			wantErr: false,
			wantMsg: "",
		},
		{
			name: "write_timeout zero means disabled",
			cfg: Config{
				Server: Server{WriteTimeout: 0},
				Upstreams: map[string]Upstream{
					"users": {Instances: []string{"http://localhost:9001"}},
				},
				Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Auth: "none", Timeout: 3 * time.Second}},
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
		t.Setenv("TEST_SECRET", strings.Repeat("k", 32))
		path := writeConfig(t, `
auth:
  algorithm: HS256
  secret_env: TEST_SECRET
  issuer: http://auth.local
  audience: api-gateway
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
		if cfg.Routes[0].Auth != "required" {
			t.Errorf("Auth = %q, want required (fail closed)", cfg.Routes[0].Auth)
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

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, `
upstreams:
  users:
    instances: ["http://localhost:9001"]
routes:
  - name: users
    prefix: /api/users
    strip_prefx: true
    upstream: users
    methods: [GET]
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() = nil, want error for unknown key")
	}
	if !strings.Contains(err.Error(), "strip_prefx") {
		t.Fatalf("Load() error = %q, want it to name the unknown key", err)
	}
}

func TestLoadRejectsEmptyFile(t *testing.T) {
	if _, err := Load(writeConfig(t, "")); err == nil {
		t.Fatal("Load() = nil, want error for empty config")
	}
}

// The shipped sample must stay loadable under strict decoding.
func TestLoadSampleConfig(t *testing.T) {
	t.Setenv("GATEWAY_JWT_SECRET", strings.Repeat("s", 32))
	if _, err := Load("../../configs/routes.yaml"); err != nil {
		t.Fatalf("Load(sample) error = %v", err)
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

// authCfg returns a valid config with one route using the given policy and a
// valid HS256 auth section; tests mutate it to break one thing at a time.
func authCfg(policy string) Config {
	return Config{
		Upstreams: map[string]Upstream{
			"users": {Instances: []string{"http://localhost:9001"}},
		},
		Routes: []Route{{Name: "users", Prefix: "/api/users", Upstream: "users", Methods: []string{"GET"}, Auth: policy}},
		Auth: &Auth{
			Algorithm: "HS256",
			SecretEnv: "TEST_SECRET",
			Issuer:    "http://auth.local",
			Audience:  "api-gateway",
		},
	}
}

// rs256 turns the HS256 auth section from authCfg into a valid RS256 one.
func rs256(c *Config) {
	c.Auth.Algorithm = "RS256"
	c.Auth.SecretEnv = ""
	c.Auth.JWKSURL = "http://auth:9100/.well-known/jwks.json"
	c.Auth.JWKSRefresh = time.Minute
}

func TestValidateAuth(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantMsg []string // every substring must appear; empty means valid
	}{
		{"valid required", func(c *Config) {}, nil},
		{"valid optional", func(c *Config) { c.Routes[0].Auth = "optional" }, nil},
		{"no auth section when no route uses auth", func(c *Config) {
			c.Auth = nil
			c.Routes[0].Auth = "none"
		}, nil},
		{"typo in route policy", func(c *Config) { c.Routes[0].Auth = "requird" }, []string{`route "users"`, `"requird"`, "want required, optional or none"}},
		{"empty route policy", func(c *Config) { c.Routes[0].Auth = "" }, []string{"invalid auth"}},
		{"auth route without auth section", func(c *Config) { c.Auth = nil }, []string{`"users"`, `top-level "auth" section is missing`}},
		{"unsupported algorithm", func(c *Config) { c.Auth.Algorithm = "ES256" }, []string{"auth.algorithm", `"ES256"`, "want HS256 or RS256"}},
		{"valid RS256", rs256, nil},
		{"RS256 does not need secret_env", func(c *Config) { rs256(c); c.Auth.SecretEnv = "" }, nil},
		{"RS256 missing jwks_url", func(c *Config) { rs256(c); c.Auth.JWKSURL = "" }, []string{"auth.jwks_url is required for RS256"}},
		{"RS256 jwks_url not http", func(c *Config) { rs256(c); c.Auth.JWKSURL = "ftp://auth/jwks" }, []string{"auth.jwks_url must be an absolute http(s) URL"}},
		{"RS256 jwks_url without host", func(c *Config) { rs256(c); c.Auth.JWKSURL = "http:///jwks" }, []string{"auth.jwks_url must be an absolute http(s) URL"}},
		{"RS256 jwks_url relative", func(c *Config) { rs256(c); c.Auth.JWKSURL = "/.well-known/jwks.json" }, []string{"auth.jwks_url must be an absolute http(s) URL"}},
		{"RS256 negative jwks_refresh", func(c *Config) { rs256(c); c.Auth.JWKSRefresh = -time.Second }, []string{"auth.jwks_refresh", "-1s"}},
		{"HS256 with jwks_url", func(c *Config) { c.Auth.JWKSURL = "http://auth:9100/jwks" }, []string{"only apply to RS256"}},
		{"HS256 with jwks_refresh", func(c *Config) { c.Auth.JWKSRefresh = time.Minute }, []string{"only apply to RS256"}},
		{"alg none", func(c *Config) { c.Auth.Algorithm = "none" }, []string{"auth.algorithm", `"none"`}},
		{"empty algorithm", func(c *Config) { c.Auth.Algorithm = "" }, []string{"auth.algorithm"}},
		{"missing secret_env", func(c *Config) { c.Auth.SecretEnv = "" }, []string{"auth.secret_env"}},
		{"missing issuer", func(c *Config) { c.Auth.Issuer = "" }, []string{"auth.issuer"}},
		{"missing audience", func(c *Config) { c.Auth.Audience = "" }, []string{"auth.audience"}},
		{"negative leeway", func(c *Config) { c.Auth.Leeway = -time.Second }, []string{"auth.leeway", "-1s"}},
		{"reports every auth problem at once", func(c *Config) {
			c.Auth.Issuer = ""
			c.Auth.Audience = ""
			c.Auth.Leeway = -time.Second
		}, []string{"auth.issuer", "auth.audience", "auth.leeway"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := authCfg("required")
			tt.mutate(&cfg)

			err := cfg.Validate()
			if len(tt.wantMsg) == 0 {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantMsg)
			}
			for _, want := range tt.wantMsg {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Validate() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadResolvesSecret(t *testing.T) {
	const yamlText = `
auth:
  algorithm: HS256
  secret_env: TEST_SECRET
  issuer: http://auth.local
  audience: api-gateway
upstreams:
  users:
    instances: ["http://localhost:9001"]
routes:
  - name: users
    prefix: /api/users
    upstream: users
    methods: [GET]
    auth: required
`

	t.Run("reads the secret from the environment", func(t *testing.T) {
		secret := strings.Repeat("k", 32)
		t.Setenv("TEST_SECRET", secret)

		cfg, err := Load(writeConfig(t, yamlText))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if string(cfg.Auth.Secret) != secret {
			t.Errorf("Auth.Secret was not resolved from TEST_SECRET")
		}
	})

	t.Run("unset variable", func(t *testing.T) {
		t.Setenv("TEST_SECRET", "")

		_, err := Load(writeConfig(t, yamlText))
		if err == nil || !strings.Contains(err.Error(), `"TEST_SECRET" is not set`) {
			t.Fatalf("Load() error = %v, want it to name the unset variable", err)
		}
	})

	t.Run("short secret is rejected without leaking it", func(t *testing.T) {
		secret := strings.Repeat("k", 31)
		t.Setenv("TEST_SECRET", secret)

		_, err := Load(writeConfig(t, yamlText))
		if err == nil || !strings.Contains(err.Error(), "31 bytes") {
			t.Fatalf("Load() error = %v, want it to report the 31-byte length", err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks the secret: %q", err)
		}
	})

	t.Run("secret is never read from the YAML file", func(t *testing.T) {
		t.Setenv("TEST_SECRET", strings.Repeat("k", 32))

		_, err := Load(writeConfig(t, strings.Replace(yamlText, "audience: api-gateway", "audience: api-gateway\n  Secret: aaaa", 1)))
		if err == nil {
			t.Fatal("Load() accepted a secret written in the YAML file")
		}
	})
}

func TestLoadRS256(t *testing.T) {
	const yamlText = `
auth:
  algorithm: RS256
  jwks_url: http://auth:9100/.well-known/jwks.json
  issuer: http://auth.local
  audience: api-gateway
upstreams:
  users:
    instances: ["http://localhost:9001"]
routes:
  - name: users
    prefix: /api/users
    upstream: users
    methods: [GET]
`

	t.Run("loads without any secret in the environment and defaults the refresh", func(t *testing.T) {
		t.Setenv("TEST_SECRET", "") // RS256 must not look for a shared secret

		cfg, err := Load(writeConfig(t, yamlText))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Auth.Secret != nil {
			t.Errorf("Auth.Secret = %q, want nil for RS256", cfg.Auth.Secret)
		}
		if cfg.Auth.JWKSRefresh != defaultJWKSRefresh {
			t.Errorf("JWKSRefresh = %s, want default %s", cfg.Auth.JWKSRefresh, defaultJWKSRefresh)
		}
	})

	t.Run("an explicit jwks_refresh is kept", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, strings.Replace(yamlText, "  audience: api-gateway\n", "  audience: api-gateway\n  jwks_refresh: 15s\n", 1)))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Auth.JWKSRefresh != 15*time.Second {
			t.Errorf("JWKSRefresh = %s, want 15s", cfg.Auth.JWKSRefresh)
		}
	})
}
