// Package regional owns the regional's profile: the regional.* settings area, the Profile
// read from it, the public document the shell reads at boot, theme.css, and the logo, all
// served under /api/regional/ (plan/design/boundaries.md, decision 4).
//
// The package imports core/settings (the declaration types and the Reader), core/files (the
// logo's bytes), core/audit, core/authz, core/db, core/telemetry, lib and model. It never
// imports core/notify, core/identity, core/people, core/module, api, app, config or a module.
// settings stores the values and edits them on its screens; regional declares them, reads
// them and serves the public face of them. Nothing here is about a person: every field of the
// public document is public (settings.md decision 6).
//
// The logo's file record keeps the owner "settings" (core.file.owner of deployed logos and
// the bucket they sit in), so regional is handed a files.Store scoped "settings".
package regional
