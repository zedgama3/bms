// Package identity says who is making a request (plan/design/identity.md): local accounts
// keyed by e-mail address, Argon2id passwords, signed session tokens in a cookie that are
// checked without a database write, the per-request Authenticator the api shell calls, and
// the break-glass superuser created only by command.
//
// It imports model, lib, core/db, core/audit, core/signals, core/telemetry, core/notify (the
// reset e-mail) and the leaf types of core/authz. It never imports core/module, api, app,
// config or a module; authz and audit never import it either: app hands them the actor
// through a closure over SessionFrom. Routes returns the package's own Route type and app converts it.
package identity
