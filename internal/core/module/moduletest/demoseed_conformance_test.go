package moduletest_test

// F66 (D53): Conformance/demoseed_provider_valid, enabled by WithDemoSeed(). Each broken
// module runs Conformance in a child process, as the other conformance mutants do.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

const demoHelperEnv = "BMS_MODULETEST_DEMO_MUTANT"

type demoMutant string

const (
	demoGood           demoMutant = "good"
	demoNotListed      demoMutant = "not_listed"       // ContributesTo lacks demoseed.Point
	demoNotProvider    demoMutant = "not_provider"     // the contribution is not a demoseed.Provider
	demoCreatesNothing demoMutant = "creates_nothing"  // Result.Items == 0
	demoPermission     demoMutant = "permission"       // the provider returns Permission
	demoOtherError     demoMutant = "other_error"      // the provider fails with another kind
	demoNoContribution demoMutant = "no_contribution"  // listed, but Contributions() has no entry
	demoUnusedOption   demoMutant = "rule_without_opt" // a bad provider, but WithDemoSeed not given
)

type demoProvider struct{ mu demoMutant }

func (p demoProvider) Seed(ctx context.Context, req demoseed.Request) (demoseed.Result, error) {
	if req.Actors == nil {
		return demoseed.Result{}, errs.E("alpha.demo.Seed", errs.Internal, os.ErrInvalid)
	}
	if _, err := req.Actors.As(ctx, demoseed.ActorLead); err != nil {
		return demoseed.Result{}, err //nolint:wrapcheck // test double
	}
	switch p.mu {
	case demoCreatesNothing:
		return demoseed.Result{}, nil
	case demoPermission, demoUnusedOption:
		return demoseed.Result{}, errs.E("alpha.demo.Seed", errs.Permission, os.ErrPermission)
	case demoOtherError:
		return demoseed.Result{}, errs.E("alpha.demo.Seed", errs.Unavailable, os.ErrClosed)
	}
	return demoseed.Result{Items: 1}, nil
}

// demoModule is the well-behaved fake module plus a Contributions method.
type demoModule struct {
	*fakeModule
	contribs map[module.ExtensionPoint]any
}

func (d demoModule) Contributions() map[module.ExtensionPoint]any { return d.contribs }

func demoManifest(mu demoMutant) module.Manifest {
	m := manifestFor(good)
	if mu != demoNotListed {
		m.ContributesTo = []module.ExtensionPoint{demoseed.Point}
	}
	return m
}

func demoFactory(mu demoMutant) module.Factory {
	inner := newFactory(good)
	return func(ctx context.Context, deps module.Deps) (module.Module, error) {
		mod, err := inner(ctx, deps)
		if err != nil {
			return nil, err //nolint:wrapcheck // test helper
		}
		contribs := map[module.ExtensionPoint]any{}
		switch mu {
		case demoNotProvider:
			contribs[demoseed.Point] = "not a provider"
		case demoNoContribution:
		default:
			contribs[demoseed.Point] = demoProvider{mu: mu}
		}
		fm, ok := mod.(*fakeModule)
		if !ok {
			return nil, errors.New("demo conformance: the module is not a fakeModule")
		}
		return demoModule{fakeModule: fm, contribs: contribs}, nil
	}
}

func demoOpts(mu demoMutant) []moduletest.Option {
	unwrap := func(ctx context.Context, mod module.Module, s moduletest.Subject) error {
		if d, ok := mod.(demoModule); ok {
			mod = d.fakeModule
		}
		return seed(ctx, mod, s)
	}
	opts := []moduletest.Option{moduletest.WithSeed(unwrap), moduletest.WithoutViews()}
	if mu != demoUnusedOption {
		opts = append(opts, moduletest.WithDemoSeed())
	}
	return opts
}

