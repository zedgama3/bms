package model

import (
	"errors"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Sensitivity is how carefully a field about a person must be handled (privacy-data).
// Levels are ordered: SensitivityPublic < SensitivityInternal < SensitivityPersonal <
// SensitivitySensitive; compare them with < and >. The zero value is not a level: Validate
// rejects it, so a field whose level was never set fails at the boundary instead of being
// treated as public. Every other method treats an invalid value as SensitivitySensitive.
type Sensitivity uint8

// The four levels, in increasing order of restriction.
const (
	SensitivityPublic    Sensitivity = iota + 1 // anyone the module shows it to
	SensitivityInternal                         // signed-in people with the module's read permission
	SensitivityPersonal                         // the person, and roles with an explicit permission
	SensitivitySensitive                        // named roles only, scoped narrowly
)

var sensitivityNames = [...]string{
	SensitivityPublic:    "public",
	SensitivityInternal:  "internal",
	SensitivityPersonal:  "personal",
	SensitivitySensitive: "sensitive",
}

const sensitivityOptions = "public, internal, personal, sensitive"

func sensitivityErr(op string) error {
	return fieldErr(op, "sensitivity", validate.CodeOneOf,
		map[string]string{"options": sensitivityOptions}, "must be one of: %s", sensitivityOptions)
}

// ParseSensitivity returns the level named by s: "public", "internal", "personal" or
// "sensitive", exactly. It fails with errs.Invalid for anything else; the error lists the
// four names and never echoes s.
func ParseSensitivity(s string) (Sensitivity, error) {
	for l := SensitivityPublic; l <= SensitivitySensitive; l++ {
		if sensitivityNames[l] == s {
			return l, nil
		}
	}
	return 0, sensitivityErr("model.ParseSensitivity")
}

// String returns the level's stable name, as ParseSensitivity accepts it, or
// "Sensitivity(<n>)" for a value outside the set. Cannot fail.
func (s Sensitivity) String() string {
	if s.valid() {
		return sensitivityNames[s]
	}
	return "Sensitivity(" + strconv.Itoa(int(s)) + ")"
}

func (s Sensitivity) valid() bool { return s >= SensitivityPublic && s <= SensitivitySensitive }

// Validate fails with errs.Invalid if s is zero or above SensitivitySensitive.
func (s Sensitivity) Validate() error {
	if !s.valid() {
		return sensitivityErr("model.Sensitivity.Validate")
	}
	return nil
}

// ReadIsAudited reports whether a read of a field at this level must be written to the
// audit log: never for public and internal; for personal, unless the reader is the person
// the field is about (self); always for sensitive. An invalid level is audited. Cannot fail.
func (s Sensitivity) ReadIsAudited(self bool) bool {
	switch s {
	case SensitivityPublic, SensitivityInternal:
		return false
	case SensitivityPersonal:
		return !self
	default: // sensitive, and any invalid level
		return true
	}
}

// MarshalText returns String() as bytes. It fails with errs.Invalid for an invalid level,
// so a response that carries an unset level fails loudly rather than emitting a placeholder.
func (s Sensitivity) MarshalText() ([]byte, error) {
	if !s.valid() {
		return nil, sensitivityErr("model.Sensitivity.MarshalText")
	}
	return []byte(s.String()), nil
}

// UnmarshalText parses text as ParseSensitivity does. On failure *s is unchanged. It fails
// with errs.Invalid for bad text and errs.Internal for a nil receiver.
func (s *Sensitivity) UnmarshalText(text []byte) error {
	if s == nil {
		return errs.E("model.Sensitivity.UnmarshalText", errs.Internal, errors.New("nil receiver"))
	}
	v, err := ParseSensitivity(string(text))
	if err != nil {
		return errs.E("model.Sensitivity.UnmarshalText", errs.Invalid, err)
	}
	*s = v
	return nil
}
