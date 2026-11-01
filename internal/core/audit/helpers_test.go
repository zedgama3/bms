package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // flakyDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // flakyDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// marker stands in for a personal value; no log, span, metric or error may carry it.
const marker = "ZQ9MARKER7"

var (
	// alice, bob and carol are invented people.
	alice = model.PersonID{0x21, 0x5c, 0x90, 0x14, 0xa3, 0x77, 0x4e, 0x02, 0xb1, 0x6d, 0x48, 0xee, 0x0f, 0x39, 0xc5, 0x86}
	bob   = model.PersonID{0x62, 0x0b, 0xd3, 0x58, 0x1e, 0x9a, 0x41, 0x7c, 0x85, 0x30, 0xf2, 0x6b, 0xa9, 0x14, 0x07, 0xdd}
	carol = model.PersonID{0x7a, 0x13, 0x2c, 0x90, 0x55, 0xe1, 0x4b, 0x6f, 0x9d, 0x08, 0x33, 0xa4, 0xc2, 0x1e, 0xf7, 0x50}

	epoch = time.Date(2026, 10, 17, 12, 0, 0, 123456789, time.UTC)
)

const testTimeout = 30 * time.Second

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// --- the signed-in actor, as app would adapt identity's session -------------------------

type sessionKey struct{}

func withSession(ctx context.Context, a audit.Actor) context.Context {
	return context.WithValue(ctx, sessionKey{}, a)
}

func sessionActor(ctx context.Context) (audit.Actor, bool) {
	a, ok := ctx.Value(sessionKey{}).(audit.Actor)
	return a, ok
}

func person(p model.PersonID) audit.Actor { return audit.Actor{Kind: audit.ActorPerson, Person: p} }

func superuser(p model.PersonID) audit.Actor {
	return audit.Actor{Kind: audit.ActorPerson, Person: p, Superuser: true}
}

// --- fake TitleSource ---------------------------------------------

type fakeTitles struct {
	mu     sync.Mutex
	titles map[model.PersonID][]string
	err    error
	calls  []model.PersonID
}

func (f *fakeTitles) TitlesOf(_ context.Context, p model.PersonID) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.titles[p]...), nil
}

func (f *fakeTitles) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// --- fault-injecting DB -----------------------------------------------------------------

// flakyDB wraps a dbtest.Fake and fails the statements its flags name.
type flakyDB struct {
	*dbtest.Fake
	mu        sync.Mutex
	failExec  error
	failQuery error
	queries   int
	// execHook, when set, decides per statement: a non-nil result fails that Exec.
	execHook func(sql string, args []any) error
	// entered, when set, is signalled by the first Exec, which then blocks until ctx is done.
	entered chan struct{}
}

func (f *flakyDB) setFailExec(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failExec = err
}

func (f *flakyDB) setExecHook(h func(sql string, args []any) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execHook = h
}

func (f *flakyDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	err := f.failExec
	entered := f.entered
	hook := f.execHook
	f.mu.Unlock()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if hook != nil {
		if herr := hook(sql, args); herr != nil {
			return pgconn.CommandTag{}, herr
		}
	}
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return pgconn.CommandTag{}, errs.E("test.flakyDB.Exec", errs.KindOf(ctx.Err()), ctx.Err())
	}
	return f.Fake.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (f *flakyDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.mu.Lock()
	f.queries++
	err := f.failQuery
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.Fake.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (f *flakyDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	f.queries++
	err := f.failQuery
	f.mu.Unlock()
	if err != nil {
		return errRow{err}
	}
	return f.Fake.QueryRow(ctx, sql, args...)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// --- fixture ----------------------------------------------------------------------------

// fx is a Log over a dbtest.Fake, a fake clock, a temp data dir and a telemetrytest capture.
type fx struct {
	log    *audit.Log
	opts   audit.Options
	db     *dbtest.Fake
	clk    *clock.Fake
	dir    string
	cap    *telemetrytest.Capture
	titles *fakeTitles
	// imported receives one signal per "audit spool imported" log line.
	imported chan struct{}
}

// newFx builds a Log whose actor comes from withSession. mods run on the Options before New.
func newFx(t testing.TB, mods ...func(*audit.Options)) *fx {
	t.Helper()
	f := &fx{
		db:       dbtest.NewFake(),
		clk:      clock.NewFake(epoch),
		dir:      t.TempDir(),
		cap:      telemetrytest.New(t),
		titles:   &fakeTitles{titles: map[model.PersonID][]string{}},
		imported: make(chan struct{}, 64),
	}
	f.opts = audit.Options{
		DB:       f.db,
		Clock:    f.clk,
		Logger:   slog.New(notifyHandler{inner: f.cap.Logger().Handler(), ch: f.imported}),
		Tracer:   f.cap.Tracer("audit"),
		Meter:    f.cap.Meter("audit"),
		Instance: "test-copy",
		DataDir:  f.dir,
		Actor:    sessionActor,
		Titles:   f.titles,
	}
	for _, m := range mods {
		m(&f.opts)
	}
	f.log = f.build(t)
	return f
}

// build makes another Log from the current Options (a second process on the same data dir).
func (f *fx) build(t testing.TB) *audit.Log {
	t.Helper()
	l, err := audit.New(f.opts)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		_ = l.Close(ctx) //nolint:errcheck // idempotent cleanup; tests that care call Close themselves
	})
	return l
}

