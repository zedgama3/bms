package identity_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func limitPolicy(limit int) *policySource {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.LoginFailureLimit = limit })
	return ps
}

// guess makes one failed sign-in for email from ip against a store with no accounts.
func guess(t *testing.T, r *rig, ip, email string) error {
	t.Helper()
	_, err := r.svc.SignIn(from(ctxT(t), ip), identity.Credentials{Email: mail(t, email), Password: pwOther})
	return keep(err)
}

// F17
func TestSignIn_BadBody_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		c     identity.Credentials
		field string
	}{
		{"empty password", identity.Credentials{Email: "ada.quill@example.org"}, "password"},
		{"empty email", identity.Credentials{Password: pwGood}, "email"},
		{"malformed email", identity.Credentials{Email: "nope", Password: pwGood}, "email"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := dbtest.NewFake()
			r := newRig(t, fake)
			_, err := r.svc.SignIn(from(ctxT(t), "198.51.100.7"), tc.c)
			wantField(t, err, tc.field, "")
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("%d statements for an invalid body", n)
			}
			if r.count(actLoginFailed) != 0 {
				t.Error("a validation failure was recorded as a failed login")
			}
		})
	}
}

// F19: an unknown address answers Unauthenticated, records login-failed with no record, and
// still spends a password hash.
func TestSignIn_Unknown_UnauthenticatedSameAsWrong(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	const addr = "nobody.here@example.org"
	start := time.Now()
	err := guess(t, r, "203.0.113.9", addr)
	elapsed := time.Since(start)
	wantKind(t, err, errs.Unauthenticated)
	// The dummy hash is verified: an Argon2id check (19 MiB, 2 passes) takes far longer
	// than a lookup against a store that answers at once.
	if elapsed < 3*time.Millisecond {
		t.Errorf("unknown address answered in %v: no dummy hash verified", elapsed)
	}
	e := r.only(actLoginFailed)
	if e.Outcome != audit.Failed {
		t.Errorf("outcome = %v, want failed", e.Outcome)
	}
	if e.Actor.Kind != audit.ActorAnonymous {
		t.Errorf("actor kind = %v, want anonymous", e.Actor.Kind)
	}
	if !e.Record.IsZero() {
		t.Errorf("record = %+v, want zero for an unknown address", e.Record)
	}
	if !e.Subject.IsZero() || len(e.Fields) != 0 || e.Reason != "" {
		t.Errorf("login-failed carries more than it should: %+v", e)
	}
	if got := r.measured(metricLogins, map[string]string{"outcome": "bad_credentials"}); got != 1 {
		t.Errorf("logins{bad_credentials} = %v, want 1", got)
	}
	r.noLeak(addr, "203.0.113.9", pwOther)
}

// The login-failed entry has no field that could hold an address.
func TestSignIn_Failed_EntryHasNoAddress(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantKind(t, guess(t, r, "203.0.113.77", "who.is.this@example.org"), errs.Unauthenticated)
	e := r.only(actLoginFailed)
	dump := fmt.Sprintf("%#v", e)
	for _, needle := range []string{"203.0.113.77", "who.is.this", "example.org"} {
		if containsFold(dump, needle) {
			t.Errorf("entry contains %q: %s", needle, dump)
		}
	}
}

// F18
func TestSignIn_RateLimited_Table(t *testing.T) {
	t.Run("per client address across different e-mail addresses", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
		for i := range 3 {
			wantKind(t, guess(t, r, "203.0.113.20", fmt.Sprintf("person%d@example.org", i)), errs.Unauthenticated)
		}
		wantRateLimited(t, guess(t, r, "203.0.113.20", "someone.new@example.org"))
		if got := r.measured(metricRateLimit, map[string]string{"bucket": "address"}); got < 1 {
			t.Errorf("rate_limited{address} = %v", got)
		}
		// another client is not affected
		wantKind(t, guess(t, r, "203.0.113.21", "someone.new@example.org"), errs.Unauthenticated)
	})
	t.Run("per submitted address from different clients, case-insensitive", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
		for i, email := range []string{"Target.One@example.org", "target.one@EXAMPLE.org", "TARGET.ONE@example.org"} {
			wantKind(t, guess(t, r, fmt.Sprintf("203.0.113.%d", 30+i), email), errs.Unauthenticated)
		}
		wantRateLimited(t, guess(t, r, "203.0.113.40", "target.one@example.org"))
		if got := r.measured(metricRateLimit, map[string]string{"bucket": "email"}); got < 1 {
			t.Errorf("rate_limited{email} = %v", got)
		}
		// another address from the same fresh client is fine
		wantKind(t, guess(t, r, "203.0.113.40", "target.two@example.org"), errs.Unauthenticated)
	})
	t.Run("limit is reached by the Nth failure, not the N+1th", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
		for range 3 {
			wantKind(t, guess(t, r, "203.0.113.60", "same@example.org"), errs.Unauthenticated)
		}
		wantRateLimited(t, guess(t, r, "203.0.113.60", "same@example.org"))
	})
	t.Run("resets after the window on the fake clock", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
		for range 3 {
			wantKind(t, guess(t, r, "203.0.113.70", "same@example.org"), errs.Unauthenticated)
		}
		wantRateLimited(t, guess(t, r, "203.0.113.70", "same@example.org"))
		wantNoErr(t, r.clk.Advance(14*time.Minute))
		wantRateLimited(t, guess(t, r, "203.0.113.70", "same@example.org"))
		wantNoErr(t, r.clk.Advance(61*time.Second))
		wantKind(t, guess(t, r, "203.0.113.70", "same@example.org"), errs.Unauthenticated)
	})
	t.Run("window follows the policy", func(t *testing.T) {
		ps := limitPolicy(3)
		ps.set(func(p *identity.Policy) { p.LoginFailureWindow = time.Minute })
		r := newRig(t, dbtest.NewFake(), withPolicy(ps))
		for range 3 {
			wantKind(t, guess(t, r, "203.0.113.71", "same@example.org"), errs.Unauthenticated)
		}
		wantRateLimited(t, guess(t, r, "203.0.113.71", "same@example.org"))
		wantNoErr(t, r.clk.Advance(61*time.Second))
		wantKind(t, guess(t, r, "203.0.113.71", "same@example.org"), errs.Unauthenticated)
	})
	t.Run("default is ten failures", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake())
		for range 10 {
			wantKind(t, guess(t, r, "203.0.113.72", "same@example.org"), errs.Unauthenticated)
		}
		wantRateLimited(t, guess(t, r, "203.0.113.72", "same@example.org"))
	})
	t.Run("the limit is checked before any lookup or hash", func(t *testing.T) {
		fake := dbtest.NewFake()
		r := newRig(t, fake, withPolicy(limitPolicy(3)))
		for range 3 {
			wantKind(t, guess(t, r, "203.0.113.73", "same@example.org"), errs.Unauthenticated)
		}
		before := len(fake.Calls())
		start := time.Now()
		wantRateLimited(t, guess(t, r, "203.0.113.73", "same@example.org"))
		if len(fake.Calls()) != before {
			t.Error("a rate-limited attempt reached the store")
		}
		if time.Since(start) > time.Second {
			t.Error("a rate-limited attempt was slow")
		}
	})
	t.Run("rate limited requests never carry the address", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
		for range 4 {
			_ = guess(t, r, "203.0.113.74", "limited.person@example.org") //nolint:errcheck // outcome asserted above
		}
		r.noLeak("limited.person", "203.0.113.74")
	})
}

