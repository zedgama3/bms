// Package settings holds what a regional configures while bms runs: typed, validated values
// with defaults, declared by the core and the modules, edited by a settings admin, audited
// and effective without a restart (plan/design/settings.md).
//
// The package imports model, lib, core/db, core/audit, core/authz and core/telemetry. It
// never imports core/module, core/identity, api, app, config or a module: module aliases the
// declaration types defined here, and the actor arrives as an authz.ActorFunc. Nothing here
// is read from the environment, and nothing in it is about a person.
//
// A value is never written to a log, a span, a metric, an audit entry or an error message,
// and a Secret value is sealed at rest and returned by no route.
package settings
