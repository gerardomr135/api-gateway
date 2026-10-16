package router

import "errors"

// Sentinel errors let callers branch with errors.Is without string matching.
// If you need to carry extra data (like the allowed methods for a 405),
// consider a custom error type and errors.As instead. Decide which and why.
var (
	ErrNotFound         = errors.New("router: no route matched")
	ErrMethodNotAllowed = errors.New("router: method not allowed")
)

type MethodNotAllowedError struct{ Allowed []string }

func (e *MethodNotAllowedError) Error() string {
	return "router: method not allowed"
}

func (e *MethodNotAllowedError) Is(target error) bool { return target == ErrMethodNotAllowed }
