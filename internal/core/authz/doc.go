// Package authz is the core's authorization contract. For now it holds only the leaf types
// that module-contract.md places here so that the module package, which sits at the top of
// the core graph, can carry declared permissions without any core package importing it:
// Permission, Scope and PermissionDecl. The authorizer itself (grants, roles, checks) is
// authz.md's and is not part of this package yet.
package authz
