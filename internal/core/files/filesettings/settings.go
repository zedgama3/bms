// Package filesettings declares the files.* settings (files.md, "Settings"; D39).
//
// core/settings imports core/audit and files imports audit and db, so files cannot import
// core/settings: the declarations live in this leaf package instead, which imports settings
// and nothing of files (as auditsettings does). app passes Settings to settings.New as the
// core area "files" and implements files.PolicySource over a Reader scoped to it.
package filesettings

import (
	"strconv"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Area is the name of the settings area, the prefix of the keys.
const Area = "files"

// The keys, bare as the area's Reader takes them.
const (
	KeyMaxBytes     = "max_bytes"
	KeyQuotaBytes   = "quota_bytes"
	KeyMaxPerRecord = "max_per_record"
)

// The defaults and ranges (files.md, "Settings"). The upper bound of max_bytes is the route
// body cap files.UploadCeiling, so the cap is always at least the policy.
const (
	DefaultMaxBytes     int64 = 25 << 20
	MinMaxBytes         int64 = 1 << 20
	MaxMaxBytes         int64 = 1 << 30
	DefaultQuotaBytes   int64 = 0
	MaxQuotaBytes       int64 = 1<<63 - 1
	DefaultMaxPerRecord int64 = 20
	MinMaxPerRecord     int64 = 1
	MaxMaxPerRecord     int64 = 200
)

// Settings declares the three keys. Titles and descriptions are the catalogue keys
// core.files.setting.<key>.title|description.
var Settings = []settings.Decl{
	{Key: KeyMaxBytes, Kind: settings.Int, Default: strconv.FormatInt(DefaultMaxBytes, 10), Check: rangeCheck(MinMaxBytes, MaxMaxBytes)},
	{Key: KeyQuotaBytes, Kind: settings.Int, Default: strconv.FormatInt(DefaultQuotaBytes, 10), Check: rangeCheck(0, MaxQuotaBytes)},
	{Key: KeyMaxPerRecord, Kind: settings.Int, Default: strconv.FormatInt(DefaultMaxPerRecord, 10), Check: rangeCheck(MinMaxPerRecord, MaxMaxPerRecord)},
}

// rangeCheck is a Check that fails with the "range" code and its min and max.
func rangeCheck(lo, hi int64) func(string) error {
	return func(v string) error {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return errs.E("filesettings.check", errs.Invalid, &validate.FieldError{Code: validate.CodeMalformed, Reason: "must be a base-10 integer"})
		}
		if n < lo || n > hi {
			return errs.E("filesettings.check", errs.Invalid, &validate.FieldError{
				Code:   validate.CodeRange,
				Params: map[string]string{"min": strconv.FormatInt(lo, 10), "max": strconv.FormatInt(hi, 10)},
				Reason: "must be between " + strconv.FormatInt(lo, 10) + " and " + strconv.FormatInt(hi, 10),
			})
		}
		return nil
	}
}
