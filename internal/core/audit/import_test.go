package audit_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// spoolLine builds one complete spool line by hand, from the spec's "Spool line" section.
func spoolLine(action string) string {
	id := model.NewID[audit.RecordID]()
	return fmt.Sprintf(`{"v":1,"id":%q,"at":"2026-10-17T12:00:00.123456789Z","instance":"other-copy","action":%q,"outcome":"succeeded","actor":{"kind":"person","person":%q}}`,
		id.String(), action, alice.String()) + "\n"
}

func goodLines(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(spoolLine("identity.login"))
	}
	return b.String()
}

// putSpool writes a file into the fixture's spool directory.
func (f *fx) putSpool(t testing.TB, name, content string) string {
	t.Helper()
	wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o700))
	p := filepath.Join(f.spoolDir(), name)
	wantNoErr(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestImportOnce_ReadOnly_ReturnsErrReadOnly(t *testing.T) {
	f := newFx(t)
	p := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(2))
	f.readOnly()
	got, err := f.log.ImportOnce(ctxT(t))
	wantReadOnly(t, err)
	if got.Files != 0 || got.Entries != 0 {
		t.Errorf("imported %+v on a read-only copy", got)
	}
	if !exists(p) {
		t.Error("a file was touched on a read-only copy")
	}
	if n := len(f.db.Calls()); n != 0 {
		t.Errorf("statements = %d", n)
	}
}

func TestImportOnce_MissingDirectory_IsNotAnError(t *testing.T) {
	f := newFx(t)
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 0 || got.Entries != 0 || len(got.Failed) != 0 {
		t.Errorf("Imported = %+v, want zero", got)
	}
}

func TestImportOnce_ImportsInBatchesOfMaxBatch_AndDeletes(t *testing.T) {
	f := newFx(t)
	small := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(3))
	big := f.putSpool(t, "0002-1-bbbbbbbb.jsonl", goodLines(2*audit.MaxBatch+500))
	ignored := f.putSpool(t, "0003-1-cccccccc.jsonl.failed", "not touched\n")
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 2 {
		t.Errorf("Files = %d, want 2", got.Files)
	}
	total := 3 + 2*audit.MaxBatch + 500
	if got.Entries+got.Duplicates != total {
		t.Errorf("Entries+Duplicates = %d, want %d", got.Entries+got.Duplicates, total)
	}
	if exists(small) || exists(big) {
		t.Error("imported files were not deleted")
	}
	if !exists(ignored) {
		t.Error("a .failed file from an earlier run was touched")
	}
	calls := f.db.Calls()
	if len(calls) != 4 { // 3 + (1000, 1000, 500)
		t.Fatalf("statements = %d, want 4 (one per batch)", len(calls))
	}
	for _, c := range calls {
		if !c.InTx {
			t.Error("a batch ran outside a Write transaction")
		}
		if s := strings.ToLower(c.SQL); !strings.Contains(s, "on conflict") || !strings.Contains(s, "do nothing") {
			t.Error("the insert lacks ON CONFLICT ... DO NOTHING")
		}
	}
}

func TestImportOnce_BadLine_RenamesFailed_ContinuesOthers(t *testing.T) {
	good := spoolLine("identity.login")
	bad := map[string]string{
		"not json":          "this is not json " + marker + "\n",
		"other version":     strings.Replace(spoolLine("identity.login"), `"v":1`, `"v":2`, 1),
		"no version":        strings.Replace(spoolLine("identity.login"), `"v":1,`, ``, 1),
		"bad action":        spoolLine("Identity.Login"),
		"bad outcome":       strings.Replace(spoolLine("identity.login"), `"succeeded"`, `"bogus"`, 1),
		"zero outcome":      strings.Replace(spoolLine("identity.login"), `"outcome":"succeeded",`, ``, 1),
		"missing id":        strings.Replace(spoolLine("identity.login"), `"id":`, `"idx":`, 1),
		"person w/o person": strings.Replace(spoolLine("identity.login"), `,"person":"`+alice.String()+`"`, ``, 1),
		"unknown actor":     strings.Replace(spoolLine("identity.login"), `"kind":"person"`, `"kind":"wizard"`, 1),
		"fields no level":   strings.Replace(spoolLine("identity.login"), `"actor"`, `"fields":["email"],"actor"`, 1),
	}
	for name, line := range bad {
		t.Run(name, func(t *testing.T) {
			f := newFx(t)
			a := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", good+good)
			b := f.putSpool(t, "0002-1-bbbbbbbb.jsonl", good+line+good)
			c := f.putSpool(t, "0003-1-cccccccc.jsonl", good)
			got, err := f.log.ImportOnce(ctxT(t))
			wantKind(t, err, errs.Invalid)
			noMarker(t, err)
			if len(got.Failed) != 1 || got.Failed[0] != "0002-1-bbbbbbbb.jsonl" {
				t.Fatalf("Failed = %v, want the one bad file's name", got.Failed)
			}
			for _, n := range got.Failed {
				if strings.Contains(n, string(os.PathSeparator)) {
					t.Errorf("Failed holds a path, want the name only: %q", n)
				}
			}
			if exists(a) || exists(c) {
				t.Error("the good files around the bad one were not imported and deleted")
			}
			if exists(b) || !exists(b+".failed") {
				t.Error("the bad file was not renamed to .failed")
			}
			if got.Files < 2 {
				t.Errorf("Files = %d, want the two good files at least", got.Files)
			}
			// The kept .failed file is not retried: a second run is clean.
			again, err := f.log.ImportOnce(ctxT(t))
			wantNoErr(t, err)
			if again.Files != 0 || len(again.Failed) != 0 {
				t.Errorf("second run = %+v", again)
			}
		})
	}
}

