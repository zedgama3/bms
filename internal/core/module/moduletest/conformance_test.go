package moduletest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const helperEnv = "BMS_MODULETEST_MUTANT"

// mutant names one deliberate way a module, or a test's use of Conformance, can break the
// contract.
type mutant string

const (
	good                mutant = "good"
	badManifest         mutant = "bad_manifest"
	exportZeroOK        mutant = "export_zero_ok"
	deleteZeroOK        mutant = "delete_zero_ok"
	deleteIgnoresRO     mutant = "delete_ignores_read_only"
	startWrites         mutant = "start_writes"
	noSubscribe         mutant = "no_subscribe"
	stopIgnoresCtx      mutant = "stop_ignores_ctx"
	stopNeedsStart      mutant = "stop_needs_start"
	exportNotObject     mutant = "export_not_object"
	exportUnknownFails  mutant = "export_unknown_fails"
	deleteUnknownFails  mutant = "delete_unknown_fails"
	routeUnderAPI       mutant = "route_under_api"
	capsNotImplemented  mutant = "caps_not_implemented"
	foreignDB           mutant = "foreign_db"
	seedIgnored         mutant = "seed_ignored"
	deleteKeepsSeeded   mutant = "delete_keeps_seeded"
	deleteReportsZero   mutant = "delete_reports_zero"
	deleteNotIdempotent mutant = "delete_not_idempotent"
	keptEmptyKind       mutant = "kept_empty_kind"
	keptZeroCount       mutant = "kept_zero_count"
	exportJunkAfterDel  mutant = "export_non_object_after_delete"
	preTypedNilCapImpl  mutant = "pre_typed_nil_capability_impl"

	logsSeededName               mutant = "logs_seeded_name"
	logsSeededEmailBenignKey     mutant = "logs_seeded_email_under_benign_key"
	logsSeededEmailDeniedKey     mutant = "logs_seeded_email_under_denied_key"
	logsSeededNote               mutant = "logs_seeded_note"
	spansSeededPhone             mutant = "spans_seeded_phone"
	goodWithoutPersonData        mutant = "good_without_person_data"
	goodKeeps                    mutant = "good_keeps"
	goodRequires                 mutant = "good_requires"
	goodUses                     mutant = "good_uses"
	acceptsAnyCapabilityType     mutant = "accepts_any_capability_type"
	wrongTypeWrongKind           mutant = "wrong_type_wrong_kind"
	wrongTypeUnnamed             mutant = "wrong_type_unnamed"
	needsUsedCapability          mutant = "needs_used_capability"
	usesAcceptsAnyCapabilityType mutant = "uses_accepts_any_capability_type"

	preRequiredNoFake   mutant = "pre_required_without_fake"
	preUndeclaredCap    mutant = "pre_undeclared_capability"
	preNilCapImpl       mutant = "pre_nil_capability_impl"
	foreignNotify       mutant = "foreign_notify"
	badTemplate         mutant = "bad_template"
	templateNoEn        mutant = "template_without_en"
	goodTemplates       mutant = "good_templates"
	logsNotifyVar       mutant = "notify_var_with_seed_marker"
	logsNotifyLink      mutant = "notify_link_with_seed_marker"
	preDuplicateCap     mutant = "pre_duplicate_capability"
	preNeitherSeedNor   mutant = "pre_neither_seed_nor_without"
	preBothSeedAndWith  mutant = "pre_both_seed_and_without"
	preNilSeed          mutant = "pre_nil_seed"
	preZeroOption       mutant = "pre_zero_option"
	preZeroOptionAlone  mutant = "pre_zero_option_with_valid"
	preUsedCapInManifst mutant = "pre_used_capability_declared_ok"
)

type alphaCap interface{ Alpha() string }
type betaCap interface{ Beta() string }

type fakeAlpha struct{}

func (fakeAlpha) Alpha() string { return "alpha" }

type fakeBeta struct{}

func (fakeBeta) Beta() string { return "beta" }

