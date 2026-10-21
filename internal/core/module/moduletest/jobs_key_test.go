package moduletest_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Rule jobs_key_not_person (jobs.md Q-JOBS13, module-contract): a job a module enqueues
// never carries a seeded person's ID in its key. The failing module runs in a child process.

const keyHelperEnv = "BMS_MODULETEST_JOBKEY_MUTANT"

type keyMutant string

const (
	keyRecord keyMutant = "record" // key names a record's own ID
	keyPerson keyMutant = "person" // key is "person:<the seeded person's ID>"
)

type keyModule struct {
	mu   keyMutant
	deps module.Deps

	lock    sync.Mutex
	started bool
	seeded  map[model.PersonID]bool
}

func keyFactory(mu keyMutant) module.Factory {
	return func(_ context.Context, deps module.Deps) (module.Module, error) {
		return &keyModule{mu: mu, deps: deps, seeded: map[model.PersonID]bool{}}, nil
	}
}

func keySeed(_ context.Context, mod module.Module, s moduletest.Subject) error {
	m, ok := mod.(*keyModule)
	if !ok {
		return errs.E("keySeed", errs.Internal, os.ErrInvalid)
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	m.seeded[s.Person] = true
	return nil
}

func (m *keyModule) Start(_ context.Context) error {
	m.lock.Lock()
	m.started = true
	m.lock.Unlock()
	return m.deps.Jobs.Register(jobs.Decl{ //nolint:wrapcheck // test module
		Name: "alpha.remind", Handler: func(context.Context, jobs.Job) error { return nil },
	})
}

func (m *keyModule) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errs.E("alpha.Stop", errs.KindOf(err), err)
	}
	return nil
}

func (m *keyModule) ExportPerson(ctx context.Context, p model.PersonID) (module.Export, error) {
	if p.IsZero() {
		return module.Export{}, errs.E("alpha.ExportPerson", errs.Invalid, os.ErrInvalid)
	}
	m.lock.Lock()
	has, started := m.seeded[p], m.started
	m.lock.Unlock()
	if !has {
		return module.Export{Data: json.RawMessage(`{}`)}, nil
	}
	if started {
		key := "signup-0001"
		if m.mu == keyPerson {
			key = "person:" + p.String()
		}
		if _, err := m.deps.Jobs.Enqueue(ctx, jobs.Request{Name: "alpha.remind", Key: key, RunAt: time.Time{}}); err != nil {
			return module.Export{}, err //nolint:wrapcheck // test module
		}
	}
	return module.Export{Data: json.RawMessage(`{"reminders":1}`)}, nil
}

func (m *keyModule) DeletePerson(ctx context.Context, p model.PersonID) (module.Deletion, error) {
	if p.IsZero() {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.Invalid, os.ErrInvalid)
	}
	if _, err := m.deps.DB.Exec(ctx, "DELETE FROM alpha.note WHERE person_id = $1", [16]byte(p)); err != nil {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.KindOf(err), err)
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	if !m.seeded[p] {
		return module.Deletion{}, nil
	}
	delete(m.seeded, p)
	return module.Deletion{Deleted: 1}, nil
}

func TestConformanceJobKeyHelper(t *testing.T) {
	name := os.Getenv(keyHelperEnv)
	if name == "" {
		t.Skip("child-process helper")
	}
	moduletest.Conformance(t, jobsManifest(), keyFactory(keyMutant(name)), jobsProvision(jobsNone), moduletest.WithSeed(keySeed))
}

func runKeyChild(t *testing.T, mu keyMutant) (string, bool) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestConformanceJobKeyHelper$", "-test.v") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), keyHelperEnv+"="+string(mu))
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr != nil
}

func TestConformance_JobsKeyNamesARecord_PassesJobsKeyNotPerson(t *testing.T) {
	out, failed := runKeyChild(t, keyRecord)
	if failed {
		t.Fatalf("a module keying its jobs by a record ID failed Conformance:\n%s", out)
	}
	if !regexp.MustCompile(`--- PASS: .*/jobs_key_not_person\b`).MatchString(out) {
		t.Errorf("no passing subtest named jobs_key_not_person in:\n%s", out)
	}
}

func TestConformance_JobsKeyCarriesPersonMarker_Fails(t *testing.T) {
	out, failed := runKeyChild(t, keyPerson)
	if !failed {
		t.Fatalf("Conformance accepted a module that keys a job by a person ID:\n%s", out)
	}
	// The rule must be the one that fails, not an unrelated one.
	if !regexp.MustCompile(`--- FAIL: .*/jobs_key_not_person\b`).MatchString(out) {
		t.Errorf("no failing subtest named jobs_key_not_person in:\n%s", out)
	}
	if strings.Contains(out, "--- FAIL: ") && regexp.MustCompile(`--- FAIL: .*/(seeded_|start_|export_)`).MatchString(out) {
		t.Errorf("an unrelated rule failed:\n%s", out)
	}
}
