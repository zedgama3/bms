package module_test

import (
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func enabled(n ...string) module.Env { return module.Env{Enabled: n} }

func TestResolve_OrderIsTopologicalAndStable(t *testing.T) {
	r := simple(t,
		mf("alpha", requires("gadget")),
		mf("beta"),
		mf("gamma", provides("gadget")),
		mf("delta", uses("widget")),
		mf("epsilon", provides("widget")),
		mf("zeta"),
	)
	env := enabled("alpha", "beta", "gamma", "delta", "epsilon", "zeta")
	first := names(resolve(t, r, env).Order())
	if len(first) != 6 {
		t.Fatalf("Order() = %v, want 6 modules", first)
	}
	if indexOf(first, "gamma") > indexOf(first, "alpha") {
		t.Errorf("provider gamma after requirer alpha: %v", first)
	}
	if indexOf(first, "epsilon") > indexOf(first, "delta") {
		t.Errorf("provider epsilon after user delta: %v", first)
	}
	for range 10 {
		if again := names(resolve(t, r, env).Order()); !slices.Equal(first, again) {
			t.Fatalf("order changed between runs: %v then %v", first, again)
		}
	}
	// The order must not depend on the order Enabled lists the modules.
	rev := slices.Clone(env.Enabled)
	slices.Reverse(rev)
	if got := names(resolve(t, r, enabled(rev...)).Order()); !slices.Equal(first, got) {
		t.Fatalf("order depends on Enabled order: %v vs %v", first, got)
	}
}

func TestResolve_IndependentModules_OrderedByName(t *testing.T) {
	r := simple(t, mf("gamma"), mf("alpha"), mf("beta"))
	got := names(resolve(t, r, enabled("gamma", "alpha", "beta")).Order())
	if want := []string{"alpha", "beta", "gamma"}; !slices.Equal(got, want) {
		t.Fatalf("Order() = %v, want %v", got, want)
	}
}

func TestResolve_DisabledModuleAppearsInGraphNotOrder(t *testing.T) {
	r := simple(t, mf("alpha", withMigrations), mf("beta"))
	p := resolve(t, r, enabled("alpha"))
	if got := names(p.Order()); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("Order() = %v", got)
	}
	g := p.Graph()
	state := map[string]module.Node{}
	for _, n := range g.Modules {
		state[n.Name] = n
	}
	if len(state) != 2 || !state["alpha"].Enabled || state["beta"].Enabled {
		t.Fatalf("graph nodes: %+v", g.Modules)
	}
	if !state["alpha"].HasSchema || state["beta"].HasSchema {
		t.Errorf("HasSchema wrong: alpha=%v beta=%v", state["alpha"].HasSchema, state["beta"].HasSchema)
	}
}

func TestResolve_NothingEnabled_EmptyPlan(t *testing.T) {
	p := resolve(t, simple(t, mf("alpha")), module.Env{})
	if len(p.Order()) != 0 || len(p.Permissions()) != 0 || len(p.Settings()) != 0 {
		t.Fatal("plan with nothing enabled must be empty")
	}
	if len(p.Graph().Modules) != 1 {
		t.Fatal("graph must still list the compiled-in module")
	}
}

func TestResolve_EdgesAndKinds(t *testing.T) {
	core := module.ExtensionPointDecl{Name: "people.person-page", Accepts: acceptsString}
	r := simple(t,
		mf("alpha", requires("gadget"), uses("widget"), subscribes("beta.thing", "people.person-created"),
			contributes("beta.page", "people.person-page")),
		mf("beta", provides("gadget"), publishes("beta.thing"), defines("beta.page")),
		mf("gamma", provides("widget")),
	)
	p := resolve(t, r, module.Env{
		Enabled:             []string{"alpha", "beta", "gamma"},
		CoreSignals:         []signals.Name{"people.person-created"},
		CoreExtensionPoints: []module.ExtensionPointDecl{core},
	})
	want := []module.Edge{
		{From: "alpha", To: "beta", Kind: module.EdgeRequires, Via: "gadget"},
		{From: "alpha", To: "gamma", Kind: module.EdgeUses, Via: "widget"},
		{From: "alpha", To: "beta", Kind: module.EdgeSubscribes, Via: "beta.thing"},
		{From: "alpha", To: "people", Kind: module.EdgeSubscribes, Via: "people.person-created"},
		{From: "alpha", To: "beta", Kind: module.EdgeContributes, Via: "beta.page"},
		{From: "alpha", To: "people", Kind: module.EdgeContributes, Via: "people.person-page"},
	}
	got := p.Graph().Edges
	if len(got) != len(want) {
		t.Fatalf("got %d edges, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing edge %+v in %+v", w, got)
		}
	}
	if w := p.Graph().Warnings; len(w) != 0 {
		t.Errorf("unexpected warnings: %+v", w)
	}
}

