package moduletest_test

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// These tests cover the jobs part of Conformance (jobs.md, Test plan: the rule
// jobs_register_scoped, and a nil Deps.Jobs becoming jobstest.NewFake(manifest.Name)). A
// failing Conformance fails its test, so the broken modules run in a child process, as in
// conformance_test.go.

const jobsHelperEnv = "BMS_MODULETEST_JOBS_MUTANT"

type jobsMutant string

const (
	jobsNone      jobsMutant = "none"       // registers nothing
	jobsOwn       jobsMutant = "own"        // registers alpha.sweep (and enqueues it)
	jobsForeign   jobsMutant = "foreign"    // registers beta.sweep
	jobsProvided  jobsMutant = "provided"   // Provision hands out a jobstest fake itself
	jobsNeedsDeps jobsMutant = "needs_deps" // New fails unless Deps.Jobs is non-nil
)

type jobsModule struct {
	mu   jobsMutant
	deps module.Deps
}

func jobsFactory(mu jobsMutant) module.Factory {
	return func(_ context.Context, deps module.Deps) (module.Module, error) {
		if mu == jobsNeedsDeps && deps.Jobs == nil {
			return nil, errs.E("alpha.New", errs.Invalid, os.ErrInvalid)
		}
		return &jobsModule{mu: mu, deps: deps}, nil
	}
}

func (m *jobsModule) Start(_ context.Context) error {
	h := func(context.Context, jobs.Job) error { return nil }
	switch m.mu {
	case jobsOwn, jobsProvided, jobsNeedsDeps:
		if err := m.deps.Jobs.Register(jobs.Decl{Name: "alpha.sweep", Every: time.Hour, Handler: h}); err != nil {
			return err //nolint:wrapcheck // test module
		}
	case jobsForeign:
		if err := m.deps.Jobs.Register(jobs.Decl{Name: "beta.sweep", Every: time.Hour, Handler: h}); err != nil {
			return err //nolint:wrapcheck // test module
		}
	}
	return nil
}

func (m *jobsModule) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errs.E("alpha.Stop", errs.KindOf(err), err)
	}
	return nil
}

func (m *jobsModule) ExportPerson(_ context.Context, p model.PersonID) (module.Export, error) {
	if p.IsZero() {
		return module.Export{}, errs.E("alpha.ExportPerson", errs.Invalid, os.ErrInvalid)
	}
	return module.Export{}, nil
}

func (m *jobsModule) DeletePerson(ctx context.Context, p model.PersonID) (module.Deletion, error) {
	if p.IsZero() {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.Invalid, os.ErrInvalid)
	}
	// A write, so a read-only copy refuses it with Unavailable wrapping db.ErrReadOnly.
	if _, err := m.deps.DB.Exec(ctx, "DELETE FROM alpha.note WHERE person_id = $1", [16]byte(p)); err != nil {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.KindOf(err), err)
	}
	return module.Deletion{}, nil
}

func jobsProvision(mu jobsMutant) module.Provision {
	return func(_ context.Context, man module.Manifest) (module.Deps, error) {
		sig, err := signalstest.NewFake(man.Name, man.Publishes, man.Subscribes)
		if err != nil {
			return module.Deps{}, err //nolint:wrapcheck // test helper
		}
		deps := module.Deps{DB: dbtest.NewFake(), Signals: sig}
		if mu == jobsProvided {
			deps.Jobs = jobstest.NewFake(man.Name)
		}
		return deps, nil
	}
}

func jobsManifest() module.Manifest { return module.Manifest{Name: "alpha"} }

func runJobsConformance(t *testing.T, mu jobsMutant) {
	t.Helper()
	moduletest.Conformance(t, jobsManifest(), jobsFactory(mu), jobsProvision(mu), moduletest.WithoutPersonData())
}

// TestConformanceJobsHelper is re-run in a child process by the tests below.
func TestConformanceJobsHelper(t *testing.T) {
	name := os.Getenv(jobsHelperEnv)
	if name == "" {
		t.Skip("child-process helper")
	}
	runJobsConformance(t, jobsMutant(name))
}

func runJobsChild(t *testing.T, mu jobsMutant) (string, bool) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestConformanceJobsHelper$", "-test.v") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), jobsHelperEnv+"="+string(mu))
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr != nil
}

func TestConformance_ModuleWithoutJobs_Passes(t *testing.T) {
	runJobsConformance(t, jobsNone)
}

func TestConformance_NilDepsJobs_BecomesFakeAndOwnPrefixPasses(t *testing.T) {
	// Provision leaves Deps.Jobs nil; Conformance supplies jobstest.NewFake(manifest.Name),
	// so a module that registers in Start with its own prefix builds, starts and passes.
	runJobsConformance(t, jobsOwn)
}

func TestConformance_NilDepsJobs_ModuleThatNeedsJobsStillBuilds(t *testing.T) {
	runJobsConformance(t, jobsNeedsDeps)
}

func TestConformance_JobsRegisterScoped_RuleRunsAndPasses(t *testing.T) {
	out, failed := runJobsChild(t, jobsOwn)
	if failed {
		t.Fatalf("a module registering its own prefix failed Conformance:\n%s", out)
	}
	if !regexp.MustCompile(`--- PASS: .*/jobs_register_scoped\b`).MatchString(out) {
		t.Errorf("no passing subtest named jobs_register_scoped in:\n%s", out)
	}
}

func TestConformance_JobsRegisterScoped_RuleRunsForAModuleWithoutJobs(t *testing.T) {
	out, failed := runJobsChild(t, jobsNone)
	if failed {
		t.Fatalf("Conformance failed:\n%s", out)
	}
	if !regexp.MustCompile(`--- PASS: .*/jobs_register_scoped\b`).MatchString(out) {
		t.Errorf("jobs_register_scoped did not run (trivially passing) in:\n%s", out)
	}
}

func TestConformance_RegistersAnotherModulesJob_FailsJobsRegisterScoped(t *testing.T) {
	out, failed := runJobsChild(t, jobsForeign)
	if !failed {
		t.Fatalf("Conformance accepted a module registering beta.sweep as alpha:\n%s", out)
	}
	// Either the rule itself or the fake's scope check inside Start must name the failure;
	// the rule is the one the spec promises.
	if !regexp.MustCompile(`--- FAIL: .*/(jobs_register_scoped|start_read_only)\b`).MatchString(out) {
		t.Errorf("no failing scoped-registration rule in:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("failure does not name the module:\n%s", out)
	}
}

func TestConformance_ProvidedJobsFake_IsKept(t *testing.T) {
	runJobsConformance(t, jobsProvided)
}
