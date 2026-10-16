// Package retry provides one backoff schedule (exponential, capped, full jitter) and one
// retry loop that honors context.Context. All waiting goes through lib/clock; the package
// never sleeps, logs, or adds deadlines of its own.
// It depends only on the standard library, lib/errs and lib/clock.
// Ported from Sojourn (D11).
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Rand returns a jitter sample in [0, 1]. Nil means math/rand/v2's Float64 (safe for
// concurrent use). A test passes func() float64 { return 1 } for the undithered schedule.
type Rand func() float64

func (r Rand) sample() float64 {
	if r == nil {
		return rand.Float64() //nolint:gosec // jitter, not security
	}
	return r()
}

// Policy describes an exponential backoff with a cap and full jitter.
type Policy struct {
	Initial     time.Duration // base delay before the second attempt; must be > 0
	Max         time.Duration // cap on the base delay; must be >= Initial
	Multiplier  float64       // growth per attempt; must be >= 1
	MaxAttempts int           // total attempts including the first; 0 = unlimited
}

// Default returns Policy{Initial: 500ms, Max: 30s, Multiplier: 2, MaxAttempts: 5}.
func Default() Policy {
	return Policy{Initial: 500 * time.Millisecond, Max: 30 * time.Second, Multiplier: 2, MaxAttempts: 5}
}

// Validate fails with errs.Invalid joining one validate.FieldError per field out of range
// (fields "initial", "max", "multiplier", "max_attempts"), so validate.Fields lists them all.
func (p Policy) Validate() error {
	v := validate.Errors{Op: "retry.Policy.Validate"}
	v.Add(validate.Positive("initial", p.Initial))
	if p.Max < p.Initial {
		v.Addf("max", "must be >= initial (%s), got %s", p.Initial, p.Max)
	}
	if !(p.Multiplier >= 1) { // also rejects NaN
		v.Addf("multiplier", "must be >= 1, got %v", p.Multiplier)
	}
	if p.MaxAttempts < 0 {
		v.Addf("max_attempts", "must be >= 0, got %d", p.MaxAttempts)
	}
	return v.Err() //nolint:wrapcheck // validate.Errors.Err already wraps with errs.E
}

// Delay returns the wait before attempt n (n >= 2) given jitter sample u:
// u * min(Max, Initial * Multiplier^(n-2)). It returns 0 for n < 2. u outside [0, 1] or
// NaN is treated as 1. Pure; cannot fail once p is valid.
func (p Policy) Delay(attempt int, u float64) time.Duration {
	if attempt < 2 || p.Initial <= 0 {
		return 0
	}
	if math.IsNaN(u) || u < 0 || u > 1 {
		u = 1
	}
	base := p.Max
	// math.Pow saturates to +Inf for large exponents, so there is no overflow.
	if grown := float64(p.Initial) * math.Pow(p.Multiplier, float64(attempt-2)); grown < float64(p.Max) {
		base = time.Duration(grown)
	}
	if u == 1 {
		return base
	}
	return time.Duration(u * float64(base))
}

// Retryable reports whether err's kind is errs.Unavailable or errs.Timeout. A nil err
// is not retryable. Cannot fail.
func Retryable(err error) bool {
	return errs.Is(err, errs.Unavailable) || errs.Is(err, errs.Timeout)
}

// Do calls fn until it returns nil, returns a non-retryable error, MaxAttempts is
// reached, or ctx is done. Waits between attempts come from p and clk.
//
// fn receives the same ctx Do received; attempt 1 runs immediately. A non-retryable error
// is returned unchanged. When attempts run out the result wraps the last error, so its
// kind is the last error's kind. When ctx ends the result has kind Canceled or Timeout
// and joins ctx.Err() with the last error. Do fails with errs.Invalid if clk or fn is nil,
// p is invalid, or p.MaxAttempts is 0 and ctx has no deadline. Safe for concurrent use.
func Do(ctx context.Context, clk clock.Clock, p Policy, rnd Rand, fn func(ctx context.Context) error) error {
	const op = "retry.Do"
	if clk == nil {
		return errs.E(op, errs.Invalid, errors.New("nil clock"))
	}
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil function"))
	}
	if err := p.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if p.MaxAttempts == 0 {
		if _, ok := ctx.Deadline(); !ok {
			return errs.E(op, errs.Invalid, errors.New("unlimited attempts require a context deadline"))
		}
	}
	var last error
	for n := 1; ; n++ {
		if ctx.Err() != nil {
			return ctxDone(ctx, last)
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if !Retryable(err) {
			return err
		}
		last = err
		if ctx.Err() != nil {
			return ctxDone(ctx, last)
		}
		if p.MaxAttempts > 0 && n >= p.MaxAttempts {
			return fmt.Errorf("retry: gave up after %d attempts: %w", n, last)
		}
		if err := clock.Wait(ctx, clk, p.Delay(n+1, rnd.sample())); err != nil {
			return ctxDone(ctx, last)
		}
	}
}

// ctxDone builds the error for a finished context, joined with the last attempt's error.
func ctxDone(ctx context.Context, last error) error {
	err := ctx.Err()
	return errs.E("retry.Do", errs.KindOf(err), errors.Join(err, last))
}

// Backoff keeps the failure count for a loop that must "back off after repeated failures,
// reset on success". Not safe for concurrent use: one per goroutine.
type Backoff struct {
	p       Policy
	rnd     Rand
	attempt int
}

// NewBackoff fails with errs.Invalid if p is invalid.
func NewBackoff(p Policy, rnd Rand) (*Backoff, error) {
	if err := p.Validate(); err != nil {
		return nil, errs.E("retry.NewBackoff", errs.Invalid, err)
	}
	return &Backoff{p: p, rnd: rnd}, nil
}

// Next records one more failure and returns the delay to wait before trying again.
// ok is false once p.MaxAttempts > 0 and that many failures have been recorded; the
// delay is then 0 and the caller decides whether to give up.
func (b *Backoff) Next() (delay time.Duration, ok bool) {
	b.attempt++
	if b.p.MaxAttempts > 0 && b.attempt >= b.p.MaxAttempts {
		return 0, false
	}
	return b.p.Delay(b.attempt+1, b.rnd.sample()), true
}

// Attempt returns the number of failures recorded since the last Reset. Cannot fail.
func (b *Backoff) Attempt() int { return b.attempt }

// Reset clears the failure count after a success. Cannot fail.
func (b *Backoff) Reset() { b.attempt = 0 }
