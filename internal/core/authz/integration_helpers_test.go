//go:build integration

package authz_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/validate"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// faultDB wraps a db.DB to inject failures and to look like a read-only copy. Safe for
// concurrent use.
type faultDB struct {
	db.DB

	mu      sync.Mutex
	readErr error // returned by Query and QueryRow
	execErr error // returned by Exec
	txErr   error // returned by every Transact
	ro      bool
}

func (f *faultDB) set(mod func(*faultDB)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mod(f)
}

func (f *faultDB) ReadOnly() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ro || f.DB.ReadOnly()
}

func (f *faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	err, ro := f.execErr, f.ro
	f.mu.Unlock()
	if ro {
		return pgconn.CommandTag{}, errs.E("test.faultDB.Exec", errs.Unavailable, db.ErrReadOnly)
	}
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return f.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (f *faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.mu.Lock()
	err := f.readErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func (f *faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	err := f.readErr
	f.mu.Unlock()
	if err != nil {
		return errRow{err}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *faultDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	f.mu.Lock()
	err, ro := f.txErr, f.ro
	f.mu.Unlock()
	if mode == db.Write && ro {
		return errs.E("test.faultDB.Transact", errs.Unavailable, db.ErrReadOnly)
	}
	if err != nil {
		return err
	}
	return f.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

var (
	errDown    = errs.E("test.down", errs.Unavailable, errors.New("database down"))
	errTimeout = errs.E("test.timeout", errs.Timeout, errors.New("database too slow"))
)

// prepareDB creates the stand-in for people's core.person (people is not part of this
// spec's set) so the foreign keys of migration 0002 have something to point at, then
// migrates authz.
func prepareDB(t testing.TB, pool *db.Pool) {
	t.Helper()
	ctx := ctxT(t)
	for _, q := range []string{
		"CREATE SCHEMA IF NOT EXISTS core",
		"CREATE TABLE IF NOT EXISTS core.person (id uuid PRIMARY KEY)",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("preparing the database: %v", err)
		}
	}
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctx, []db.Set{authz.Set})
	wantNoErr(t, err)
}

// ensurePerson inserts a stand-in person row.
func ensurePerson(t testing.TB, pool *db.Pool, p model.PersonID) {
	t.Helper()
	if _, err := pool.Exec(ctxT(t), "INSERT INTO core.person (id) VALUES ($1) ON CONFLICT DO NOTHING", [16]byte(p)); err != nil {
		t.Fatalf("inserting a person: %v", err)
	}
}

// irig is a rig over a real, migrated, throw-away database.
type irig struct {
	*rig
	pool *db.Pool
	fdb  *faultDB
}

// newRigNoSeed migrates but does not seed starter membership.
func newRigNoSeed(t *testing.T, mods ...func(*authz.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	prepareDB(t, pool)
	fdb := &faultDB{DB: pool}
	r := newUnitRig(t, fdb, mods...)
	ir := &irig{rig: r, pool: pool, fdb: fdb}
	ensurePerson(t, pool, r.su)
	return ir
}

// newRig migrates and seeds, as bms migrate does.
func newRig(t *testing.T, mods ...func(*authz.Options)) *irig {
	t.Helper()
	r := newRigNoSeed(t, mods...)
	_, err := r.svc.Seed(ctxT(t), r.opts.Declared)
	wantNoErr(t, err)
	return r
}

// sctx is a context acting as the superuser.
func (r *irig) sctx() context.Context { return asSuper(ctxT(r.t), r.su) }

// person returns a new person who exists in the database.
func (r *irig) person() model.PersonID {
	r.t.Helper()
	p := newPerson()
	ensurePerson(r.t, r.pool, p)
	return p
}

func (r *irig) pctx(p model.PersonID) context.Context { return as(ctxT(r.t), p) }

// roleKey returns the starter role with key.
func (r *irig) roleKey(key string) authz.Role {
	r.t.Helper()
	roles, err := r.svc.ListRoles(r.sctx())
	wantNoErr(r.t, err)
	for _, ro := range roles {
		if ro.Key == key {
			return ro
		}
	}
	r.t.Fatalf("no starter role %q", key)
	return authz.Role{}
}

// mkRole creates a custom role as the superuser.
func (r *irig) mkRole(scope authz.Scope, perms ...authz.Permission) authz.Role {
	r.t.Helper()
	ro, err := r.svc.CreateRole(r.sctx(), authz.RoleInput{Name: r.name("Role"), Scope: scope, Permissions: perms})
	wantNoErr(r.t, err)
	return ro
}

// give grants role to p as the superuser.
func (r *irig) give(p model.PersonID, ro authz.Role, dept model.DepartmentID, ends time.Time) authz.Grant {
	r.t.Helper()
	g, err := r.svc.Grant(r.sctx(), authz.GrantInput{Person: p, Role: ro.ID, Department: dept, EndsAt: ends})
	wantNoErr(r.t, err)
	return g
}

// holder returns a new person holding perms site-wide through a new site role.
func (r *irig) holder(perms ...authz.Permission) model.PersonID {
	r.t.Helper()
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeSite, perms...), model.DepartmentID{}, time.Time{})
	return p
}

// deptHolder returns a new person holding perms in department d through a new department role.
func (r *irig) deptHolder(d model.DepartmentID, perms ...authz.Permission) model.PersonID {
	r.t.Helper()
	p := r.person()
	r.give(p, r.mkRole(authz.ScopeDepartment, perms...), d, time.Time{})
	return p
}

// admin returns a new person holding the starter access-admin role.
func (r *irig) admin() model.PersonID {
	r.t.Helper()
	p := r.person()
	r.give(p, r.roleKey(authz.RoleAccessAdmin), model.DepartmentID{}, time.Time{})
	return p
}

// grantsOf lists every grant of p, ended ones too, as the superuser.
func (r *irig) grantsOf(p model.PersonID) []authz.Grant {
	r.t.Helper()
	page, err := r.svc.ListGrants(r.sctx(), authz.GrantQuery{Person: p, IncludeEnded: true, Limit: authz.MaxPageSize})
	wantNoErr(r.t, err)
	return page.Grants
}

func hasPerm(ro authz.Role, p authz.Permission) bool {
	for _, rp := range ro.Permissions {
		if rp.Name == p {
			return true
		}
	}
	return false
}

// ---- helpers shared by the integration tests only ----

var seqCounter atomic.Int32

const testTimeout = 90 * time.Second

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// wantField checks Invalid carrying a field error; code "" accepts any code.
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && (code == "" || fe.Code == code) {
			return
		}
	}
	t.Fatalf("no field error %q/%q in %v", field, code, validate.Fields(err))
}