func isRequiresFamily(mu mutant) bool {
	switch mu {
	case goodRequires, acceptsAnyCapabilityType, wrongTypeWrongKind, wrongTypeUnnamed,
		preRequiredNoFake, preNilCapImpl, preTypedNilCapImpl, preDuplicateCap:
		return true
	}
	return false
}

func isUsesFamily(mu mutant) bool {
	switch mu {
	case goodUses, needsUsedCapability, usesAcceptsAnyCapabilityType, preUsedCapInManifst:
		return true
	}
	return false
}

func manifestFor(mu mutant) module.Manifest {
	m := module.Manifest{
		Name:        "alpha",
		Permissions: nil,
		Publishes:   []signals.Name{"alpha.greeted"},
		Subscribes:  []signals.Name{"alpha.greeted"},
	}
	switch {
	case mu == badManifest:
		m.Name = "Bad Name"
	case mu == badTemplate:
		m.Templates = fstest.MapFS{"greeted.en.tmpl": {Data: []byte("no subject header\n\nbody")}}
	case mu == templateNoEn:
		m.Templates = fstest.MapFS{"greeted.es.tmpl": {Data: []byte("Subject: Hola\n\ncuerpo")}}
	case mu == goodTemplates:
		m.Templates = fstest.MapFS{
			"greeted.en.tmpl": {Data: []byte("Subject: Hi from {{.Site.Name}}\nVars: word\n\nYou said {{.Vars.word}}. {{.Link}}")},
			"greeted.es.tmpl": {Data: []byte("Subject: Hola desde {{.Site.Name}}\nVars: word\n\nDijiste {{.Vars.word}}. {{.Link}}")},
		}
	case mu == capsNotImplemented:
		m.Provides = []module.Capability{"greeter"}
	case isRequiresFamily(mu):
		m.Requires = []module.Capability{"alphacap"}
	case isUsesFamily(mu):
		m.Uses = []module.Capability{"betacap"}
	}
	return m
}

// row is the personal data the fake module holds about one person.
type row struct{ legal, email, phone, note string }

// fakeModule is a small well-behaved module over its own in-memory store; mu breaks one
// rule.
type fakeModule struct {
	mu      mutant
	deps    module.Deps
	started bool

	lock sync.Mutex
	rows map[model.PersonID]row
	seen map[model.PersonID]bool
	kept map[model.PersonID]bool
}

func newFactory(mu mutant) module.Factory {
	return func(_ context.Context, deps module.Deps) (module.Module, error) {
		if deps.Logger == nil || deps.Tracer == nil || deps.Clock == nil || deps.DB == nil || deps.Signals == nil || deps.Notify == nil {
			return nil, errs.E("alpha.New", errs.Invalid, os.ErrInvalid)
		}
		if err := checkCapabilities(mu, deps); err != nil {
			return nil, err
		}
		return &fakeModule{
			mu: mu, deps: deps,
			rows: map[model.PersonID]row{}, seen: map[model.PersonID]bool{},
			kept: map[model.PersonID]bool{},
		}, nil
	}
}

func checkCapabilities(mu mutant, deps module.Deps) error {
	switch {
	case isRequiresFamily(mu):
		v, err := deps.Links.Capability("alphacap")
		if err != nil {
			return fmt.Errorf("alpha.New: %w", err)
		}
		switch mu {
		case acceptsAnyCapabilityType:
			return nil
		case wrongTypeWrongKind:
			if _, ok := v.(alphaCap); !ok {
				return errs.E("alpha.New", errs.Conflict, fmt.Errorf("capability %q has the wrong type", "alphacap"))
			}
		case wrongTypeUnnamed:
			if _, ok := v.(alphaCap); !ok {
				return errs.E("alpha.New", errs.Invalid, fmt.Errorf("a capability has the wrong type"))
			}
		default:
			if _, ok := v.(alphaCap); !ok {
				return errs.E("alpha.New", errs.Invalid, fmt.Errorf("capability %q has type %T, want alphaCap", "alphacap", v))
			}
		}
	case isUsesFamily(mu):
		v, err := deps.Links.Capability("betacap")
		if err != nil {
			if errs.Is(err, errs.NotFound) && mu != needsUsedCapability {
				return nil // hide the feature
			}
			return fmt.Errorf("alpha.New: %w", err)
		}
		if mu == usesAcceptsAnyCapabilityType {
			return nil
		}
		if _, ok := v.(betaCap); !ok {
			return errs.E("alpha.New", errs.Invalid, fmt.Errorf("capability %q has type %T, want betaCap", "betacap", v))
		}
	}
	return nil
}

