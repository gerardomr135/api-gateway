package middleware

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestChainOrder should prove that Chain(h, A, B) runs A before B on the way
// in and B before A on the way out.
//
// Hint: make each test middleware append its name to a shared []string
// before and after calling next ("A-in", "B-in", "h", "B-out", "A-out"),
// then compare the slice. Use httptest.NewRecorder + httptest.NewRequest.
func TestChainOrder(t *testing.T) {
	names := []string{}

	a := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			names = append(names, "A-in")
			next.ServeHTTP(w, r)
			names = append(names, "A-out")
		})
	}

	b := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			names = append(names, "B-in")
			next.ServeHTTP(w, r)
			names = append(names, "B-out")
		})
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		names = append(names, "h")
	})

	chained := Chain(h, a, b)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	chained.ServeHTTP(w, req)

	expected := []string{"A-in", "B-in", "h", "B-out", "A-out"}
	if !slices.Equal(names, expected) {
		t.Errorf("expected %v, got %v", expected, names)
	}
}
