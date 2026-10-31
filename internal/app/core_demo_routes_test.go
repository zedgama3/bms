package app

import (
	"testing"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// builtCoreWith is builtCore with the configuration adjusted before the parts are built.
func builtCoreWith(t *testing.T, adjust func(*config.Config)) *assembly {
	t.Helper()
	a, _ := provisionAssembly(t)
	a.cfg = config.Default()
	a.cfg.Instance.Name = "zqtest"
	a.cfg.Data.Dir = t.TempDir()
	a.cfg.Session.Key = secret.New(placeholderSessionKey)
	adjust(&a.cfg)
	a.corePerms, a.parts = nil, coreParts
	if err := a.buildParts(t.Context()); err != nil {
		t.Fatalf("buildParts over db.None(): %v", err)
	}
	return a
}

func demoRouteSet(a *assembly) (module.ModuleRoutes, bool) {
	for _, rs := range a.routeSets {
		if rs.Module == demoArea {
			return rs, true
		}
	}
	return module.ModuleRoutes{}, false
}

// The demo route set exists only in demo mode: elsewhere /api/demo is a 404 and `bms openapi`
// (demo off) does not document it (core_demo.go; demo.md).
func TestDemoRoutes_OnlyInDemoMode(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		a := builtCore(t)
		if a.demo == nil {
			t.Error("the demo service is built in every process")
		}
		if rs, ok := demoRouteSet(a); ok {
			t.Errorf("demo off: route set %q is mounted with %d routes, want none", rs.Module, len(rs.Routes))
		}
	})
	t.Run("on", func(t *testing.T) {
		a := builtCoreWith(t, func(c *config.Config) {
			c.Demo.Enabled = true
			c.Demo.PublicURL = "https://demo.example.org"
		})
		rs, ok := demoRouteSet(a)
		if !ok {
			t.Fatal("demo on: no demo route set mounted")
		}
		if len(rs.Routes) == 0 || len(rs.OpenAPI) == 0 {
			t.Errorf("demo route set has %d routes and %d bytes of fragment", len(rs.Routes), len(rs.OpenAPI))
		}
	})
}