// seed is the moduletest.Seed of the fake: it writes every marker value to the module's
// own store.
func seed(_ context.Context, mod module.Module, s moduletest.Subject) error {
	fm, ok := mod.(*fakeModule)
	if !ok {
		return fmt.Errorf("seed: module is %T, want *fakeModule", mod)
	}
	fm.lock.Lock()
	defer fm.lock.Unlock()
	fm.rows[s.Person] = row{legal: s.LegalName, email: s.Email, phone: s.Phone, note: s.Note}
	fm.seen[s.Person] = true
	return nil
}

func (m *fakeModule) ExportPerson(ctx context.Context, p model.PersonID) (module.Export, error) {
	if p.IsZero() && m.mu != exportZeroOK {
		return module.Export{}, errs.E("alpha.ExportPerson", errs.Invalid, os.ErrInvalid)
	}
	switch m.mu {
	case exportNotObject:
		return module.Export{Data: json.RawMessage(`[1,2]`)}, nil
	case exportUnknownFails:
		if !p.IsZero() {
			return module.Export{}, errs.E("alpha.ExportPerson", errs.NotFound, os.ErrNotExist)
		}
	}
	m.lock.Lock()
	r, ok := m.rows[p]
	seen := m.seen[p]
	m.lock.Unlock()
	if !ok && m.mu == exportJunkAfterDel && seen {
		return module.Export{Data: json.RawMessage(`[1]`)}, nil
	}
	if !ok || m.mu == seedIgnored {
		return module.Export{}, nil
	}
	m.leak(ctx, r)
	data, err := json.Marshal(map[string]string{"legal_name": r.legal, "email": r.email, "phone": r.phone, "note": r.note})
	if err != nil {
		return module.Export{}, errs.E("alpha.ExportPerson", errs.Internal, err)
	}
	return module.Export{Data: data}, nil
}

// leak is where the logging mutants break the privacy rule.
func (m *fakeModule) leak(ctx context.Context, r row) {
	if !m.started {
		return // leak only once running, so the privacy rule (which starts the module) is the one that catches it
	}
	switch m.mu {
	case logsSeededName:
		m.deps.Logger.InfoContext(ctx, "exported", slog.String("detail", r.legal))
	case logsSeededEmailBenignKey:
		m.deps.Logger.InfoContext(ctx, "exported", slog.String("contact", r.email))
	case logsSeededEmailDeniedKey:
		m.deps.Logger.InfoContext(ctx, "exported", slog.String("email", r.email))
	case logsSeededNote:
		m.deps.Logger.InfoContext(ctx, "exported", slog.String("detail", r.note))
	case logsNotifyVar, logsNotifyLink:
		msg := notify.Message{Template: "alpha.greeted", To: []model.PersonID{{1}}, Key: "k"}
		if m.mu == logsNotifyVar {
			msg.Vars = map[string]string{"detail": r.legal}
		} else {
			msg.Link = "/alpha/" + r.note
		}
		if err := m.deps.Notify.Send(ctx, msg); err != nil {
			m.deps.Logger.ErrorContext(ctx, "send failed", slog.String("kind", errs.KindOf(err).String()))
		}
	case spansSeededPhone:
		_, span := m.deps.Tracer.Start(ctx, "alpha.export", telemetry.WithAttrs(telemetry.Text("detail", r.phone)))
		span.End()
	}
}

