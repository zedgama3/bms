package auditlog_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // flakyDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // flakyDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

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

// --- the signed-in actor, as app would adapt identity's session ---------------------------

type sessionKey struct{}

func withSession(ctx context.Context, a authz.Actor) context.Context {
	return context.WithValue(ctx, sessionKey{}, a)
}

func sessionActor(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(sessionKey{}).(authz.Actor)
	return a, ok
}

func person(p model.PersonID) authz.Actor { return authz.Actor{Person: p} }

func superuser(p model.PersonID) authz.Actor { return authz.Actor{Person: p, Superuser: true} }

// --- fault-injecting DB -------------------------------------------------------------------

// flakyDB wraps a dbtest.Fake and fails the statements its fields name.
type flakyDB struct {
	*dbtest.Fake
	mu        sync.Mutex
	failExec  error
	failQuery error
	queries   int
	// countRow, when set, answers a QueryRow whose SQL mentions the key with that count.
	countRow map[string]int64
	counted  []countedCall
}

type countedCall struct {
	key  string
	args []any
}

// answerCount makes a QueryRow whose SQL contains key answer n and remembers its args.
func (f *flakyDB) answerCount(key string, n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countRow == nil {
		f.countRow = map[string]int64{}
	}
	f.countRow[key] = n
}

func (f *flakyDB) countedCalls(key string) [][]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]any
	for _, c := range f.counted {
		if c.key == key {
			out = append(out, c.args)
		}
	}
	return out
}

type numRow struct{ n int64 }

func (r numRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		if p, ok := dest[0].(*int64); ok {
			*p = r.n
			return nil
		}
	}
	return errors.New("numRow: unexpected destination")
}

func (f *flakyDB) setFailQuery(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failQuery = err
}

func (f *flakyDB) queryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries
}

// ctxErr is what a real pool answers for a done ctx.
func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errs.E("test.flakyDB", errs.KindOf(err), err)
	}
	return nil
}

func (f *flakyDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := ctxErr(ctx); err != nil {
		return pgconn.CommandTag{}, err
	}
	f.mu.Lock()
	err := f.failExec
	f.mu.Unlock()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return f.Fake.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (f *flakyDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
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
	if err := ctxErr(ctx); err != nil {
		return errRow{err}
	}
	f.mu.Lock()
	f.queries++
	err := f.failQuery
	for k, n := range f.countRow {
		if strings.Contains(sql, k) {
			f.counted = append(f.counted, countedCall{key: k, args: append([]any(nil), args...)})
			f.mu.Unlock()
			return numRow{n: n}
		}
	}
	f.mu.Unlock()
	if err != nil {
		return errRow{err}
	}
	return f.Fake.QueryRow(ctx, sql, args...)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// --- fixture ------------------------------------------------------------------------------

// fx is a Service over a fake database, a fake clock, the authz fake, a settings fake for
// the "audit" area and a jobs fake scoped to "audit".
type fx struct {
	svc      *auditlog.Service
	opts     auditlog.Options
	fake     *dbtest.Fake
	db       *flakyDB
	clk      *clock.Fake
	authz    *authztest.Fake
	settings *settingstest.Fake
	jobs     *jobstest.Fake
	rec      *audittest.Fake // the "audit"-scoped Recorder: what Search, Export and the purge record
}

// newFx builds a Service whose actor comes from withSession. mods run on the Options before
// New. Nothing is granted: a test grants what it needs.
func newFx(t testing.TB, mods ...func(*auditlog.Options)) *fx {
	t.Helper()
	f := f0(t)
	for _, m := range mods {
		m(&f.opts)
	}
	svc, err := auditlog.New(f.opts)
	if err != nil {
		t.Fatalf("auditlog.New: %v", err)
	}
	f.svc = svc
	return f
}

// f0 is a fixture with valid Options and no Service yet.
func f0(t testing.TB) *fx {
	t.Helper()
	tel := telemetrytest.New(t)
	f := &fx{
		fake:     dbtest.NewFake(),
		clk:      clock.NewFake(epoch),
		authz:    authztest.NewFake(),
		settings: settingstest.NewFake("audit", auditlog.Settings),
		jobs:     jobstest.NewFake("audit"),
		rec:      audittest.NewFake("audit"),
	}
	f.db = &flakyDB{Fake: f.fake}
	f.authz.SetActor(sessionActor)
	f.opts = auditlog.Options{
		DB:       f.db,
		Clock:    f.clk,
		Logger:   tel.Logger(),
		Tracer:   tel.Tracer("auditlog"),
		Authz:    f.authz,
		Actor:    sessionActor,
		Settings: f.settings,
		Jobs:     f.jobs,
		Audit:    f.rec,
	}
	return f
}

// readerFx is a Service whose reader (carol) holds both audit permissions.
func readerFx(t testing.TB) *fx {
	t.Helper()
	f := newFx(t)
	f.authz.Grant(carol, auditlog.PermissionSearch)
	f.authz.Grant(carol, auditlog.PermissionExport)
	return f
}

func asReviewer(ctx context.Context) context.Context { return withSession(ctx, person(carol)) }

// recorded returns the entries recorded so far with this action.
func (f *fx) recorded(action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range f.rec.Recorded() {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

// --- assertions ---------------------------------------------------------------------------

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

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly")
	}
}

func advance(t testing.TB, c *clock.Fake, d time.Duration) {
	t.Helper()
	if err := c.Advance(d); err != nil {
		t.Fatalf("clock.Advance: %v", err)
	}
}
