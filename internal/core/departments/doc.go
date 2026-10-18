// Package departments holds the regional's standing departments: name, description and the
// department's shared mailbox, archiving and restoring, the Directory modules label their
// records with, and authz's DepartmentSource (plan/design/departments-events.md).
//
// The package imports model, lib, core/db, core/audit, core/authz, core/signals and
// core/telemetry. It never imports core/module, core/identity (the actor arrives as an
// authz.ActorFunc), api, app, config or a module. Leadership is not stored here: it is a
// department-scoped grant of department-admin (authz). Nothing here is personal data.
package departments
