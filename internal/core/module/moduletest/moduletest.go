// Package moduletest proves a module keeps the module contract. Every module's test suite
// calls Conformance; the hello module is the reference.
package moduletest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/demo/demotest"
	"github.com/zedgama3/bms/internal/core/departments/departmentstest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/events/eventstest"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// person is the invented person the unknown-person rules export and delete. It is not
// exported and differs from subject.Person, so a fake that was seeded for the subject cannot
// also be asked to be empty.
var person = model.PersonID{0x7a, 0x3e, 0x11, 0xc2, 0x5d, 0x94, 0x4b, 0x0a, 0x9e, 0x61, 0x2f, 0x8c, 0x30, 0xd7, 0x15, 0xb4}

// subject is the known, invented person the seeded rules act on. It reaches a module only
// through Seed.
var subject = Subject{
	Person:    model.PersonID{0x1d, 0x52, 0xb8, 0x07, 0xe3, 0x6a, 0x4f, 0x91, 0x80, 0x2c, 0xd4, 0x39, 0x5e, 0xa7, 0x66, 0x0b},
	LegalName: "Marker Quillfeather",
	Email:     "marker.quillfeather@example.invalid",
	Phone:     "+15550100773",
	Note:      "conformance-marker-note-0773",
}

// scrubbedEmail is the token telemetry's Scrub substitutes for e-mail-shaped text. Nothing
// else in a Conformance run produces it, so its presence in a log or span proves an e-mail
// reached telemetry.
const scrubbedEmail = "[email]"

// ruleTimeout bounds every call a rule makes, and the wait for a Stop that may hang.
const ruleTimeout = 30 * time.Second

// Subject is the known person the seeded rules act on. The values are fixed, invented and
// reach a module only through Seed; moduletest exports no person ID or marker constant, and
// the ID the unknown-person rules use is unexported and differs from Person, so a seeded
// fake cannot also be asked to be empty.
type Subject struct {
	Person    model.PersonID // never zero; never the unknown-person ID
	LegalName string         // "Marker Quillfeather": a name has no shape telemetry can scrub, so it is found verbatim
	Email     string         // "marker.quillfeather@example.invalid": a leak shows as "[email]" after Scrub, or as a Violation under a denied key
	Phone     string         // "+15550100773": digits only, found verbatim
	Note      string         // "conformance-marker-note-0773": free text (an address, a narrative), found verbatim
}

// Seed writes records about subject into the module built as mod, putting each of
// subject's marker values into every field of the matching kind the records have (a
// legal-name field gets LegalName, an e-mail field Email, a phone field Phone, a free-text
// field such as a note, an address or a narrative gets Note), so a record that reaches a
// log or span is caught. It touches no other person. An error fails the rule.
//
// Conformance cannot reach a module's store, and the production Factory over Deps.DB
// cannot be seeded because dbtest.Fake holds no rows. So the factory passed to Conformance
// builds the module over the test's in-memory fake store (the one the module's unit tests
// already use), and Seed writes to that store (shared with the factory closure, or reached
// by asserting mod to the module's concrete type).
type Seed func(ctx context.Context, mod module.Module, subject Subject) error

// Option configures Conformance. Only the With* functions make one; the zero Option is
// invalid and fails Conformance before the first rule.
type Option struct{ apply func(*options) error }

// options is what the Options add up to.
type options struct {
	caps       []capOption
	seed       Seed
	seedNum    int // times WithSeed was given
	withoutNum int // times WithoutPersonData was given
	noViews    bool
	demoNum    int // times WithDemoSeed was given
}

// capOption is one WithCapability.
type capOption struct {
	name module.Capability
	impl any
}

// WithCapability makes the stub provider for capability name return impl from
// Capabilities(), so the module's Factory can assert it to the contract interface
// (decision 5, F32). name must be in m.Requires or m.Uses, impl non-nil, each name given
// once; Conformance fails t before the first rule otherwise. It is mandatory for every
// name in m.Requires. For a name in m.Uses it is optional: given, the rules run with the
// capability present; not given, the capability is absent and Links.Capability returns
// errs.NotFound, which the module must survive. The rule
// used_capability_absent_builds_and_starts runs the absent path regardless. Cannot fail.
func WithCapability(name module.Capability, impl any) Option {
	return Option{apply: func(o *options) error {
		o.caps = append(o.caps, capOption{name: name, impl: impl})
		return nil
	}}
}

// WithSeed registers how a rule puts records about a known person into the module. The
// rules that need data (seeded_person_exports_data,
// seeded_person_delete_counts_and_is_idempotent, no_personal_data_in_logs) call seed once
// each, on a fresh module, after Plan.Build and before Start. A nil seed fails Conformance
// before the first rule. Cannot fail.
func WithSeed(seed Seed) Option {
	return Option{apply: func(o *options) error {
		o.seed = seed
		o.seedNum++
		return nil
	}}
}

// WithoutPersonData states that the module holds nothing about any person (its
// ExportPerson is always empty and its DeletePerson always zero). The seeded rules then
// pass trivially and no_personal_data_in_logs runs without a seed. A module whose spec
// lists any table or field about a person must not use it; the reviewer checks this
// against the spec's Data section. Cannot fail.
func WithoutPersonData() Option {
	return Option{apply: func(o *options) error {
		o.withoutNum++
		return nil
	}}
}

// WithoutViews states that the module sends no response types on the wire (D44): the
// rules views_tagged and visibility_settings_declared then pass trivially. Without it,
// Manifest.Views must list every response type, tagged as privacy-tags.md describes, and
// must not be empty when the module has routes. A module with routes whose handlers write
// no JSON body (moduletest's own fixtures) is the case it exists for; a module with any
// JSON response never uses it. Cannot fail.
func WithoutViews() Option {
	return Option{apply: func(o *options) error {
		o.noViews = true
		return nil
	}}
}

