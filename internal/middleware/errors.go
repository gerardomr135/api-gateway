package middleware

import (
	"encoding/json"
	"net/http"
)

// Error codes used in the JSON error body. They are part of the gateway's
// public contract: clients can switch on them, unlike the human-readable
// status text.
const (
	CodeBadRequest       = "bad_request"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeInternal         = "internal_error"
	CodeBadGateway       = "bad_gateway"
	CodeNotImplemented   = "not_implemented"
	CodeTimeout          = "timeout"
	CodeRequestTimeout   = "request_timeout"
)

// messages are the human-readable explanations paired with each code. They
// are fixed strings on purpose: never put err.Error() or a panic value here,
// or internal details leak to clients.
var messages = map[string]string{
	CodeBadRequest:       "The request could not be understood.",
	CodeNotFound:         "No route matches the requested path.",
	CodeMethodNotAllowed: "The method is not allowed for this route.",
	CodeInternal:         "The gateway hit an unexpected error.",
	CodeBadGateway:       "The upstream service could not be reached.",
	CodeNotImplemented:   "This endpoint is not implemented yet.",
	CodeTimeout:          "The upstream service took too long to respond.",
	CodeRequestTimeout:   "The request took too long to send.",
}

type errorBody struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteError writes the gateway's standard JSON error response:
//
//	{"error":"<code>","message":"<explanation>","request_id":"<id>"}
//
// It lives in middleware (not its own package) because it needs
// RequestIDFrom and Recovery itself uses it; router and proxy already import
// middleware, so every error path shares one format without a new dependency.
// Headers set earlier (e.g. Allow on a 405) are preserved.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	msg, ok := messages[code]
	if !ok {
		msg = http.StatusText(status)
	}
	_ = json.NewEncoder(w).Encode(errorBody{Error: code, Message: msg, RequestID: requestIDFor(w, r)})
}

// requestIDFor returns the request ID for r, falling back to the response
// header. Recovery is the outermost middleware, so the request it holds
// predates RequestID's context.WithValue and carries no ID; but RequestID
// already set the header on w, which is the same writer Recovery sees.
func requestIDFor(w http.ResponseWriter, r *http.Request) string {
	if id := RequestIDFrom(r.Context()); id != "" {
		return id
	}
	return w.Header().Get(HeaderRequestID)
}