func (m *fakeModule) DeletePerson(ctx context.Context, p model.PersonID) (module.Deletion, error) {
	if p.IsZero() && m.mu != deleteZeroOK {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.Invalid, os.ErrInvalid)
	}
	if m.mu == deleteUnknownFails && !p.IsZero() {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.NotFound, os.ErrNotExist)
	}
	if m.mu == deleteIgnoresRO {
		return module.Deletion{}, nil
	}
	if _, err := m.deps.DB.Exec(ctx, "DELETE FROM alpha.thing WHERE person = $1", p); err != nil {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.KindOf(err), err)
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	r, ok := m.rows[p]
	if !ok {
		if m.mu == deleteNotIdempotent && m.seen[p] {
			return module.Deletion{Deleted: 1}, nil
		}
		return module.Deletion{}, nil
	}
	m.leak(ctx, r)
	switch m.mu {
	case deleteKeepsSeeded:
		return module.Deletion{Deleted: 1}, nil
	case deleteReportsZero:
		delete(m.rows, p)
		return module.Deletion{}, nil
	case keptEmptyKind:
		return module.Deletion{Kept: []module.Kept{{Kind: "", Count: 1}}}, nil
	case keptZeroCount:
		return module.Deletion{Kept: []module.Kept{{Kind: "ledger", Count: 0}}}, nil
	case goodKeeps:
		if m.kept[p] {
			return module.Deletion{}, nil
		}
		m.kept[p] = true
		return module.Deletion{Kept: []module.Kept{{Kind: "ledger", Count: 1}}}, nil
	}
	delete(m.rows, p)
	return module.Deletion{Deleted: 1}, nil
}

func (m *fakeModule) Start(ctx context.Context) error {
	m.started = true
	if m.mu == startWrites {
		if _, err := m.deps.DB.Exec(ctx, "INSERT INTO alpha.heartbeat DEFAULT VALUES"); err != nil {
			return errs.E("alpha.Start", errs.KindOf(err), err)
		}
	}
	if m.mu == noSubscribe {
		return nil
	}
	err := m.deps.Signals.Subscribe("alpha.greeted", func(context.Context, signals.Signal) error { return nil })
	if err != nil {
		return errs.E("alpha.Start", errs.KindOf(err), err)
	}
	return nil
}

func (m *fakeModule) Stop(ctx context.Context) error {
	if m.mu == stopNeedsStart && !m.started {
		return errs.E("alpha.Stop", errs.Internal, os.ErrInvalid)
	}
	if m.mu == stopIgnoresCtx {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return errs.E("alpha.Stop", errs.KindOf(err), err)
	}
	return nil
}

// Routes and OpenAPI make the module a RouteProvider.
func (m *fakeModule) Routes() []module.Route {
	p := "/greeting"
	if m.mu == routeUnderAPI {
		p = "/api/alpha/greeting"
	}
	return []module.Route{{Method: "GET", Pattern: p, Handler: http.NotFoundHandler()}}
}

func (m *fakeModule) OpenAPI() []byte { return []byte(`{"paths":{}}`) }

// provision hands out fresh fakes, as a module's own test would.
func provisionFor(mu mutant) module.Provision {
	return func(_ context.Context, man module.Manifest) (module.Deps, error) {
		sig, err := signalstest.NewFake(man.Name, man.Publishes, man.Subscribes)
		if err != nil {
			return module.Deps{}, err //nolint:wrapcheck // test helper; the caller reports it
		}
		var d db.DB = dbtest.NewFake()
		if mu == foreignDB {
			d = db.None()
		}
		deps := module.Deps{DB: d, Signals: sig}
		if mu == foreignNotify {
			deps.Notify = foreignNotifier{}
		}
		return deps, nil
	}
}

// foreignNotifier is a Notifier that is not a notifytest.Fake.
type foreignNotifier struct{}

func (foreignNotifier) Send(context.Context, notify.Message) error { return nil }

func (foreignNotifier) Configured(context.Context) (bool, error) { return true, nil }

