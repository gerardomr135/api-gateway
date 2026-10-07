package ratelimit

import "testing"

// TestTokenBucketBurstThenRefill: with burst 3 and rate 1/s,
// three immediate calls pass, the fourth fails, and after advancing the fake
// clock by 1s exactly one more passes. Control time through b.now.
func TestTokenBucketBurstThenRefill(t *testing.T) {
	t.Skip("TODO(M6): remove this Skip and implement")
}

// TestTokenBucketConcurrent: 100 goroutines call Allow on one bucket with
// burst N and a frozen clock. Exactly N calls must succeed.
// Always run with `go test -race`.
func TestTokenBucketConcurrent(t *testing.T) {
	t.Skip("TODO(M6): remove this Skip and implement")
}
