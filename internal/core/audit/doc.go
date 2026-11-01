// Package audit keeps an append-only record of who did what and who saw what (plan/design/
// audit.md). Writers hold a Recorder scoped to their area; the log stamps the actor, the
// time and the trace, writes to core.audit_entry (in the caller's transaction when there is
// one) or, on a read-only copy, to a local spool that the importer moves into the table
// when the copy can write. Reading the log (search, export, the count about a person), its
// routes and the retention job belong to core/auditlog; this package keeps the recorder, the
// spool and the vocabulary (Query, Page, Cursor, Record) both share.
//
// It imports no module, not core/module and not auditlog.
package audit
