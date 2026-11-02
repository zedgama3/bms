// Package people holds who each participant is: their profile (playa name first), their
// contact details and emergency contacts, the consents they gave, search, account linking
// and the person page (plan/design/people.md). It also answers for its own rows to
// datarights, which carries out the export and deletion of a person across the core and
// every module (PeopleSource: ExportPerson, DeletePerson, Exists; plan/design/datarights.md).
//
// The package imports model, lib, core/db, core/audit, core/authz, core/identity (its
// signal names), core/signals, core/telemetry and contracts/personpage. It never imports
// core/module, core/datarights, api, app, config or a module: the modules' page
// contributions reach it through app's Service.Link.
package people
