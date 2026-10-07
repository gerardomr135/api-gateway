// Package config loads and validates the gateway's route table (M2).
//
// Two rules for this package:
//
//  1. Parse, then VALIDATE, then hand out an immutable value. Nobody downstream
//     should ever need to re-check "is this URL valid?" at request time.
//  2. Fail fast. A gateway that starts with a broken route table is worse than
//     one that refuses to start: the second is caught by the deploy, the first
//     by your users.
//
// The struct tags below match configs/routes.yaml. To parse YAML you may add
// gopkg.in/yaml.v3 (the one dependency allowed for M2), or switch the file to
// JSON and stay pure stdlib. Check whether your chosen decoder handles
// time.Duration from strings like "5s"; if not, you'll need a custom type
// with an UnmarshalYAML/UnmarshalJSON method.
package config

import (
	"errors"
	"fmt"
	"time"
)

// Config is the root of routes.yaml.
type Config struct {
	Server    Server              `yaml:"server"`
	Upstreams map[string]Upstream `yaml:"upstreams"`
	Routes    []Route             `yaml:"routes"`
}

// Server holds listener settings.
type Server struct {
	Listen            string        `yaml:"listen"`
	AdminListen       string        `yaml:"admin_listen"`        // M10/M11
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"` // M4
	IdleTimeout       time.Duration `yaml:"idle_timeout"`        // M4
}

// Upstream is a named group of interchangeable backend instances.
type Upstream struct {
	Instances      []string        `yaml:"instances"`       // raw URLs; validate them!
	Balancer       string          `yaml:"balancer"`        // M7: round_robin | least_conn
	HealthCheck    *HealthCheck    `yaml:"health_check"`    // M7: nil = no active checks
	CircuitBreaker *CircuitBreaker `yaml:"circuit_breaker"` // M8
	MaxConcurrent  int             `yaml:"max_concurrent"`  // M8 bulkhead; 0 = unlimited
}

// HealthCheck configures active probing (M7).
type HealthCheck struct {
	Path           string        `yaml:"path"`
	Interval       time.Duration `yaml:"interval"`
	UnhealthyAfter int           `yaml:"unhealthy_after"`
	HealthyAfter   int           `yaml:"healthy_after"`
}

// CircuitBreaker configures per-upstream breaking (M8).
type CircuitBreaker struct {
	FailureThreshold    int           `yaml:"failure_threshold"`
	OpenFor             time.Duration `yaml:"open_for"`
	HalfOpenMaxRequests int           `yaml:"half_open_max_requests"`
}

// Route maps a public path prefix to an upstream plus per-route policy.
type Route struct {
	Name        string        `yaml:"name"`
	Prefix      string        `yaml:"prefix"`
	StripPrefix bool          `yaml:"strip_prefix"`
	Methods     []string      `yaml:"methods"`
	Upstream    string        `yaml:"upstream"`   // key into Config.Upstreams
	Type        string        `yaml:"type"`       // "" (proxy) | "aggregate" (M9)
	Timeout     time.Duration `yaml:"timeout"`    // M4
	Auth        string        `yaml:"auth"`       // M5: required | optional | none
	RateLimit   *RateLimit    `yaml:"rate_limit"` // M6
	Retry       *Retry        `yaml:"retry"`      // M8
}

// RateLimit is a token-bucket policy (M6).
type RateLimit struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

// Retry is a per-route retry policy (M8).
type Retry struct {
	MaxAttempts int           `yaml:"max_attempts"`
	BaseBackoff time.Duration `yaml:"base_backoff"`
}

// Load reads the file at path, decodes it, applies defaults and validates it.
func Load(path string) (*Config, error) {
	// TODO(M2):
	//   1. os.ReadFile(path)
	//   2. decode into a Config
	//   3. applyDefaults(&cfg)
	//   4. if err := cfg.Validate(); err != nil { return nil, err }
	// Wrap errors with context: fmt.Errorf("config %s: %w", path, err).
	return nil, fmt.Errorf("config.Load(%q): %w", path, errors.ErrUnsupported)
}

// applyDefaults fills zero values with sensible defaults (e.g. Listen ":8080",
// Auth "none", Timeout a few seconds). Decide: should defaults live here, or
// should a missing timeout be a validation error? Write your reasoning in NOTES.md.
func applyDefaults(cfg *Config) {
	// TODO(M2)
}

// Validate checks the whole config and reports ALL problems, not just the
// first one (hint: errors.Join, Go 1.20+). Ideas for checks:
//   - every route references an upstream that exists
//   - no two routes share the same prefix
//   - prefixes start with "/" and don't end with "/"
//   - every instance URL parses and has scheme http/https and a host
//   - methods are valid HTTP methods
//   - aggregate routes don't set an upstream; proxy routes must
func (c *Config) Validate() error {
	// TODO(M2)
	return nil
}
