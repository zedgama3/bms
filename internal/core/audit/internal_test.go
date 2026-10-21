package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // refusingDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
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

// ---- F36b: set aside by cause, insert what precedes the refused record ----

// refusingDB fails every insert whose rows include the poison action with err, and records
// the rows of every insert that went through.
type refusingDB struct {
	*dbtest.Fake
	poison string
	err    error
	mu     sync.Mutex
	rows   int
}

func (d *refusingDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	for _, a := range args {
		body, ok := a.([]byte)
		if !ok {
			continue
		}
		if bytes.Contains(body, []byte(d.poison)) {
			return pgconn.CommandTag{}, d.err
		}
		var rows []json.RawMessage
		if json.Unmarshal(body, &rows) == nil {
			d.mu.Lock()
			d.rows += len(rows)
			d.mu.Unlock()
		}
	}
	return d.Fake.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (d *refusingDB) inserted() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rows
}

func poisonedLine(t *testing.T) []byte {
	t.Helper()
	return bytes.Replace(goodLine(t), []byte("identity.login"), []byte("identity.poisoned"), 1)
}

func TestImport_DataRefusal_InsertsRecordsBeforeIt_SetsFileAside(t *testing.T) {
	d := &refusingDB{Fake: dbtest.NewFake(), poison: "identity.poisoned",
		err: errs.E("test", errs.Invalid, &db.Error{Code: "23514", Constraint: "entry_check"})}
	l, c := newLogOn(t, d, t.TempDir())
	var parts [][]byte
	for range 5 {
		parts = append(parts, goodLine(t))
	}
	parts = append(parts, poisonedLine(t), goodLine(t), goodLine(t))
	writeSpoolFile(t, l, "0001-1-aaaaaaaa.jsonl", parts...)
	writeSpoolFile(t, l, "0002-1-bbbbbbbb.jsonl", goodLine(t), goodLine(t))
	out, err := l.ImportOnce(t.Context())
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("ImportOnce kind = %v, want Invalid", errs.KindOf(err))
	}
	if got := d.inserted(); got != 7 {
		t.Errorf("%d rows inserted, want 7 (the five before the refused record and the second file)", got)
	}
	if len(out.Failed) != 1 || out.Files != 1 {
		t.Errorf("result = %+v, want one failed file and one imported", out)
	}
	if _, serr := os.Stat(filepath.Join(l.spoolDir, "0001-1-aaaaaaaa.jsonl"+failedSuffix)); serr != nil {
		t.Errorf("file not set aside: %v", serr)
	}
	warned := false
	for _, r := range c.Records() {
		if r.Level == slog.LevelWarn && r.Attrs["line"] == "6" && r.Attrs["file"] == "0001-1-aaaaaaaa.jsonl" {
			warned = true
		}
	}
	if !warned {
		t.Error("no warning naming the file and line 6")
	}
	if c.Contains("poisoned") {
		t.Error("a value from the refused record leaked into telemetry")
	}
}

func TestImport_DataRefusal_BeforeAParseError_InsertsThePrecedingRecords(t *testing.T) {
	d := &refusingDB{Fake: dbtest.NewFake(), poison: "identity.poisoned",
		err: errs.E("test", errs.Invalid, &db.Error{Code: "22001"})}
	l, _ := newLogOn(t, d, t.TempDir())
	writeSpoolFile(t, l, "0001-1-aaaaaaaa.jsonl", goodLine(t), goodLine(t), poisonedLine(t), []byte("not json\n"))
	out, err := l.ImportOnce(t.Context())
	if !errs.Is(err, errs.Invalid) || len(out.Failed) != 1 {
		t.Fatalf("ImportOnce = %+v, %v; want the file set aside", out, err)
	}
	if got := d.inserted(); got != 2 {
		t.Errorf("%d rows inserted, want 2", got)
	}
}

