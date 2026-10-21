package privacy

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Field codes privacy adds to the shell's vocabulary (validate has the rest).
const (
	codeRequired    = validate.CodeRequired
	codeMalformed   = validate.CodeMalformed
	codeOneOf       = validate.CodeOneOf
	codeUnknown     = "unknown"
	codeDuplicate   = "duplicate"
	codeNotOmitable = "not_omittable"
	codeUngrouped   = "ungrouped"
	codeNotStruct   = "not_struct"
	codeTooMany     = "too_many"
	codeTooLong     = "too_long"
	codeTooFew      = "too_few"
	codeMarshaler   = "marshaler"
	codeDynamic     = "dynamic"
	codeUnsettable  = "unsettable"
)

// Audience names who may see a group of fields. Built-in audiences are below; a module may
// add its own through Group.Extra and assert them per record through Relation.Also.
type Audience string

// The built-in audiences.
const (
	AudienceNobody   Audience = "nobody"    // never returned; the feature is off
	AudienceSelf     Audience = "self"      // the person the record is about
	AudienceHolders  Audience = "holders"   // the subject, and holders of Group.Permission in the record's extent
	AudienceSignedIn Audience = "signed_in" // the subject, and any signed-in viewer who may see the record
)

const (
	maxAudienceBytes = 32
	// maxGroupNameBytes keeps "visibility_<name>" inside the 64-byte setting key limit.
	maxGroupNameBytes = 64 - len(settingKeyPrefix)
	settingKeyPrefix  = "visibility_"
)

// identRE is the grammar of audience and group names: lower-case snake case, which is also
// what a setting key and an enum option accept.
var identRE = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func isBuiltin(a Audience) bool {
	switch a {
	case AudienceNobody, AudienceSelf, AudienceHolders, AudienceSignedIn:
		return true
	}
	return false
}

// Validate fails errs.Invalid (code malformed) unless a is an ident of at most 32 bytes.
func (a Audience) Validate() error {
	if len(a) <= maxAudienceBytes && identRE.MatchString(string(a)) {
		return nil
	}
	acc := validate.Errors{Op: "privacy.Audience.Validate"}
	acc.Addf("audience", codeMalformed, "must be lower-case snake case, at most %d bytes", maxAudienceBytes)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Group is a named set of fields a regional configures as one setting, visibility_<Name>.
type Group struct {
	Name       string           // ident; the setting is visibility_<Name>; unique within an area
	Permission authz.Permission // who counts as holders; required when Options includes holders
	Default    Audience         // must be in Options
	Options    []Audience       // what the regional may choose, in display order; at least two; built-in or Extra
	Extra      []Audience       // module-defined audiences; must not collide with the built-in names
}

// Validate fails errs.Invalid with field codes: name required/malformed; permission
// required (holders offered) or malformed; default one_of (not in Options); options
// too_few/duplicate/unknown; extra duplicate/malformed.
func (g Group) Validate() error {
	acc := validate.Errors{Op: "privacy.Group.Validate"}
	g.validate(&acc, "")
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// validate records the problems with g under prefix (for example "groups[2]."), "" for none.
func (g Group) validate(acc *validate.Errors, prefix string) {
	switch {
	case g.Name == "":
		acc.Addf(prefix+"name", codeRequired, "is required")
	case len(g.Name) > maxGroupNameBytes || !identRE.MatchString(g.Name):
		acc.Addf(prefix+"name", codeMalformed, "must be lower-case snake case, at most %d bytes", maxGroupNameBytes)
	}
	offersHolders := slices.Contains(g.Options, AudienceHolders)
	switch {
	case g.Permission == "" && offersHolders:
		acc.Addf(prefix+"permission", codeRequired, "is required when holders is offered")
	case g.Permission != "" && g.Permission.Validate() != nil:
		acc.Addf(prefix+"permission", codeMalformed, "must be <module>.<ident>(.<ident>)*")
	}
	seenExtra := map[Audience]bool{}
	for _, e := range g.Extra {
		switch {
		case isBuiltin(e) || seenExtra[e]:
			acc.Addf(prefix+"extra", codeDuplicate, "an extra audience repeats a built-in or another extra")
		case e.Validate() != nil:
			acc.Addf(prefix+"extra", codeMalformed, "must be lower-case snake case, at most %d bytes", maxAudienceBytes)
		}
		seenExtra[e] = true
	}
	if len(g.Options) < 2 {
		acc.Addf(prefix+"options", codeTooFew, "at least two options are needed")
	}
	seenOpt := map[Audience]bool{}
	for _, o := range g.Options {
		switch {
		case seenOpt[o]:
			acc.Addf(prefix+"options", codeDuplicate, "an option is listed twice")
		case !isBuiltin(o) && !slices.Contains(g.Extra, o):
			acc.Addf(prefix+"options", codeUnknown, "an option is neither built in nor one of the group's extra audiences")
		}
		seenOpt[o] = true
	}
	if !slices.Contains(g.Options, g.Default) {
		acc.Addf(prefix+"default", codeOneOf, "must be one of the options")
	}
}

// settingKey returns the key of the setting that holds this group's audience:
// "visibility_<Name>".
func (g Group) settingKey() string { return settingKeyPrefix + g.Name }

// Settings returns one settings.Decl per group: Key "visibility_<name>", Kind Enum,
// Options = the group's Options as strings, Default = the group's Default. A manifest
// appends them to Manifest.Settings; a core area to its Settings slice. Consumer: every
// adopter's manifest; Manifest.Validate checks the result is present. Fails errs.Invalid
// (fields groups[j].<field>) when a group fails Validate or two groups share a name.
func Settings(groups []Group) ([]settings.Decl, error) {
	acc := validate.Errors{Op: "privacy.Settings"}
	validateGroups(&acc, groups)
	if err := acc.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	out := make([]settings.Decl, 0, len(groups))
	for _, g := range groups {
		opts := make([]string, len(g.Options))
		for i, o := range g.Options {
			opts[i] = string(o)
		}
		out = append(out, settings.Decl{
			Key: g.settingKey(), Kind: settings.Enum, Default: string(g.Default), Options: opts,
		})
	}
	return out, nil
}

// validateGroups records every group's problems under groups[j] and repeated names.
func validateGroups(acc *validate.Errors, groups []Group) {
	seen := make(map[string]bool, len(groups))
	for j, g := range groups {
		prefix := fmt.Sprintf("groups[%d].", j)
		g.validate(acc, prefix)
		if g.Name != "" && seen[g.Name] {
			acc.Addf(prefix+"name", codeDuplicate, "two groups share a name")
		}
		seen[g.Name] = true
	}
}

// groupNames is the set of declared group names.
func groupNames(groups []Group) map[string]bool {
	m := make(map[string]bool, len(groups))
	for _, g := range groups {
		m[g.Name] = true
	}
	return m
}

func internalErr(op, format string, args ...any) error {
	return errs.E(op, errs.Internal, fmt.Errorf(format, args...))
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + strings.TrimPrefix(name, ".")
}
