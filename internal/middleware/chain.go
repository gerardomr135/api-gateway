// Package middleware holds the gateway's cross-cutting concerns (M3, M5, M6).
//
// Every middleware has the same shape: it takes the next handler and returns
// a new handler that does something before and/or after calling next.
//
//	Chain(h, A, B, C)  ==  A(B(C(h)))
//
//	request  ──► A ──► B ──► C ──► h
//	response ◄── A ◄── B ◄── C ◄── h
//
// That's the "onion": the FIRST middleware listed is the OUTERMOST layer.
// It sees the request first and the response last.
package middleware

import "net/http"

// Middleware decorates an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain wraps h with mws so that mws[0] is the outermost layer.
//
// Before implementing: on paper, work out which direction you must iterate
// over mws to get mws[0] outermost. Then prove it with chain_test.go.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	// TODO(M3)
	return h
}
