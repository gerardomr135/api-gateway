package middleware

import "testing"

// TestChainOrder should prove that Chain(h, A, B) runs A before B on the way
// in and B before A on the way out.
//
// Hint: make each test middleware append its name to a shared []string
// before and after calling next ("A-in", "B-in", "h", "B-out", "A-out"),
// then compare the slice. Use httptest.NewRecorder + httptest.NewRequest.
func TestChainOrder(t *testing.T) {
	t.Skip("TODO(M3): remove this Skip and implement")
}
