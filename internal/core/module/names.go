package module

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

const (
	codeMalformed = "malformed"
	maxIdent      = 64
	identReason   = "must be lower-case letters and digits in kebab-case, starting with a letter, at most 64 bytes"
)

// identRE is the segment grammar every declared name uses: "[a-z][a-z0-9]*(-[a-z0-9]+)*".
// authz keeps an unexported copy for authz.Permission because it may not import this package.
var identRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func isIdent(s string) bool { return len(s) <= maxIdent && identRE.MatchString(s) }

// dotted reports whether s is exactly "<ident>.<ident>".
func dotted(s string) bool {
	a, b, found := strings.Cut(s, ".")
	return found && isIdent(a) && isIdent(b)
}

// Capability names a service that exactly one enabled module provides and others require
// or use (D19). Its Go interface lives in internal/contracts/<capability>/, which exports
// the name as an untyped string constant (const Name = "volunteer-credit") so it converts
// to this type. Format: one ident.
type Capability string

// Validate fails with errs.Invalid if c is not one ident. The error is a
// validate.FieldError on the field "name".
func (c Capability) Validate() error {
	if isIdent(string(c)) {
		return nil
	}
	acc := validate.Errors{Op: "module.Capability.Validate"}
	acc.Addf("name", codeMalformed, "%s", identReason)
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// ExtensionPoint names a place where any number of modules contribute something the
// definer shows or collects (D19): "<definer>.<ident>", for example "people.person-page".
// The definer is a module name or a core area name (people, events, departments).
type ExtensionPoint string

// Validate fails with errs.Invalid unless p is "<ident>.<ident>". The error is a
// validate.FieldError on the field "name".
func (p ExtensionPoint) Validate() error {
	if dotted(string(p)) {
		return nil
	}
	acc := validate.Errors{Op: "module.ExtensionPoint.Validate"}
	acc.Addf("name", codeMalformed, "must be <definer>.<ident>, each %s", strings.TrimPrefix(identReason, "must be "))
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Definer returns the part before the first ".". Cannot fail; meaningful once Validate
// passes.
func (p ExtensionPoint) Definer() string {
	definer, _, _ := strings.Cut(string(p), ".")
	return definer
}

// ExtensionPointDecl declares an extension point and how to recognise a valid
// contribution, so Build can reject a wrong type at start-up with both module names.
type ExtensionPointDecl struct {
	Name ExtensionPoint
	// Accepts reports whether v implements the point's contract interface, typically
	// func(v any) bool { _, ok := v.(personpage.Section); return ok }. Must not be nil.
	Accepts func(v any) bool
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, on the
// fields "name" and "accepts": Name fails Validate, or Accepts is nil.
func (d ExtensionPointDecl) Validate() error {
	acc := validate.Errors{Op: "module.ExtensionPointDecl.Validate"}
	asField(&acc, "name", d.Name.Validate())
	if d.Accepts == nil {
		acc.Addf("accepts", validate.CodeRequired, "must not be nil")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// asField records every problem in err under one field path, keeping the codes and
// reasons; an err with no field errors is recorded under field with its message.
func asField(acc *validate.Errors, field string, err error) {
	if err == nil {
		return
	}
	fes := validate.Fields(err)
	if len(fes) == 0 {
		acc.Addf(field, "", "%s", err.Error())
		return
	}
	for _, fe := range fes {
		acc.Addf(field, fe.Code, "%s", fe.Reason)
	}
}

// asNested records every problem in err under prefix, appending the problem's own field
// ("permissions[2]" + "name" gives "permissions[2].name").
func asNested(acc *validate.Errors, prefix string, err error) {
	if err == nil {
		return
	}
	fes := validate.Fields(err)
	if len(fes) == 0 {
		acc.Addf(prefix, "", "%s", err.Error())
		return
	}
	for _, fe := range fes {
		field := prefix
		if fe.Field != "" {
			field += "." + fe.Field
		}
		acc.Addf(field, fe.Code, "%s", fe.Reason)
	}
}

// invalid builds an errs.Invalid with op and a formatted cause.
func invalid(op, format string, args ...any) error {
	return errs.E(op, errs.Invalid, fmt.Errorf(format, args...))
}

// internal builds an errs.Internal with op and a formatted cause.
func internal(op, format string, args ...any) error {
	return errs.E(op, errs.Internal, fmt.Errorf(format, args...))
}

// errNoProvider is the cause of a NotFound for an optional capability nobody provides.
func errNoProvider(c Capability) error {
	return fmt.Errorf("no enabled module provides capability %q", c)
}