// wantCode checks a Conflict (or any kind) that carries a coded field error, whatever
// field it sits on: the spec names the codes, not the fields, for conflicts.
func wantCode(t testing.TB, err error, kind errs.Kind, code string) {
	t.Helper()
	wantKind(t, err, kind)
	for _, fe := range validate.Fields(err) {
		if fe.Code == code {
			return
		}
	}
	t.Fatalf("no field error with code %q in %v", code, validate.Fields(err))
}

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly: %v", err)
	}
}

func (b *failBus) fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

// name returns a fresh unique role name.
func (r *rig) name(prefix string) string {
	return fmt.Sprintf("%s %d", prefix, seqCounter.Add(1))
}

// actions lists the audit actions recorded so far, in order.
func (r *rig) actions() []string {
	var out []string
	for _, e := range r.rec.Recorded() {
		out = append(out, string(e.Action))
	}
	return out
}

// entries returns the recorded entries with action a.
func (r *rig) entries(a string) []audit.Entry {
	var out []audit.Entry
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == a {
			out = append(out, e)
		}
	}
	return out
}

// sigsOf returns published signals with name n.
func (r *rig) sigsOf(n signals.Name) []signals.Signal {
	var out []signals.Signal
	for _, s := range r.bus.Published() {
		if s.Name == n {
			out = append(out, s)
		}
	}
	return out
}
