package authz

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	codeMalformed = "malformed"
	maxIdent      = 64
)

// identRE is module-contract.md's ident grammar. module keeps its own copy because authz
// may not import module; a shared lib package is proposed in the implementation report.
var identRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func validIdent(s string) bool { return len(s) <= maxIdent && identRE.MatchString(s) }

// Permission names one thing a person may be allowed to do:
// "<module>.<ident>(.<ident>)*", for example "volunteers.shifts.manage". The first segment
// is the declaring module's name or a core area (people, events, departments, accounts,
// access, settings, audit, core). A permission is declared in code and granted by a
// regional (09 §4). Every segment is lower-case letters and digits in kebab-case, at most
// 64 bytes.
type Permission string

// Validate fails with errs.Invalid unless p has two or more ident segments. The error is a
// validate.FieldError on the field "name".
func (p Permission) Validate() error {
	segments := strings.Split(string(p), ".")
	ok := len(segments) >= 2
	for _, s := range segments {
		ok = ok && validIdent(s)
	}
	if ok {
		return nil
	}
	acc := validate.Errors{Op: "authz.Permission.Validate"}
	acc.Addf("name", codeMalformed,
		"must be <module>.<ident>(.<ident>)*, each ident lower-case letters, digits and single hyphens, at most 64 bytes")
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Module returns the first segment. Cannot fail; meaningful once Validate passes.
func (p Permission) Module() string {
	first, _, _ := strings.Cut(string(p), ".")
	return first
}

// Scope says how a grant of the permission may be limited (09 §4). The zero value is not a
// scope: PermissionDecl.Validate rejects it.
type Scope uint8

// The scopes a permission may declare.
const (
	ScopeSite       Scope = iota + 1 // granted site-wide only, e.g. events.manage
	ScopeDepartment                  // granted site-wide or for one department, e.g. volunteers.shifts.manage
)

// String returns "site", "department" or "Scope(<n>)". Cannot fail.
func (s Scope) String() string {
	switch s {
	case ScopeSite:
		return "site"
	case ScopeDepartment:
		return "department"
	default:
		return "Scope(" + strconv.Itoa(int(s)) + ")"
	}
}

// PermissionDecl is one declared permission. Its title and description are message keys
// derived from Name (module-contract decision 8), not text.
type PermissionDecl struct {
	Name  Permission
	Scope Scope
	// Reaches is the highest sensitivity of any field the permission lets a person read
	// or change (model.md, Proposed change 3). authz shows it to access admins and uses
	// it in separation-of-duties rules; a permission that reaches sensitive data stands
	// out in the role editor.
	Reaches model.Sensitivity
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem, on the fields
// "name", "scope" and "reaches": Name invalid, Scope zero or out of range, Reaches failing
// model.Sensitivity.Validate.
func (d PermissionDecl) Validate() error {
	acc := validate.Errors{Op: "authz.PermissionDecl.Validate"}
	if d.Name.Validate() != nil {
		acc.Addf("name", codeMalformed,
			"must be <module>.<ident>(.<ident>)*, each ident lower-case letters, digits and single hyphens, at most 64 bytes")
	}
	if d.Scope != ScopeSite && d.Scope != ScopeDepartment {
		acc.Addf("scope", validate.CodeOneOf, "must be site or department")
	}
	if d.Reaches.Validate() != nil {
		acc.Addf("reaches", validate.CodeOneOf, "must be one of: public, internal, personal, sensitive")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
