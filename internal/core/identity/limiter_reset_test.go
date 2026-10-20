package identity

import (
	"net/netip"
	"testing"
	"time"
)

// The reset limiter is a second pair of buckets of the sign-in limiter's type (D40, F93).
func TestResetLimiter_NamesItsBuckets_AndIsIndependentOfSignIn(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	a := netip.MustParseAddr("192.0.2.9")
	const window = 15 * time.Minute

	signIn, reset := newLimiter(), newResetLimiter()
	for range 2 {
		if _, b, ok := reset.reserve(a, "who@example.org", clock, 2, 2, window); !ok {
			t.Fatalf("refused early by %q", b)
		}
	}
	if _, b, ok := reset.reserve(a, "who@example.org", clock, 2, 2, window); ok || b != bucketResetAddress {
		t.Errorf("client bucket: ok=%v bucket=%q, want full %q", ok, b, bucketResetAddress)
	}
	if _, b, ok := reset.reserve(netip.MustParseAddr("192.0.2.10"), "who@example.org", clock, 2, 2, window); ok || b != bucketResetEmail {
		t.Errorf("address bucket: ok=%v bucket=%q, want full %q", ok, b, bucketResetEmail)
	}
	// Sign-in's buckets saw none of it.
	if _, b, ok := signIn.reserve(a, "who@example.org", clock, 2, 2, window); !ok {
		t.Errorf("sign-in refused by %q after reset requests", b)
	}
}
