// Package audit keeps an append-only record of who did what and who saw what (plan/design/
// audit.md). Writers hold a Recorder scoped to their area; the log stamps the actor, the
// time and the trace, writes to core.audit_entry (in the caller's transaction when there is
// one) or, on a read-only copy, to a local spool that the importer moves into the table
// when the copy can write. Audit reviewers search and export through Search and Export,
// which are themselves audited.
//
// It imports no module and not core/module: Routes returns []httpx.Route
// and app converts it with module.CoreRoutes.
package audit
