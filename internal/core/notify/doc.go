// Package notify delivers a notification to a person through a channel: a caller names a
// template and recipients inside its own transaction, notify renders the text in each
// recipient's language, queues it durably, and a sender loop on the writing copy delivers it
// over SMTP with retries (plan/design/notify.md).
//
// The package imports model, lib, core/db, core/audit, core/authz, core/telemetry and
// core/settings (only for the Decl type of Settings). It never imports core/identity,
// core/people, core/module, api, app, config or a module: what it needs from those arrives
// through the seams in Options, which app fills. The channel types live in notify/channel
// (a leaf, so the SMTP channel in notify/smtp, which this package uses by default, does not import it) and are
// re-exported here as aliases.
//
// No address, subject, body, template variable or token is ever written to a log, a span, a
// metric, an audit entry or an error message: messages are named by ID, templates by name,
// failures by a short token.
package notify
