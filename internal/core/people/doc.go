// Package people holds who each participant is: their profile (playa name first), their
// contact details and emergency contacts, the consents they gave, and the one place that
// exports or deletes everything bms holds about them (plan/design/people.md).
//
// The package imports model, lib, core/db, core/audit, core/authz, core/identity (its
// signal names), core/notify (the Notifier seam and Message), core/signals, core/telemetry
// and contracts/personpage. It never imports core/module, api, app, config or a module: the
// modules' PersonData and page contributions reach it through app's adapters (Service.Link).
package people
