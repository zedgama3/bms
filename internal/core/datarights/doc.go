// Package datarights owns "export everything about person X" and "delete everything about
// person X" across the core and the modules, the deletion requests people file
// (core.data_request), the notice to the people who decide them, the export and delete
// commands, and the routes under /api/datarights/ (plan/design/boundaries.md, decision 5;
// the use cases and their failure modes are people.md's F28 to F44 and F57 to F64).
//
// The package imports model, lib, core/db, core/audit, core/authz, core/notify (the Notifier
// seam and Message), core/files (the shape of files' deletion), core/people (the permission
// names), core/signals and core/telemetry.
// It never imports core/module, api, app, config or a module. The types module-contract
// aliases (module.PersonData, Export, Deletion, Kept) are declared here; every source is
// handed in as a value of the service's own shape, with no adapter: the core ones through
// Options and the modules' through Link.
package datarights
