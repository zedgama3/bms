package hello_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/greeter"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

// ---- Conformance ----

func TestConformance(t *testing.T) {
	var (
		mu     sync.Mutex
		stores = map[*hello.Module]*hello.FakeStore{}
	)
	factory := func(_ context.Context, deps module.Deps) (module.Module, error) {
		st := hello.NewFakeStore()
		m, err := hello.NewWithSeams(deps, hello.Seams{
			Store:    st,
			Actor:    actorIs(alice),
			Allow:    hello.AllowAll{},
			Greeting: wordIs("howdy"),
		})
		if err != nil {
			return nil, fmt.Errorf("NewWithSeams: %w", err)
		}
		mu.Lock()
		stores[m] = st
		mu.Unlock()
		return m, nil
	}
	provision := func(_ context.Context, m module.Manifest) (module.Deps, error) {
		sig, err := signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)
		if err != nil {
			return module.Deps{}, fmt.Errorf("signalstest.NewFake: %w", err)
		}
		return module.Deps{Signals: sig}, nil
	}
	seed := func(ctx context.Context, mod module.Module, subject moduletest.Subject) error {
		m, ok := mod.(*hello.Module)
		if !ok {
			return errors.New("the module under test is not a *hello.Module")
		}
		mu.Lock()
		st := stores[m]
		mu.Unlock()
		if st == nil {
			return errors.New("no store for the module under test")
		}
		return st.Insert(ctx, greeting.Greeting{
			ID:        model.NewID[greeting.ID](),
			Person:    subject.Person,
			Text:      "howdy",
			CreatedAt: epoch,
		})
	}
	moduletest.Conformance(t, hello.Manifest, factory, provision, moduletest.WithSeed(seed))
}

// ---- Manifest and settings ----

// F31
func TestManifest_Validates(t *testing.T) {
	if err := hello.Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
	if hello.Manifest.Name != "hello" {
		t.Errorf("Name = %q", hello.Manifest.Name)
	}
}

func TestManifest_DeclaresSpecifiedNames(t *testing.T) {
	m := hello.Manifest
	if hello.PermissionGreetingsRead != "hello.greetings.read" || hello.PermissionGreetingsWrite != "hello.greetings.write" {
		t.Errorf("permission names drifted: %q %q", hello.PermissionGreetingsRead, hello.PermissionGreetingsWrite)
	}
	if hello.SignalGreeted != "hello.greeted" || hello.PayloadPerson != "person" || hello.PointBanner != "hello.banner" {
		t.Errorf("signal, payload or point name drifted")
	}
	if hello.SettingGreeting != "greeting" || hello.DefaultGreeting != "hello" || hello.MaxGreetingBytes != 64 {
		t.Errorf("setting constants drifted")
	}
	want := map[authz.Permission]bool{hello.PermissionGreetingsRead: true, hello.PermissionGreetingsWrite: true}
	if len(m.Permissions) != len(want) {
		t.Fatalf("Permissions = %+v, want exactly the two", m.Permissions)
	}
	for _, p := range m.Permissions {
		if !want[p.Name] {
			t.Errorf("unexpected permission %q", p.Name)
		}
		if p.Scope != authz.ScopeSite || p.Reaches != model.SensitivityInternal {
			t.Errorf("%s: scope %v reaches %v, want site/internal", p.Name, p.Scope, p.Reaches)
		}
	}
	if len(m.Publishes) != 1 || m.Publishes[0] != hello.SignalGreeted || len(m.Subscribes) != 1 || m.Subscribes[0] != hello.SignalGreeted {
		t.Errorf("signals = %v / %v", m.Publishes, m.Subscribes)
	}
	if len(m.Provides) != 1 || m.Provides[0] != greeter.Name || len(m.Requires) != 0 || len(m.Uses) != 0 {
		t.Errorf("capabilities = %v %v %v", m.Provides, m.Requires, m.Uses)
	}
	if len(m.Defines) != 1 || m.Defines[0].Name != hello.PointBanner || len(m.ContributesTo) != 1 || m.ContributesTo[0] != hello.PointBanner {
		t.Errorf("extension points = %+v / %v", m.Defines, m.ContributesTo)
	}
	if m.Migrations == nil {
		t.Errorf("Migrations is nil")
	}
}

