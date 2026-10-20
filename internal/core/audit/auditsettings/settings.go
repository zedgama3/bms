// Package auditsettings declares the audit.* settings (audit.md, "Settings"; D39).
//
// The spec has audit export Settings itself, but core/settings imports core/audit (its
// Options.Audit is an audit.Recorder), so core/audit cannot import core/settings: the
// declarations live in this leaf package instead, which imports settings and nothing of
// audit. app passes Settings to settings.New as the core area "audit".
package auditsettings

import (
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Area is the name of the settings area, the prefix of the keys.
const Area = "audit"

// Retention bounds, written in hours because the Duration kind is time.ParseDuration
// (audit.md decision 15: a floor of 365 days when set, a ceiling of 100 years).
const (
	MinRetention = 8760 * time.Hour   // 365 days
	MaxRetention = 876000 * time.Hour // 100 years
)

// Settings declares the two audit keys. Titles and descriptions are the catalogue keys
// core.audit.setting.<key>.title|description (and .option.<opt> for self_view).
//
//   - retention: how long an entry is kept; 0 keeps entries forever (D30), otherwise
//     8760h..876000h. The retention job (jobs, M4) reads it through
//     Scoped("audit").Duration("retention") and purges on the writing copy only.
//   - self_view: what a person is told of who read their data (D22); off until the Later
//     ViewsOf feature ships, read by that feature only.
var Settings = []settings.Decl{
	{Key: "retention", Kind: settings.Duration, Default: "0", Check: checkRetention},
	{Key: "self_view", Kind: settings.Enum, Default: "off", Options: []string{"off", "viewed", "fields", "titles"}},
}

// checkRetention accepts 0 (keep forever) or a duration within MinRetention..MaxRetention.
// A value that does not parse is settings' to report. Pure.
func checkRetention(v string) error {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d == 0 {
		return nil //nolint:nilerr // settings reports a value that does not parse; 0 keeps entries forever
	}
	if d < MinRetention || d > MaxRetention {
		return validate.Range("retention", d, MinRetention, MaxRetention) //nolint:wrapcheck // a field error, re-fielded by settings
	}
	return nil
}
