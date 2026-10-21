// Package privacy decides, field by field, which of a record's fields a viewer gets back
// (plan/design/privacy-tags.md).
//
// A response type tags every exported wire field with `privacy:"<level>[,group=<name>]"`.
// A Group is a named set of fields whose audience the regional chooses as one setting,
// visibility_<name>. A service builds a Viewer (ViewerOf) and a Policy (PolicyOf) once per
// use case, calls Filter on each record with that record's Relation, and audits what the
// Result reports. Check is the conformance check that no wire field ships untagged.
//
// The package imports model, lib, core/authz, core/settings and core/audit (two constants).
// It never imports core/identity, core/module, core/people, api, app, config or a module.
// It has no tables, routes, signals or screens and carries no human-readable text.
package privacy