func TestResolve_CoreSignalsAndPointsSatisfyChecks(t *testing.T) {
	r := simple(t, mf("alpha", subscribes("people.person-deleted"), contributes("events.event-created")))
	env := module.Env{
		Enabled:     []string{"alpha"},
		CoreSignals: []signals.Name{"people.person-deleted"},
		CoreExtensionPoints: []module.ExtensionPointDecl{
			{Name: "events.event-created", Accepts: acceptsString},
		},
	}
	resolve(t, r, env)
	// Without the core declarations the same registry is rejected.
	_, err := r.Resolve(enabled("alpha"))
	wantKind(t, err, errs.Invalid)
}

func TestResolve_UnknownEnabledName_ReturnsInvalid(t *testing.T) {
	r := simple(t, mf("alpha"))
	_, err := r.Resolve(enabled("alpha", "nosuch"))
	wantKind(t, err, errs.Invalid)
}

func TestResolve_EnvInvalid_ReturnsInvalid(t *testing.T) {
	r := simple(t, mf("alpha"))
	goodPoint := module.ExtensionPointDecl{Name: "people.person-page", Accepts: acceptsString}
	tests := []struct {
		name string
		env  module.Env
	}{
		{"repeated name", enabled("alpha", "alpha")},
		{"empty name", enabled("alpha", "")},
		{"invalid core signal", module.Env{CoreSignals: []signals.Name{"nodot"}}},
		{"core signal not prefixed by a core area", module.Env{CoreSignals: []signals.Name{"alpha.thing"}}},
		{"core signal prefixed by unknown area", module.Env{CoreSignals: []signals.Name{"zzz.thing"}}},
		{"core point invalid name", module.Env{CoreExtensionPoints: []module.ExtensionPointDecl{{Name: "nodot", Accepts: acceptsString}}}},
		{"core point nil accepts", module.Env{CoreExtensionPoints: []module.ExtensionPointDecl{{Name: "people.x"}}}},
		{"valid point plus bad", module.Env{CoreExtensionPoints: []module.ExtensionPointDecl{goodPoint, {Name: "bad"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantKind(t, tc.env.Validate(), errs.Invalid)
			_, err := r.Resolve(tc.env)
			wantKind(t, err, errs.Invalid)
		})
	}
	ok := module.Env{
		Enabled:             []string{"alpha"},
		CoreSignals:         []signals.Name{"people.person-created", "events.event-created"},
		CoreExtensionPoints: []module.ExtensionPointDecl{goodPoint},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid Env: %v", err)
	}
	if err := (module.Env{}).Validate(); err != nil {
		t.Fatalf("zero Env: %v", err)
	}
}

func TestResolve_RequiredCapabilityMissing_ReturnsInvalid(t *testing.T) {
	t.Run("no provider compiled in", func(t *testing.T) {
		_, err := simple(t, mf("alpha", requires("gadget"))).Resolve(enabled("alpha"))
		wantKind(t, err, errs.Invalid)
	})
	t.Run("provider disabled", func(t *testing.T) {
		r := simple(t, mf("alpha", requires("gadget")), mf("beta", provides("gadget")))
		_, err := r.Resolve(enabled("alpha"))
		wantKind(t, err, errs.Invalid)
	})
}

func TestResolve_TwoProviders_ReturnsConflict(t *testing.T) {
	r := simple(t, mf("alpha", provides("gadget")), mf("beta", provides("gadget")), mf("gamma", requires("gadget")))
	_, err := r.Resolve(enabled("alpha", "beta", "gamma"))
	wantKind(t, err, errs.Conflict)
	// Even when nobody uses the capability.
	r = simple(t, mf("alpha", provides("gadget")), mf("beta", provides("gadget")))
	_, err = r.Resolve(enabled("alpha", "beta"))
	wantKind(t, err, errs.Conflict)
	// One of the two disabled is fine.
	resolve(t, r, enabled("alpha"))
}

func TestResolve_CapabilityCycle_ReturnsInvalid(t *testing.T) {
	r := simple(t,
		mf("alpha", requires("capb"), provides("capa")),
		mf("beta", uses("capa"), provides("capb")),
	)
	_, err := r.Resolve(enabled("alpha", "beta"))
	wantKind(t, err, errs.Invalid)

	// A module that provides and requires one capability never reaches Resolve: F8 stops it at Add.
	wantKind(t, module.NewRegistry().Add(mf("gamma", requires("capa"), provides("capa")), okFactory("gamma")), errs.Invalid)
}

func TestResolve_SubscribeUnknownSignal_ReturnsInvalid(t *testing.T) {
	r := simple(t, mf("alpha", subscribes("beta.thing")))
	_, err := r.Resolve(enabled("alpha"))
	wantKind(t, err, errs.Invalid)

	// Known publisher module, but it does not publish that name.
	r = simple(t, mf("alpha", subscribes("beta.thing")), mf("beta", publishes("beta.other")))
	_, err = r.Resolve(enabled("alpha", "beta"))
	wantKind(t, err, errs.Invalid)
	_, err = r.Resolve(enabled("alpha"))
	wantKind(t, err, errs.Invalid)
}

func TestResolve_ContributeUnknownPoint_ReturnsInvalid(t *testing.T) {
	r := simple(t, mf("alpha", contributes("beta.page")))
	_, err := r.Resolve(enabled("alpha"))
	wantKind(t, err, errs.Invalid)

	r = simple(t, mf("alpha", contributes("beta.page")), mf("beta", defines("beta.other")))
	_, err = r.Resolve(enabled("alpha", "beta"))
	wantKind(t, err, errs.Invalid)
}

func TestResolve_ReportsAllProblems(t *testing.T) {
	r := simple(t,
		mf("alpha", requires("gadget"), subscribes("beta.thing"), contributes("beta.page")),
		mf("gamma", requires("widget")),
	)
	_, err := r.Resolve(enabled("alpha", "gamma", "nosuch", "nosuch2"))
	wantKind(t, err, errs.Invalid)
	if got := len(validate.Fields(err)); got < 6 {
		t.Fatalf("Resolve reported %d problems, want at least 6 (2 unknown names, 2 missing capabilities, signal, point)", got)
	}
}

func TestResolve_DisabledOtherSide_Warns(t *testing.T) {
	r := simple(t,
		mf("alpha", subscribes("beta.thing"), contributes("beta.page"), uses("widget")),
		mf("beta", publishes("beta.thing"), defines("beta.page")),
	)
	p := resolve(t, r, enabled("alpha"))
	ws := p.Graph().Warnings
	if len(ws) < 3 {
		t.Fatalf("got %d warnings, want 3 (subscription, contribution, unused capability): %+v", len(ws), ws)
	}
	for _, w := range ws {
		if w.Module != "alpha" || w.Message == "" {
			t.Errorf("bad warning %+v", w)
		}
	}
	// With the other side enabled and providing, there is nothing to warn about.
	r = simple(t,
		mf("alpha", subscribes("beta.thing"), contributes("beta.page"), uses("widget")),
		mf("beta", publishes("beta.thing"), defines("beta.page")),
		mf("gamma", provides("widget")),
	)
	if ws := resolve(t, r, enabled("alpha", "beta", "gamma")).Graph().Warnings; len(ws) != 0 {
		t.Fatalf("unexpected warnings: %+v", ws)
	}
}

func TestResolve_CanBeCalledRepeatedly(t *testing.T) {
	r := simple(t, mf("alpha"), mf("beta"))
	a := resolve(t, r, enabled("alpha", "beta"))
	b := resolve(t, r, enabled("alpha"))
	if len(a.Order()) != 2 || len(b.Order()) != 1 {
		t.Fatal("Resolve must be pure over the registry")
	}
}

func TestPlanPermissionsAndSettings_EnabledModulesOnly(t *testing.T) {
	r := simple(t,
		mf("alpha", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{perm("alpha.read"), perm("alpha.write")}
			m.Settings = []module.SettingDecl{{Key: "greeting", Kind: module.SettingText, Default: "hello"}}
		}),
		mf("beta", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{perm("beta.read")}
			m.Settings = []module.SettingDecl{{Key: "n", Kind: module.SettingInt, Default: "1"}}
		}),
		mf("gamma", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{perm("gamma.read")}
		}),
	)
	p := resolve(t, r, enabled("alpha", "beta"))
	var got []string
	for _, d := range p.Permissions() {
		got = append(got, string(d.Name))
	}
	slices.Sort(got)
	if want := []string{"alpha.read", "alpha.write", "beta.read"}; !slices.Equal(got, want) {
		t.Fatalf("Permissions() = %v, want %v", got, want)
	}
	byMod := map[string][]module.SettingDecl{}
	for _, ms := range p.Settings() {
		byMod[ms.Module] = ms.Settings
	}
	if len(byMod["alpha"]) != 1 || byMod["alpha"][0].Key != "greeting" || len(byMod["beta"]) != 1 {
		t.Fatalf("Settings() = %+v", p.Settings())
	}
	if _, ok := byMod["gamma"]; ok {
		t.Fatal("disabled module's settings were returned")
	}
}

