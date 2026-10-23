package vol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Date is a civil date (no zone). Its JSON form is "YYYY-MM-DD"; the zero Date means
// "none" and marshals as null.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the civil date of t as seen in loc (UTC when loc is nil). Cannot fail.
func DateOf(t time.Time, loc *time.Location) Date {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return Date{Year: y, Month: m, Day: d}
}

// IsZero reports whether d is the zero Date ("none"). Cannot fail.
func (d Date) IsZero() bool { return d == Date{} }

// Before reports whether d is strictly earlier than o. Cannot fail.
func (d Date) Before(o Date) bool {
	switch {
	case d.Year != o.Year:
		return d.Year < o.Year
	case d.Month != o.Month:
		return d.Month < o.Month
	default:
		return d.Day < o.Day
	}
}

// AddDays returns the date n days later (earlier for a negative n). Cannot fail.
func (d Date) AddDays(n int) Date {
	return DateOf(time.Date(d.Year, d.Month, d.Day+n, 0, 0, 0, 0, time.UTC), time.UTC)
}

// Time returns midnight of d in loc (UTC when nil). Cannot fail.
func (d Date) Time(loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// String returns "YYYY-MM-DD", or "" for the zero Date. Cannot fail.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// Validate fails errs.Invalid (field "date", code "malformed") for an impossible date. The
// zero Date is valid.
func (d Date) Validate() error {
	if d.IsZero() {
		return nil
	}
	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
	if d.Year >= 1 && d.Year <= 9999 && t.Year() == d.Year && t.Month() == d.Month && t.Day() == d.Day {
		return nil
	}
	acc := validate.Errors{Op: "vol.Date.Validate"}
	acc.Addf("date", validate.CodeMalformed, "must be a real calendar date")
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// MarshalJSON writes "YYYY-MM-DD", or null for the zero Date. Fails errs.Invalid for an
// impossible date.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(d.String())
	if err != nil {
		return nil, errs.E("vol.Date.MarshalJSON", errs.Internal, err)
	}
	return b, nil
}

// UnmarshalJSON reads "YYYY-MM-DD", "" or null (the zero Date). Fails errs.Invalid (code
// "malformed") for anything else.
func (d *Date) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*d = Date{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errs.E("vol.Date.UnmarshalJSON", errs.Invalid, fmt.Errorf("a date is a string YYYY-MM-DD: %w", err))
	}
	if s == "" {
		*d = Date{}
		return nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return errs.E("vol.Date.UnmarshalJSON", errs.Invalid, fmt.Errorf("a date is a string YYYY-MM-DD: %w", err))
	}
	*d = DateOf(t, time.UTC)
	return nil
}
