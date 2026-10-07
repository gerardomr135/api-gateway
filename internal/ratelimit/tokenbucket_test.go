package ratelimit

import "testing"

// TestTokenBucketBurstThenRefill: with burst 3 and rate 1/s,
// three immediate calls pass, the fourth fails, and after advancing the fake
// clock by 1s exactly one more passes. Control time through b.now.
//
// Alternative worth trying (Go 1.25+): run the test inside synctest.Test from
// testing/synctest. Inside its "bubble" time.Now and time.Sleep use a fake
// clock that jumps forward instantly, so you could drop the injectable `now`
// field entirely. Write it both ways and note which design you prefer.
func TestTokenBucketBurstThenRefill(t *testing.T) {
	t.Skip("TODO(M6): remove this Skip and implement")
}

// TestTokenBucketConcurrent: 100 goroutines call Allow on one bucket with
// burst N and a frozen clock. Exactly N calls must succeed.
// Always run with `go test -race`.
func TestTokenBucketConcurrent(t *testing.T) {
	t.Skip("TODO(M6): remove this Skip and implement")
}
