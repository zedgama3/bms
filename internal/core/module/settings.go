package module

import "github.com/zedgama3/bms/internal/core/settings"

// The declaration types live in core/settings (settings.md: settings cannot import module,
// so they move there as authz.Permission and signals.Name did) and are aliased here, so
// manifests are unchanged (D39).

// SettingKind is the type of a setting's value; see settings.Kind. The zero value is invalid.
type SettingKind = settings.Kind

// The setting kinds.
const (
	SettingBool     = settings.Bool     // "true" or "false"
	SettingInt      = settings.Int      // a base-10 integer
	SettingDuration = settings.Duration // time.ParseDuration form, for example "72h"
	SettingText     = settings.Text     // free text, up to 4096 bytes
	SettingEnum     = settings.Enum     // exactly one of Options
	SettingSecret   = settings.Secret   // free text, sealed at rest, never returned; Default must be ""
)

// SettingDecl declares one runtime setting of a module (settings.md stores and edits it
// under the key "<module>.<key>"). Its title and description are message keys, not text
// (decision 8): module.<name>.setting.<key>.title|description|option.<opt>. The key grammar
// is [a-z][a-z0-9]*(_[a-z0-9]+)*, at most 64 bytes (underscores, because ICU arguments
// allow no hyphen). Validate is settings.Decl.Validate: errs.Invalid joining one
// validate.FieldError per problem (key, kind, options, default).
type SettingDecl = settings.Decl
