// Package aggregate implements API composition endpoints (M9).
//
// GET /api/order-summary/{id}
//
//	             ┌─► GET orders/{id} ─┐  (sequential: we need user_id, product_ids)
//	client ──►  gw                    ├─► GET users/{user_id}      ┐ parallel
//	                                  └─► GET products?ids=...     ┘
//	             ◄── one JSON document, with "warnings" if a part failed
//
// Latency is order + max(users, products), not the sum. Measure it.
package aggregate

import (
	"net/http"
)

// OrderSummary is the composed response. A nil field encodes as JSON null,
// which tells the client "this part failed" instead of a misleading zero value.
// TODO(M9): replace `any` with real types (or json.RawMessage to pass upstream
// JSON through untouched) and decide which is the better gateway behavior.
type OrderSummary struct {
	Order    any      `json:"order"`
	User     any      `json:"user"`
	Products any      `json:"products"`
	Warnings []string `json:"warnings,omitempty"`
}

// Handler serves the order summary. It calls upstreams through an
// *http.Client whose Transport is the SAME resilient RoundTripper stack the
// proxies use (M4 + M8), so timeouts, retries and breakers apply here too.
type Handler struct {
	Client      *http.Client
	OrdersURL   string
	UsersURL    string
	ProductsURL string
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO(M9):
	//   1. id := r.PathValue("id") (Go 1.22 mux patterns) or parse the path.
	//   2. Fetch the order. If THIS fails, the whole request fails (502/504).
	//   3. errgroup.WithContext(r.Context()) (golang.org/x/sync/errgroup) for
	//      users + products. Should a products failure cancel the users call?
	//      If not, errgroup's cancel-on-first-error is the wrong tool. Think!
	//      Stdlib alternative: sync.WaitGroup.Go (Go 1.25+) launches and
	//      tracks a goroutine in one call; you collect the errors yourself.
	//   4. Forward X-Request-ID and identity headers on every sub-request.
	//   5. Encode OrderSummary; add a warning for each degraded part.
	http.Error(w, "TODO(M9): aggregation", http.StatusNotImplemented)
}