func TestImport_InvalidWithoutADataCondition_KeepsTheFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"authentication failure", errs.E("test", errs.Invalid, &db.Error{Code: "28P01"})},
		{"database missing", errs.E("test", errs.Invalid, &db.Error{Code: "3D000"})},
		{"no database error at all", errs.E("test", errs.Invalid, errors.New("refused"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &refusingDB{Fake: dbtest.NewFake(), poison: "identity.login", err: tc.err}
			l, _ := newLogOn(t, d, t.TempDir())
			path := writeSpoolFile(t, l, "0001-1-aaaaaaaa.jsonl", goodLine(t), goodLine(t))
			out, err := l.ImportOnce(t.Context())
			if err == nil || errs.Is(err, errs.Invalid) {
				t.Fatalf("ImportOnce kind = %v, want a transient failure", errs.KindOf(err))
			}
			if len(out.Failed) != 0 {
				t.Errorf("Failed = %v, want none", out.Failed)
			}
			if _, serr := os.Stat(path); serr != nil {
				t.Errorf("the file must be kept for retry: %v", serr)
			}
		})
	}
}

func TestDataRefusal_Table(t *testing.T) {
	for code, want := range map[string]bool{
		"22001": true, "22P02": true, "23502": true, "23503": true, "23505": true, "23514": true,
		"28P01": false, "3D000": false, "08006": false, "40001": false, "53300": false, "": false,
	} {
		if got := dataRefusal(errs.E("test", errs.Invalid, &db.Error{Code: code})); got != want {
			t.Errorf("dataRefusal(%q) = %v, want %v", code, got, want)
		}
	}
	if dataRefusal(errors.New("plain")) {
		t.Error("dataRefusal(plain error) = true")
	}
}

// ---- the ActorFunc may name the operator ----

func TestResolveActor_ActorFuncReturnsOperator_IsTheOperator(t *testing.T) {
	l := readOnlyLog(t)
	l.opts.Actor = func(context.Context) (Actor, bool) {
		return Actor{Kind: ActorOperator, Titles: []string{"ignored"}}, true
	}
	got, err := l.resolveActor(t.Context(), Actor{})
	if err != nil {
		t.Fatalf("resolveActor: %v", err)
	}
	if got.Kind != ActorOperator || len(got.Titles) != 0 || !got.Person.IsZero() {
		t.Errorf("actor = %+v, want the operator with no person and no titles", got)
	}
}

// deliveryCtx returns a ctx for which signals.InDelivery is true, as a handler receives it.
func deliveryCtx(t *testing.T) context.Context {
	t.Helper()
	bus, err := signalstest.NewFake("hello", nil, []signals.Name{"people.person-created"})
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	var got context.Context
	if err := bus.Subscribe("people.person-created", func(ctx context.Context, _ signals.Signal) error {
		got = ctx
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	sig := signals.Signal{ID: model.NewID[signals.SignalID](), Name: "people.person-created", At: time.Now(), Payload: signals.Payload{}}
	if err := bus.Deliver(t.Context(), sig); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got == nil || !signals.InDelivery(got) {
		t.Fatal("the handler's ctx is not InDelivery")
	}
	return got
}

func TestResolveActor_Order_GivenThenDeliveryThenActorFuncThenAnonymous(t *testing.T) {
	l := readOnlyLog(t)
	operator := func(context.Context) (Actor, bool) { return Actor{Kind: ActorOperator}, true }
	none := func(context.Context) (Actor, bool) { return Actor{}, false }
	for _, tc := range []struct {
		name  string
		fn    ActorFunc
		given Actor
		ctx   context.Context
		want  ActorKind
	}{
		{"given wins", operator, Actor{Kind: ActorAnonymous}, t.Context(), ActorAnonymous},
		{"delivery beats the function", operator, Actor{}, deliveryCtx(t), ActorSystem},
		{"the function answers", operator, Actor{}, t.Context(), ActorOperator},
		{"no answer is anonymous", none, Actor{}, t.Context(), ActorAnonymous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l.opts.Actor = tc.fn
			got, err := l.resolveActor(tc.ctx, tc.given)
			if err != nil || got.Kind != tc.want {
				t.Errorf("resolveActor = %+v, %v; want kind %v", got, err, tc.want)
			}
		})
	}
}
