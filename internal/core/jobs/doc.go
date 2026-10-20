// Package jobs runs work that belongs to no request, when it is due, on the writing copy,
// with retries (plan/design/jobs.md). Two shapes share one table, core.job: a scheduled job
// (a slot every N minutes or hours, never overlapping itself) and a deferred job (enqueued by
// a use case inside its own transaction, with record references as arguments and a time to
// run). Rows are leased with FOR UPDATE SKIP LOCKED, so several processes on one database
// share the work and a read-only copy does none of it. The guarantee is at-least-once: a
// handler is idempotent.
//
// The package imports model, lib, core/db, core/signals, core/audit, core/authz (leaf types
// and Authorizer) and core/telemetry. It never imports core/settings, core/identity,
// core/people, core/notify, core/module, api, app, config or a module: what it needs from
// those arrives through Options, which app fills.
//
// No argument is ever written to a log, a span or a metric: a job is named by its name and
// ID, a failure by its kind and operation.
package jobs
