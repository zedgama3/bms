package identity_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// tally runs n sign-ins in parallel against one submitted address (each from its own client
// address, so only the per-address bucket can stop them) and counts the outcomes.
func tally(t *testing.T, r *rig, n int, email, password string) (unauth, limited, other int) {
	t.Helper()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.svc.SignIn(from(ctxT(t), fmt.Sprintf("192.0.2.%d", i+1)),
				identity.Credentials{Email: mail(t, email), Password: password})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errs.Is(err, errs.Unauthenticated):
				unauth++
			case errs.Is(err, errs.Unavailable) && isRateLimited(err):
				limited++
			default:
				other++
			}
		}()
	}
	wg.Wait()
	return unauth, limited, other
}

// The limiter reserves its slot before the password is checked, so a burst of concurrent
// guesses cannot run more than the limit's worth of verifications.
func TestSignIn_ConcurrentWrongGuesses_AtMostLimitVerifications(t *testing.T) {
	const limit, burst = 3, 24
	r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(limit)))
	unauth, limited, other := tally(t, r, burst, "target.person@example.org", pwOther)
	if other != 0 {
		t.Fatalf("%d sign-ins ended with another error", other)
	}
	if unauth > limit {
		t.Errorf("%d password verifications ran, at most %d allowed", unauth, limit)
	}
	if unauth+limited != burst || limited < burst-limit {
		t.Errorf("unauthenticated=%d rate-limited=%d of %d", unauth, limited, burst)
	}
	if got := r.count(actLoginFailed); got > limit {
		t.Errorf("%d login-failed entries, want at most %d (one per verification)", got, limit)
	}
}

func TestSignIn_IPv6_SameSlash64_SharesBucket(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withPolicy(clientPolicy(10)))
	// ten failures from ten addresses inside 2001:db8:1:2::/64
	for i := range 10 {
		ip := fmt.Sprintf("2001:db8:1:2:%x::%x", i+1, i+1)
		wantKind(t, guess(t, r, ip, fmt.Sprintf("v6person%d@example.org", i)), errs.Unauthenticated)
	}
	// another address in the same /64, a fresh e-mail address: limited by the shared bucket
	wantRateLimited(t, guess(t, r, "2001:db8:1:2:1234:5678:9abc:def0", "v6fresh@example.org"))
	if got := r.measured(metricRateLimit, map[string]string{"bucket": "address"}); got < 1 {
		t.Errorf("rate_limited{address} = %v", got)
	}
	// the neighbouring /64 is a different client
	wantKind(t, guess(t, r, "2001:db8:1:3::1", "v6fresh@example.org"), errs.Unauthenticated)
}

func TestSignIn_IPv4_AddressesStaySeparate(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withPolicy(clientPolicy(10)))
	wantNoErr(t, distinctGuesses(t, r, "192.0.2.1", "v4person", 10))
	wantRateLimited(t, guess(t, r, "192.0.2.1", "v4fresh@example.org"))
	wantKind(t, guess(t, r, "192.0.2.2", "v4fresh@example.org"), errs.Unauthenticated) // a neighbour is not the same client
}

// The dev cookie is read only over plain http; over https it is ignored even with
// AllowPlaintext (the production cookie is the only one an https request may carry).
func TestAuthenticate_DevCookieIgnoredOverHTTPSEvenWithAllowPlaintext(t *testing.T) {
	a := newAuthRig(t, withPlaintext)
	req := requestWithCookie(t, identity.DevCookieName, a.validToken().encode(sessionKeyText))
	ctx, person, err := a.svc.Authenticate(from(ctxT(t), "198.51.100.7"), req) // from() sets scheme https
	wantNoErr(t, err)
	if !person.IsZero() {
		t.Error("dev cookie honoured over https")
	}
	if _, ok := identity.SessionFrom(ctx); ok {
		t.Error("session from a dev cookie over https")
	}
	if n := len(a.fake.Calls()); n != 0 {
		t.Errorf("%d statements for an ignored cookie", n)
	}
}
