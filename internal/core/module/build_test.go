package module_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func buildPlan(t *testing.T, p *module.Plan) (*module.Set, error) {
	t.Helper()
	s, err := p.Build(t.Context(), okSchema(), provisioner(t, nil))
	if err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	return s, nil
}

func mustBuild(t *testing.T, r *module.Registry, env module.Env) *module.Set {
	t.Helper()
	s, err := buildPlan(t, resolve(t, r, env))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return s
}

func TestBuild_CallsSchemaThenProvisionThenFactoryInOrder(t *testing.T) {
	rec := &recorder{}
	r := reg(t,
		e(mf("alpha", withMigrations), recFactory(rec, "alpha", &plain{name: "alpha"})),
		e(mf("beta"), recFactory(rec, "beta", &plain{name: "beta"})),
		e(mf("gamma", withMigrations), recFactory(rec, "gamma", &plain{name: "gamma"})),
	)
	p := resolve(t, r, enabled("alpha", "beta", "gamma"))
	s, err := p.Build(t.Context(), &fakeSchema{rec: rec}, provisioner(t, rec))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"schema:alpha", "schema:gamma", // every schema check first (D17)
		"provision:alpha", "factory:alpha",
		"provision:beta", "factory:beta",
		"provision:gamma", "factory:gamma",
	}
	if got := rec.entries(); !slices.Equal(got, want) {
		t.Fatalf("call order:\n got %v\nwant %v", got, want)
	}
	if len(s.Modules()) != 3 {
		t.Fatalf("Modules() has %d entries", len(s.Modules()))
	}
}

func TestBuild_SkipsSchemaCheckWhenNoMigrations(t *testing.T) {
	rec := &recorder{}
	r := simple(t, mf("alpha"), mf("beta"))
	p := resolve(t, r, enabled("alpha", "beta"))
	if _, err := p.Build(t.Context(), &fakeSchema{rec: rec}, provisioner(t, nil)); err != nil {
		t.Fatal(err)
	}
	if got := rec.entries(); len(got) != 0 {
		t.Fatalf("CheckSchema called for modules without migrations: %v", got)
	}
}

func TestBuild_EmptyPlan_ReturnsEmptySet(t *testing.T) {
	s := mustBuild(t, module.NewRegistry(), module.Env{})
	if len(s.Modules()) != 0 || len(s.Routes()) != 0 || len(s.PersonData()) != 0 {
		t.Fatal("empty plan must build an empty set")
	}
	ctx := t.Context()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestBuild_ModuleWithoutOptionalInterfaces_Passes(t *testing.T) {
	s := mustBuild(t, simple(t, mf("alpha")), enabled("alpha"))
	if got := s.Modules(); len(got) != 1 || got[0].Manifest.Name != "alpha" || got[0].Module == nil {
		t.Fatalf("Modules() = %+v", got)
	}
	if len(s.Routes()) != 0 {
		t.Fatalf("a module without RouteProvider produced routes: %+v", s.Routes())
	}
}

func TestBuild_ProvisionFails_WrapsWithModule(t *testing.T) {
	r := simple(t, mf("alpha"), mf("beta"))
	p := resolve(t, r, enabled("alpha", "beta"))
	base := provisioner(t, nil)
	prov := func(ctx context.Context, m module.Manifest) (module.Deps, error) {
		if m.Name == "beta" {
			return module.Deps{}, errs.E("test.provision", errs.Unavailable, errBoom)
		}
		return base(ctx, m)
	}
	_, err := p.Build(t.Context(), okSchema(), prov)
	wantKind(t, err, errs.Unavailable)
	if msg := err.Error(); !strings.Contains(msg, "beta") {
		t.Errorf("error does not name the module: %v", err)
	}
}

func TestBuild_DepsMissingLoggerTracerOrClock_ReturnsInvalid(t *testing.T) {
	tests := map[string]func(*module.Deps){
		"logger": func(d *module.Deps) { d.Logger = nil },
		"tracer": func(d *module.Deps) { d.Tracer = nil },
		"clock":  func(d *module.Deps) { d.Clock = nil },
	}
	for name, strip := range tests {
		t.Run(name, func(t *testing.T) {
			factoryCalled := false
			r := reg(t, e(mf("alpha"), func(context.Context, module.Deps) (module.Module, error) {
				factoryCalled = true
				return &plain{}, nil
			}))
			base := provisioner(t, nil)
			prov := func(ctx context.Context, m module.Manifest) (module.Deps, error) {
				d, err := base(ctx, m)
				strip(&d)
				return d, err
			}
			_, err := resolve(t, r, enabled("alpha")).Build(t.Context(), okSchema(), prov)
			wantKind(t, err, errs.Invalid)
			if factoryCalled {
				t.Error("factory ran with incomplete Deps")
			}
		})
	}
}

func TestBuild_SchemaMismatch_ReportsEveryModuleAndBuildsNothing(t *testing.T) {
	rec := &recorder{}
	r := reg(t,
		e(mf("alpha", withMigrations), recFactory(rec, "alpha", &plain{})),
		e(mf("beta", withMigrations), recFactory(rec, "beta", &plain{})),
		e(mf("gamma", withMigrations), recFactory(rec, "gamma", &plain{})),
	)
	chk := &fakeSchema{rec: rec, errs: map[string]error{
		"alpha": errs.E("test.check", errs.Conflict, errBoom),
		"gamma": errs.E("test.check", errs.Conflict, errBoom),
	}}
	_, err := resolve(t, r, enabled("alpha", "beta", "gamma")).Build(t.Context(), chk, provisioner(t, rec))
	wantKind(t, err, errs.Conflict)
	want := []string{"schema:alpha", "schema:beta", "schema:gamma"}
	if got := rec.entries(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want only the three schema checks %v", got, want)
	}
	for _, n := range []string{"alpha", "gamma"} {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("error does not mention failing module %s: %v", n, err)
		}
	}
}

