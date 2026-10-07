// Package router matches an incoming request to a configured route (M2).
//
// Matching rule: LONGEST PREFIX on PATH-SEGMENT boundaries.
//
//	prefix /api/user   matches /api/user, /api/user/7
//	prefix /api/user   does NOT match /api/username   (not a segment boundary!)
//	prefixes /api and /api/users both match /api/users/1 -> pick /api/users
//
// The router only decides "which route?". It does not proxy, authenticate or
// rate limit. Keeping it a pure function of (method, path) makes it trivial
// to unit-test and to benchmark (M11: `go test -bench . -benchmem`).
//
// Hot reload (M11) will swap a whole *Router atomically, so treat a Router
// as immutable after New returns: no setters, no locks.
package router

import (
	"net/http"

	"github.com/yourname/api-gateway/internal/config"
)

// Router holds the compiled route table.
type Router struct {
	// TODO(M2): choose a data structure. A slice sorted by prefix length
	// (longest first) is simple and fast enough for tens of routes.
	// A radix tree is the "real" answer for thousands; don't start there.
	routes []config.Route
}

// Match is the result of a successful lookup.
type Match struct {
	Route *config.Route
	// UpstreamPath is the path to send upstream (after strip_prefix).
	UpstreamPath string
}

// New compiles the route table. It may assume cfg was already validated.
func New(routes []config.Route) *Router {
	// TODO(M2): copy and sort the routes; never keep a reference to a slice
	// the caller might mutate later.
	return &Router{routes: routes}
}

// Match finds the route for a request.
//
// Return values to design:
//   - (match, nil)                     -> found
//   - (nil, ErrNotFound)               -> no prefix matched        -> 404
//   - (nil, ErrMethodNotAllowed + set) -> prefix matched, bad method -> 405 + Allow header
//
// Think about how the caller learns the allowed methods for the Allow header.
func (rt *Router) Match(method, path string) (*Match, error) {
	// TODO(M2)
	return nil, ErrNotFound
}

// ServeHTTP lets the Router act as the terminal handler: match, then dispatch
// to the route's proxy. You'll need a way to look up a handler per route,
// e.g. a map[string]http.Handler keyed by route name, passed into New.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO(M2)
	http.Error(w, "router: not implemented", http.StatusNotImplemented)
}
