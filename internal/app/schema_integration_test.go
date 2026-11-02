//go:build integration

package app

import (
	"io"
	"reflect"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// TestSchemaStatus_Integration_SameAsTheMigratorOverThePlansSets: after migrate, the named
// reader (boundaries.md D1) reports exactly what the old closure did, a.migrator.Status over
// setsFor(plan.Order()): the same statuses in the same set order, every set current.
func TestSchemaStatus_Integration_SameAsTheMigratorOverThePlansSets(t *testing.T) {
	ctx := t.Context()
	u := itDB(t)
	env, _ := itEnv(t, u.String())
	if r := itMain(ctx, env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	p := &process{
		args: []string{"serve"}, environ: env, lookup: lookupIn(env), stdout: io.Discard, stderr: io.Discard,
		clock: clock.Real(), registry: module.Default(), parts: coreParts, version: "dev",
	}
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	a := newAssembly(p, cfg)
	if err := a.startTelemetry(ctx, p.lookup, io.Discard); err != nil {
		t.Fatalf("startTelemetry: %v", err)
	}
	t.Cleanup(func() {
		if err := a.closeQuiet(t.Context()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	enabled, err := enabledModules(p.registry, cfg.Modules.Disabled)
	if err != nil {
		t.Fatalf("enabledModules: %v", err)
	}
	if err := a.resolve(ctx, p.registry, enabled); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := a.openDatabase(ctx); err != nil {
		t.Fatalf("openDatabase: %v", err)
	}

	sets := a.setsFor(a.plan.Order())
	want, err := a.migrator.Status(ctx, sets)
	if err != nil {
		t.Fatalf("migrator.Status: %v", err)
	}
	reader, err := newSchemaStatus(a.migrator, a.setsFor(a.plan.Order()))
	if err != nil {
		t.Fatalf("newSchemaStatus: %v", err)
	}
	got, err := reader.Status(ctx)
	if err != nil {
		t.Fatalf("reader.Status: %v", err)
	}
	if len(got) == 0 || len(got) != len(sets)+1 && len(got) != len(sets) {
		t.Fatalf("got %d statuses for %d sets", len(got), len(sets))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reader = %+v\nmigrator = %+v", got, want)
	}
	for _, st := range got {
		if st.Err != nil || st.Built != st.Applied || st.Applied == 0 {
			t.Errorf("set %s not current after migrate: built %d applied %d err %v", st.Set, st.Built, st.Applied, st.Err)
		}
	}
}
