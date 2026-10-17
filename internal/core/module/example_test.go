package module_test

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/model"
)

// inert is the smallest module: it holds no personal data and does nothing.
type inert struct{}

func (inert) Start(context.Context) error { return nil }
func (inert) Stop(context.Context) error  { return nil }
func (inert) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (inert) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

func ExampleRegistry_Resolve() {
	reg := module.NewRegistry()
	manifest := module.Manifest{
		Name: "alpha",
		Permissions: []authz.PermissionDecl{
			{Name: "alpha.things.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
		},
		Publishes: []signals.Name{"alpha.thing-made"},
	}
	factory := func(context.Context, module.Deps) (module.Module, error) { return inert{}, nil }
	if err := reg.Add(manifest, factory); err != nil {
		fmt.Println("add:", err)
		return
	}
	plan, err := reg.Resolve(module.Env{Enabled: []string{"alpha"}})
	if err != nil {
		fmt.Println("resolve:", err)
		return
	}
	for _, m := range plan.Order() {
		fmt.Println(m.Name)
	}
	fmt.Println(plan.Permissions()[0].Name)
	// Output:
	// alpha
	// alpha.things.read
}

func ExampleManifest_Validate() {
	m := module.Manifest{
		Name:        "alpha",
		Permissions: []authz.PermissionDecl{{Name: "beta.read", Scope: authz.ScopeSite, Reaches: model.SensitivityPublic}},
	}
	fmt.Println(m.Validate() != nil)
	// Output: true
}