// optsFor returns the Conformance options a careful test author would pass for mu, or the
// deliberately wrong ones for the precondition scenarios.
func optsFor(mu mutant) []moduletest.Option {
	withSeed := moduletest.WithSeed(seed)
	alpha := moduletest.WithCapability("alphacap", fakeAlpha{})
	beta := moduletest.WithCapability("betacap", fakeBeta{})
	switch mu {
	case goodWithoutPersonData:
		return []moduletest.Option{moduletest.WithoutPersonData()}
	case goodRequires, acceptsAnyCapabilityType, wrongTypeWrongKind, wrongTypeUnnamed:
		return []moduletest.Option{alpha, withSeed}
	case goodUses, needsUsedCapability, usesAcceptsAnyCapabilityType, preUsedCapInManifst:
		return []moduletest.Option{beta, withSeed}
	case preRequiredNoFake:
		return []moduletest.Option{withSeed}
	case preUndeclaredCap:
		return []moduletest.Option{moduletest.WithCapability("gammacap", fakeAlpha{}), withSeed}
	case preNilCapImpl:
		return []moduletest.Option{moduletest.WithCapability("alphacap", nil), withSeed}
	case preTypedNilCapImpl:
		return []moduletest.Option{moduletest.WithCapability("alphacap", (*fakeAlpha)(nil)), withSeed}
	case preDuplicateCap:
		return []moduletest.Option{alpha, alpha, withSeed}
	case preNeitherSeedNor:
		return nil
	case preBothSeedAndWith:
		return []moduletest.Option{withSeed, moduletest.WithoutPersonData()}
	case preNilSeed:
		return []moduletest.Option{moduletest.WithSeed(nil)}
	case preZeroOption:
		return []moduletest.Option{{}}
	case preZeroOptionAlone:
		return []moduletest.Option{withSeed, {}}
	}
	return []moduletest.Option{withSeed}
}

// TestConformanceHelper is the body that the parent tests re-run in a child process, so a
// failing Conformance does not fail the parent. It does nothing unless the env var is set.
func TestConformanceHelper(t *testing.T) {
	name := os.Getenv(helperEnv)
	if name == "" {
		t.Skip("child-process helper")
	}
	mu := mutant(name)
	moduletest.Conformance(t, manifestFor(mu), newFactory(mu), provisionFor(mu), optsFor(mu)...)
}

func TestConformance_WellBehavedModule_Passes(t *testing.T) {
	moduletest.Conformance(t, manifestFor(good), newFactory(good), provisionFor(good), optsFor(good)...)
}

func TestConformance_WithoutPersonData_Passes(t *testing.T) {
	moduletest.Conformance(t, manifestFor(goodWithoutPersonData), newFactory(goodWithoutPersonData),
		provisionFor(goodWithoutPersonData), optsFor(goodWithoutPersonData)...)
}

func TestConformance_KeptRecords_Passes(t *testing.T) {
	moduletest.Conformance(t, manifestFor(goodKeeps), newFactory(goodKeeps), provisionFor(goodKeeps), optsFor(goodKeeps)...)
}

func TestConformance_RequiredCapabilityFaked_Passes(t *testing.T) {
	moduletest.Conformance(t, manifestFor(goodRequires), newFactory(goodRequires), provisionFor(goodRequires), optsFor(goodRequires)...)
}

func TestConformance_UsedCapabilityPresentAndAbsent_Passes(t *testing.T) {
	m := manifestFor(goodUses)
	t.Run("present", func(t *testing.T) {
		moduletest.Conformance(t, m, newFactory(goodUses), provisionFor(goodUses), optsFor(goodUses)...)
	})
	t.Run("absent", func(t *testing.T) {
		moduletest.Conformance(t, m, newFactory(goodUses), provisionFor(goodUses), moduletest.WithSeed(seed))
	})
}

