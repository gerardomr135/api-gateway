package middleware

import (
	"context"
	"sync/atomic"
)

// UnmatchedRoute is what RouteNameFrom reports when no route was selected
// (404, 405, 400) or when no holder is in the context.
const UnmatchedRoute = "-"

const routeHolderKey ctxKey = iota + 1

// routeHolder is a mutable slot that travels INWARD in the request context.
// Context values only flow downward (a child context is invisible to the
// layer that created its parent), so the router can't hand its decision back
// to AccessLog with context.WithValue. Instead AccessLog plants a pointer and
// the router fills it in.
type routeHolder struct {
	name atomic.Pointer[string]
}

// WithRouteHolder returns ctx carrying an empty route-name slot.
func WithRouteHolder(ctx context.Context) context.Context {
	return context.WithValue(ctx, routeHolderKey, &routeHolder{})
}

// SetRouteName records the matched route's name in the slot planted by
// WithRouteHolder. It is a no-op if ctx has no slot, so the router stays
// usable without AccessLog (e.g. in router tests).
func SetRouteName(ctx context.Context, name string) {
	if h, ok := ctx.Value(routeHolderKey).(*routeHolder); ok {
		h.name.Store(&name)
	}
}

// RouteNameFrom returns the matched route's name, or UnmatchedRoute.
func RouteNameFrom(ctx context.Context) string {
	if h, ok := ctx.Value(routeHolderKey).(*routeHolder); ok {
		if name := h.name.Load(); name != nil {
			return *name
		}
	}
	return UnmatchedRoute
}
