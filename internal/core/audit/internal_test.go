package audit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// readOnlyLog builds a Log over a read-only fake database, so Record goes to the spool.
func readOnlyLog(t *testing.T) *Log {
	t.Helper()
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	c := telemetrytest.New(t)
	l, err := New(Options{
		DB:       fake,
		Clock:    clock.NewFake(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)),
		Logger:   c.Logger(),
		Tracer:   c.Tracer("audit"),
		Instance: "test-copy",
		DataDir:  t.TempDir(),
		Actor:    func(context.Context) (Actor, bool) { return Actor{}, false },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := l.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return l
}

func loginEntries(n int) []Entry {
	out := make([]Entry, n)
	for i := range out {
		out[i] = Entry{Action: "identity.login", Outcome: Succeeded}
	}
	return out
}

func TestSeam_EncodeFailure_RecordReturnsInternal(t *testing.T) {
	l := readOnlyLog(t)
	l.encode = func(wireRecord) ([]byte, error) { return nil, errors.New("encode refused") }
	rec, err := l.Scoped("identity")
	if err != nil {
		t.Fatalf("Scoped: %v", err)
	}
	err = rec.Record(t.Context(), loginEntries(2)...)
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("kind = %v, want Internal", errs.KindOf(err))
	}
	if _, serr := os.Stat(l.spoolDir); serr == nil {
		if entries, rerr := os.ReadDir(l.spoolDir); rerr != nil || len(entries) != 0 {
			t.Errorf("a failed encode left spool files (read error %v)", rerr)
		}
	}
}

// countingFile wraps a real file and counts Sync calls.
type countingFile struct {
	spoolFile
	mu    *sync.Mutex
	syncs *int
}

func (c countingFile) Sync() error {
	c.mu.Lock()
	*c.syncs++
	c.mu.Unlock()
	return c.spoolFile.Sync() //nolint:wrapcheck // test double passes the real error through
}

func TestSeam_SpoolSyncsOncePerRecordCall(t *testing.T) {
	l := readOnlyLog(t)
	var (
		mu    sync.Mutex
		syncs int
	)
	l.spool.open = func(path string) (spoolFile, error) {
		f, err := openExclusive(path)
		if err != nil {
			return nil, err
		}
		return countingFile{spoolFile: f, mu: &mu, syncs: &syncs}, nil
	}
	rec, err := l.Scoped("identity")
	if err != nil {
		t.Fatalf("Scoped: %v", err)
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return syncs
	}
	// A multi-entry batch is one Write and one Sync.
	if err := rec.Record(t.Context(), loginEntries(5)...); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("syncs after one 5-entry call = %d, want 1", got)
	}
	for i := 0; i < 3; i++ {
		if err := rec.Record(t.Context(), loginEntries(1)...); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if got := count(); got != 4 {
		t.Fatalf("syncs after four calls = %d, want 4", got)
	}
}

// ---- review fixes ----

