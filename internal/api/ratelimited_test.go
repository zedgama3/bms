package api_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// identity Proposed change 1: anything wrapping httpx.ErrRateLimited is a 429.
func TestMapper_HttpxRateLimited_429RetryAfter60(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"bare sentinel", httpx.ErrRateLimited},
		{"identity limiter", errs.E("identity.Service.SignIn", errs.Unavailable, httpx.ErrRateLimited)},
		{"wrapped again", fmt.Errorf("sign in: %w", errs.E("identity.Service.SignUp", errs.Unavailable, httpx.ErrRateLimited))},
		{"joined", errors.Join(errors.New("other"), errs.E("x", errs.Unavailable, httpx.ErrRateLimited))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cur := tc.err
			r := errRig(t, &cur, module.AccessPublic)
			rec := r.do("GET", "/api/alpha/e")
			wantProblem(t, rec, 429, httpx.CodeRateLimited)
			if got := rec.Header().Get("Retry-After"); got != "60" {
				t.Errorf("Retry-After = %q, want 60", got)
			}
		})
	}
}

func TestMapper_PlainUnavailable_IsNot429(t *testing.T) {
	cur := error(errs.E("identity.Service.SignIn", errs.Unavailable, errors.New("store down")))
	r := errRig(t, &cur, module.AccessPublic)
	rec := r.do("GET", "/api/alpha/e")
	wantProblem(t, rec, 503, "unavailable")
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}
}

// telemetry.md: intake's sentinel is httpx's (or wraps it), so one mapper rule serves both.
func TestIntakeErrRateLimited_IsHttpxErrRateLimited(t *testing.T) {
	if !errors.Is(intake.ErrRateLimited, httpx.ErrRateLimited) {
		t.Fatal("intake.ErrRateLimited does not match httpx.ErrRateLimited")
	}
	cur := error(errs.E("intake.Accept", errs.Unavailable, intake.ErrRateLimited))
	r := errRig(t, &cur, module.AccessPublic)
	wantProblem(t, r.do("GET", "/api/alpha/e"), 429, httpx.CodeRateLimited)
}

func TestMapper_RateLimitedBody_CarriesNoPersonalData(t *testing.T) {
	cur := error(errs.E("identity.Service.SignIn", errs.Unavailable, httpx.ErrRateLimited))
	r := errRig(t, &cur, module.AccessPublic)
	p := wantProblem(t, r.do("GET", "/api/alpha/e"), 429, httpx.CodeRateLimited)
	if len(p.Fields) != 0 {
		t.Errorf("a rate-limit problem lists fields: %+v", p.Fields)
	}
}
