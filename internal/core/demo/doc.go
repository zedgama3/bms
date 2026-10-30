// Package demo lets strangers use a public copy of bms safely: made-up data in, nothing real
// out (plan/design/demo.md, D53). It owns the demo switch and the banner data (Info), the
// seed that builds one fictional regional through the services as the demo accounts (Seed,
// and demoseed.Point for the modules' own data), the reset that returns a demo database to
// that state (Reset), the Guard that keeps the shared accounts usable (Protected), the mail
// capture that keeps every notification inside the demo (Channel, ListMail) and the upload
// cap (Policy).
//
// It imports model, lib, core/db, core/audit, core/authz, core/identity (Credentials,
// Session, WithSession), core/people, core/departments, core/events, core/settings,
// core/notify (Channel, Envelope), core/files (Policy, PolicySource), core/telemetry and
// contracts/demoseed. It never imports core/module, api, app, config or a module; only app
// imports it. With Options.Enabled false (the default) the part declares its empty tables
// and nothing else: production installs never see it.
package demo