// newLogOn builds a Log over d with its spool under dir.
func newLogOn(t *testing.T, d db.DB, dir string) (*Log, *telemetrytest.Capture) {
	t.Helper()
	c := telemetrytest.New(t)
	l, err := New(Options{
		DB:       d,
		Clock:    clock.NewFake(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)),
		Logger:   c.Logger(),
		Tracer:   c.Tracer("audit"),
		Instance: "test-copy",
		DataDir:  dir,
		Actor:    func(context.Context) (Actor, bool) { return Actor{}, false },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := l.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return l, c
}

// goodLine is one valid spool line.
func goodLine(t *testing.T) []byte {
	t.Helper()
	rec := Record{
		ID:       model.NewID[RecordID](),
		At:       time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		Instance: "test-copy",
		Entry:    Entry{Action: "identity.login", Outcome: Succeeded, Actor: Actor{Kind: ActorAnonymous}},
	}
	b, err := encodeWire(toWire(rec))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return append(b, '\n')
}

func writeSpoolFile(t *testing.T, l *Log, name string, parts ...[]byte) string {
	t.Helper()
	if err := ensureDir(l.spoolDir); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	path := filepath.Join(l.spoolDir, name)
	if err := os.WriteFile(path, bytes.Join(parts, nil), 0o600); err != nil {
		t.Fatalf("write spool file: %v", err)
	}
	return path
}

type fixedTitles []string

func (f fixedTitles) TitlesOf(context.Context, model.PersonID) ([]string, error) {
	return []string(f), nil
}

func TestCleanTitles_StripsControlAndFormatCharacters(t *testing.T) {
	l := readOnlyLog(t)
	l.opts.Titles = fixedTitles{"Lead\x00", "\u200b\x00", " Range\u200dr\t ", "Gate\ufffd", "\x1b[0m"}
	got, err := l.titlesOf(t.Context(), model.NewID[model.PersonID]())
	if err != nil {
		t.Fatalf("titlesOf: %v", err)
	}
	want := []string{"Lead", "Ranger", "Gate", "[0m"}
	if !slices.Equal(got, want) {
		t.Errorf("titles = %q, want %q", got, want)
	}
}

// failingSyncFile fails Sync from the nth call on.
type failingSyncFile struct {
	spoolFile
	calls  *int
	failAt int
}

func (f failingSyncFile) Sync() error {
	*f.calls++
	if *f.calls >= f.failAt {
		return errors.New("disk full")
	}
	return f.spoolFile.Sync() //nolint:wrapcheck // test double
}

func TestSpoolFailedSync_CutsTheBatchBack(t *testing.T) {
	l := readOnlyLog(t)
	calls := 0
	l.spool.open = func(path string) (spoolFile, error) {
		f, err := openExclusive(path)
		if err != nil {
			return nil, err
		}
		return failingSyncFile{spoolFile: f, calls: &calls, failAt: 2}, nil
	}
	rec, err := l.Scoped("identity")
	if err != nil {
		t.Fatalf("Scoped: %v", err)
	}
	if err := rec.Record(t.Context(), loginEntries(1)...); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := rec.Record(t.Context(), loginEntries(3)...); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("second Record kind = %v, want Unavailable", errs.KindOf(err))
	}
	l.spool.open = openExclusive
	if err := rec.Record(t.Context(), loginEntries(1)...); err != nil {
		t.Fatalf("third Record: %v", err)
	}
	// The caller was told the second batch failed, so none of its lines may be on disk.
	files, err := filepath.Glob(filepath.Join(l.spoolDir, "*"+spoolSuffix))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	lines := 0
	for _, p := range files {
		b, err := os.ReadFile(p) //nolint:gosec // a test file
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		lines += bytes.Count(b, []byte("\n"))
	}
	if lines != 2 {
		t.Errorf("spool holds %d lines, want 2 (the batch that failed must be cut out)", lines)
	}
}

func TestSpoolShutdown_RefusesLaterAppends(t *testing.T) {
	l := readOnlyLog(t)
	if err := l.spool.shutdown(true); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	err := l.spool.append(goodLine(t))
	if !errs.Is(err, errs.Unavailable) {
		t.Fatalf("append after shutdown kind = %v, want Unavailable", errs.KindOf(err))
	}
	if _, serr := os.Stat(l.spoolDir); serr == nil {
		if entries, rerr := os.ReadDir(l.spoolDir); rerr != nil || len(entries) != 0 {
			t.Errorf("a refused append opened a file (read error %v)", rerr)
		}
	}
}

func TestEnsureDir_TightensAWiderMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	if err := os.Mkdir(dir, 0o755); err != nil { //nolint:gosec // the point of the test
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // beat the umask
		t.Fatalf("chmod: %v", err)
	}
	if err := ensureDir(dir); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", st.Mode().Perm())
	}
}

func TestImport_OverlongLine_SetsFileAside(t *testing.T) {
	fake := dbtest.NewFake()
	l, c := newLogOn(t, fake, t.TempDir())
	long := bytes.Repeat([]byte("x"), maxLineBytes+10)
	writeSpoolFile(t, l, "0001-1-aaaaaaaa.jsonl", goodLine(t), append(long, '\n'), goodLine(t))
	out, err := l.ImportOnce(t.Context())
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("ImportOnce kind = %v, want Invalid", errs.KindOf(err))
	}
	if len(out.Failed) != 1 || out.Entries != 0 && out.Entries != 1 {
		t.Errorf("result = %+v", out)
	}
	if len(fake.Calls()) != 1 {
		t.Errorf("%d inserts, want 1 (the line before the long one)", len(fake.Calls()))
	}
	if _, serr := os.Stat(filepath.Join(l.spoolDir, "0001-1-aaaaaaaa.jsonl"+failedSuffix)); serr != nil {
		t.Errorf("file not set aside: %v", serr)
	}
	warned := false
	for _, r := range c.Records() {
		if r.Level == slog.LevelWarn && r.Attrs["line"] == "2" && r.Attrs["problem"] == "line too long" {
			warned = true
		}
	}
	if !warned {
		t.Error("no warning naming line 2 and the problem")
	}
}