func TestBuild_SchemaUnavailableAndTimeout_KeepCheckerKind(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Invalid} {
		r := simple(t, mf("alpha", withMigrations))
		chk := &fakeSchema{errs: map[string]error{"alpha": errs.E("test.check", k, errBoom)}}
		_, err := resolve(t, r, enabled("alpha")).Build(t.Context(), chk, provisioner(t, nil))
		wantKind(t, err, k)
	}
}

func TestBuild_FactoryFails_WrapsWithModule(t *testing.T) {
	r := reg(t, e(mf("alpha"), func(context.Context, module.Deps) (module.Module, error) {
		return nil, errs.E("test.factory", errs.Conflict, errBoom)
	}))
	_, err := buildPlan(t, resolve(t, r, enabled("alpha")))
	wantKind(t, err, errs.Conflict)
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error does not name the module: %v", err)
	}
}

func TestBuild_FactoryReturnsNilModule_ReturnsInternal(t *testing.T) {
	r := reg(t, e(mf("alpha"), func(context.Context, module.Deps) (module.Module, error) { return nil, nil }))
	_, err := buildPlan(t, resolve(t, r, enabled("alpha")))
	wantKind(t, err, errs.Internal)
}

func TestBuild_FactoryReceivesProvisionedDepsWithLinks(t *testing.T) {
	var got module.Deps
	r := reg(t, e(mf("alpha"), func(_ context.Context, d module.Deps) (module.Module, error) {
		got = d
		return &plain{}, nil
	}))
	mustBuild(t, r, enabled("alpha"))
	if got.Logger == nil || got.Tracer == nil || got.Clock == nil {
		t.Fatalf("factory deps incomplete: %+v", got)
	}
	if got.Links == nil {
		t.Fatal("Build did not set Deps.Links")
	}
}