// WithDemoSeed states that the module contributes demo data (D53): its manifest lists
// demoseed.Point in ContributesTo and Contributions() returns a demoseed.Provider. The rule
// demoseed_provider_valid then runs the provider against the module's fakes with
// demotest.NewFakeActors() and requires it to create at least one record (Result.Items > 0)
// and to return no Permission error. The module is started first (as `bms demo seed` does)
// and the fake event has dates, so a provider may rely on both. Optional; without it the
// rule is skipped. Conformance
// fails t before the first rule when the manifest does not list demoseed.Point. Cannot fail.
func WithDemoSeed() Option {
	return Option{apply: func(o *options) error {
		o.demoNum++
		return nil
	}}
}

// isNil reports whether v is nil or an interface holding a nil pointer, map, func, chan,
// slice or interface, which a Factory's type assertion would accept and then crash on.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Chan, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// resolveOptions applies opts and checks them against the manifest (F48-F52). It returns
// one message per problem, each naming the module and the option.
func resolveOptions(m module.Manifest, opts []Option) (options, []string) {
	var (
		o        options
		problems []string
	)
	for i, opt := range opts {
		if opt.apply == nil {
			problems = append(problems, fmt.Sprintf("option %d is the zero Option; build options with moduletest.With*", i+1))
			continue
		}
		if err := opt.apply(&o); err != nil {
			problems = append(problems, fmt.Sprintf("option %d: %v", i+1, err))
		}
	}
	declared := func(c module.Capability) bool {
		return slices.Contains(m.Requires, c) || slices.Contains(m.Uses, c)
	}
	seen := map[module.Capability]bool{}
	for _, c := range o.caps {
		switch {
		case !declared(c.name):
			problems = append(problems, fmt.Sprintf("WithCapability(%q): module %q neither requires nor uses that capability", c.name, m.Name))
		case isNil(c.impl):
			problems = append(problems, fmt.Sprintf("WithCapability(%q): the implementation is nil", c.name))
		case seen[c.name]:
			problems = append(problems, fmt.Sprintf("WithCapability(%q) is given more than once", c.name))
		}
		seen[c.name] = true
	}
	for _, c := range m.Requires {
		if !seen[c] {
			problems = append(problems, fmt.Sprintf("module %q requires capability %q: add moduletest.WithCapability(%q, fake)", m.Name, c, c))
		}
	}
	if o.demoNum > 1 {
		problems = append(problems, "WithDemoSeed is given more than once")
	}
	if o.demoNum > 0 && !slices.Contains(m.ContributesTo, module.ExtensionPoint(demoseed.Point)) {
		problems = append(problems, fmt.Sprintf("WithDemoSeed: module %q does not list %q in ContributesTo", m.Name, demoseed.Point))
	}
	switch {
	case o.seedNum > 1:
		problems = append(problems, "WithSeed is given more than once")
	case o.withoutNum > 1:
		problems = append(problems, "WithoutPersonData is given more than once")
	case o.seedNum > 0 && o.withoutNum > 0:
		problems = append(problems, "WithSeed and WithoutPersonData are both given; choose one")
	case o.seedNum > 0 && o.seed == nil:
		problems = append(problems, "WithSeed(nil): the seed is nil")
	case o.seedNum == 0 && o.withoutNum == 0:
		problems = append(problems, fmt.Sprintf("module %q needs exactly one of moduletest.WithSeed and moduletest.WithoutPersonData", m.Name))
	}
	return o, problems
}

// Conformance runs the contract checks against one module. provision supplies the
// module's dependencies as fakes, as app's Provision does (a dbtest.Fake in DB, a
// signalstest.Fake in Signals scoped to the manifest, and the later fields); it is called
// once per rule, so each rule gets fresh fakes; nil is allowed. Conformance replaces Logger
// and Tracer with telemetrytest.New(t)'s (any policy violation fails the rule), fills a nil
// Clock, DB, Signals or Audit with a fake (an audittest.NewFake scoped to the module), a nil
// Authz with authztest.NewFake() (it denies everything) and a nil People with
// peopletest.NewFake(), a nil Departments with departmentstest.NewFake(), a nil Events with
// eventstest.NewFake(), a nil Settings with settingstest.NewFake(Manifest.Name, Manifest.Settings), a nil Notify with
// notifytest.NewFake(Manifest.Name), a nil Jobs with jobstest.NewFake(Manifest.Name), and sets
// Links through Plan.Build. A DB, Signals, Audit, Notify or Jobs that is not a dbtest.Fake,
// signalstest.Fake, audittest.Fake, notifytest.Fake or jobstest.Fake fails t (t.Fatalf),
// naming the module, because the rules need to make the database read-only, to read back the
// subscriptions and the registered jobs and to scan the audit entries, the messages and the
// jobs the module recorded for personal data (no_personal_data_in_logs).
//
// opts supply what the rules cannot learn from the manifest: the value behind each
// capability the module requires or uses (WithCapability), and how to put a known person's
// records into the module (WithSeed) or the statement that it holds none
// (WithoutPersonData). Exactly one of WithSeed and WithoutPersonData is required, and
// WithCapability is required for every name in m.Requires. Conformance fails t before the
// first rule when the options disagree with the manifest, naming the option and the
// capability; otherwise it runs the rules as named subtests, in this order, and stops at
// the first that fails, so the failing subtest's name is the broken rule: manifest_valid,
// views_tagged, visibility_settings_declared, templates_register, registers_under_its_name, implements_declared, demoseed_provider_valid (only with WithDemoSeed), rejects_wrong_capability_type,
// used_capability_absent_builds_and_starts, start_read_only, subscribes_every_declared_name,
// jobs_register_scoped, stop_honors_context, stop_without_start_is_nil, export_zero_id, delete_zero_id,
// export_unknown_person_is_empty, delete_unknown_person_is_zero_and_idempotent,
// delete_read_only, export_is_json_object, seeded_person_exports_data,
// seeded_person_delete_counts_and_is_idempotent, routes_under_prefix and
// no_personal_data_in_logs. The last also runs jobs_key_not_person as a subtest of its own
// (no_personal_data_in_logs/jobs_key_not_person) on the same seeded run: a dedupe key never
// carries the seeded person's ID.
func Conformance(t *testing.T, m module.Manifest, f module.Factory, provision module.Provision, opts ...Option) {
	t.Helper()
	if f == nil {
		t.Fatal("moduletest.Conformance: the factory is nil")
	}
	o, problems := resolveOptions(m, opts)
	if len(problems) > 0 {
		t.Fatalf("moduletest.Conformance(%q): %s", m.Name, strings.Join(problems, "; "))
	}
	r := &rig{m: m, f: f, provision: provision, opts: o}
	rules := []struct {
		name string
		run  func(*testing.T, *rig)
	}{
		{"manifest_valid", ruleManifestValid},
		{"views_tagged", ruleViewsTagged},
		{"visibility_settings_declared", ruleVisibilitySettingsDeclared},
		{"templates_register", ruleTemplatesRegister},
		{"registers_under_its_name", ruleRegisters},
		{"implements_declared", ruleImplementsDeclared},
		{"demoseed_provider_valid", ruleDemoSeedProviderValid},
		{"rejects_wrong_capability_type", ruleRejectsWrongCapabilityType},
		{"used_capability_absent_builds_and_starts", ruleUsedCapabilityAbsent},
		{"start_read_only", ruleStartReadOnly},
		{"subscribes_every_declared_name", ruleSubscribes},
		{"jobs_register_scoped", ruleJobsScoped},
		{"stop_honors_context", ruleStopHonorsContext},
		{"stop_without_start_is_nil", ruleStopWithoutStart},
		{"export_zero_id", ruleExportZeroID},
		{"delete_zero_id", ruleDeleteZeroID},
		{"export_unknown_person_is_empty", ruleExportUnknown},
		{"delete_unknown_person_is_zero_and_idempotent", ruleDeleteUnknown},
		{"delete_read_only", ruleDeleteReadOnly},
		{"export_is_json_object", ruleExportJSONObject},
		{"seeded_person_exports_data", ruleSeededExports},
		{"seeded_person_delete_counts_and_is_idempotent", ruleSeededDeletes},
		{"routes_under_prefix", ruleRoutesUnderPrefix},
		{"no_personal_data_in_logs", ruleNoPersonalDataInLogs},
	}
	for _, rule := range rules {
		if !t.Run(rule.name, func(t *testing.T) { rule.run(t, r) }) {
			return
		}
	}
}

