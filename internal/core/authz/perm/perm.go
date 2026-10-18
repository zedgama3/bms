// Package perm holds the leaf types of authorization: Permission, Scope and
// PermissionDecl. They sit in their own package, imported only by core/authz (which
// re-exports them as type aliases, so everyone keeps writing authz.Permission), because the
// audit contract names them in its own seams while authz records through audit: audit
// imports perm, authz imports audit, and neither imports the other. It imports model and lib
// only.
package perm

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	codeMalformed = "malformed"
	codeDuplicate = "duplicate"
	maxIdent      = 64
)

// identRE is module-contract.md's ident grammar. module keeps its own copy because authz
// may not import module; a shared lib package is proposed in the implementation report.
var identRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidIdent reports whether s is an ident: lower-case letters and digits in kebab-case,
// starting with a letter, at most 64 bytes. Cannot fail.
func ValidIdent(s string) bool { return len(s) <= maxIdent && identRE.MatchString(s) }

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
		ok = ok && ValidIdent(s)
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

// The starter role keys migration 0001 of authz creates (authz decision 12), here so that
// packages authz records through, such as audit, can name them in a PermissionDecl without
// importing authz, which re-exports them as authz.Role*. RoleParticipant is the base role;
// RoleDepartmentAdmin is department-scoped; the rest are site roles.
const (
	RoleParticipant       = "participant"
	RoleAccountAdmin      = "account-admin"
	RoleAccessAdmin       = "access-admin"
	RoleDepartmentSteward = "department-steward"
	RoleEventAdmin        = "event-admin"
	RoleSettingsAdmin     = "settings-admin"
	RoleAuditReviewer     = "audit-reviewer"
	RoleDepartmentAdmin   = "department-admin"
)

// SiteAdminRoles are the six site-wide administrative starter roles: the starter roles of
// api's core.modules.read. Callers must not modify it.
var SiteAdminRoles = []string{
	RoleAccountAdmin, RoleAccessAdmin, RoleDepartmentSteward,
	RoleEventAdmin, RoleSettingsAdmin, RoleAuditReviewer,
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
	// StarterRoles are the keys of the starter roles the permission is seeded into, once,
	// by `bms migrate` (authz decision 12): RoleAccessAdmin and the other Role*
	// constants. Each is an ident and appears once. A key with no role is skipped at seed
	// time, and a regional's later edits to a role are never undone.
	StarterRoles []string
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem, on the fields
// "name", "scope", "reaches" and "starter_roles[j]": Name invalid, Scope zero or out of
// range, Reaches failing model.Sensitivity.Validate, a starter role key that is not an
// ident (malformed) or is repeated (duplicate).
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
	seen := make(map[string]struct{}, len(d.StarterRoles))
	for j, key := range d.StarterRoles {
		field := "starter_roles[" + strconv.Itoa(j) + "]"
		if !ValidIdent(key) {
			acc.Addf(field, codeMalformed, "must be a role key: lower-case letters, digits and single hyphens, at most 64 bytes")
			continue
		}
		if _, dup := seen[key]; dup {
			acc.Addf(field, codeDuplicate, "each starter role may be listed once")
		}
		seen[key] = struct{}{}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