func TestBuild_ProvisionedLinksAreOverwritten(t *testing.T) {
	// Links is set by Build, never by Provision.
	foreign := &module.Links{}
	var got *module.Links
	r := reg(t, e(mf("alpha"), func(_ context.Context, d module.Deps) (module.Module, error) {
		got = d.Links
		return &plain{}, nil
	}))
	base := provisioner(t, nil)
	prov := func(ctx context.Context, m module.Manifest) (module.Deps, error) {
		d, err := base(ctx, m)
		d.Links = foreign
		return d, err
	}
	if _, err := resolve(t, r, enabled("alpha")).Build(t.Context(), okSchema(), prov); err != nil {
		t.Fatal(err)
	}
	if got == foreign || got == nil {
		t.Fatal("Build must replace Deps.Links with the module's own view")
	}
}

func TestBuild_CapabilitiesMismatchManifest_ReturnsInvalid(t *testing.T) {
	gadget := struct{}{}
	tests := []struct {
		name     string
		manifest module.Manifest
		mod      module.Module
		ok       bool
	}{
		{"matching", mf("alpha", provides("gadget")), &capsMod{&plain{}, map[module.Capability]any{"gadget": gadget}}, true},
		{"two matching", mf("alpha", provides("a", "b")), &capsMod{&plain{}, map[module.Capability]any{"a": gadget, "b": gadget}}, true},
		{"provides but no provider interface", mf("alpha", provides("gadget")), &plain{}, false},
		{"missing key", mf("alpha", provides("a", "b")), &capsMod{&plain{}, map[module.Capability]any{"a": gadget}}, false},
		{"different key", mf("alpha", provides("gadget")), &capsMod{&plain{}, map[module.Capability]any{"widget": gadget}}, false},
		{"extra key", mf("alpha", provides("gadget")), &capsMod{&plain{}, map[module.Capability]any{"gadget": gadget, "widget": gadget}}, false},
		{"nil value", mf("alpha", provides("gadget")), &capsMod{&plain{}, map[module.Capability]any{"gadget": nil}}, false},
		{"nil map", mf("alpha", provides("gadget")), &capsMod{&plain{}, nil}, false},
		{"values but nothing declared", mf("alpha"), &capsMod{&plain{}, map[module.Capability]any{"gadget": gadget}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := reg(t, e(tc.manifest, constFactory(tc.mod)))
			_, err := buildPlan(t, resolve(t, r, enabled("alpha")))
			if tc.ok {
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestBuild_ContributionsMismatchManifest_ReturnsInvalid(t *testing.T) {
	beta := mf("beta", defines("beta.page", "beta.card"))
	tests := []struct {
		name     string
		manifest module.Manifest
		mod      module.Module
		ok       bool
	}{
		{"matching", mf("alpha", contributes("beta.page")), &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.page": "x"}}, true},
		{"contributes but no interface", mf("alpha", contributes("beta.page")), &plain{}, false},
		{"missing key", mf("alpha", contributes("beta.page", "beta.card")), &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.page": "x"}}, false},
		{"different key", mf("alpha", contributes("beta.page")), &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.card": "x"}}, false},
		{"extra key", mf("alpha", contributes("beta.page")), &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.page": "x", "beta.card": "x"}}, false},
		{"values but nothing declared", mf("alpha"), &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.page": "x"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := reg(t, e(tc.manifest, constFactory(tc.mod)), e(beta, okFactory("beta")))
			_, err := buildPlan(t, resolve(t, r, enabled("alpha", "beta")))
			if tc.ok {
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestBuild_ContributionRejectedByDefiner_ReturnsInvalid(t *testing.T) {
	definer := mf("beta", defines("beta.page"))
	good := &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.page": "a string"}}
	bad := &contribMod{&plain{}, map[module.ExtensionPoint]any{"beta.page": 42}}
	contributor := mf("alpha", contributes("beta.page"))

	r := reg(t, e(contributor, constFactory(good)), e(definer, okFactory("beta")))
	if _, err := buildPlan(t, resolve(t, r, enabled("alpha", "beta"))); err != nil {
		t.Fatalf("accepted contribution: %v", err)
	}
	r = reg(t, e(contributor, constFactory(bad)), e(definer, okFactory("beta")))
	_, err := buildPlan(t, resolve(t, r, enabled("alpha", "beta")))
	wantKind(t, err, errs.Invalid)
	for _, n := range []string{"alpha", "beta", "beta.page"} {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("error does not name %q: %v", n, err)
		}
	}
}

func TestBuild_CoreExtensionPointAcceptsIsChecked(t *testing.T) {
	core := module.ExtensionPointDecl{Name: "people.person-page", Accepts: acceptsString}
	env := module.Env{Enabled: []string{"alpha"}, CoreExtensionPoints: []module.ExtensionPointDecl{core}}
	mk := func(v any) *module.Registry {
		return reg(t, e(mf("alpha", contributes("people.person-page")),
			constFactory(&contribMod{&plain{}, map[module.ExtensionPoint]any{"people.person-page": v}})))
	}
	if _, err := buildPlan(t, resolve(t, mk("ok"), env)); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	_, err := buildPlan(t, resolve(t, mk(7), env))
	wantKind(t, err, errs.Invalid)
}

func TestBuild_ConsumerRejectsProviderType_FailsStartup(t *testing.T) {
	type doer interface{ Do() }
	r := reg(t,
		e(mf("alpha", provides("gadget")), constFactory(&capsMod{&plain{}, map[module.Capability]any{"gadget": "not a doer"}})),
		e(mf("beta", requires("gadget")), func(_ context.Context, d module.Deps) (module.Module, error) {
			v, err := d.Links.Capability("gadget")
			if err != nil {
				return nil, fmt.Errorf("beta: %w", err)
			}
			if _, ok := v.(doer); !ok {
				return nil, errs.E("beta.New", errs.Invalid, errBoom)
			}
			return &plain{}, nil
		}),
	)
	_, err := buildPlan(t, resolve(t, r, enabled("alpha", "beta")))
	wantKind(t, err, errs.Invalid)
}

func TestBuild_LinksCapabilityWorksInFactory(t *testing.T) {
	impl := &struct{ name string }{"the impl"}
	var seen any
	r := reg(t,
		e(mf("alpha", provides("gadget")), constFactory(&capsMod{&plain{}, map[module.Capability]any{"gadget": impl}})),
		e(mf("beta", requires("gadget")), func(_ context.Context, d module.Deps) (module.Module, error) {
			v, err := d.Links.Capability("gadget")
			if err != nil {
				return nil, fmt.Errorf("beta: %w", err)
			}
			seen = v
			return &plain{}, nil
		}),
	)
	mustBuild(t, r, enabled("alpha", "beta"))
	if seen != any(impl) {
		t.Fatalf("consumer got %v, want the provider's value", seen)
	}
}

func TestBuild_LinksContributorsWorksAfterBuild(t *testing.T) {
	var definerLinks *module.Links
	var errInFactory error
	r := reg(t,
		e(mf("alpha", defines("alpha.page")), func(_ context.Context, d module.Deps) (module.Module, error) {
			definerLinks = d.Links
			_, errInFactory = d.Links.Contributors("alpha.page") // F43: before the freeze
			return &plain{}, nil
		}),
		e(mf("beta", contributes("alpha.page")), constFactory(&contribMod{&plain{}, map[module.ExtensionPoint]any{"alpha.page": "from beta"}})),
		e(mf("gamma", contributes("alpha.page")), constFactory(&contribMod{&plain{}, map[module.ExtensionPoint]any{"alpha.page": "from gamma"}})),
	)
	mustBuild(t, r, enabled("alpha", "beta", "gamma"))
	wantKind(t, errInFactory, errs.Internal)
	got, err := definerLinks.Contributors("alpha.page")
	if err != nil {
		t.Fatal(err)
	}
	want := []module.Contribution{{Module: "beta", Value: "from beta"}, {Module: "gamma", Value: "from gamma"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Contributors = %+v, want %+v (build order)", got, want)
	}
}

func TestBuild_DisabledContributorIsNotListed(t *testing.T) {
	var links *module.Links
	r := reg(t,
		e(mf("alpha", defines("alpha.page")), func(_ context.Context, d module.Deps) (module.Module, error) {
			links = d.Links
			return &plain{}, nil
		}),
		e(mf("beta", contributes("alpha.page")), constFactory(&contribMod{&plain{}, map[module.ExtensionPoint]any{"alpha.page": "b"}})),
	)
	mustBuild(t, r, enabled("alpha"))
	got, err := links.Contributors("alpha.page")
	if err != nil || len(got) != 0 {
		t.Fatalf("Contributors = %+v, %v; want none and no error", got, err)
	}
}

func TestBuild_InvalidOrDuplicateRoute_ReturnsInvalid(t *testing.T) {
	for _, tc := range routeTable() {
		t.Run(tc.name, func(t *testing.T) {
			mod := &apiMod{plain: &plain{}, routes: []module.Route{tc.r}, openapi: []byte(`{}`)}
			r := reg(t, e(mf("alpha"), constFactory(mod)))
			_, err := buildPlan(t, resolve(t, r, enabled("alpha")))
			if tc.ok {
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
	t.Run("duplicate method and pattern", func(t *testing.T) {
		mod := &apiMod{plain: &plain{}, routes: []module.Route{route("GET", "/a"), route("GET", "/a")}, openapi: []byte(`{}`)}
		_, err := buildPlan(t, resolve(t, reg(t, e(mf("alpha"), constFactory(mod))), enabled("alpha")))
		wantKind(t, err, errs.Invalid)
	})
	t.Run("same pattern different method", func(t *testing.T) {
		mod := &apiMod{plain: &plain{}, routes: []module.Route{route("GET", "/a"), route("POST", "/a")}, openapi: []byte(`{}`)}
		mustBuild(t, reg(t, e(mf("alpha"), constFactory(mod))), enabled("alpha"))
	})
}

func TestBuild_EmptyOpenAPI_ReturnsInvalid(t *testing.T) {
	for _, frag := range [][]byte{nil, {}} {
		mod := &apiMod{plain: &plain{}, routes: []module.Route{route("GET", "/a")}, openapi: frag}
		_, err := buildPlan(t, resolve(t, reg(t, e(mf("alpha"), constFactory(mod))), enabled("alpha")))
		wantKind(t, err, errs.Invalid)
	}
}

func TestBuild_RouteProviderWithoutDescriber_ReturnsInvalid(t *testing.T) {
	mod := &routesMod{plain: &plain{}, routes: []module.Route{route("GET", "/a")}}
	_, err := buildPlan(t, resolve(t, reg(t, e(mf("alpha"), constFactory(mod))), enabled("alpha")))
	wantKind(t, err, errs.Invalid)
}

func TestBuild_ContextCanceled_ReturnsCanceled(t *testing.T) {
	p := resolve(t, simple(t, mf("alpha")), enabled("alpha"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := p.Build(ctx, okSchema(), provisioner(t, nil))
	wantKind(t, err, errs.Canceled)
}

func TestBuild_DeadlinePassed_ReturnsTimeout(t *testing.T) {
	p := resolve(t, simple(t, mf("alpha")), enabled("alpha"))
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
	defer cancel()
	_, err := p.Build(ctx, okSchema(), provisioner(t, nil))
	wantKind(t, err, errs.Timeout)
}

func TestBuild_ContextCanceledDuringBuild_ReturnsCanceled(t *testing.T) {
	r := simple(t, mf("alpha"), mf("beta"), mf("gamma"))
	p := resolve(t, r, enabled("alpha", "beta", "gamma"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	base := provisioner(t, nil)
	prov := func(c context.Context, m module.Manifest) (module.Deps, error) {
		if m.Name == "alpha" {
			cancel()
		}
		return base(c, m)
	}
	_, err := p.Build(ctx, okSchema(), prov)
	wantKind(t, err, errs.Canceled)
}

func TestBuild_NilSchemaOrProvision_ReturnsInvalid(t *testing.T) {
	p := resolve(t, simple(t, mf("alpha")), enabled("alpha"))
	_, err := p.Build(t.Context(), nil, provisioner(t, nil))
	wantKind(t, err, errs.Invalid)
	_, err = p.Build(t.Context(), okSchema(), nil)
	wantKind(t, err, errs.Invalid)
}
