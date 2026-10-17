package model

import (
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
)

// TimeRange is the half-open interval [Start, End). Both are instants; any zone is fine,
// because every comparison is on the instant. JSON form: {"start": RFC3339, "end": RFC3339}.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Codes set by TimeRange.Validate.
const codeEndAfterStart = "end_after_start"

// Validate fails with errs.Invalid, joining one validate.FieldError per problem: "start"
// must be set, "end" must be set, and "end" must be after "start".
func (r TimeRange) Validate() error {
	v := validate.Errors{Op: "model.TimeRange.Validate"}
	if r.Start.IsZero() {
		v.Addf("start", validate.CodeRequired, "must be set")
	}
	if r.End.IsZero() {
		v.Addf("end", validate.CodeRequired, "must be set")
	}
	if !r.Start.IsZero() && !r.End.IsZero() && !r.Start.Before(r.End) {
		v.Addf("end", codeEndAfterStart, "must be after start")
	}
	return v.Err() //nolint:wrapcheck // validate.Errors.Err already wraps with errs.E
}

// Contains reports Start <= t < End. Cannot fail; meaningful once Validate passes.
func (r TimeRange) Contains(t time.Time) bool {
	return !t.Before(r.Start) && t.Before(r.End)
}

// Overlaps reports whether the two half-open ranges share any instant:
// r.Start < o.End && o.Start < r.End. Two ranges that only touch do not overlap.
// Cannot fail; meaningful once both validate.
func (r TimeRange) Overlaps(o TimeRange) bool {
	return r.Start.Before(o.End) && o.Start.Before(r.End)
}

// Duration returns End.Sub(Start). Cannot fail; meaningful once Validate passes.
func (r TimeRange) Duration() time.Duration { return r.End.Sub(r.Start) }
