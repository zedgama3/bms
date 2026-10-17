package module_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/model"
)

func TestGraph_IsJSONSerializable(t *testing.T) {
	accepts := module.ExtensionPointDecl{Name: "people.person-page", Accepts: acceptsString}
	r := simple(t,
		mf("alpha", func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{{Name: "alpha.read", Scope: authz.ScopeDepartment, Reaches: model.SensitivitySensitive}}
			m.Settings = []module.SettingDecl{{
				Key: "n", Kind: module.SettingInt, Default: "3",
				Check: func(string) error { return nil },
			}}
			m.Migrations = nil
		}, requires("gadget"), uses("widget"), publishes("alpha.done"), subscribes("beta.thing", "people.person-created"),
			defines("alpha.page"), contributes("beta.page", "people.person-page")),
		mf("beta", provides("gadget"), publishes("beta.thing"), defines("beta.page"), withMigrations),
		mf("gamma"),
	)
	p := resolve(t, r, module.Env{
		Enabled:             []string{"alpha", "beta"},
		CoreSignals:         []signals.Name{"people.person-created"},
		CoreExtensionPoints: []module.ExtensionPointDecl{accepts},
	})
	g := p.Graph()
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("Marshal(Graph): %v (a handler, func or fs.FS leaked into the graph)", err)
	}
	var back module.Graph
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	again, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Fatalf("round trip changed the graph:\n%s\n%s", raw, again)
	}
	if len(back.Modules) != 3 || len(back.Edges) != len(g.Edges) {
		t.Fatalf("round trip lost data: %d modules, %d edges", len(back.Modules), len(back.Edges))
	}
	// Defines are names only.
	var alpha module.Node
	for _, n := range back.Modules {
		if n.Name == "alpha" {
			alpha = n
		}
	}
	if !reflect.DeepEqual(alpha.Defines, []module.ExtensionPoint{"alpha.page"}) {
		t.Errorf("Defines = %v", alpha.Defines)
	}
	if len(alpha.Settings) != 1 || alpha.Settings[0].Check != nil {
		t.Errorf("Settings after round trip = %+v (Check must not be serialized)", alpha.Settings)
	}
}

func TestGraph_NodesCarryDeclarations(t *testing.T) {
	r := simple(t, mf("alpha", func(m *module.Manifest) {
		m.Permissions = []authz.PermissionDecl{perm("alpha.read")}
	}, provides("gadget"), publishes("alpha.done")))
	g := resolve(t, r, enabled("alpha")).Graph()
	if len(g.Modules) != 1 {
		t.Fatal("want one node")
	}
	n := g.Modules[0]
	if n.Name != "alpha" || !n.Enabled || len(n.Permissions) != 1 || len(n.Provides) != 1 || len(n.Publishes) != 1 {
		t.Fatalf("node = %+v", n)
	}
}