func TestImportOnce_TruncatedTail_DroppedAndCounted(t *testing.T) {
	t.Run("two lines and a tail", func(t *testing.T) {
		f := newFx(t)
		tail := strings.TrimSuffix(spoolLine("identity.login"), "\n")
		p := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(2)+tail[:len(tail)/2])
		got, err := f.log.ImportOnce(ctxT(t))
		wantNoErr(t, err)
		if got.Truncated != 1 {
			t.Errorf("Truncated = %d, want 1", got.Truncated)
		}
		if got.Entries+got.Duplicates != 2 {
			t.Errorf("Entries+Duplicates = %d, want the 2 complete lines", got.Entries+got.Duplicates)
		}
		if exists(p) || len(f.failedFiles(t)) != 0 {
			t.Error("a truncated tail must delete the file, not fail it")
		}
	})
	t.Run("only a tail", func(t *testing.T) {
		f := newFx(t)
		p := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", `{"v":1,"id":"`)
		got, err := f.log.ImportOnce(ctxT(t))
		wantNoErr(t, err)
		if got.Truncated != 1 || got.Entries != 0 {
			t.Errorf("Imported = %+v", got)
		}
		if exists(p) {
			t.Error("file kept")
		}
		if n := len(f.db.Calls()); n != 0 {
			t.Errorf("statements = %d for a file with no complete line", n)
		}
	})
	t.Run("a complete tail ending in newline is not truncated", func(t *testing.T) {
		f := newFx(t)
		f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(1))
		got, err := f.log.ImportOnce(ctxT(t))
		wantNoErr(t, err)
		if got.Truncated != 0 {
			t.Errorf("Truncated = %d", got.Truncated)
		}
	})
}

func TestImportOnce_InsertFails_KeepsFile_RerunIdempotent(t *testing.T) {
	flaky := &flakyDB{}
	f := newFx(t, func(o *audit.Options) { o.DB = flaky })
	flaky.Fake = f.db
	a := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(2))
	b := f.putSpool(t, "0002-1-bbbbbbbb.jsonl", goodLines(2))
	flaky.setFailExec(errs.E("test", errs.Unavailable, errors.New("connection lost")))
	_, err := f.log.ImportOnce(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !exists(a) || !exists(b) {
		t.Fatal("a file was deleted though its insert failed")
	}
	if len(f.failedFiles(t)) != 0 {
		t.Error("a database failure must not turn a file into .failed")
	}
	flaky.setFailExec(nil)
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 2 || exists(a) || exists(b) {
		t.Errorf("rerun: %+v", got)
	}
}

func TestImportOnce_Unreadable_ReturnsUnavailable(t *testing.T) {
	t.Run("spool path is a file", func(t *testing.T) {
		f := newFx(t)
		wantNoErr(t, os.MkdirAll(filepath.Dir(f.spoolDir()), 0o700))
		wantNoErr(t, os.WriteFile(f.spoolDir(), []byte("x"), 0o600))
		_, err := f.log.ImportOnce(ctxT(t))
		wantKind(t, err, errs.Unavailable)
	})
	t.Run("file unreadable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads any file")
		}
		f := newFx(t)
		p := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(1))
		wantNoErr(t, os.Chmod(p, 0))
		t.Cleanup(func() { _ = os.Chmod(p, 0o600) }) //nolint:errcheck // best-effort restore
		_, err := f.log.ImportOnce(ctxT(t))
		wantKind(t, err, errs.Unavailable)
		if !exists(p) {
			t.Error("an unreadable file was removed")
		}
	})
}

func TestImportOnce_CtxDone_ReturnsCanceled(t *testing.T) {
	f := newFx(t)
	p := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := f.log.ImportOnce(ctx)
	wantKind(t, err, errs.Canceled)
	if !exists(p) {
		t.Error("file removed on a canceled import")
	}
}

func TestImportOnce_SkipsNonJSONLFiles(t *testing.T) {
	f := newFx(t)
	other := f.putSpool(t, "notes.txt", "hello\n")
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 0 || !exists(other) {
		t.Errorf("Imported = %+v; unrelated file exists = %v", got, exists(other))
	}
}

func TestImportOnce_Concurrent_IsSerialized(t *testing.T) {
	f := newFx(t)
	f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(10))
	f.putSpool(t, "0002-1-bbbbbbbb.jsonl", goodLines(10))
	results := make(chan audit.Imported, 8)
	runConcurrent(t, 8, func() error {
		got, err := f.log.ImportOnce(t.Context())
		results <- got
		if err != nil {
			return fmt.Errorf("ImportOnce: %w", err)
		}
		return nil
	})
	close(results)
	files := 0
	for r := range results {
		files += r.Files
	}
	if files != 2 {
		t.Errorf("files imported across 8 concurrent calls = %d, want 2 (each file once)", files)
	}
	if len(f.spoolFiles(t)) != 0 {
		t.Error("files left behind")
	}
}

// --- Run ----------------------------------------------------------------------------------

func startRun(t *testing.T, l *audit.Log) (cancel func(), done <-chan error) {
	t.Helper()
	ctx, c := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() { ch <- l.Run(ctx) }()
	t.Cleanup(c)
	return c, ch
}

func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	return <-done
}

func TestRun_DirUncreatable_ReturnsInvalid(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	wantNoErr(t, os.WriteFile(blocker, []byte("x"), 0o600))
	f := newFx(t, func(o *audit.Options) { o.DataDir = blocker })
	err := f.log.Run(ctxT(t))
	wantKind(t, err, errs.Invalid)
	wantField(t, err, "data.dir")
}

func TestRun_CtxDone_ReturnsNil(t *testing.T) {
	f := newFx(t)
	cancel, done := startRun(t, f.log)
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1)) // the ticker exists once the first import is done
	st, err := os.Stat(f.spoolDir())
	wantNoErr(t, err)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("spool dir mode = %v, want 0700", st.Mode().Perm())
	}
	cancel()
	wantNoErr(t, waitRun(t, done))
	if err := f.clk.BlockUntil(ctxT(t), 0); err != nil {
		t.Error("Run left its ticker running")
	}
}

func TestRun_ImportsAtStart_AndOnEveryTick(t *testing.T) {
	f := newFx(t)
	atStart := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(2))
	cancel, done := startRun(t, f.log)
	<-f.imported // the import at start has logged: its files are gone
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1))
	if exists(atStart) {
		t.Error("Run did not import once at start")
	}
	later := f.putSpool(t, "0002-1-bbbbbbbb.jsonl", goodLines(2))
	drain(f.imported)
	wantNoErr(t, f.clk.Advance(audit.DefaultImportInterval))
	<-f.imported
	if exists(later) {
		t.Error("the tick did not import the new file")
	}
	cancel()
	wantNoErr(t, waitRun(t, done))
}

func TestRun_UsesImportInterval(t *testing.T) {
	f := newFx(t, func(o *audit.Options) { o.ImportInterval = 5 * 1e9 })
	cancel, done := startRun(t, f.log)
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1))
	later := f.putSpool(t, "0002-1-bbbbbbbb.jsonl", goodLines(1))
	drain(f.imported)
	wantNoErr(t, f.clk.Advance(5*1e9))
	<-f.imported
	if exists(later) {
		t.Error("the tick did not import the new file")
	}
	cancel()
	wantNoErr(t, waitRun(t, done))
}

func TestRun_ReadOnlyCopy_KeepsFiles_AndKeepsRunning(t *testing.T) {
	f := newFx(t)
	p := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(2))
	f.readOnly()
	cancel, done := startRun(t, f.log)
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1)) // the failed first import did not stop Run
	wantNoErr(t, f.clk.Advance(audit.DefaultImportInterval))
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1))
	if !exists(p) {
		t.Error("a read-only copy imported a file")
	}
	// The copy becomes writable: the next tick imports.
	f.db.SetReadOnly(false)
	drain(f.imported)
	wantNoErr(t, f.clk.Advance(audit.DefaultImportInterval))
	<-f.imported
	if exists(p) {
		t.Error("the tick after the copy could write did not import")
	}
	cancel()
	wantNoErr(t, waitRun(t, done))
}

func TestRun_Twice_ReturnsConflict(t *testing.T) {
	f := newFx(t)
	cancel, done := startRun(t, f.log)
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1))
	err := f.log.Run(ctxT(t))
	wantKind(t, err, errs.Conflict)
	cancel()
	wantNoErr(t, waitRun(t, done))
}

func TestRun_AfterClose_ReturnsInternal(t *testing.T) {
	f := newFx(t)
	wantNoErr(t, f.log.Close(ctxT(t)))
	wantKind(t, f.log.Run(ctxT(t)), errs.Internal)
}

func TestClose_StopsRun_AndWaitsForIt(t *testing.T) {
	f := newFx(t)
	_, done := startRun(t, f.log)
	wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1))
	wantNoErr(t, f.log.Close(ctxT(t)))
	select {
	case err := <-done:
		wantNoErr(t, err)
	default:
		t.Fatal("Close returned while Run was still running")
	}
}

func TestImportOnce_OversizedLine_MarksFileFailed(t *testing.T) {
	f := newFx(t)
	huge := strings.Replace(spoolLine("identity.login"), `"instance"`, `"pad":"`+strings.Repeat("x", 70*1024)+`","instance"`, 1)
	a := f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(1))
	b := f.putSpool(t, "0002-1-bbbbbbbb.jsonl", goodLines(1)+huge+goodLines(1))
	c := f.putSpool(t, "0003-1-cccccccc.jsonl", goodLines(1))
	got, err := f.log.ImportOnce(ctxT(t))
	wantKind(t, err, errs.Invalid)
	if len(got.Failed) != 1 || got.Failed[0] != "0002-1-bbbbbbbb.jsonl" {
		t.Fatalf("Failed = %v, want the oversized file", got.Failed)
	}
	if exists(b) || !exists(b+".failed") {
		t.Error("the oversized file was not renamed to .failed")
	}
	if exists(a) || exists(c) {
		t.Error("the other files were not imported")
	}
}

func TestRun_TightensAnExistingSpoolDirectoryTo0700(t *testing.T) {
	t.Run("Run", func(t *testing.T) {
		f := newFx(t)
		wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o755))            //nolint:gosec // a loose pre-existing directory is the case under test
		wantNoErr(t, os.Chmod(f.spoolDir(), 0o755))               //nolint:gosec // the umask may have narrowed it
		wantNoErr(t, os.Chmod(filepath.Dir(f.spoolDir()), 0o755)) //nolint:gosec // same
		cancel, done := startRun(t, f.log)
		wantNoErr(t, f.clk.BlockUntil(ctxT(t), 1))
		st, err := os.Stat(f.spoolDir())
		wantNoErr(t, err)
		if st.Mode().Perm() != 0o700 {
			t.Errorf("spool dir mode = %v, want 0700", st.Mode().Perm())
		}
		cancel()
		wantNoErr(t, waitRun(t, done))
	})
	t.Run("first spool write", func(t *testing.T) {
		f := newFx(t)
		wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o755)) //nolint:gosec // a loose pre-existing directory is the case under test
		wantNoErr(t, os.Chmod(f.spoolDir(), 0o755))    //nolint:gosec // the umask may have narrowed it
		f.readOnly()
		wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
		st, err := os.Stat(f.spoolDir())
		wantNoErr(t, err)
		if st.Mode().Perm() != 0o700 {
			t.Errorf("spool dir mode = %v, want 0700", st.Mode().Perm())
		}
	})
}

func TestClose_WhileImportRunning_ReturnsPromptly(t *testing.T) {
	flaky := &flakyDB{entered: make(chan struct{}, 1)}
	f := newFx(t, func(o *audit.Options) { o.DB = flaky })
	flaky.Fake = f.db
	f.putSpool(t, "0001-1-aaaaaaaa.jsonl", goodLines(2))
	_, done := startRun(t, f.log) // Run's own context is never canceled by the test
	<-flaky.entered               // the first import is inside its insert and blocked
	closed := make(chan error, 1)
	go func() { closed <- f.log.Close(ctxT(t)) }()
	wantNoErr(t, <-closed) // Close interrupts the import instead of waiting for it
	wantNoErr(t, waitRun(t, done))
}