// TestDemoConformanceHelper is the child-process body; it does nothing unless the env var is set.
func TestDemoConformanceHelper(t *testing.T) {
	name := os.Getenv(demoHelperEnv)
	if name == "" {
		t.Skip("child-process helper")
	}
	mu := demoMutant(name)
	moduletest.Conformance(t, demoManifest(mu), demoFactory(mu), provisionFor(good), demoOpts(mu)...)
}

func runDemoChild(t *testing.T, mu demoMutant) (string, bool) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestDemoConformanceHelper$", "-test.v") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), demoHelperEnv+"="+string(mu))
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr != nil
}

func TestConformance_DemoSeed_ValidProvider_Passes(t *testing.T) {
	out, failed := runDemoChild(t, demoGood)
	if failed {
		t.Fatalf("a valid provider failed Conformance:\n%s", out)
	}
	if !strings.Contains(out, "--- PASS: TestDemoConformanceHelper/demoseed_provider_valid") {
		t.Errorf("rule demoseed_provider_valid did not run and pass:\n%s", out)
	}
}

func TestConformance_DemoSeed_WithoutOption_RuleSkipped(t *testing.T) {
	out, failed := runDemoChild(t, demoUnusedOption)
	if failed {
		t.Fatalf("a module that did not ask for the rule failed it:\n%s", out)
	}
	if strings.Contains(out, "--- FAIL") {
		t.Errorf("unexpected failure:\n%s", out)
	}
	if strings.Contains(out, "--- PASS: TestDemoConformanceHelper/demoseed_provider_valid") {
		t.Errorf("the rule ran without WithDemoSeed():\n%s", out)
	}
}

func TestConformance_DemoSeed_NotListedInContributesTo_Fails(t *testing.T) {
	out, failed := runDemoChild(t, demoNotListed)
	if !failed {
		t.Fatalf("Conformance accepted WithDemoSeed with demoseed.Point missing from ContributesTo:\n%s", out)
	}
	if !strings.Contains(out, demoseed.Point) {
		t.Errorf("failure does not name the extension point:\n%s", out)
	}
	// F66: fails t before the first rule.
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
}

func TestConformance_DemoSeed_ContributionNotAProvider_Fails(t *testing.T) {
	out, failed := runDemoChild(t, demoNotProvider)
	if !failed {
		t.Fatalf("Conformance accepted a contribution that is not a demoseed.Provider:\n%s", out)
	}
	if preconditionRan(out) {
		t.Errorf("a rule ran before the precondition failed:\n%s", out)
	}
}

func TestConformance_DemoSeed_NoContributionEntry_Fails(t *testing.T) {
	_, failed := runDemoChild(t, demoNoContribution)
	if !failed {
		t.Fatal("Conformance accepted a module whose Contributions() has no demo.seed entry")
	}
}

func TestConformance_DemoSeed_CreatesNothing_FailsTheRule(t *testing.T) {
	out, failed := runDemoChild(t, demoCreatesNothing)
	if !failed {
		t.Fatalf("a provider that creates nothing passed:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL: TestDemoConformanceHelper/demoseed_provider_valid") {
		t.Errorf("the failure is not in demoseed_provider_valid:\n%s", out)
	}
}

func TestConformance_DemoSeed_ProviderReturnsPermission_FailsTheRule(t *testing.T) {
	out, failed := runDemoChild(t, demoPermission)
	if !failed {
		t.Fatalf("a provider that returns Permission passed:\n%s", out)
	}
	if !strings.Contains(out, "--- FAIL: TestDemoConformanceHelper/demoseed_provider_valid") {
		t.Errorf("the failure is not in demoseed_provider_valid:\n%s", out)
	}
}

func TestConformance_DemoSeed_ProviderOtherError_FailsTheRule(t *testing.T) {
	out, failed := runDemoChild(t, demoOtherError)
	if !failed {
		t.Fatalf("a provider that fails passed:\n%s", out)
	}
}

// IsCoreArea: demo joins the reserved core areas (F66 amendment).
func TestIsCoreArea_Demo(t *testing.T) {
	if !module.IsCoreArea("demo") {
		t.Error(`IsCoreArea("demo") = false, want true`)
	}
}
