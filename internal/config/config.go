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
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
	data, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config.Load(%q): %w", path, err)
	}
	defer data.Close()

	cfg := &Config{}

	yamlDecoder := yaml.NewDecoder(data)
	yamlDecoder.KnownFields(true) // catch typos in configs/routes.yaml

	err = yamlDecoder.Decode(cfg)
	if errors.Is(err, io.EOF) { // Decode (unlike Unmarshal) errors on an empty file
		err = errors.New("config is empty")
	}
	if err != nil {
		return nil, fmt.Errorf("config.Load(%q): %w", path, err)
	}

	applyDefaults(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config.Load(%q): %w", path, err)
	}

	return cfg, nil
}

// applyDefaults fills zero values with sensible defaults (e.g. Listen ":8080",
// Auth "none", Timeout a few seconds). Decide: should defaults live here, or
// should a missing timeout be a validation error? Write your reasoning in NOTES.md.
func applyDefaults(cfg *Config) {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}
	if cfg.Server.AdminListen == "" {
		cfg.Server.AdminListen = ":9090"
	}
	if cfg.Server.ReadHeaderTimeout == 0 {
		cfg.Server.ReadHeaderTimeout = 5 * time.Second
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 30 * time.Second
	}

	for i := range cfg.Routes {
		route := &cfg.Routes[i]
		if route.Auth == "" {
			route.Auth = "none"
		}
		if route.Timeout == 0 {
			route.Timeout = 5 * time.Second
		}
	}
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
	errs := []error{}

	upstreamNames := map[string]struct{}{}
	for name, upstream := range c.Upstreams {
		if len(upstream.Instances) == 0 {
			errs = append(errs, fmt.Errorf("upstream %q has no instances", name))
		}

		for _, instance := range upstream.Instances {
			instanceURL, err := url.Parse(instance)
			if err != nil {
				errs = append(errs, fmt.Errorf("upstream %q has invalid instance URL %q", name, instance))
				continue
			}

			if instanceURL.Scheme != "http" && instanceURL.Scheme != "https" {
				errs = append(errs, fmt.Errorf("upstream %q has instance URL %q with invalid scheme %q", name, instance, instanceURL.Scheme))
			}

			if instanceURL.Host == "" {
				errs = append(errs, fmt.Errorf("upstream %q has instance URL %q with empty host", name, instance))
			}
		}
		upstreamNames[name] = struct{}{}
	}

	routeNames := map[string]struct{}{}
	prefixes := map[string]struct{}{}
	for _, route := range c.Routes {
		if _, ok := upstreamNames[route.Upstream]; !ok && route.Type != "aggregate" {
			errs = append(errs, fmt.Errorf("route %q references unknown upstream %q", route.Name, route.Upstream))
		}
		if _, ok := prefixes[route.Prefix]; ok {
			errs = append(errs, fmt.Errorf("duplicate prefix %q", route.Prefix))
		} else {
			prefixes[route.Prefix] = struct{}{}
		}
		switch {
		case !strings.HasPrefix(route.Prefix, "/"):
			errs = append(errs, fmt.Errorf("invalid prefix %q: must start with \"/\"", route.Prefix))
		case strings.HasSuffix(route.Prefix, "/"):
			// Also rejects "/": the router matches on prefix+"/" segment
			// boundaries, so a trailing slash makes the route never match.
			errs = append(errs, fmt.Errorf("invalid prefix %q: must not end with \"/\"", route.Prefix))
		}
		for _, method := range route.Methods {
			if !isValidHTTPMethod(method) {
				errs = append(errs, fmt.Errorf("route %q has invalid method %q", route.Name, method))
			}
		}
		if _, ok := routeNames[route.Name]; ok {
			errs = append(errs, fmt.Errorf("duplicate route name %q", route.Name))
		} else {
			routeNames[route.Name] = struct{}{}
		}
		if len(route.Methods) == 0 {
			errs = append(errs, fmt.Errorf("route %q has no methods", route.Name))
		}
		//  Empty Upstream on a proxy route. Only the unknown-name check exists. Requiring an upstream for non-aggregate routes, and none for aggregate ones, is still not enforced.
		if route.Type != "aggregate" && route.Upstream == "" {
			errs = append(errs, fmt.Errorf("route %q is a proxy route but has no upstream", route.Name))
		}
		if route.Type == "aggregate" && route.Upstream != "" {
			errs = append(errs, fmt.Errorf("route %q is an aggregate route but has an upstream", route.Name))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func isValidHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
