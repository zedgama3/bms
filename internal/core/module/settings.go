package module

import (
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
)

// maxTextSetting is the longest value of a SettingText setting, in bytes.
const maxTextSetting = 4096

// SettingKind is the type of a setting's value. The zero value is invalid.
type SettingKind uint8

// The setting kinds.
const (
	SettingBool     SettingKind = iota + 1 // "true" or "false"
	SettingInt                             // a base-10 integer
	SettingDuration                        // time.ParseDuration form, for example "72h"
	SettingText                            // free text, up to 4096 bytes
	SettingEnum                            // exactly one of Options
)

// String returns "bool", "int", "duration", "text", "enum" or "SettingKind(<n>)". Cannot fail.
func (k SettingKind) String() string {
	switch k {
	case SettingBool:
		return "bool"
	case SettingInt:
		return "int"
	case SettingDuration:
		return "duration"
	case SettingText:
		return "text"
	case SettingEnum:
		return "enum"
	default:
		return "SettingKind(" + strconv.Itoa(int(k)) + ")"
	}
}

func (k SettingKind) valid() bool { return k >= SettingBool && k <= SettingEnum }

// SettingDecl declares one runtime setting of a module (settings.md stores and edits it
// under the key "<module>.<key>"). Its title and description are message keys, not text
// (decision 8).
type SettingDecl struct {
	Key     string      // one ident
	Kind    SettingKind // how Default and every value are parsed
	Default string      // text form; must parse as Kind and pass Check
	Options []string    // SettingEnum only: the allowed values, each one ident, at least two
	// Check is an optional extra rule on the text value ("must be between 1 and 24"); nil
	// means the kind's parse is the whole rule. It must be pure and safe for concurrent
	// use. (module-contract.md names this field Validate, which Go cannot allow next to
	// the Validate method; see the implementation report.)
	Check func(value string) error `json:"-"`
}

// Validate fails with errs.Invalid, joining one validate.FieldError per problem, on the
// fields "key", "kind", "options" and "default": Key not an ident; Kind invalid; Options
// given for a kind other than enum, or fewer than two or not idents or duplicated for
// enum; Default not parsable as Kind; Default rejected by Check.
func (d SettingDecl) Validate() error {
	acc := validate.Errors{Op: "module.SettingDecl.Validate"}
	if !isIdent(d.Key) {
		acc.Addf("key", codeMalformed, "%s", identReason)
	}
	if !d.Kind.valid() {
		acc.Addf("kind", validate.CodeOneOf, "must be one of: bool, int, duration, text, enum")
	}
	optionsOK := d.checkOptions(&acc)
	if d.Kind.valid() && (d.Kind != SettingEnum || optionsOK) {
		if reason := d.parseDefault(); reason != "" {
			acc.Addf("default", codeMalformed, "%s", reason)
		} else if d.Check != nil {
			if err := d.Check(d.Default); err != nil {
				acc.Addf("default", "rejected", "rejected by the setting's own rule: %s", err.Error())
			}
		}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// checkOptions records the problems with Options and reports whether they are usable.
func (d SettingDecl) checkOptions(acc *validate.Errors) bool {
	if !d.Kind.valid() {
		return false
	}
	if d.Kind != SettingEnum {
		if len(d.Options) > 0 {
			acc.Addf("options", "unexpected", "only an enum setting has options")
		}
		return true
	}
	ok := true
	if len(d.Options) < 2 {
		acc.Addf("options", validate.CodeRequired, "an enum setting needs at least two options")
		ok = false
	}
	seen := make(map[string]struct{}, len(d.Options))
	for _, o := range d.Options {
		if !isIdent(o) {
			acc.Addf("options", codeMalformed, "every option must be lower-case letters and digits in kebab-case, at most 64 bytes")
			ok = false
			break
		}
	}
	for _, o := range d.Options {
		if _, dup := seen[o]; dup {
			acc.Addf("options", "duplicate", "options must not repeat")
			ok = false
			break
		}
		seen[o] = struct{}{}
	}
	return ok
}

// parseDefault returns "" when Default parses as Kind, otherwise the reason it does not.
func (d SettingDecl) parseDefault() string {
	switch d.Kind {
	case SettingBool:
		if d.Default != "true" && d.Default != "false" {
			return `must be "true" or "false"`
		}
	case SettingInt:
		if _, err := strconv.ParseInt(d.Default, 10, 64); err != nil {
			return "must be a base-10 integer"
		}
	case SettingDuration:
		if _, err := time.ParseDuration(d.Default); err != nil {
			return `must be a duration such as "72h"`
		}
	case SettingText:
		if len(d.Default) > maxTextSetting {
			return "must be at most 4096 bytes"
		}
	case SettingEnum:
		for _, o := range d.Options {
			if o == d.Default {
				return ""
			}
		}
		return "must be one of the options"
	}
	return ""
}
