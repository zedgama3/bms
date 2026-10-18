package authz

import "github.com/zedgama3/bms/internal/core/authz/perm"

// The leaf types live in package perm so that audit, which authz records through, can name
// them without importing authz. They are aliases: authz.Permission and perm.Permission are
// one type, and every method and constant below is the leaf's.

// Permission names one thing a person may be allowed to do ("<module>.<ident>(.<ident>)*",
// for example "volunteers.shifts.manage"). See perm.Permission.
type Permission = perm.Permission

// Scope says how a grant of a permission may be limited. See perm.Scope.
type Scope = perm.Scope

// PermissionDecl is one declared permission, with the starter roles it is seeded into.
// See perm.PermissionDecl.
type PermissionDecl = perm.PermissionDecl

// The scopes a permission may declare.
const (
	ScopeSite       = perm.ScopeSite       // granted site-wide only
	ScopeDepartment = perm.ScopeDepartment // granted site-wide or for one department
)