// rig is what the rules share: the module under test.
type rig struct {
	m         module.Manifest
	f         module.Factory
	provision module.Provision
	opts      options
}

// scenario says how the stubs behave in one build.
type scenario struct {
	readOnly bool
	// wrong, when set, makes the stub for that capability return an empty struct instead of
	// the configured value.
	wrong module.Capability
	// omitUsed leaves every capability in Uses without a provider.
	omitUsed bool
}

// capabilities returns the value each stubbed capability gets under sc. A capability
// missing from the result has no provider.
func (r *rig) capabilities(sc scenario) map[module.Capability]any {
	out := map[module.Capability]any{}
	impl := map[module.Capability]any{}
	for _, c := range r.opts.caps {
		impl[c.name] = c.impl
	}
	for _, c := range r.m.Requires {
		out[c] = impl[c]
	}
	for _, c := range r.m.Uses {
		if v, ok := impl[c]; ok && !sc.omitUsed {
			out[c] = v
		}
	}
	if sc.wrong != "" {
		out[sc.wrong] = struct{}{}
	}
	return out
}

// harness is one built module with the fakes it was given.
type harness struct {
	t       *testing.T
	rig     *rig
	set     *module.Set
	mod     module.Module
	db      *dbtest.Fake
	sig     *signalstest.Fake
	audit   *audittest.Fake  // the module under test's recorder
	notify  *notifytest.Fake // the module under test's notifier
	jobs    *jobstest.Fake   // the module under test's job registrar
	files   *filestest.Fake  // the module under test's file store
	authz   *authztest.Fake  // the module under test's authorizer, when it is the fake
	depts   *departmentstest.Fake
	events  *eventstest.Fake
	people  *peopletest.Fake
	clock   clock.Clock // the module under test's clock
	capture *telemetrytest.Capture
	started bool
	stopped bool
}

func (r *rig) ctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), ruleTimeout)
	t.Cleanup(cancel)
	return ctx
}

// build builds the module (with stubs for what it needs) through Plan.Build, on a database
// that is read-only when readOnly is set. It fails t on any error.
func (r *rig) build(t *testing.T, readOnly bool) *harness {
	t.Helper()
	h, err := r.tryBuild(t, scenario{readOnly: readOnly})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return h
}

// tryBuild is build for a scenario, returning the Build error instead of failing t. A
// Resolve failure or a missing module still fails t. The harness is nil when err is not.
func (r *rig) tryBuild(t *testing.T, sc scenario) (*harness, error) {
	t.Helper()
	ctx := r.ctx(t)
	reg, env := r.registry(t, sc)
	plan, err := reg.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	h := &harness{t: t, rig: r, capture: telemetrytest.New(t)}
	set, err := plan.Build(ctx, noSchemaCheck{}, h.provisioner(sc.readOnly))
	if err != nil {
		return nil, err //nolint:wrapcheck // the rules inspect the kind and text as Build returned them
	}
	for _, b := range set.Modules() {
		if b.Manifest.Name == r.m.Name {
			h.mod = b.Module
		}
	}
	if h.mod == nil {
		t.Fatalf("Build did not return module %q", r.m.Name)
	}
	h.set = set
	t.Cleanup(h.cleanup)
	return h, nil
}

