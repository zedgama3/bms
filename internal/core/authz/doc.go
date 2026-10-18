// Package authz decides whether a person may perform an action (authz.md). It holds the
// catalogue of permissions the core and the modules declare, the roles a regional builds
// from them, the grants it makes (site-wide or for one department, with an optional end
// date) and its separation-of-duties rules, and answers Allow, AllowIn and ExtentOf for
// the actor an app-supplied closure reads from the session. It also fills the session's
// permission list (PermissionsOf), the audit log's actor titles (TitlesOf) and the access
// API (Routes, OpenAPI).
//
// Permission, Scope and PermissionDecl live in package perm and are re-exported here as
// aliases, so that audit can name them without importing authz. authz imports model, lib,
// core/db, core/audit, core/signals and core/telemetry; never core/identity, core/module,
// api, app, config or a module. authz/postgres is the only place with SQL, authz/rows the
// row types they share, authz/authztest the fake modules test with.
package authz
