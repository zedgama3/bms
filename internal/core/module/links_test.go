package module_test

import (
	"context"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// linkFixture builds alpha (defines alpha.page; requires gadget; uses widget; undeclared use
// elsewhere), beta (provides gadget, contributes to alpha.page) and captures alpha's Links.
func linkFixture(t *testing.T) (alpha *module.Links, factoryLinks *module.Links, impl any) {
	t.Helper()
	impl = &struct{ n int }{1}
	var captured *module.Links
	r := reg(t,
		e(mf("alpha", requires("gadget"), uses("widget"), defines("alpha.page")), func(_ context.Context, d module.Deps) (module.Module, error) {
			captured = d.Links
			return &plain{}, nil
		}),
		e(mf("beta", provides("gadget"), contributes("alpha.page")), constFactory(&struct {
			*plain
			*capsPart
			*contribPart
		}{&plain{}, &capsPart{map[module.Capability]any{"gadget": impl}}, &contribPart{map[module.ExtensionPoint]any{"alpha.page": "c"}}})),
	)
	mustBuild(t, r, enabled("alpha", "beta"))
	return captured, captured, impl
}

type capsPart struct{ m map[module.Capability]any }

func (c *capsPart) Capabilities() map[module.Capability]any { return c.m }

type contribPart struct{ m map[module.ExtensionPoint]any }

func (c *contribPart) Contributions() map[module.ExtensionPoint]any { return c.m }

func TestLinksCapability_Declared_ReturnsImplementation(t *testing.T) {
	l, _, impl := linkFixture(t)
	got, err := l.Capability("gadget")
	if err != nil || got != impl {
		t.Fatalf("Capability(gadget) = %v, %v", got, err)
	}
}

func TestLinksCapability_Undeclared_ReturnsInvalid(t *testing.T) {
	l, _, _ := linkFixture(t)
	for _, name := range []module.Capability{"other", "", "Bad Name"} {
		_, err := l.Capability(name)
		wantKind(t, err, errs.Invalid)
	}
}

func TestLinksCapability_UsedButAbsent_ReturnsNotFound(t *testing.T) {
	l, _, _ := linkFixture(t)
	_, err := l.Capability("widget")
	wantKind(t, err, errs.NotFound)
}

func TestLinksContributors_Undeclared_ReturnsInvalid(t *testing.T) {
	l, _, _ := linkFixture(t)
	_, err := l.Contributors("beta.page")
	wantKind(t, err, errs.Invalid)
	_, err = l.Contributors("")
	wantKind(t, err, errs.Invalid)
}

func TestLinksContributors_Declared_ReturnsContributions(t *testing.T) {
	l, _, _ := linkFixture(t)
	got, err := l.Contributors("alpha.page")
	if err != nil || len(got) != 1 || got[0] != (module.Contribution{Module: "beta", Value: "c"}) {
		t.Fatalf("Contributors = %+v, %v", got, err)
	}
}

func TestLinks_BeforeFreezeOrZero_ReturnsInternal(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		var l module.Links
		_, err := l.Capability("gadget")
		wantKind(t, err, errs.Internal)
		_, err = l.Contributors("alpha.page")
		wantKind(t, err, errs.Internal)
	})
	t.Run("new zero pointer", func(t *testing.T) {
		l := &module.Links{}
		_, err := l.Contributors("alpha.page")
		wantKind(t, err, errs.Internal)
	})
	t.Run("Contributors from a factory", func(t *testing.T) {
		var got error
		r := reg(t, e(mf("alpha", defines("alpha.page")), func(_ context.Context, d module.Deps) (module.Module, error) {
			_, got = d.Links.Contributors("alpha.page")
			return &plain{}, nil
		}))
		mustBuild(t, r, enabled("alpha"))
		wantKind(t, got, errs.Internal)
	})
}

func TestLinks_SafeForConcurrentUse(t *testing.T) {
	l, _, impl := linkFixture(t)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := l.Capability("gadget"); err != nil || got != impl {
				t.Errorf("Capability: %v, %v", got, err)
			}
			if got, err := l.Contributors("alpha.page"); err != nil || len(got) != 1 {
				t.Errorf("Contributors: %v, %v", got, err)
			}
			if _, err := l.Capability("widget"); !errs.Is(err, errs.NotFound) {
				t.Errorf("widget: %v", err)
			}
		}()
	}
	wg.Wait()
}
