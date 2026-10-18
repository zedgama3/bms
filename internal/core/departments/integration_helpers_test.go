//go:build integration

package departments_test

import (
	"context"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/model"
)

// irig is a departments rig over a real, migrated, throw-away PostgreSQL database.
type irig struct {
	*rig
	pool    *db.Pool
	steward model.PersonID // holds departments.manage and departments.details.manage site-wide
}

func migrateDepartments(t testing.TB, pool *db.Pool, more ...db.Set) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), append(more, departments.Set))
	wantNoErr(t, err)
}

func newIRig(t *testing.T, mods ...func(*departments.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrateDepartments(t, pool)
	r := newRig(t, pool, mods...)
	ir := &irig{rig: r, pool: pool, steward: newPerson()}
	r.az.Grant(ir.steward, departments.PermissionManage)
	r.az.Grant(ir.steward, departments.PermissionDetailsManage)
	return ir
}

func (r *irig) sctx() context.Context { return asPerson(ctxT(r.t), r.steward) }

// mk creates a department as the steward.
func (r *irig) mk(name string) departments.Department {
	r.t.Helper()
	d, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: name, Description: "d of " + name})
	if err != nil {
		r.t.Fatalf("Create(%q): %v", name, err)
	}
	return d
}

// mkArchived creates and archives a department.
func (r *irig) mkArchived(name string) departments.Department {
	r.t.Helper()
	d := r.mk(name)
	a, err := r.svc.Archive(r.sctx(), d.ID)
	if err != nil {
		r.t.Fatalf("Archive: %v", err)
	}
	return a
}

// holder returns a new person with perms site-wide.
func (r *irig) holder(perms ...authz.Permission) model.PersonID {
	p := newPerson()
	for _, perm := range perms {
		r.az.Grant(p, perm)
	}
	return p
}

// mark counts the audit entries and signals so far.
func (r *irig) mark() (entries, sigs int) {
	return len(r.rec.Recorded()), len(r.bus.Published())
}

// wantCoded checks an error of any kind that carries a field error with the given field and code.
func wantCoded(t testing.TB, err error, kind errs.Kind, field, code string) {
	t.Helper()
	wantKind(t, err, kind)
	if !hasField(err, field, code) {
		t.Fatalf("no field error %q/%q in %v", field, code, validate.Fields(err))
	}
}

func (g *gateRec) failAction(a audit.Action, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failOn == nil {
		g.failOn = map[audit.Action]bool{}
	}
	g.failOn[a] = true
	g.err = err
}

func (b *failBus) fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

func entriesOf(es []audit.Entry, action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range es {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

func sigsOf(s []signals.Signal, n signals.Name) []signals.Signal {
	var out []signals.Signal
	for _, x := range s {
		if x.Name == n {
			out = append(out, x)
		}
	}
	return out
}

func advance(t testing.TB, c *clock.Fake, d time.Duration) {
	t.Helper()
	if err := c.Advance(d); err != nil {
		t.Fatalf("Advance: %v", err)
	}
}