// Eviction at MaxTrackedKeys needs 10 000 failed hashes; run with BMS_SLOW_TESTS=1.
func TestSignIn_RateLimited_EvictsOldestAtMaxTrackedKeys(t *testing.T) {
	if os.Getenv("BMS_SLOW_TESTS") == "" {
		t.Skip("set BMS_SLOW_TESTS=1: needs identity.MaxTrackedKeys failed password hashes")
	}
	r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
	for range 3 {
		wantKind(t, guess(t, r, "203.0.113.80", "oldest@example.org"), errs.Unauthenticated)
	}
	wantRateLimited(t, guess(t, r, "203.0.113.81", "oldest@example.org"))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range identity.MaxTrackedKeys {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ip := fmt.Sprintf("10.%d.%d.%d", (i>>16)&255, (i>>8)&255, i&255)
			_ = guess(t, r, ip, fmt.Sprintf("filler%d@example.org", i)) //nolint:errcheck // filler traffic
		}()
	}
	wg.Wait()
	wantKind(t, guess(t, r, "203.0.113.82", "oldest@example.org"), errs.Unauthenticated)
}

// F22
func TestSignIn_StoreUnavailable_PassesKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errDown, errs.Unavailable},
		{"timeout", errTimeout, errs.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fdb := newFaultDB(dbtest.NewFake())
			fdb.set(func(f *faultDB) { f.readErr = tc.err })
			r := newRig(t, fdb)
			wantKind(t, guess(t, r, "198.51.100.7", "ada.quill@example.org"), tc.kind)
			if r.count(actLoginFailed) != 0 {
				t.Error("a store failure was recorded as a failed login")
			}
		})
	}
}

func TestSignIn_CancelledContext_Canceled(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, cancel := context.WithCancel(from(ctxT(t), "198.51.100.7"))
	cancel()
	_, err := r.svc.SignIn(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Canceled)
}

func TestSignIn_DeadlinePassed_Timeout(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, cancel := context.WithDeadline(from(ctxT(t), "198.51.100.7"), time.Now().Add(-time.Second))
	defer cancel()
	_, err := r.svc.SignIn(ctx, identity.Credentials{Email: mail(t, "ada.quill@example.org"), Password: pwGood})
	wantKind(t, err, errs.Timeout)
}

// F60: more hashes than HashConcurrency in flight; the ones waiting give up with their ctx.
func TestHash_WaitsOnSemaphore_CtxDone(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(100)))
	const n = 40
	results := make(chan error, n)
	for i := range n {
		go func() {
			ctx, cancel := context.WithTimeout(from(ctxT(t), fmt.Sprintf("192.0.2.%d", i+1)), 60*time.Millisecond)
			defer cancel()
			_, err := r.svc.SignIn(ctx, identity.Credentials{Email: mail(t, fmt.Sprintf("queue%d@example.org", i)), Password: pwOther})
			results <- err
		}()
	}
	timeouts := 0
	for range n {
		err := <-results
		switch {
		case errs.Is(err, errs.Timeout), errs.Is(err, errs.Canceled):
			timeouts++
		case errs.Is(err, errs.Unauthenticated):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if timeouts == 0 {
		t.Errorf("no sign-in gave up waiting for a hash slot among %d (HashConcurrency=%d)", n, identity.HashConcurrency)
	}
}

// F18 and enumeration: sign-in failures are counted per submitted address whether or not
// an account exists, so the limiter cannot be used to probe.
func TestSignIn_Limiter_KeysOnSubmittedAddress_NotOnExistence(t *testing.T) {
	// Covered with a real account in TestSignIn_EnumerationResistance_Integration; here the
	// store knows no one, and the behaviour must be the same as for a known address.
	r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)))
	for range 3 {
		wantKind(t, guess(t, r, "203.0.113.90", "ghost@example.org"), errs.Unauthenticated)
	}
	wantRateLimited(t, guess(t, r, "203.0.113.91", "ghost@example.org"))
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