func TestEnvValidate_CoreExtensionPointWithoutCoreAreaPrefix_ReturnsInvalid(t *testing.T) {
	env := module.Env{
		Enabled: []string{"alpha"},
		CoreExtensionPoints: []module.ExtensionPointDecl{
			{Name: "people.person-page", Accepts: acceptsString},
			{Name: "alpha.page", Accepts: acceptsString},
		},
	}
	err := env.Validate()
	wantField(t, err, "coreExtensionPoints[1].name")
	for _, f := range validate.Fields(err) {
		if f.Field == "coreExtensionPoints[1].name" && f.Code != "prefix" {
			t.Errorf("code = %q, want prefix", f.Code)
		}
	}
	_, err = simple(t, mf("alpha")).Resolve(env)
	wantField(t, err, "coreExtensionPoints[1].name")
}

func TestOrderAndModules_ReturnCopies(t *testing.T) {
	mk := func() *module.Registry {
		man := mf("alpha", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{perm("alpha.read")}
			m.Settings = []module.SettingDecl{{Key: "k", Kind: module.SettingBool, Default: "true"}}
		}, provides("gadget"))
		return reg(t, e(man, constFactory(&capsMod{&plain{}, map[module.Capability]any{"gadget": 1}})))
	}
	p := resolve(t, mk(), enabled("alpha"))
	o := p.Order()
	o[0].Permissions[0].Name = "alpha.mutated"
	o[0].Settings[0].Key = "mutated"
	o[0].Provides[0] = "mutated"
	again := p.Order()[0]
	if again.Permissions[0].Name != "alpha.read" || again.Settings[0].Key != "k" || again.Provides[0] != "gadget" {
		t.Fatalf("Plan.Order exposes internal slices: %+v", again)
	}

	s := mustBuild(t, mk(), enabled("alpha"))
	m := s.Modules()
	m[0].Manifest.Permissions[0].Name = "alpha.mutated"
	m[0].Manifest.Provides[0] = "mutated"
	got := s.Modules()[0].Manifest
	if got.Permissions[0].Name != "alpha.read" || got.Provides[0] != "gadget" {
		t.Fatalf("Set.Modules exposes internal slices: %+v", got)
	}
}