// provisioner is the module.Provision handed to Build: the module's own provision for the
// module under test, fakes for the stubs, and the common scoped services for all.
func (h *harness) provisioner(readOnly bool) module.Provision {
	r := h.rig
	return func(ctx context.Context, m module.Manifest) (module.Deps, error) {
		var d module.Deps
		if m.Name == r.m.Name && r.provision != nil {
			var err error
			if d, err = r.provision(ctx, m); err != nil {
				return module.Deps{}, fmt.Errorf("the module's provision: %w", err)
			}
		}
		d.Logger = h.capture.Logger().With(slog.String("module", m.Name))
		d.Tracer = h.capture.Tracer("module." + m.Name)
		if d.Clock == nil {
			d.Clock = clock.NewFake(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
		}
		fake, ok := d.DB.(*dbtest.Fake)
		if d.DB != nil && !ok {
			h.t.Fatalf("the provision for %q returned a DB of type %T; Conformance needs a *dbtest.Fake (or nil) to make it read-only", m.Name, d.DB)
		}
		if !ok {
			fake = dbtest.NewFake()
		}
		d.DB = fake
		bus, ok := d.Signals.(*signalstest.Fake)
		if d.Signals != nil && !ok {
			h.t.Fatalf("the provision for %q returned Signals of type %T; Conformance needs a *signalstest.Fake (or nil) to read back the subscriptions", m.Name, d.Signals)
		}
		if !ok {
			var err error
			if bus, err = signalstest.NewFake(m.Name, m.Publishes, m.Subscribes); err != nil {
				return module.Deps{}, fmt.Errorf("signalstest.NewFake: %w", err)
			}
		}
		d.Signals = bus
		rec, ok := d.Audit.(*audittest.Fake)
		if d.Audit != nil && !ok {
			h.t.Fatalf("the provision for %q returned an Audit of type %T; Conformance needs an *audittest.Fake (or nil) to scan what was recorded", m.Name, d.Audit)
		}
		if !ok {
			rec = audittest.NewFake(m.Name)
		}
		d.Audit = rec
		if d.Authz == nil {
			d.Authz = authztest.NewFake()
		}
		if d.People == nil {
			d.People = peopletest.NewFake()
		}
		if d.Departments == nil {
			d.Departments = departmentstest.NewFake()
		}
		if d.Events == nil {
			d.Events = eventstest.NewFake()
		}
		if d.Settings == nil {
			d.Settings = settingstest.NewFake(m.Name, m.Settings)
		}
		out, ok := d.Notify.(*notifytest.Fake)
		if d.Notify != nil && !ok {
			h.t.Fatalf("the provision for %q returned a Notify of type %T; Conformance needs a *notifytest.Fake (or nil) to scan what was sent", m.Name, d.Notify)
		}
		if !ok {
			out = notifytest.NewFake(m.Name)
		}
		d.Notify = out
		jf, ok := d.Jobs.(*jobstest.Fake)
		if d.Jobs != nil && !ok {
			h.t.Fatalf("the provision for %q returned a Jobs of type %T; Conformance needs a *jobstest.Fake (or nil) to scan what was registered and enqueued", m.Name, d.Jobs)
		}
		if !ok {
			jf = jobstest.NewFake(m.Name)
		}
		d.Jobs = jf
		ff, ok := d.Files.(*filestest.Fake)
		if d.Files != nil && !ok {
			h.t.Fatalf("the provision for %q returned a Files of type %T; Conformance needs a *filestest.Fake (or nil) to scan what was staged and attached", m.Name, d.Files)
		}
		if !ok {
			ff = filestest.NewFake(m.Name)
		}
		d.Files = ff
		if m.Name == r.m.Name {
			fake.SetReadOnly(readOnly)
			h.db, h.sig, h.audit, h.notify, h.jobs, h.files = fake, bus, rec, out, jf, ff
			// The demo rule registers its request in the standard fakes; a module that supplied
			// its own keeps them, and the rule then relies on what it supplied.
			h.clock = d.Clock
			if f, ok := d.Authz.(*authztest.Fake); ok {
				h.authz = f
			}
			if f, ok := d.Departments.(*departmentstest.Fake); ok {
				h.depts = f
			}
			if f, ok := d.Events.(*eventstest.Fake); ok {
				h.events = f
			}
			if f, ok := d.People.(*peopletest.Fake); ok {
				h.people = f
			}
		}
		return d, nil
	}
}

func (h *harness) start(ctx context.Context) {
	h.t.Helper()
	if err := h.mod.Start(ctx); err != nil {
		h.t.Fatalf("Start: %v", err)
	}
	h.started = true
}

// cleanup stops a module that a rule started and did not stop itself.
func (h *harness) cleanup() {
	if !h.started || h.stopped {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ruleTimeout)
	defer cancel()
	if err := h.mod.Stop(ctx); err != nil {
		h.t.Errorf("Stop at cleanup: %v", err)
	}
}

func (h *harness) stop(ctx context.Context) {
	h.t.Helper()
	h.stopped = true
	if err := h.mod.Stop(ctx); err != nil {
		h.t.Fatalf("Stop: %v", err)
	}
}

// noSchemaCheck is the SchemaChecker of a run with no database.
type noSchemaCheck struct{}

func (noSchemaCheck) CheckSchema(context.Context, string, fs.FS) error { return nil }

// registry returns a fresh registry holding the module under test and the stub modules
// that stand in for what it needs, and the Env that enables them.
func (r *rig) registry(t *testing.T, sc scenario) (*module.Registry, module.Env) {
	t.Helper()
	reg := module.NewRegistry()
	if err := reg.Add(r.m, r.f); err != nil {
		t.Fatalf("Add: %v", err)
	}
	env := module.Env{Enabled: []string{r.m.Name}}
	stubs := map[string]*stub{}
	stubFor := func(name string) *stub {
		s, ok := stubs[name]
		if !ok {
			s = &stub{m: module.Manifest{Name: name}, caps: map[module.Capability]any{}}
			stubs[name] = s
		}
		return s
	}
	caps := r.capabilities(sc)
	for i, c := range slices.Concat(r.m.Requires, r.m.Uses) {
		v, ok := caps[c]
		if !ok {
			continue
		}
		s := stubFor(fmt.Sprintf("stubcap%d", i+1))
		s.m.Provides = append(s.m.Provides, c)
		s.caps[c] = v
	}
	for _, n := range r.m.Subscribes {
		if n.Publisher() == r.m.Name {
			continue
		}
		if probe := (module.Env{CoreSignals: []signals.Name{n}}); probe.Validate() == nil {
			env.CoreSignals = append(env.CoreSignals, n)
			continue
		}
		s := stubFor(n.Publisher())
		s.m.Publishes = append(s.m.Publishes, n)
	}
	for _, p := range r.m.ContributesTo {
		if p.Definer() == r.m.Name {
			continue
		}
		decl := module.ExtensionPointDecl{Name: p, Accepts: func(any) bool { return true }}
		if probe := (module.Env{CoreExtensionPoints: []module.ExtensionPointDecl{decl}}); probe.Validate() == nil {
			env.CoreExtensionPoints = append(env.CoreExtensionPoints, decl)
			continue
		}
		s := stubFor(p.Definer())
		s.m.Defines = append(s.m.Defines, decl)
	}
	names := make([]string, 0, len(stubs))
	for n := range stubs {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		s := stubs[n]
		if err := reg.Add(s.m, s.factory); err != nil {
			t.Fatalf("cannot stand in for module %q, which %q needs: %v", n, r.m.Name, err)
		}
		env.Enabled = append(env.Enabled, n)
	}
	return reg, env
}

// stub is a module that stands in for another module the module under test needs.
type stub struct {
	m    module.Manifest
	caps map[module.Capability]any
}

func (s *stub) factory(context.Context, module.Deps) (module.Module, error) { return s, nil }

func (s *stub) Capabilities() map[module.Capability]any {
	out := make(map[module.Capability]any, len(s.caps))
	maps.Copy(out, s.caps)
	return out
}

func (*stub) Start(context.Context) error { return nil }
func (*stub) Stop(context.Context) error  { return nil }
func (*stub) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (*stub) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

// ---- rules ----

func ruleManifestValid(t *testing.T, r *rig) {
	if err := r.m.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
}

// ruleViewsTagged proves F65 (privacy-tags): every Views type passes privacy.Check, and a
// module with routes lists at least one view unless WithoutViews was given.
func ruleViewsTagged(t *testing.T, r *rig) {
	if r.opts.noViews {
		return
	}
	if err := privacy.Check(r.m.Views, r.m.Groups); err != nil {
		t.Fatalf("Manifest.Views: %v", err)
	}
	if len(r.m.Views) > 0 {
		return
	}
	h := r.build(t, false)
	for _, mr := range h.set.Routes() {
		if mr.Module == r.m.Name && len(mr.Routes) > 0 {
			t.Fatalf("module %q has routes but Manifest.Views is empty: list its response types, or pass moduletest.WithoutViews() if no route writes a JSON body", r.m.Name)
		}
	}
}

// ruleVisibilitySettingsDeclared proves privacy.Settings(Groups) is present in
// Manifest.Settings with equal kind, options and default.
func ruleVisibilitySettingsDeclared(t *testing.T, r *rig) {
	if r.opts.noViews {
		return
	}
	decls, err := privacy.Settings(r.m.Groups)
	if err != nil {
		t.Fatalf("Manifest.Groups: %v", err)
	}
	for _, want := range decls {
		i := slices.IndexFunc(r.m.Settings, func(s module.SettingDecl) bool { return s.Key == want.Key })
		if i < 0 {
			t.Fatalf("Manifest.Settings lacks %q: append privacy.Settings(Groups)", want.Key)
		}
		got := r.m.Settings[i]
		if got.Kind != want.Kind || got.Default != want.Default || !slices.Equal(got.Options, want.Options) {
			t.Fatalf("Manifest.Settings %q differs from privacy.Settings(Groups)", want.Key)
		}
	}
}

// ruleTemplatesRegister proves F58: every file of Manifest.Templates passes the checks
// notify.Register runs, so a bad template fails here and not at start-up.
func ruleTemplatesRegister(t *testing.T, r *rig) {
	if err := notify.CheckTemplates(r.m.Name, r.m.Templates); err != nil {
		t.Fatalf("Manifest.Templates: %v", err)
	}
}

func ruleRegisters(t *testing.T, r *rig) {
	reg, env := r.registry(t, scenario{})
	plan, err := reg.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve with only %q enabled: %v", r.m.Name, err)
	}
	found := false
	for _, m := range plan.Order() {
		found = found || m.Name == r.m.Name
	}
	if !found {
		t.Fatalf("the plan does not contain %q", r.m.Name)
	}
}

