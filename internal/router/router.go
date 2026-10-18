// Package router matches an incoming request to a configured route (M2).
//
// Matching rule: LONGEST PREFIX on PATH-SEGMENT boundaries.
//
//	prefix /api/user   matches /api/user, /api/user/7
//	prefix /api/user   does NOT match /api/username   (not a segment boundary!)
//	prefixes /api and /api/users both match /api/users/1 -> pick /api/users
//
// Paths are matched in their ESCAPED form (URL.EscapedPath), never decoded:
// "%2F" is data, not a separator, and "/api/%75sers" must not sneak onto the
// "/api/users" route. Anything that doesn't match literally fails closed (404).
// Dot-segments ("." / "..") are rejected up front (400) because the gateway and
// the upstream could otherwise resolve different paths.
//
// The router only decides "which route?". It does not proxy, authenticate or
// rate limit. Keeping it a pure function of (method, path) makes it trivial
// to unit-test and to benchmark (M11: `go test -bench . -benchmem`).
//
// Hot reload (M11) will swap a whole *Router atomically, so treat a Router
// as immutable after New returns: no setters, no locks.
package router

import (
	"cmp"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/yourname/api-gateway/internal/config"
)

// Router holds the compiled route table.
type Router struct {
	// A radix tree is the "real" answer for thousands; don't start there.
	routes   []config.Route
	handlers map[string]http.Handler // keyed by route name
}

// Match is the result of a successful lookup.
type Match struct {
	Route *config.Route
	// UpstreamPath is the path to send upstream (after strip_prefix).
	UpstreamPath string
}

// New compiles the route table. It may assume cfg was already validated.
func New(routes []config.Route, handlers map[string]http.Handler) (*Router, error) {
	routes = slices.Clone(routes)
	handlers = maps.Clone(handlers)

	slices.SortFunc(routes, func(a, b config.Route) int {
		return cmp.Compare(len(b.Prefix), len(a.Prefix))
	})

	for _, route := range routes {
		if _, ok := handlers[route.Name]; !ok {
			return nil, errors.New("router: missing handler for route " + route.Name)
		}
	}

	return &Router{routes: routes, handlers: handlers}, nil
}

// Match finds the route for a request. path must be the ESCAPED request path.
//
// Return values to design:
//   - (match, nil)                     -> found
//   - (nil, ErrNotFound)               -> no prefix matched        -> 404
//   - (nil, ErrMethodNotAllowed + set) -> prefix matched, bad method -> 405 + Allow header
//
// Think about how the caller learns the allowed methods for the Allow header.
func (rt *Router) Match(method, path string) (*Match, error) {
	for i := range rt.routes {
		if path == rt.routes[i].Prefix || strings.HasPrefix(path, rt.routes[i].Prefix+"/") {
			if !slices.Contains(rt.routes[i].Methods, method) {
				return nil, &MethodNotAllowedError{Allowed: rt.routes[i].Methods}
			}

			upstreamPath := path
			//Compute UpstreamPath. If StripPrefix is false, it's path unchanged. If it's true, it's strings.TrimPrefix(path, prefix), and an empty result becomes "/". The test expects /api/users → / and /api/users/42 → /42.
			if rt.routes[i].StripPrefix {
				upstreamPath = strings.TrimPrefix(path, rt.routes[i].Prefix)
				if upstreamPath == "" {
					upstreamPath = "/"
				}
			}

			// If StripPrefix is false and path is longer than prefix, UpstreamPath is path unchanged.
			return &Match{Route: &rt.routes[i], UpstreamPath: upstreamPath}, nil
		}
	}

	return nil, ErrNotFound
}

// ServeHTTP lets the Router act as the terminal handler: match, then dispatch
// to the route's proxy. You'll need a way to look up a handler per route,
// e.g. a map[string]http.Handler keyed by route name, passed into New.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if hasDotSegment(r.URL.Path) {
		http.Error(w, "router: bad request", http.StatusBadRequest)
		return
	}

	match, err := rt.Match(r.Method, r.URL.EscapedPath())
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, "router: not found", http.StatusNotFound)
		case errors.Is(err, ErrMethodNotAllowed):
			if e, ok := errors.AsType[*MethodNotAllowedError](err); ok {
				w.Header().Set("Allow", strings.Join(e.Allowed, ", "))
			}
			http.Error(w, "router: method not allowed", http.StatusMethodNotAllowed)
		default:
			http.Error(w, "router: internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Path and RawPath must move together: Path is the decoded form we route
	// on, RawPath keeps the client's encoding (e.g. %2F) for the upstream.
	decoded, err := url.PathUnescape(match.UpstreamPath)
	if err != nil {
		http.Error(w, "router: bad request", http.StatusBadRequest)
		return
	}

	r2 := r.Clone(r.Context())
	r2.URL.Path = decoded
	r2.URL.RawPath = match.UpstreamPath

	rt.handlers[match.Route.Name].ServeHTTP(w, r2)
}

// hasDotSegment reports whether a decoded path has a "." or ".." segment.
// Checking the decoded form also catches "%2e%2e".
func hasDotSegment(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}
