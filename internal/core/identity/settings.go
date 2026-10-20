package identity

import (
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Settings declares the accounts.* keys for the settings core area "accounts" (identity.md,
// "Settings"; D39): the keys, kinds, defaults and ranges of the Policy fields. app passes
// them, with CheckSettings, to settings.New and implements PolicySource over the area's
// scoped Reader. Titles and descriptions are the catalogue keys
// core.accounts.setting.<key>.title|description (and .option.<opt> for signup). Each Check
// is pure and agrees with Policy.Validate, which stays the check at the point of use.
//
// accounts.password_reset_lifetime joins them with the forgotten-password flow, which waits
// for notify.
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
}

// CheckSettings is the area's cross-field rule over the effective values (bare keys,
// defaults filled in): accounts.client_failure_limit is never below
// accounts.login_failure_limit, because many people share one client address at a venue.
// It fails errs.Invalid with a validate.FieldError named by the bare key
// ("client_failure_limit", code range). A value that does not parse is the per-key Check's
// to report, so it is skipped here. Pure.
func CheckSettings(values map[string]string) error {
	login, err := strconv.ParseInt(strings.TrimSpace(values["login_failure_limit"]), 10, 64)
	if err != nil {
		return nil //nolint:nilerr // the key's own Check reports a value that does not parse
	}
	client, err := strconv.ParseInt(strings.TrimSpace(values["client_failure_limit"]), 10, 64)
	if err != nil {
		return nil //nolint:nilerr // the key's own Check reports a value that does not parse
	}
	if client < login {
		return validate.Range("client_failure_limit", client, login, int64(1000)) //nolint:wrapcheck // a field error, re-fielded by settings
	}
	return nil
}

// durationRange is a Check: the value parses as a duration within lo..hi. A value that does
// not parse is settings' to report; this Check then says nothing.
func durationRange(field string, lo, hi time.Duration) func(string) error {
	return func(v string) error {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return nil //nolint:nilerr // settings reports a value that does not parse
		}
		return validate.Range(field, d, lo, hi) //nolint:wrapcheck // a field error, re-fielded by settings
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
