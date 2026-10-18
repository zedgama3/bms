// Package events holds the events the regional puts on: name, kind, dates as instants plus
// the IANA zone they are shown in, the schedule inside an event, closing and reopening, the
// event kinds, the Directory modules label their records with, and the events.event-setup
// extension point that lets modules copy their data into a new event
// (plan/design/departments-events.md).
//
// The package imports model, lib, core/db, core/audit, core/authz, core/signals,
// core/telemetry and contracts/eventsetup. It never imports core/module, core/identity (the
// actor arrives as an authz.ActorFunc), api, app, config or a module. Nothing here is
// personal data.
package events