func ruleImplementsDeclared(t *testing.T, r *rig) {
	r.build(t, false)
}

// ruleDemoSeedProviderValid proves the module's demo provider (D53, F66): Contributions()
// returns a demoseed.Provider for demoseed.Point, and running it against the module's fakes
// with demotest.NewFakeActors() creates at least one record and returns no Permission error.
// The rule makes the demo actors superusers of the authorizer fake (when it is the standard
// fake) and registers the request's departments, event and people in the other fakes, so a
// Permission error means the provider acts as an actor it does not have, not that a fake was
// empty. The event is registered with dates (Thursday 10:00 to the following Monday 12:00, at
// least 21 days after the module's clock, zone UTC), and the module is started before the
// provider runs, as `bms demo seed` does, and stopped at cleanup. It is skipped without
// WithDemoSeed.
func ruleDemoSeedProviderValid(t *testing.T, r *rig) {
	if r.opts.demoNum == 0 {
		t.Skip("WithDemoSeed was not given")
	}
	h := r.build(t, false)
	c, ok := h.mod.(module.Contributor)
	if !ok {
		t.Fatalf("module %q lists %q in ContributesTo but does not implement module.Contributor", r.m.Name, demoseed.Point)
	}
	v, ok := c.Contributions()[module.ExtensionPoint(demoseed.Point)]
	if !ok {
		t.Fatalf("module %q: Contributions() has no entry for %q", r.m.Name, demoseed.Point)
	}
	prov, ok := v.(demoseed.Provider)
	if !ok {
		t.Fatalf("module %q: the contribution for %q is a %T, not a demoseed.Provider", r.m.Name, demoseed.Point, v)
	}
	actors := demotest.NewFakeActors()
	req := actors.Request()
	if h.authz != nil {
		h.authz.SetActor(func(ctx context.Context) (authz.Actor, bool) {
			p, ok := identity.PersonFrom(ctx)
			return authz.Actor{Person: p}, ok
		})
		for _, p := range req.People {
			h.authz.Superuser(p)
		}
	}
	if h.depts != nil {
		for k, id := range req.Departments {
			h.depts.Add(id, k)
		}
	}
	if h.events != nil {
		// The core always gives the event dates (its step 5), so the fake does too, from the
		// module's clock by the core's own rule; a provider never invents dates (D1.5).
		dates, derr := demo.EventDates(h.clock.Now(), "UTC")
		if derr != nil {
			t.Fatalf("the demo event dates: %v", derr)
		}
		h.events.Add(events.Info{ID: req.Event, Name: "Demo event", Kind: events.KindBurn, Zone: "UTC", Dates: dates})
	}
	if h.people != nil {
		for k, id := range req.People {
			h.people.Add(id, k)
		}
	}
	// bms demo seed runs in an assembly whose modules are started, so a provider may rely on
	// what Start set up (jobs it registered, signals it subscribed to); the harness stops
	// the module at cleanup (D1.6).
	h.start(r.ctx(t))
	res, err := prov.Seed(r.ctx(t), req)
	if errs.Is(err, errs.Permission) {
		t.Fatalf("module %q: the demo provider returned Permission (it acts as an actor without the permission): %v", r.m.Name, err)
	}
	if err != nil {
		t.Fatalf("module %q: the demo provider failed: %v", r.m.Name, err)
	}
	if res.Items < 1 {
		t.Fatalf("module %q: the demo provider created nothing (Result.Items = %d); a contributor must add at least one record", r.m.Name, res.Items)
	}
}

