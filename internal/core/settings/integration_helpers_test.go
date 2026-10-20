//go:build integration

package settings_test

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// irig is a settings rig over a real, migrated, throw-away PostgreSQL database.
type irig struct {
	*rig
	pool  *db.Pool
	admin model.PersonID // holds settings.read and settings.manage
}

func migrateSettings(t testing.TB, pool *db.Pool) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{settings.Set})
	wantNoErr(t, err)
}

func newIRig(t *testing.T, mods ...func(*settings.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrateSettings(t, pool)
	return newIRigOn(t, pool, pool, mods...)
}

// newIRigOn builds another Service over an existing database (a second process, a
// restart, a wrapper that injects faults).
func newIRigOn(t *testing.T, d db.DB, pool *db.Pool, mods ...func(*settings.Options)) *irig {
	t.Helper()
	r := newRig(t, d, mods...)
	ir := &irig{rig: r, pool: pool, admin: newPerson()}
	r.az.Grant(ir.admin, settings.PermissionRead)
	r.az.Grant(ir.admin, settings.PermissionManage)
	return ir
}

func (r *irig) actx() context.Context { return asPerson(ctxT(r.t), r.admin) }

func (r *irig) mustSet(kv ...string) settings.Revision {
	r.t.Helper()
	rev, err := r.svc.Set(r.actx(), changes(kv...))
	if err != nil {
		r.t.Fatalf("Set(%v): %v", kv, err)
	}
	return rev
}

func (r *irig) mustReset(keys ...string) settings.Revision {
	r.t.Helper()
	rev, err := r.svc.Set(r.actx(), resetting(keys...))
	if err != nil {
		r.t.Fatalf("Set(reset %v): %v", keys, err)
	}
	return rev
}

func (r *irig) doc() settings.Document {
	r.t.Helper()
	d, err := r.svc.Document(r.actx())
	if err != nil {
		r.t.Fatalf("Document: %v", err)
	}
	return d
}

// dbRevision reads the stored revision straight from the table.
func (r *irig) dbRevision() int64 {
	r.t.Helper()
	var rev int64
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT revision FROM core.setting_revision WHERE id = 1").Scan(&rev); err != nil {
		r.t.Fatalf("revision: %v", err)
	}
	return rev
}

func (r *irig) rowCount() int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT count(*) FROM core.setting").Scan(&n); err != nil {
		r.t.Fatalf("count: %v", err)
	}
	return n
}

// stored returns the stored text form of key (ok false when there is no row).
func (r *irig) stored(key string) (value string, ok bool) {
	r.t.Helper()
	var v *string
	err := r.pool.QueryRow(ctxT(r.t), "SELECT value FROM core.setting WHERE key = $1", key).Scan(&v)
	if err != nil {
		return "", false
	}
	if v == nil {
		return "", true
	}
	return *v, true
}

func (r *irig) sealed(key string) []byte {
	r.t.Helper()
	var b []byte
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT sealed FROM core.setting WHERE key = $1", key).Scan(&b); err != nil {
		r.t.Fatalf("sealed(%s): %v", key, err)
	}
	return b
}

func (r *irig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("exec %q: %v", sql, err)
	}
}

func entriesFor(es []audit.Entry, action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range es {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

// eventually polls cond without sleeping: it yields the processor until cond holds or the
// test's deadline passes.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	ctx := ctxT(t)
	for !cond() {
		select {
		case <-ctx.Done():
			t.Fatalf("gave up waiting for %s", what)
		default:
			runtime.Gosched()
		}
	}
}

// countMeasures counts increments of an instrument whose attribute attr equals val.
func countMeasures(ms []telemetry.Measurement, instrument, attr, val string) int {
	n := 0
	for _, m := range ms {
		if m.Instrument == instrument && (attr == "" || m.Attrs[attr] == val) {
			n++
		}
	}
	return n
}

// warnings returns the captured warn-level records that carry the setting attribute (or,
// with an empty key, any warn record).
func warnings(r *rig, key string) []telemetry.LogRecord {
	var out []telemetry.LogRecord
	for _, rec := range r.cap.Records() {
		if rec.Level != slog.LevelWarn {
			continue
		}
		if key == "" || rec.Attrs["setting"] == key {
			out = append(out, rec)
		}
	}
	return out
}

// ---- helpers only the integration tests use ----

// Every person, address and value in this package is invented.
const testTimeout = 90 * time.Second

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly: %v", err)
	}
}

func settingOf(doc settings.Document, key string) (settings.SettingDoc, bool) {
	for _, a := range doc.Areas {
		for _, s := range a.Settings {
			if s.Key == key {
				return s, true
			}
		}
	}
	return settings.SettingDoc{}, false
}

func (f *faultDB) transactions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.txCount
}

func unavailable() error { return errs.E("test", errs.Unavailable, errors.New("database down")) }

func secretNew(v string) secret.Secret { return secret.New(v) }

func secretNone() secret.Secret { return secret.Secret{} }

// validateReasons lists the field errors of err as "field/code: reason", for leak checks.
func validateReasons(err error) []string {
	var out []string
	for _, fe := range validate.Fields(err) {
		out = append(out, fe.Field+"/"+fe.Code+": "+fe.Reason)
		for _, v := range fe.Params {
			out = append(out, v)
		}
	}
	return out
}