func TestManifest_BannerPointAcceptsOnlyBanners(t *testing.T) {
	accepts := hello.Manifest.Defines[0].Accepts
	if accepts == nil {
		t.Fatal("Accepts is nil")
	}
	if !accepts(greeter.Banner{Key: "module.x.banner.y"}) {
		t.Errorf("Accepts(greeter.Banner) = false")
	}
	for _, v := range []any{nil, "module.x.banner.y", &greeter.Banner{}, 3, struct{ Key string }{}} {
		if accepts(v) {
			t.Errorf("Accepts(%T) = true, want false", v)
		}
	}
}

func TestManifest_Migrations_HoldTwoFiles(t *testing.T) {
	v, err := db.Version(hello.Manifest.Migrations)
	if err != nil {
		t.Fatalf("db.Version: %v", err)
	}
	if v != 2 {
		t.Errorf("Version = %d, want 2", v)
	}
}

func TestSetting_Greeting_DeclaredAsSpecified(t *testing.T) {
	if len(hello.Manifest.Settings) != 1 {
		t.Fatalf("Settings = %+v", hello.Manifest.Settings)
	}
	s := hello.Manifest.Settings[0]
	if s.Key != "greeting" || s.Kind != module.SettingText || s.Default != "hello" || s.Check == nil {
		t.Errorf("setting = %+v", s)
	}
	if err := s.Check(s.Default); err != nil {
		t.Errorf("the default fails its own check: %v", err)
	}
}

func TestSetting_Greeting_Check(t *testing.T) {
	check := hello.Manifest.Settings[0].Check
	bytes := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"default", "hello", true},
		{"one byte", "a", true},
		{"max bytes", bytes(64), true},
		{"over max by one", bytes(65), false},
		{"empty", "", false},
		{"only spaces", "   ", false},
		{"spaces trimmed to fit", "  " + bytes(64) + "  ", true},
		{"spaces trimmed to empty tab", "\t \t", false},
		{"multibyte at the limit", strings.Repeat("é", 32), true},
		{"multibyte over the limit", strings.Repeat("é", 33), false},
		{"nul", "he\x00llo", false},
		{"newline inside", "he\nllo", false},
		{"tab inside", "he\tllo", false},
		{"escape", "he\x1bllo", false},
		{"invalid utf-8", "he\xffllo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := check(tc.value)
			if tc.ok {
				if err != nil {
					t.Fatalf("Check(%q) = %v, want nil", tc.value, err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Check(%q) = %v, want Invalid", tc.value, err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				if fe.Field == "hello.greeting" {
					found = true
				}
			}
			if !found {
				t.Errorf("Check(%q) has no field error on hello.greeting: %v", tc.value, validate.Fields(err))
			}
		})
	}
}

func TestPlan_Settings_CarriesGreeting(t *testing.T) {
	f := newFx(t, cfg{})
	var found bool
	for _, ms := range f.plan.Settings() {
		if ms.Module != "hello" {
			continue
		}
		for _, s := range ms.Settings {
			if s.Key == hello.SettingGreeting && s.Default == hello.DefaultGreeting {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("Plan.Settings() lacks hello.greeting: %+v", f.plan.Settings())
	}
	perms := 0
	for _, p := range f.plan.Permissions() {
		if strings.HasPrefix(string(p.Name), "hello.") {
			perms++
		}
	}
	if perms != 2 {
		t.Errorf("Plan.Permissions() carries %d hello permissions, want 2", perms)
	}
}

// ---- capability and contribution ----

func TestCapabilities_Greeter_ReturnsGreeting(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Greeting: wordIs("ahoy")}})
	caps := f.mod.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("Capabilities = %v, want only %q", caps, greeter.Name)
	}
	g, ok := caps[greeter.Name].(greeter.Greeter)
	if !ok {
		t.Fatalf("capability %q is %T, not a greeter.Greeter", greeter.Name, caps[greeter.Name])
	}
	got, err := g.Greeting(ctxT(t))
	if err != nil || got != "ahoy" {
		t.Errorf("Greeting = %q, %v; want ahoy", got, err)
	}
}

// F26
func TestGreeter_SourceFails_ReturnsKind(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{seams: hello.Seams{Greeting: wordFails(errs.E("test", kind, errors.New("down")))}})
			g := greeterOf(t, f.mod)
			if _, err := g.Greeting(ctxT(t)); !errs.Is(err, kind) {
				t.Fatalf("err = %v, want %v", err, kind)
			}
		})
	}
}