func (f *fx) scoped(t testing.TB, area string) audit.Recorder {
	t.Helper()
	r, err := f.log.Scoped(area)
	if err != nil {
		t.Fatalf("Scoped(%q): %v", area, err)
	}
	return r
}

func (f *fx) spoolDir() string { return filepath.Join(f.dir, filepath.FromSlash(audit.SpoolDirName)) }

// spoolFiles lists the *.jsonl files in the spool, sorted by name.
func (f *fx) spoolFiles(t testing.TB) []string {
	t.Helper()
	return globSorted(t, filepath.Join(f.spoolDir(), "*.jsonl"))
}

func (f *fx) failedFiles(t testing.TB) []string {
	t.Helper()
	return globSorted(t, filepath.Join(f.spoolDir(), "*.failed"))
}

func globSorted(t testing.TB, pattern string) []string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	sort.Strings(m)
	return m
}

// spoolLines returns every spooled line (raw) across the spool, oldest file first. Every
// line must end in a newline.
func (f *fx) spoolLines(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, p := range f.spoolFiles(t) {
		b, err := os.ReadFile(p) //nolint:gosec // p comes from globbing the test's own temp dir
		if err != nil {
			t.Fatalf("read spool file: %v", err)
		}
		if len(b) == 0 {
			continue
		}
		if !bytes.HasSuffix(b, []byte("\n")) {
			t.Fatalf("spool file %s does not end in a newline", filepath.Base(p))
		}
		out = append(out, strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")...)
	}
	return out
}

// spooled decodes every spooled line into a generic map.
func (f *fx) spooled(t testing.TB) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range f.spoolLines(t) {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("spool line is not JSON: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func (f *fx) readOnly() { f.db.SetReadOnly(true) }

// --- assertions -------------------------------------------------------------------------

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("got kind %v, want %v", errs.KindOf(err), kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error (kind %v)", errs.KindOf(err))
	}
}

// wantField fails unless err carries a validate.FieldError whose Field starts with prefix.
func wantField(t testing.TB, err error, prefix string) {
	t.Helper()
	for _, fe := range validate.Fields(err) {
		if strings.HasPrefix(fe.Field, prefix) {
			return
		}
	}
	var got []string
	for _, fe := range validate.Fields(err) {
		got = append(got, fe.Field)
	}
	t.Fatalf("no field error under %q; fields: %v", prefix, got)
}

func noMarker(t testing.TB, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), marker) {
		t.Fatalf("error text leaks the marker")
	}
}

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly")
	}
}

// notifyHandler passes records to inner and signals on ch for each info record that carries
// a "files" attribute: the importer's one line per import, logged after the files are gone.
type notifyHandler struct {
	inner slog.Handler
	ch    chan<- struct{}
}

func (h notifyHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h notifyHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.inner.Handle(ctx, r)
	if r.Level == slog.LevelInfo {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "files" {
				select {
				case h.ch <- struct{}{}:
				default:
				}
				return false
			}
			return true
		})
	}
	return err //nolint:wrapcheck // slog.Handler contract
}

func (h notifyHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return notifyHandler{inner: h.inner.WithAttrs(as), ch: h.ch}
}

func (h notifyHandler) WithGroup(n string) slog.Handler {
	return notifyHandler{inner: h.inner.WithGroup(n), ch: h.ch}
}

// drain empties ch without blocking.
func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// --- entries ----------------------------------------------------------------------------

func loginEntry() audit.Entry {
	return audit.Entry{Action: "identity.login", Outcome: audit.Succeeded}
}

func readEntry(subject model.PersonID) audit.Entry {
	return audit.Entry{
		Action:  "identity.account.read",
		Outcome: audit.Succeeded,
		Subject: subject,
		Record:  audit.RefOf("identity.account", model.AccountID{1}),
		Fields:  []string{"email", "last_login_at"},
		Level:   model.SensitivityPersonal,
	}
}

func str(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func obj(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// advance moves the fake clock and fails the test if it cannot.
func advance(t testing.TB, c *clock.Fake, d time.Duration) {
	t.Helper()
	if err := c.Advance(d); err != nil {
		t.Fatalf("clock.Advance: %v", err)
	}
}

func list(v any) []any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	return l
}
