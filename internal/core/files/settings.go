package files

// The files.* settings (files.md, "Settings"; D39), moved here from the package filesettings
// (boundaries.md decision 6): files declares its own keys and reads them through the
// settings.Reader in Options.Settings.

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The keys of the files area, bare as the area's Reader takes them.
const (
	keyMaxBytes     = "max_bytes"
	keyQuotaBytes   = "quota_bytes"
	keyMaxPerRecord = "max_per_record"
)

// The defaults and ranges (files.md, "Settings"). The upper bound of max_bytes is the route
// body cap UploadCeiling, so the cap is always at least the policy.
const (
	defaultMaxBytes     int64 = 25 << 20
	minMaxBytes         int64 = 1 << 20
	maxMaxBytes         int64 = 1 << 30
	defaultQuotaBytes   int64 = 0
	maxQuotaBytes       int64 = 1<<63 - 1
	defaultMaxPerRecord int64 = 20
	minMaxPerRecord     int64 = 1
	maxMaxPerRecord     int64 = 200
)

// Settings declares the three files.* keys: max_bytes, quota_bytes and max_per_record. app
// passes them to settings.New as the core area "files" and hands files a Reader scoped to
// it. Titles and descriptions are the catalogue keys core.files.setting.<key>.title|
// description. Callers must not modify it.
var Settings = []settings.Decl{
	{Key: keyMaxBytes, Kind: settings.Int, Default: strconv.FormatInt(defaultMaxBytes, 10), Check: rangeCheck(minMaxBytes, maxMaxBytes)},
	{Key: keyQuotaBytes, Kind: settings.Int, Default: strconv.FormatInt(defaultQuotaBytes, 10), Check: rangeCheck(0, maxQuotaBytes)},
	{Key: keyMaxPerRecord, Kind: settings.Int, Default: strconv.FormatInt(defaultMaxPerRecord, 10), Check: rangeCheck(minMaxPerRecord, maxMaxPerRecord)},
}

// readerPolicy maps the three keys of a Reader scoped to the files area onto Policy, read
// at every call so a changed setting applies to the next upload. Fails with the Reader's
// kind (errs.Invalid for a reader of another area, Unavailable, Timeout or Canceled before
// settings' first load), naming the key.
type readerPolicy struct{ r settings.Reader }

func (p readerPolicy) Policy(ctx context.Context) (Policy, error) {
	maxBytes, err := p.r.Int(ctx, keyMaxBytes)
	if err != nil {
		return Policy{}, fmt.Errorf("files.%s: %w", keyMaxBytes, err)
	}
	quota, err := p.r.Int(ctx, keyQuotaBytes)
	if err != nil {
		return Policy{}, fmt.Errorf("files.%s: %w", keyQuotaBytes, err)
	}
	perRecord, err := p.r.Int(ctx, keyMaxPerRecord)
	if err != nil {
		return Policy{}, fmt.Errorf("files.%s: %w", keyMaxPerRecord, err)
	}
	return Policy{MaxBytes: maxBytes, QuotaBytes: quota, MaxPerRecord: int(perRecord)}, nil
}

// capped lowers p to ceiling c: each non-zero field of c is a maximum, and a zero
// (unlimited) QuotaBytes becomes c.QuotaBytes. It never raises a field, and a zero c changes
// nothing.
func (p Policy) capped(c Policy) Policy {
	if c.MaxBytes > 0 {
		p.MaxBytes = min(p.MaxBytes, c.MaxBytes)
	}
	if c.QuotaBytes > 0 {
		if p.QuotaBytes <= 0 {
			p.QuotaBytes = c.QuotaBytes
		}
		p.QuotaBytes = min(p.QuotaBytes, c.QuotaBytes)
	}
	if c.MaxPerRecord > 0 {
		p.MaxPerRecord = min(p.MaxPerRecord, c.MaxPerRecord)
	}
	return p
}

// rangeCheck is a Check that fails with the "range" code and its min and max.
func rangeCheck(lo, hi int64) func(string) error {
	return func(v string) error {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return errs.E("files.settings.check", errs.Invalid, &validate.FieldError{Code: validate.CodeMalformed, Reason: "must be a base-10 integer"})
		}
		if n < lo || n > hi {
			return errs.E("files.settings.check", errs.Invalid, &validate.FieldError{
				Code:   validate.CodeRange,
				Params: map[string]string{"min": strconv.FormatInt(lo, 10), "max": strconv.FormatInt(hi, 10)},
				Reason: "must be between " + strconv.FormatInt(lo, 10) + " and " + strconv.FormatInt(hi, 10),
			})
		}
		return nil
	}
}
