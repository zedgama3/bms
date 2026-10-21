//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package moduletest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The files part of Conformance (files.md, Test plan "Consumers' tests"): a nil Deps.Files is
// filled with filestest.NewFake(manifest.Name); the leak rule scans the fake's Files() and
// Staged() names for the seed markers; and a module's seeded delete passes only when its
// DeletePerson left no file whose subject is the person. A failing Conformance fails its own
// test, so the broken modules run in a child process as in jobs_conformance_test.go.

const filesHelperEnv = "BMS_MODULETEST_FILES_MUTANT"

type filesMutant string

const (
	filesGood      filesMutant = "good"       // attaches a clean file about the subject; DeletePerson removes it
	filesLeakName  filesMutant = "leak_name"  // names the file after the subject's legal name
	filesLeakStage filesMutant = "leak_stage" // stages a file named after the legal name and never attaches it
	filesKeepsFile filesMutant = "keeps_file" // DeletePerson leaves the subject's file behind
	filesNoUse     filesMutant = "no_use"     // never touches Deps.Files
)

type filesModule struct {
	mu   filesMutant
	deps module.Deps
	rec  audit.Ref
	own  bool // the no_use mutant's own row about the subject, which has nothing to do with files
}

type thingTag struct{}

func filesFactory(mu filesMutant) module.Factory {
	return func(_ context.Context, deps module.Deps) (module.Module, error) {
		if deps.Files == nil {
			return nil, errs.E("alpha.New", errs.Invalid, errors.New("Deps.Files is nil; Conformance must supply filestest.NewFake"))
		}
		return &filesModule{mu: mu, deps: deps}, nil
	}
}

func (m *filesModule) Start(context.Context) error { return nil }
func (m *filesModule) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errs.E("alpha.Stop", errs.KindOf(err), err)
	}
	return nil
}

var tinyPDF = []byte("%PDF-1.4\n% invented\ntrailer\n%%EOF\n")

func (m *filesModule) seed(ctx context.Context, s moduletest.Subject) error {
	if m.mu == filesNoUse {
		m.own = true
		return nil
	}
	name := "certificate.pdf"
	if m.mu == filesLeakName || m.mu == filesLeakStage {
		name = s.LegalName + ".pdf"
	}
	st, err := m.deps.Files.Stage(ctx, files.Upload{Name: name, Accept: files.Documents, Body: bytes.NewReader(tinyPDF)})
	if err != nil {
		return err //nolint:wrapcheck // test module
	}
	if m.mu == filesLeakStage {
		return nil
	}
	m.rec = audit.RefOf("alpha.thing", model.NewID[model.ID[thingTag]]())
	return m.deps.DB.Transact(ctx, db.Write, func(ctx context.Context) error {
		_, err := m.deps.Files.Attach(ctx, st, files.Attachment{Record: m.rec, Subject: s.Person, Level: model.SensitivityPersonal})
		return err //nolint:wrapcheck // test module
	})
}

func (m *filesModule) ExportPerson(ctx context.Context, p model.PersonID) (module.Export, error) {
	if p.IsZero() {
		return module.Export{}, errs.E("alpha.ExportPerson", errs.Invalid, os.ErrInvalid)
	}
	if m.own {
		return module.Export{Data: json.RawMessage(`{"notes":1}`)}, nil
	}
	if m.rec.IsZero() {
		return module.Export{Data: json.RawMessage(`{}`)}, nil
	}
	fs, err := m.deps.Files.ByRecord(ctx, m.rec)
	if err != nil || len(fs) == 0 {
		return module.Export{Data: json.RawMessage(`{}`)}, err //nolint:wrapcheck // test module
	}
	return module.Export{Data: json.RawMessage(`{"files":1}`)}, nil
}