func TestGreeter_CancelledContextWithDefaultSource_ReturnsCanceled(t *testing.T) {
	f := newFx(t, cfg{declaredDefault: true})
	g := greeterOf(t, f.mod)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Greeting(ctx); !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
}

func TestGreeter_NeedsNoActorAndNoPermission(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: hello.DenyAll{}}})
	g := greeterOf(t, f.mod)
	if _, err := g.Greeting(ctxT(t)); err != nil {
		t.Fatalf("Greeting: %v", err)
	}
}

func TestContributions_Banner_HasOwnKey(t *testing.T) {
	f := newFx(t, cfg{})
	c := f.mod.Contributions()
	if len(c) != 1 {
		t.Fatalf("Contributions = %v", c)
	}
	b, ok := c[hello.PointBanner].(greeter.Banner)
	if !ok {
		t.Fatalf("contribution is %T, want greeter.Banner", c[hello.PointBanner])
	}
	if !strings.HasPrefix(b.Key, "module.hello.banner.") || b.Key == "module.hello.banner." {
		t.Errorf("Key = %q, want module.hello.banner.<ident>", b.Key)
	}
}

func greeterOf(t *testing.T, m *hello.Module) greeter.Greeter {
	t.Helper()
	g, ok := m.Capabilities()[greeter.Name].(greeter.Greeter)
	if !ok {
		t.Fatalf("capability %q is not a greeter.Greeter", greeter.Name)
	}
	return g
}

// ---- person data ----

// F21
func TestExportPerson_ZeroID_ReturnsInvalid(t *testing.T) {
	f := newFx(t, cfg{})
	if _, err := f.mod.ExportPerson(ctxT(t), model.PersonID{}); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
}

func TestExportPerson_ReturnsGreetingsJSON(t *testing.T) {
	f := newFx(t, cfg{})
	seedRows(t, f.store, alice, 2)
	seedRows(t, f.store, bob, 3)
	ex, err := f.mod.ExportPerson(ctxT(t), alice)
	if err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
	obj := mustJSONObject(t, ex.Data)
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(obj["greetings"], &rows); err != nil {
		t.Fatalf("greetings is not an array of objects: %v (%s)", err, ex.Data)
	}
	if len(rows) != 2 {
		t.Fatalf("greetings = %d rows, want 2 (only alice's)", len(rows))
	}
	for _, r := range rows {
		for _, k := range []string{"id", "greeting", "created_at"} {
			if _, ok := r[k]; !ok {
				t.Errorf("row lacks %q: %v", k, r)
			}
		}
		for _, k := range []string{"person", "person_id", "Person"} {
			if _, ok := r[k]; ok {
				t.Errorf("row carries %q; the person stays off the wire", k)
			}
		}
	}
	for _, k := range []string{"person", "person_id"} {
		if _, ok := obj[k]; ok {
			t.Errorf("export carries %q", k)
		}
	}
	if strings.Contains(string(ex.Data), bob.String()) || strings.Contains(string(ex.Data), alice.String()) {
		t.Errorf("export contains a person ID")
	}
	if sp := f.spansNamed("hello.export-person"); len(sp) != 1 {
		t.Errorf("hello.export-person spans = %d, want 1", len(sp))
	}
}

func TestExportPerson_UnknownPerson_ReturnsEmptyArray(t *testing.T) {
	f := newFx(t, cfg{})
	ex, err := f.mod.ExportPerson(ctxT(t), bob)
	if err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
	obj := mustJSONObject(t, ex.Data)
	if got := strings.TrimSpace(string(obj["greetings"])); got != "[]" {
		t.Errorf("greetings = %s, want [] (never null)", got)
	}
}

// F22
func TestExportPerson_StoreFails_ReturnsKind(t *testing.T) {
	f := newFx(t, cfg{})
	f.store.Fail("ListByPerson", errs.E("test", errs.Unavailable, errors.New("boom")))
	if _, err := f.mod.ExportPerson(ctxT(t), alice); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v, want Unavailable", err)
	}
}

func TestExportPerson_ReadOnlyDatabase_Works(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	seedRows(t, f.store, alice, 1)
	if _, err := f.mod.ExportPerson(ctxT(t), alice); err != nil {
		t.Fatalf("ExportPerson on a read-only copy: %v", err)
	}
}