// ruleRejectsWrongCapabilityType proves F32 for this module: for each capability it
// requires or uses in turn, the stub returns an empty struct for that name and the
// configured value for the rest, and Build must fail with Invalid naming the capability.
func ruleRejectsWrongCapabilityType(t *testing.T, r *rig) {
	for _, c := range slices.Concat(r.m.Requires, r.m.Uses) {
		_, err := r.tryBuild(t, scenario{wrong: c})
		if err == nil {
			t.Fatalf("Build succeeded although the provider of capability %q returned the wrong type; assert the type in the Factory and fail with errs.Invalid naming the capability", c)
		}
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("Build with a wrong-typed %q failed with kind %s, want %s: %v", c, errs.KindOf(err), errs.Invalid, err)
		}
		if !strings.Contains(err.Error(), string(c)) {
			t.Fatalf("Build with a wrong-typed %q failed without naming the capability: %v", c, err)
		}
	}
}

// ruleUsedCapabilityAbsent proves the module survives every capability in Uses having no
// provider: Build, Start and Stop all succeed.
func ruleUsedCapabilityAbsent(t *testing.T, r *rig) {
	if len(r.m.Uses) == 0 {
		return
	}
	h, err := r.tryBuild(t, scenario{omitUsed: true})
	if err != nil {
		t.Fatalf("Build without a provider for %v (a used capability is optional; treat NotFound from Links.Capability as \"hide the feature\"): %v", r.m.Uses, err)
	}
	ctx := r.ctx(t)
	h.start(ctx)
	h.stop(ctx)
}

func ruleStartReadOnly(t *testing.T, r *rig) {
	h := r.build(t, true)
	h.start(r.ctx(t))
}

func ruleSubscribes(t *testing.T, r *rig) {
	h := r.build(t, false)
	h.start(r.ctx(t))
	want := slices.Clone(r.m.Subscribes)
	slices.Sort(want)
	got := h.sig.Handlers()
	if !slices.Equal(got, want) {
		t.Fatalf("after Start the module subscribed to %v, but its manifest declares %v", got, want)
	}
}

// ruleJobsScoped proves the module's job names carry its own prefix: every job it registered
// in Start is "<name>.<ident>". The Registrar it was given refuses another owner's name, so a
// module that registered a foreign name has already failed Start; this rule also names the
// jobs it declared when they are wrong in a way the scope check let through.
func ruleJobsScoped(t *testing.T, r *rig) {
	h := r.build(t, false)
	h.start(r.ctx(t))
	for _, d := range h.jobs.Registered() {
		if d.Name.Owner() != r.m.Name {
			t.Errorf("the module registered job %q, whose owner is %q; a module's jobs are named %s.<ident>", d.Name, d.Name.Owner(), r.m.Name)
		}
	}
}

func ruleStopHonorsContext(t *testing.T, r *rig) {
	h := r.build(t, false)
	h.start(r.ctx(t))
	done, cancel := context.WithCancel(t.Context())
	cancel()
	result := make(chan error, 1)
	go func() { result <- h.mod.Stop(done) }()
	select {
	case err := <-result:
		h.stopped = true
		if errs.Is(err, errs.Timeout) || errs.Is(err, errs.Canceled) {
			return
		}
		t.Fatalf("Stop with a done context must fail with Timeout or Canceled, got %v", err)
	case <-time.After(ruleTimeout):
		h.stopped = true
		t.Fatal("Stop with a done context did not return")
	}
}

func ruleStopWithoutStart(t *testing.T, r *rig) {
	h := r.build(t, false)
	if err := h.mod.Stop(r.ctx(t)); err != nil {
		t.Fatalf("Stop without Start: %v", err)
	}
}

func ruleExportZeroID(t *testing.T, r *rig) {
	h := r.build(t, false)
	_, err := h.mod.ExportPerson(r.ctx(t), model.PersonID{})
	wantKind(t, "ExportPerson(zero)", err, errs.Invalid)
}

func ruleDeleteZeroID(t *testing.T, r *rig) {
	h := r.build(t, false)
	_, err := h.mod.DeletePerson(r.ctx(t), model.PersonID{})
	wantKind(t, "DeletePerson(zero)", err, errs.Invalid)
}

func wantKind(t *testing.T, what string, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s succeeded, want %s", what, kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("%s failed with kind %s, want %s: %v", what, errs.KindOf(err), kind, err)
	}
}

func ruleExportUnknown(t *testing.T, r *rig) {
	h := r.build(t, false)
	exp, err := h.mod.ExportPerson(r.ctx(t), person)
	if err != nil {
		t.Fatalf("ExportPerson for a person the store does not hold: %v", err)
	}
	if err := emptyObject(exp.Data); err != nil {
		t.Fatalf("ExportPerson for a person the store does not hold: %v", err)
	}
}