// A used capability that is given is present for the module, one that is not given is absent.
func TestConformance_UsedCapability_PresenceFollowsOption(t *testing.T) {
	m := manifestFor(goodUses)
	var mu sync.Mutex
	var present, absent int
	f := func(ctx context.Context, deps module.Deps) (module.Module, error) {
		_, err := deps.Links.Capability("betacap")
		mu.Lock()
		if err == nil {
			present++
		} else if errs.Is(err, errs.NotFound) {
			absent++
		}
		mu.Unlock()
		return newFactory(goodUses)(ctx, deps)
	}
	t.Run("given", func(t *testing.T) {
		moduletest.Conformance(t, m, f, provisionFor(good), moduletest.WithCapability("betacap", fakeBeta{}), moduletest.WithSeed(seed))
	})
	mu.Lock()
	gotPresent, gotAbsent := present, absent
	mu.Unlock()
	if gotPresent == 0 {
		t.Errorf("the factory never saw betacap although WithCapability gave it")
	}
	// Even when given, the rule used_capability_absent_builds_and_starts builds without it.
	if gotAbsent == 0 {
		t.Errorf("used_capability_absent_builds_and_starts never built the module without betacap")
	}
}

func TestConformance_SeedRunsPerRuleOnFreshModule(t *testing.T) {
	var mu sync.Mutex
	var mods []module.Module
	counting := func(ctx context.Context, mod module.Module, s moduletest.Subject) error {
		mu.Lock()
		mods = append(mods, mod)
		mu.Unlock()
		return seed(ctx, mod, s)
	}
	moduletest.Conformance(t, manifestFor(good), newFactory(good), provisionFor(good), moduletest.WithSeed(counting))
	mu.Lock()
	defer mu.Unlock()
	if len(mods) != 3 {
		t.Fatalf("seed ran %d times, want 3 (one per seeded rule)", len(mods))
	}
	for i := range mods {
		for j := i + 1; j < len(mods); j++ {
			if mods[i] == mods[j] {
				t.Errorf("seed %d and %d got the same module value; each must get a fresh one", i, j)
			}
		}
	}
}

func TestConformance_SeedSubject_IsFilledAndDistinct(t *testing.T) {
	var subjects []moduletest.Subject
	var mu sync.Mutex
	probe := func(ctx context.Context, mod module.Module, s moduletest.Subject) error {
		mu.Lock()
		subjects = append(subjects, s)
		mu.Unlock()
		return seed(ctx, mod, s)
	}
	moduletest.Conformance(t, manifestFor(good), newFactory(good), provisionFor(good), moduletest.WithSeed(probe))
	mu.Lock()
	defer mu.Unlock()
	if len(subjects) == 0 {
		t.Fatal("seed never ran")
	}
	for _, s := range subjects {
		if s.Person.IsZero() {
			t.Error("Subject.Person is zero")
		}
		if s.LegalName == "" || s.Email == "" || s.Phone == "" || s.Note == "" {
			t.Errorf("Subject has an empty marker: %+v", s)
		}
		if !strings.Contains(s.Email, "@") || !strings.HasSuffix(s.Email, ".invalid") {
			t.Errorf("Subject.Email is not an invented .invalid address")
		}
	}
}

// runChild runs the helper test for mu in a fresh process and returns its output and whether
// it failed.
func runChild(t *testing.T, mu mutant) (string, bool) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestConformanceHelper$", "-test.v") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), helperEnv+"="+string(mu))
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr != nil
}

var allRules = []string{
	"manifest_valid", "templates_register", "registers_under_its_name", "implements_declared",
	"rejects_wrong_capability_type", "used_capability_absent_builds_and_starts",
	"start_read_only", "subscribes_every_declared_name", "stop_honors_context",
	"stop_without_start_is_nil", "export_zero_id", "delete_zero_id",
	"export_unknown_person_is_empty", "delete_unknown_person_is_zero_and_idempotent",
	"delete_read_only", "export_is_json_object", "seeded_person_exports_data",
	"seeded_person_delete_counts_and_is_idempotent", "routes_under_prefix",
	"no_personal_data_in_logs",
}

func TestConformance_RunsEveryNamedRule(t *testing.T) {
	out, failed := runChild(t, good)
	if failed {
		t.Fatalf("a well-behaved module failed Conformance:\n%s", out)
	}
	for _, rule := range allRules {
		if !regexp.MustCompile(`--- PASS: .*/` + rule + `\b`).MatchString(out) {
			t.Errorf("no passing subtest named %q in:\n%s", rule, out)
		}
	}
}