func TestImport_BadField_WarnsWithLineAndFieldNameOnly(t *testing.T) {
	const marker = "ZQ9SECRET7"
	l, c := newLogOn(t, dbtest.NewFake(), t.TempDir())
	bad := bytes.Replace(goodLine(t), []byte(`"outcome":"succeeded"`), []byte(`"outcome":"`+marker+`"`), 1)
	writeSpoolFile(t, l, "0001-1-aaaaaaaa.jsonl", goodLine(t), goodLine(t), bad)
	if _, err := l.ImportOnce(t.Context()); !errs.Is(err, errs.Invalid) {
		t.Fatalf("ImportOnce kind = %v, want Invalid", errs.KindOf(err))
	}
	var found bool
	for _, r := range c.Records() {
		if r.Level == slog.LevelWarn && r.Attrs["line"] == "3" && strings.HasPrefix(r.Attrs["problem"], "field ") {
			found = true
		}
	}
	if !found {
		t.Error("no warning naming line 3 and a field")
	}
	if c.Contains(marker) {
		t.Error("a value from the bad line leaked into telemetry")
	}
}

// blockingDB blocks every transaction until its context ends, signalling when one starts.
type blockingDB struct {
	*dbtest.Fake
	started chan struct{}
	once    sync.Once
}

func (b *blockingDB) Transact(ctx context.Context, _ db.Mode, _ func(context.Context) error) error {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return errs.E("test.blockingDB", errs.KindOf(ctx.Err()), ctx.Err())
}

func TestClose_CancelsARunningImport(t *testing.T) {
	d := &blockingDB{Fake: dbtest.NewFake(), started: make(chan struct{})}
	l, _ := newLogOn(t, d, t.TempDir())
	writeSpoolFile(t, l, "0001-1-aaaaaaaa.jsonl", goodLine(t))
	runDone := make(chan error, 1)
	go func() { runDone <- l.Run(context.Background()) }()
	<-d.started // the first import is inside its insert

	if err := l.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned %v after Close, want nil", err)
		}
	default:
		t.Fatal("Close returned while Run was still importing")
	}
	if _, serr := os.Stat(filepath.Join(l.spoolDir, "0001-1-aaaaaaaa.jsonl")); serr != nil {
		t.Errorf("the interrupted import must keep its file: %v", serr)
	}
}

func TestSpoolCounts_Table(t *testing.T) {
	dataDir := t.TempDir()
	if w, f, err := SpoolCounts(dataDir); err != nil || w != 0 || f != 0 {
		t.Fatalf("no spool directory: %d, %d, %v; want 0, 0, nil", w, f, err)
	}
	dir := filepath.Join(dataDir, filepath.FromSlash(SpoolDirName))
	if err := os.MkdirAll(filepath.Join(dir, "sub.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"1-1-aa.jsonl", "2-1-bb.jsonl", "3-1-cc.jsonl.failed", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w, f, err := SpoolCounts(dataDir)
	if err != nil || w != 2 || f != 1 {
		t.Errorf("SpoolCounts = %d waiting, %d failed, %v; want 2, 1, nil (a directory and a stray file are not spool files)", w, f, err)
	}
}

func TestSpoolCounts_PathProblems_ReturnInvalid(t *testing.T) {
	t.Run("a regular file where the directory should be", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := SpoolCounts(blocker)
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v, want Invalid", err)
		}
		if strings.Contains(err.Error(), blocker) {
			t.Errorf("the message carries the path: %v", err)
		}
	})
	t.Run("a directory this user cannot read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads everything")
		}
		dataDir := t.TempDir()
		dir := filepath.Join(dataDir, filepath.FromSlash(SpoolDirName))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // restore the 0700 spool mode so TempDir cleanup can remove it
				t.Errorf("restore mode: %v", err)
			}
		})
		if _, _, err := SpoolCounts(dataDir); !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v, want Invalid", err)
		}
	})
}