func ruleDeleteUnknown(t *testing.T, r *rig) {
	h := r.build(t, false)
	for i := 1; i <= 2; i++ {
		d, err := h.mod.DeletePerson(r.ctx(t), person)
		if err != nil {
			t.Fatalf("DeletePerson call %d for a person the store does not hold: %v", i, err)
		}
		if d.Deleted != 0 || d.Anonymized != 0 || len(d.Kept) != 0 {
			t.Fatalf("DeletePerson call %d for a person the store does not hold reported %+v, want a zero Deletion", i, d)
		}
	}
}

func ruleDeleteReadOnly(t *testing.T, r *rig) {
	h := r.build(t, true)
	_, err := h.mod.DeletePerson(r.ctx(t), person)
	wantKind(t, "DeletePerson on a read-only copy", err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("DeletePerson on a read-only copy failed without wrapping db.ErrReadOnly: %v", err)
	}
}

func ruleExportJSONObject(t *testing.T, r *rig) {
	h := r.build(t, false)
	exp, err := h.mod.ExportPerson(r.ctx(t), person)
	if err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
	if len(exp.Data) == 0 {
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(exp.Data, &obj); err != nil || obj == nil {
		t.Fatalf("ExportPerson returned Data that is not a JSON object (%d bytes)", len(exp.Data))
	}
}

// seed puts records about subject into the built module when the module opted into a seed.
func (h *harness) seed(ctx context.Context) {
	h.t.Helper()
	if h.rig.opts.seed == nil {
		return
	}
	if err := h.rig.opts.seed(ctx, h.mod, subject); err != nil {
		h.t.Fatalf("Seed: %v", err)
	}
}

func ruleSeededExports(t *testing.T, r *rig) {
	if r.opts.seed == nil {
		return
	}
	h := r.build(t, false)
	ctx := r.ctx(t)
	h.seed(ctx)
	exp, err := h.mod.ExportPerson(ctx, subject.Person)
	if err != nil {
		t.Fatalf("ExportPerson for the seeded person: %v", err)
	}
	obj := decodeObject(exp.Data)
	if obj == nil {
		t.Fatalf("ExportPerson for the seeded person did not return a JSON object (%d bytes); wire the factory to the store the seed writes to", len(exp.Data))
	}
	if err := emptyObject(exp.Data); err == nil {
		t.Fatal("ExportPerson for the seeded person is empty; wire the factory to the store the seed writes to, and make ExportPerson cover every table")
	}
}

func ruleSeededDeletes(t *testing.T, r *rig) {
	if r.opts.seed == nil {
		return
	}
	h := r.build(t, false)
	ctx := r.ctx(t)
	h.seed(ctx)
	d, err := h.mod.DeletePerson(ctx, subject.Person)
	if err != nil {
		t.Fatalf("DeletePerson for the seeded person: %v", err)
	}
	kept := 0
	for _, k := range d.Kept {
		if k.Kind == "" || k.Count < 1 {
			t.Fatalf("DeletePerson reported a Kept %+v, want a non-empty Kind and Count >= 1", k)
		}
		kept += k.Count
	}
	if d.Deleted+d.Anonymized+kept < 1 {
		t.Fatalf("DeletePerson for the seeded person reported %+v, want at least one record deleted, anonymized or kept", d)
	}
	exp, err := h.mod.ExportPerson(ctx, subject.Person)
	if err != nil {
		t.Fatalf("ExportPerson after DeletePerson: %v", err)
	}
	if len(d.Kept) == 0 {
		if err := emptyObjectStrict(exp.Data); err != nil {
			t.Fatalf("ExportPerson after DeletePerson, which kept nothing: %v", err)
		}
	}
	again, err := h.mod.DeletePerson(ctx, subject.Person)
	if err != nil {
		t.Fatalf("second DeletePerson for the seeded person: %v", err)
	}
	if again.Deleted != 0 || again.Anonymized != 0 || len(again.Kept) != 0 {
		t.Fatalf("second DeletePerson reported %+v, want a zero Deletion (deletion is idempotent)", again)
	}
	// files' own DeletePerson, which people runs after the modules, refuses while a file about
	// the person remains (files.md F46): the module must have deleted them all.
	err = h.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		_, err := h.files.DeletePerson(ctx, subject.Person)
		return err //nolint:wrapcheck // the rule reports it
	})
	if err != nil {
		t.Fatalf("after DeletePerson a file about the seeded person remains (files' DeletePerson answers %v); delete the person's files with DeleteByRecord", err)
	}
}

func ruleRoutesUnderPrefix(t *testing.T, r *rig) {
	h := r.build(t, false)
	for _, mr := range h.set.Routes() {
		if mr.Module != r.m.Name {
			continue
		}
		for _, rt := range mr.Routes {
			if !strings.HasPrefix(rt.Pattern, "/") {
				t.Errorf("route %s %s: the pattern must start with /", rt.Method, rt.Pattern)
			}
			if strings.HasPrefix(rt.Pattern, "/api/") {
				t.Errorf("route %s %s: the pattern is relative to /api/%s, not absolute", rt.Method, rt.Pattern, r.m.Name)
			}
		}
	}
}