func TestConformance_BrokenModule_FailsTheNamedRule(t *testing.T) {
	tests := []struct {
		mu   mutant
		rule string
	}{
		{badManifest, "manifest_valid"},
		// F58
		{badTemplate, "templates_register"},
		{templateNoEn, "templates_register"},
		// F59
		{logsNotifyVar, "no_personal_data_in_logs"},
		{logsNotifyLink, "no_personal_data_in_logs"},
		{exportZeroOK, "export_zero_id"},
		{deleteZeroOK, "delete_zero_id"},
		{deleteIgnoresRO, "delete_read_only"},
		{startWrites, "start_read_only"},
		{noSubscribe, "subscribes_every_declared_name"},
		{stopIgnoresCtx, "stop_honors_context"},
		{stopNeedsStart, "stop_without_start_is_nil"},
		{exportNotObject, "export_is_json_object"},
		{exportUnknownFails, "export_unknown_person_is_empty"},
		{deleteUnknownFails, "delete_unknown_person_is_zero_and_idempotent"},
		{routeUnderAPI, "routes_under_prefix"},
		{capsNotImplemented, "implements_declared"},
		// F53
		{acceptsAnyCapabilityType, "rejects_wrong_capability_type"},
		{wrongTypeWrongKind, "rejects_wrong_capability_type"},
		{wrongTypeUnnamed, "rejects_wrong_capability_type"},
		{usesAcceptsAnyCapabilityType, "rejects_wrong_capability_type"},
		// F54
		{needsUsedCapability, "used_capability_absent_builds_and_starts"},
		// F55
		{seedIgnored, "seeded_person_exports_data"},
		// F56
		{deleteKeepsSeeded, "seeded_person_delete_counts_and_is_idempotent"},
		{deleteReportsZero, "seeded_person_delete_counts_and_is_idempotent"},
		{deleteNotIdempotent, "seeded_person_delete_counts_and_is_idempotent"},
		{keptEmptyKind, "seeded_person_delete_counts_and_is_idempotent"},
		{keptZeroCount, "seeded_person_delete_counts_and_is_idempotent"},
		{exportJunkAfterDel, "seeded_person_delete_counts_and_is_idempotent"},
		// F57
		{logsSeededName, "no_personal_data_in_logs"},
		{logsSeededEmailBenignKey, "no_personal_data_in_logs"},
		{logsSeededEmailDeniedKey, "no_personal_data_in_logs"},
		{logsSeededNote, "no_personal_data_in_logs"},
		{spansSeededPhone, "no_personal_data_in_logs"},
	}
	for _, tc := range tests {
		t.Run(string(tc.mu), func(t *testing.T) {
			out, failed := runChild(t, tc.mu)
			if !failed {
				t.Fatalf("Conformance passed a module that breaks %s:\n%s", tc.rule, out)
			}
			if !regexp.MustCompile(`--- FAIL: .*/` + tc.rule + `\b`).MatchString(out) {
				t.Errorf("expected subtest %q to fail by name; output:\n%s", tc.rule, out)
			}
		})
	}
}

// The first rule a leaking module trips is the privacy rule, not an earlier one: the
// mutants above must reach no_personal_data_in_logs with every other rule passing.
func TestConformance_LeakMutants_PassEveryOtherRule(t *testing.T) {
	for _, mu := range []mutant{logsNotifyVar, logsNotifyLink, logsSeededName, logsSeededEmailBenignKey, logsSeededEmailDeniedKey, logsSeededNote, spansSeededPhone} {
		t.Run(string(mu), func(t *testing.T) {
			out, _ := runChild(t, mu)
			for _, rule := range allRules {
				if rule == "no_personal_data_in_logs" {
					continue
				}
				if !regexp.MustCompile(`--- PASS: .*/` + rule + `\b`).MatchString(out) {
					t.Errorf("rule %q did not pass for a module that only leaks in logs:\n%s", rule, out)
				}
			}
		})
	}
}

