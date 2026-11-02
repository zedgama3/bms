package module

import (
	"context"
	"log/slog"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// Factory builds a module from its dependencies. It validates the fields of deps it uses
// (go-conventions: constructors validate), builds its services, and needs no database
// read and never writes: `bms openapi` and `bms check` build every module against a DB
// that refuses every statement (app.md decision 7), so a read at construction fails the
// build step. A module builds on a read-only copy. ctx carries app's 30 s Build deadline.
type Factory func(ctx context.Context, deps Deps) (Module, error)

// Module is what every module gives the core at runtime. The optional interfaces below
// add routes, an API description, capability implementations and contributions; Build
// checks that a module implements exactly what its manifest declares (F29 to F31, F33, F34).
type Module interface {
	PersonData

	// Start begins the module's background work: it registers signal handlers and jobs
	// through Deps and launches goroutines that exit when Stop is called. It subscribes at
	// most once per declared signal name and never publishes (a signal describes a write;
	// signals.md). It returns once that is set up, without blocking, and must succeed on a
	// read-only database (it performs no writes). It fails with the kind of whatever it
	// tried; Set.Start then stops the modules already started.
	Start(ctx context.Context) error

	// Stop ends the work Start began and waits for it, within ctx. It is called once,
	// after Start returned nil, and must be safe to call when Start was never called.
	// Fails with errs.Timeout or errs.Canceled when ctx ends first (F47), joined with any
	// goroutine's error.
	Stop(ctx context.Context) error
}

// PersonData is how a module answers "export everything about person X" and "delete
// everything about person X" (privacy-data). Every module implements it, including one
// that holds no personal data (it returns an empty Export and a zero Deletion), so the
// obligation is visible in code and tested by moduletest. The caller is the core
// (datarights.md, boundaries.md decision 5) with an actor who has already been authorized
// for people.data.export or people.data.delete and whose action datarights audits; a module
// does not re-check that permission, and these methods are never reachable through a
// module's own routes.
//
// It is an alias of datarights.PersonData, so a module's value goes to datarights as it is,
// with no adapter:
//
//	ExportPerson(ctx, person model.PersonID) (Export, error)
//
// returns every record this module holds about person, at every sensitivity level, as one
// JSON object the module's spec documents (field names are stable, so a person can read
// their own data). An empty Data means nothing is held. It fails with errs.Invalid for a
// zero person and with the store's kind otherwise.
//
//	DeletePerson(ctx, person model.PersonID) (Deletion, error)
//
// deletes or irreversibly anonymizes every record this module holds about person, including
// attachments, and reports what it did. Records the module's spec says must be kept (a
// legal or safety reason) are counted under Kept with their kind. It is idempotent: a
// person with no records yields a zero Deletion and nil. It fails with errs.Invalid for a
// zero person; on a read-only copy, with errs.Unavailable wrapping db.ErrReadOnly, the kind
// db returns for a write (db.md).
type PersonData = datarights.PersonData

// Export is one module's share of a person's data export (an alias of datarights.Export).
type Export = datarights.Export

// Deletion reports what DeletePerson did (an alias of datarights.Deletion).
type Deletion = datarights.Deletion

// Kept is one kind of record a module keeps on deletion; the reason is documented in the
// module's spec and shown through the message key module.<name>.kept.<kind> (an alias of
// datarights.Kept).
type Kept = datarights.Kept

// APIDescriber is implemented by a RouteProvider that documents its routes; every
// RouteProvider must implement it. The api shell merges the fragments into one OpenAPI
// document (api.md decision 11 defines the fragment form: one JSON object with paths
// relative to /api/<name>, components.schemas whose names start with the module's name,
// and tags; "$ref"s point at the module's own schemas or the base ones, never another
// module's).
type APIDescriber interface {
	// OpenAPI returns the module's fragment, embedded at build time. Cannot fail; Build
	// rejects an empty result (F34).
	OpenAPI() []byte
}

// CapabilityProvider is implemented by a module whose manifest lists Provides.
type CapabilityProvider interface {
	// Capabilities returns one implementation per capability in Manifest.Provides, keyed
	// by name; each value implements that contract's interface. Cannot fail; Build checks
	// the keys against the manifest (F29) and consumers assert the type (decision 5).
	Capabilities() map[Capability]any
}

// Contributor is implemented by a module whose manifest lists ContributesTo.
type Contributor interface {
	// Contributions returns one value per point in Manifest.ContributesTo. Cannot fail;
	// Build checks the keys (F30) and asks the definer's Accepts (F31).
	Contributions() map[ExtensionPoint]any
}

// Deps is what the core gives a module. app builds one per module (Provision), scoped to
// it where that matters: the logger carries module=<name>; Signals, Settings and Jobs
// accept only what the manifest declared; Links sees only the module's own declarations.
// Fields typed by later specs are added when those specs are Approved; adding a field is
// not a breaking change for modules. A module validates the fields it uses.
type Deps struct {
	Logger *slog.Logger     // scoped (module=<name>); never nil (Build checks)
	Tracer telemetry.Tracer // scope module.<name>; never nil (Build checks)
	Clock  clock.Clock      // never nil (Build checks)
	Links  *Links           // set by Build, never by Provision

	DB      db.DB       // the module's schema is its own responsibility; db reports read-only
	Signals signals.Bus // Provision sets disp.Scope(m.Name, m.Publishes, m.Subscribes)

	// Audit records what the module audits (audit.md): scoped to the module by Provision
	// with Scoped(m.Name), so an action outside "<name>." fails errs.Invalid. Nil only in
	// tests that do not audit; moduletest.Conformance fills a nil one with an
	// audittest.Fake.
	Audit audit.Recorder

	// Authz decides what the actor in ctx may do (authz.md; M2): the module passes its own
	// permission names, checking each use case first with Allow (a site-wide permission),
	// AllowIn (one record of a department) or ExtentOf (a list, search, count or export
	// filters by the extent it returns). Shared, not scoped: Provision sets it. Nil only in
	// tests that do not check permissions; moduletest.Conformance fills a nil one with an
	// authztest.Fake, which denies everything, so a module that forgets its check fails its
	// tests rather than the burner.
	Authz authz.Authorizer

	// People is the directory of people (people.md; M2): NamesOf and ConsentOf. Shared, not
	// scoped: Provision sets it. A foreign key to core.person(id) is allowed and uses ON
	// DELETE RESTRICT, because people calls the module's DeletePerson first. Nil only in
	// tests that do not look people up; moduletest.Conformance fills a nil one with a
	// peopletest.Fake.
	People people.Directory

	// Departments is the directory of departments (departments-events.md; M3): NamesOf and
	// Active. Shared, not scoped: Provision sets it, unscoped, because both methods need no
	// actor. A module checks Active before writing a department's record; a foreign key to
	// core.department(id) is allowed with ON DELETE RESTRICT, and the table is never read
	// directly. Nil only in tests that do not look departments up; moduletest.Conformance
	// fills a nil one with a departmentstest.Fake.
	Departments departments.Directory

	// Events is the directory of events (departments-events.md; M3): InfoOf. Shared, not
	// scoped: Provision sets it, unscoped. A foreign key to core.event(id) is allowed with ON
	// DELETE RESTRICT, and the table is never read directly. Nil only in tests that do not
	// look events up; moduletest.Conformance fills a nil one with an eventstest.Fake.
	Events events.Directory

	// Settings reads the module's own settings (settings.md; M4): bare keys, Text for a
	// text or enum value, Secret for a sealed one. Scoped to the module by Provision with
	// Scoped(m.Name), so only its declared keys answer; nothing is permission-checked (a
	// module reads its own as the system). A module never caches a value: a read is a map
	// lookup. Nil only in tests that read no setting; moduletest.Conformance fills a nil one
	// with settingstest.NewFake(Manifest.Name, Manifest.Settings).
	Settings settings.Reader

	// Notify queues e-mail to people (notify.md; M4): Send and Configured. Scoped to the
	// module by Provision with Scoped(m.Name), so only a template named "<name>.<ident>",
	// shipped in Manifest.Templates, is accepted (otherwise errs.Invalid, field "template",
	// code "scope"). Send joins the transaction in ctx and never sends inline; it fails
	// errs.Unsupported wrapping notify.ErrNotConfigured when e-mail is not set up. Nil only
	// in tests that send nothing; moduletest.Conformance fills a nil one with
	// notifytest.NewFake(Manifest.Name).
	Notify notify.Notifier

	// Jobs registers the module's background jobs and enqueues its deferred ones (jobs.md;
	// M4): Register in Start with names "<name>.<ident>" (the declaration; no I/O, works on a
	// read-only copy), Enqueue inside the use case's transaction with record references as
	// arguments. Scoped to the module by Provision with Scoped(m.Name), so a name of another
	// owner fails errs.Invalid (field "name", code "scope"). Handlers run as the system, are
	// idempotent and may run more than once. Nil only in tests that register no job;
	// moduletest.Conformance fills a nil one with jobstest.NewFake(Manifest.Name).
	Jobs jobs.Registrar

	// Files keeps the bytes the module attaches to its records (files.md; M4): Stage an upload
	// before the module's transaction, Attach it inside its Write transaction with the record,
	// Open and Serve it from the module's own route after the module's own permission check.
	// Scoped to the module by Provision with Scoped(m.Name), so the module sees only its own
	// files and a record whose kind is not "<name>.<ident>" fails errs.Invalid (field
	// "record", code "scope"). A module never gets the object store itself. Nil only in tests
	// that attach nothing; moduletest.Conformance fills a nil one with
	// filestest.NewFake(Manifest.Name).
	Files files.Store
}