func ruleNoPersonalDataInLogs(t *testing.T, r *rig) {
	h := r.build(t, false)
	ctx := r.ctx(t)
	h.seed(ctx)
	h.start(ctx)
	// Before the delete below removes them: the files the seed staged and attached.
	if err := filesLeak(h.files); err != nil {
		t.Fatal(err)
	}
	if _, err := h.mod.ExportPerson(ctx, subject.Person); err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
	if _, err := h.mod.DeletePerson(ctx, subject.Person); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	h.stop(ctx)
	for _, marker := range []string{subject.LegalName, subject.Phone, subject.Note, subject.Email, scrubbedEmail} {
		if h.capture.Contains(marker) {
			// The marker is invented, so quoting it leaks nothing.
			t.Fatalf("a log record, span or stdout holds personal data (%q); log IDs, never fields", marker)
		}
	}
	// telemetrytest fails the test at cleanup on any policy violation, which covers a denied
	// key such as email or legal_name.
	if err := auditLeak(h.audit.Recorded()); err != nil {
		t.Fatal(err)
	}
	if err := sentLeak(h.notify.Sent()); err != nil {
		t.Fatal(err)
	}
	if err := enqueuedLeak(h.jobs.Enqueued()); err != nil {
		t.Fatal(err)
	}
	// A dedupe key shows on the /jobs screen and in logs, so a module keys a job by the
	// record's own ID (the sign-up, the shift, the upload), never by a person (jobs decision
	// 22, Q-JOBS13). Same seeded run, so the seed runs once per rule.
	t.Run("jobs_key_not_person", func(t *testing.T) {
		if err := keyPersonLeak(h.jobs.Enqueued()); err != nil {
			t.Error(err)
		}
	})
}

// filesLeak returns an error when the name of a file the module staged or kept carries a
// seed marker or "[email]": a stored name is chosen by the upload, never copied from a record.
func filesLeak(f *filestest.Fake) error {
	markers := []string{subject.LegalName, subject.Phone, subject.Note, subject.Email, scrubbedEmail}
	var names []string
	for _, x := range f.Files() {
		names = append(names, x.Name)
	}
	for _, x := range f.Staged() {
		names = append(names, x.Name)
	}
	for i, name := range names {
		for _, marker := range markers {
			if strings.Contains(name, marker) {
				// The marker is invented, so quoting it leaks nothing.
				return fmt.Errorf("file name %d holds personal data (%q); name an upload by what the client sent, never by a record's fields", i, marker)
			}
		}
	}
	return nil
}

// keyPersonLeak returns an error when a job's dedupe key holds the seeded person's ID, with or
// without its dashes, in any letter case.
func keyPersonLeak(reqs []jobs.Request) error {
	id := strings.ToLower(subject.Person.String())
	forms := []string{id, strings.ReplaceAll(id, "-", "")}
	for i, req := range reqs {
		key := strings.ToLower(req.Key)
		for _, f := range forms {
			if strings.Contains(key, f) {
				return fmt.Errorf("enqueued job %d (%s) has the seeded person's ID in its key; key a job by the record's own ID, never a person's", i, req.Name)
			}
		}
	}
	return nil
}

// enqueuedLeak returns an error when a job a module enqueued carries a seed marker or
// "[email]" in its key or its arguments' keys or identifiers: a job holds record references
// only (jobs decision 5). The arguments are IDs, so this guards a future change of Args.
func enqueuedLeak(reqs []jobs.Request) error {
	markers := []string{subject.LegalName, subject.Phone, subject.Note, subject.Email, scrubbedEmail}
	for i, req := range reqs {
		texts := []string{req.Key}
		for _, k := range slices.Sorted(maps.Keys(req.Args)) {
			texts = append(texts, k, req.Args[k].String())
		}
		for _, text := range texts {
			for _, marker := range markers {
				if strings.Contains(text, marker) {
					// The marker is invented, so quoting it leaks nothing.
					return fmt.Errorf("enqueued job %d (%s) holds personal data (%q) in its key or arguments; pass record references only", i, req.Name, marker)
				}
			}
		}
	}
	return nil
}

// sentLeak returns an error when a message a module queued carries a seed marker or
// "[email]" in a variable or the link: a message holds the recipient's own data at most,
// never a value taken from seeded records (notify decision 10; rule
// no_personal_data_in_logs, F59).
func sentLeak(msgs []notify.Message) error {
	markers := []string{subject.LegalName, subject.Phone, subject.Note, subject.Email, scrubbedEmail}
	for i, m := range msgs {
		texts := []string{m.Link}
		for _, k := range slices.Sorted(maps.Keys(m.Vars)) {
			texts = append(texts, m.Vars[k])
		}
		for _, text := range texts {
			for _, marker := range markers {
				if strings.Contains(text, marker) {
					// The marker is invented, so quoting it leaks nothing.
					return fmt.Errorf("message %d (%s) holds personal data (%q) in its Vars or Link; pass only the recipient's own data, never a value from a record", i, m.Template, marker)
				}
			}
		}
	}
	return nil
}

// auditLeak returns an error when an audit entry's Reason or Fields hold a seed marker or
// "[email]": an entry names fields and IDs, never a value (audit.md; module-contract rule
// no_personal_data_in_logs).
func auditLeak(entries []audit.Entry) error {
	markers := []string{subject.LegalName, subject.Phone, subject.Note, subject.Email, scrubbedEmail}
	for i, e := range entries {
		for _, text := range append([]string{e.Reason}, e.Fields...) {
			for _, marker := range markers {
				if strings.Contains(text, marker) {
					// The marker is invented, so quoting it leaks nothing.
					return fmt.Errorf("audit entry %d (%s) holds personal data (%q) in its Reason or Fields; record field names, never values", i, e.Action, marker)
				}
			}
		}
	}
	return nil
}

// emptyObject reports an error unless data is empty or a JSON object whose values are all
// empty (null, "", 0, false, [] or {}): the module holds nothing about the person. Data
// that is not a JSON object passes here; export_is_json_object reports it.
func emptyObject(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	obj := decodeObject(data)
	for k, v := range obj {
		if !emptyValue(v) {
			return fmt.Errorf("the data holds something under %q, want nothing", k)
		}
	}
	return nil
}

// emptyObjectStrict is emptyObject for data that must be a JSON object when it is not
// empty: anything else, "still here" or [1] or null, is data that was not deleted.
func emptyObjectStrict(data json.RawMessage) error {
	if len(data) > 0 && decodeObject(data) == nil {
		return fmt.Errorf("the data is not a JSON object (%d bytes), so it cannot be empty", len(data))
	}
	return emptyObject(data)
}

// decodeObject returns the JSON object in data, or nil when data is not one.
func decodeObject(data json.RawMessage) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj
}

func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case float64:
		return x == 0
	case bool:
		return !x
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}