func TestExportPerson_NeedsNoActor(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: hello.DenyAll{}}})
	seedRows(t, f.store, alice, 1)
	if _, err := f.mod.ExportPerson(ctxT(t), alice); err != nil {
		t.Fatalf("ExportPerson runs as the system for people: %v", err)
	}
}

// F23
func TestDeletePerson_ZeroID_ReturnsInvalid(t *testing.T) {
	f := newFx(t, cfg{})
	seedRows(t, f.store, alice, 1)
	if _, err := f.mod.DeletePerson(ctxT(t), model.PersonID{}); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if len(f.store.Rows()) != 1 {
		t.Errorf("a rejected delete removed rows")
	}
}

func TestDeletePerson_RemovesOnlyThatPersonAndIsIdempotent(t *testing.T) {
	f := newFx(t, cfg{})
	seedRows(t, f.store, alice, 3)
	seedRows(t, f.store, bob, 2)
	got, err := f.mod.DeletePerson(ctxT(t), alice)
	if err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if got.Deleted != 3 || got.Anonymized != 0 || len(got.Kept) != 0 {
		t.Errorf("Deletion = %+v, want 3 deleted, nothing anonymised or kept", got)
	}
	rows := f.store.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows left = %d, want bob's 2", len(rows))
	}
	for _, r := range rows {
		if r.Person != bob {
			t.Errorf("a row of %v survived", r.Person)
		}
	}
	again, err := f.mod.DeletePerson(ctxT(t), alice)
	if err != nil {
		t.Fatalf("second DeletePerson: %v", err)
	}
	if again.Deleted != 0 {
		t.Errorf("second Deleted = %d, want 0", again.Deleted)
	}
	if sp := f.spansNamed("hello.delete-person"); len(sp) != 2 {
		t.Errorf("hello.delete-person spans = %d, want 2", len(sp))
	}
	ex, err := f.mod.ExportPerson(ctxT(t), alice)
	if err != nil {
		t.Fatalf("ExportPerson: %v", err)
	}
	if got := strings.TrimSpace(string(mustJSONObject(t, ex.Data)["greetings"])); got != "[]" {
		t.Errorf("export after delete = %s, want []", got)
	}
}

// F24
func TestDeletePerson_ReadOnly_ReturnsUnavailable(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	seedRows(t, f.store, alice, 2)
	_, err := f.mod.DeletePerson(ctxT(t), alice)
	if !errs.Is(err, errs.Unavailable) || !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("err = %v, want Unavailable wrapping db.ErrReadOnly", err)
	}
	if len(f.store.Rows()) != 2 {
		t.Errorf("rows were removed on a read-only copy")
	}
}

// F25
func TestDeletePerson_StoreFails_ReturnsKind(t *testing.T) {
	f := newFx(t, cfg{})
	f.store.Fail("DeleteByPerson", errs.E("test", errs.Timeout, errors.New("slow")))
	if _, err := f.mod.DeletePerson(ctxT(t), alice); !errs.Is(err, errs.Timeout) {
		t.Fatalf("err = %v, want Timeout", err)
	}
}

func TestPersonData_IsRegisteredInSet(t *testing.T) {
	f := newFx(t, cfg{})
	found := false
	for _, pd := range f.set.PersonData() {
		if pd.Module == "hello" {
			found = true
		}
	}
	if !found {
		t.Errorf("Set.PersonData() lacks hello")
	}
}

// ---- lifecycle through the Set ----

func TestSet_StartAndStop_RunsTheCounter(t *testing.T) {
	f := newFx(t, cfg{rogueKey: "module.rogue.banner.hi"})
	if err := f.set.Start(ctxT(t)); err != nil {
		t.Fatalf("Set.Start: %v", err)
	}
	f.deliver(t, alice)
	if got := f.mod.Seen(); got != 1 {
		t.Errorf("Seen = %d, want 1", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := f.set.Stop(ctx); err != nil {
		t.Fatalf("Set.Stop: %v", err)
	}
}

func TestSet_Start_BadBannerKey_FailsWithInvalid(t *testing.T) {
	f := newFx(t, cfg{rogueKey: "module.hello.banner.welcome"})
	if err := f.set.Start(ctxT(t)); !errs.Is(err, errs.Invalid) {
		t.Fatalf("Set.Start err = %v, want Invalid", err)
	}
}