// preconditionRan reports whether any rule subtest started in the child's output.
func preconditionRan(out string) bool {
	return strings.Contains(out, "=== RUN   TestConformanceHelper/")
}

func TestConformance_RequiredCapabilityWithoutFake_FailsEarly(t *testing.T) { // F48
	out, failed := runChild(t, preRequiredNoFake)
	if !failed {
		t.Fatalf("Conformance accepted a Requires capability with no WithCapability:\n%s", out)
	}
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
	if !strings.Contains(out, "alphacap") || !strings.Contains(out, "alpha") {
		t.Errorf("failure does not name the module and the capability:\n%s", out)
	}
}

func TestConformance_UndeclaredCapabilityOption_FailsEarly(t *testing.T) { // F49
	out, failed := runChild(t, preUndeclaredCap)
	if !failed {
		t.Fatalf("Conformance accepted WithCapability for an undeclared capability:\n%s", out)
	}
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
	if !strings.Contains(out, "gammacap") {
		t.Errorf("failure does not name the capability:\n%s", out)
	}
}

func TestConformance_NilCapabilityImpl_FailsEarly(t *testing.T) { // F50
	out, failed := runChild(t, preNilCapImpl)
	if !failed {
		t.Fatalf("Conformance accepted a nil capability impl:\n%s", out)
	}
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
	if !strings.Contains(out, "alphacap") {
		t.Errorf("failure does not name the capability:\n%s", out)
	}
}

func TestConformance_TypedNilCapabilityImpl_FailsEarly(t *testing.T) { // F50
	out, failed := runChild(t, preTypedNilCapImpl)
	if !failed {
		t.Fatalf("Conformance accepted a typed-nil capability impl:\n%s", out)
	}
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
	if !strings.Contains(out, "alphacap") {
		t.Errorf("failure does not name the capability:\n%s", out)
	}
}

func TestConformance_DuplicateCapabilityOption_FailsEarly(t *testing.T) { // F51
	out, failed := runChild(t, preDuplicateCap)
	if !failed {
		t.Fatalf("Conformance accepted the same capability twice:\n%s", out)
	}
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
	if !strings.Contains(out, "alphacap") {
		t.Errorf("failure does not name the capability:\n%s", out)
	}
}

func TestConformance_SeedOrWithoutPersonDataRequired_FailsEarly(t *testing.T) { // F52
	tests := []struct {
		name string
		mu   mutant
	}{
		{"neither", preNeitherSeedNor},
		{"both", preBothSeedAndWith},
		{"nil_seed", preNilSeed},
		{"zero_option", preZeroOption},
		{"zero_option_next_to_a_valid_one", preZeroOptionAlone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, failed := runChild(t, tc.mu)
			if !failed {
				t.Fatalf("Conformance accepted an invalid option set:\n%s", out)
			}
			if preconditionRan(out) {
				t.Errorf("a rule ran before the precondition failed:\n%s", out)
			}
		})
	}
	t.Run("names_the_option", func(t *testing.T) {
		out, _ := runChild(t, preNeitherSeedNor)
		if !strings.Contains(out, "WithSeed") || !strings.Contains(out, "WithoutPersonData") {
			t.Errorf("failure does not name the options:\n%s", out)
		}
	})
}

func TestConformance_ForeignDBInDeps_FailsNamingTheModule(t *testing.T) {
	out, failed := runChild(t, foreignDB)
	if !failed {
		t.Fatalf("Conformance accepted a Deps.DB that is not a dbtest fake:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("failure does not name the module:\n%s", out)
	}
}

func TestConformance_ForeignNotifyInDeps_FailsNamingTheModule(t *testing.T) {
	out, failed := runChild(t, foreignNotify)
	if !failed {
		t.Fatalf("Conformance accepted a Deps.Notify that is not a notifytest fake:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("failure does not name the module:\n%s", out)
	}
}

func TestConformance_ModuleWithTemplates_Passes(t *testing.T) {
	moduletest.Conformance(t, manifestFor(goodTemplates), newFactory(goodTemplates), provisionFor(goodTemplates), optsFor(goodTemplates)...)
}