func (m *filesModule) DeletePerson(ctx context.Context, p model.PersonID) (module.Deletion, error) {
	if p.IsZero() {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.Invalid, os.ErrInvalid)
	}
	if _, err := m.deps.DB.Exec(ctx, "DELETE FROM alpha.note WHERE person_id = $1", [16]byte(p)); err != nil {
		return module.Deletion{}, errs.E("alpha.DeletePerson", errs.KindOf(err), err)
	}
	if m.own {
		m.own = false
		return module.Deletion{Deleted: 1}, nil
	}
	if m.rec.IsZero() {
		return module.Deletion{}, nil
	}
	if m.mu == filesKeepsFile {
		return module.Deletion{Deleted: 1}, nil // claims success, leaves the file
	}
	n := 0
	err := m.deps.DB.Transact(ctx, db.Write, func(ctx context.Context) error {
		var derr error
		n, derr = m.deps.Files.DeleteByRecord(ctx, m.rec)
		return derr //nolint:wrapcheck // test module
	})
	return module.Deletion{Deleted: n}, err
}

func filesProvision(man module.Manifest) (module.Deps, error) {
	sig, err := signalstest.NewFake(man.Name, man.Publishes, man.Subscribes)
	if err != nil {
		return module.Deps{}, err //nolint:wrapcheck // test helper
	}
	return module.Deps{DB: dbtest.NewFake(), Signals: sig}, nil // Deps.Files left nil on purpose
}

func runFilesConformance(t *testing.T, mu filesMutant) {
	t.Helper()
	factory := func(ctx context.Context, deps module.Deps) (module.Module, error) {
		return filesFactory(mu)(ctx, deps)
	}
	seed := func(ctx context.Context, mod module.Module, s moduletest.Subject) error {
		fm, ok := mod.(*filesModule)
		if !ok {
			return errs.E("alpha.seed", errs.Internal, errors.New("not a filesModule"))
		}
		return fm.seed(ctx, s)
	}
	moduletest.Conformance(t, module.Manifest{Name: "alpha"}, factory,
		func(_ context.Context, man module.Manifest) (module.Deps, error) { return filesProvision(man) },
		moduletest.WithSeed(seed), moduletest.WithoutViews())
}

func TestConformanceFilesHelper(t *testing.T) {
	name := os.Getenv(filesHelperEnv)
	if name == "" {
		t.Skip("child-process helper")
	}
	runFilesConformance(t, filesMutant(name))
}

func runFilesChild(t *testing.T, mu filesMutant) (string, bool) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestConformanceFilesHelper$", "-test.v") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), filesHelperEnv+"="+string(mu))
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr != nil
}

func TestConformance_NilDepsFiles_BecomesScopedFake(t *testing.T) {
	// Provision leaves Deps.Files nil; the module's New refuses a nil, and its seed attaches to
	// a record in its own area "alpha", which only a fake scoped to manifest.Name accepts.
	out, failed := runFilesChild(t, filesGood)
	if failed {
		t.Fatalf("a module using Deps.Files failed Conformance:\n%s", out)
	}
	if !regexp.MustCompile(`--- PASS: .*/seeded_person_delete_counts_and_is_idempotent\b`).MatchString(out) {
		t.Errorf("the seeded delete rule did not run and pass in:\n%s", out)
	}
}

func TestConformance_ModuleNotUsingFiles_StillPasses(t *testing.T) {
	out, failed := runFilesChild(t, filesNoUse)
	if failed {
		t.Fatalf("Conformance failed for a module that ignores Deps.Files:\n%s", out)
	}
}

func TestConformance_FileNamedAfterTheSubject_FailsTheLeakRule(t *testing.T) {
	for _, mu := range []filesMutant{filesLeakName, filesLeakStage} {
		t.Run(string(mu), func(t *testing.T) {
			out, failed := runFilesChild(t, mu)
			if !failed {
				t.Fatalf("Conformance accepted a file named after the subject's legal name:\n%s", out)
			}
			if !regexp.MustCompile(`--- FAIL: `).MatchString(out) {
				t.Errorf("no failing rule in:\n%s", out)
			}
		})
	}
}

func TestConformance_DeletePersonLeavesSubjectFile_FailsSeededDelete(t *testing.T) {
	out, failed := runFilesChild(t, filesKeepsFile)
	if !failed {
		t.Fatalf("Conformance accepted a DeletePerson that left the subject's file behind:\n%s", out)
	}
}
