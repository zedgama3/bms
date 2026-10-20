package identity

import (
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Settings declares the accounts.* keys for the settings core area "accounts" (identity.md,
// "Settings"; D39): the keys, kinds, defaults and ranges of the Policy fields. app passes
// them, with CheckSettings, to settings.New and implements PolicySource over the area's
// scoped Reader. Titles and descriptions are the catalogue keys
// core.accounts.setting.<key>.title|description (and .option.<opt> for signup). Each Check
// is pure and agrees with Policy.Validate, which stays the check at the point of use.
var Settings = []settings.Decl{
	{Key: "signup", Kind: settings.Enum, Default: string(SignupOpen),
		Options: []string{string(SignupOpen), string(SignupInvitation)}},
	{Key: "session_lifetime", Kind: settings.Duration, Default: "720h",
		Check: durationRange("session_lifetime", time.Hour, 8760*time.Hour)},
	{Key: "superuser_session_lifetime", Kind: settings.Duration, Default: "1h",
		Check: durationRange("superuser_session_lifetime", 10*time.Minute, 24*time.Hour)},
	{Key: "password_min_length", Kind: settings.Int, Default: strconv.Itoa(PasswordMinLengthFloor),
		Check: intRange("password_min_length", PasswordMinLengthFloor, 128)},
	{Key: "login_failure_limit", Kind: settings.Int, Default: "10",
		Check: intRange("login_failure_limit", 3, 100)},
	{Key: "client_failure_limit", Kind: settings.Int, Default: "100",
		Check: intRange("client_failure_limit", 10, 1000)},
	{Key: "login_failure_window", Kind: settings.Duration, Default: "15m",
		Check: durationRange("login_failure_window", time.Minute, 24*time.Hour)},
	{Key: "password_reset_lifetime", Kind: settings.Duration, Default: "1h",
		Check: durationRange("password_reset_lifetime", 10*time.Minute, 24*time.Hour)},
	{Key: "password_reset_client_limit", Kind: settings.Int, Default: "20",
		Check: intRange("password_reset_client_limit", 5, 1000)},
	{Key: "password_reset_address_limit", Kind: settings.Int, Default: "5",
		Check: intRange("password_reset_address_limit", 1, 100)},
	{Key: "password_reset_cooldown", Kind: settings.Duration, Default: "5m",
		Check: durationRange("password_reset_cooldown", 0, time.Hour)},
}

// CheckSettings is the area's cross-field rule over the effective values (bare keys,
// defaults filled in). Three rules, all reported together when several fail:
// client_failure_limit is never below login_failure_limit, and (D41)
// password_reset_client_limit is never below password_reset_address_limit, because many
// people share one client address at a venue; password_reset_cooldown is never above
// password_reset_lifetime, or a person whose link expired could not ask for another. It
// fails errs.Invalid with one validate.FieldError per broken rule, named by the bare key of
// the value that must move (code range, min and max as the bound). A value that does not
// parse is the per-key Check's to report, so the rule that needs it is skipped. Pure.
func CheckSettings(values map[string]string) error {
	v := validate.Errors{Op: "identity.CheckSettings"}
	if login, client, ok := intPair(values, "login_failure_limit", "client_failure_limit"); ok && client < login {
		v.Add(validate.Range("client_failure_limit", client, login, int64(1000)))
	}
	if addr, client, ok := intPair(values, "password_reset_address_limit", "password_reset_client_limit"); ok && client < addr {
		v.Add(validate.Range("password_reset_client_limit", client, addr, int64(1000)))
	}
	cooldown, cerr := time.ParseDuration(strings.TrimSpace(values["password_reset_cooldown"]))
	lifetime, lerr := time.ParseDuration(strings.TrimSpace(values["password_reset_lifetime"]))
	if cerr == nil && lerr == nil && cooldown > lifetime {
		v.Add(durationOutOfRange("password_reset_cooldown", cooldown, 0, lifetime))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// intPair parses two integer values by key; ok is false when either does not parse.
func intPair(values map[string]string, a, b string) (x, y int64, ok bool) {
	x, err := strconv.ParseInt(strings.TrimSpace(values[a]), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	y, err = strconv.ParseInt(strings.TrimSpace(values[b]), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return x, y, true
}

// durationRange is a Check: the value parses as a duration within lo..hi. A value that does
// not parse is settings' to report; this Check then says nothing.
func durationRange(field string, lo, hi time.Duration) func(string) error {
	return func(v string) error {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return nil //nolint:nilerr // settings reports a value that does not parse
		}
		if d < lo || d > hi {
			return durationOutOfRange(field, d, lo, hi)
		}
		return nil
	}
}

// intRange is a Check: the value parses as an integer within lo..hi.
func intRange(field string, lo, hi int64) func(string) error {
	return func(v string) error {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil //nolint:nilerr // settings reports a value that does not parse
		}
		return validate.Range(field, n, lo, hi) //nolint:wrapcheck // a field error, re-fielded by settings
	}
}

// plainDuration writes d in the plainest whole unit that divides it ("1h", "8760h", "10m",
// "30s"), not time.Duration.String's "1h0m0s", so a range error reads well to a person and
// the text can be typed back as a value.
func plainDuration(d time.Duration) string {
	switch {
	case d != 0 && d%time.Hour == 0:
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	case d != 0 && d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	case d != 0 && d%time.Second == 0:
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	default:
		return d.String()
	}
}

// durationOutOfRange is validate.Range for durations with min and max in plain units.
func durationOutOfRange(field string, got, lo, hi time.Duration) error {
	return errs.E("", errs.Invalid, &validate.FieldError{
		Field: field, Code: validate.CodeRange,
		Params: map[string]string{"min": plainDuration(lo), "max": plainDuration(hi)},
		Reason: "must be between " + plainDuration(lo) + " and " + plainDuration(hi) + ", got " + plainDuration(got),
	})
}
