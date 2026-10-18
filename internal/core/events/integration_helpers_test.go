//go:build integration

package events_test

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
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/model"
)

// irig is an events rig over a real, migrated, throw-away PostgreSQL database.
type irig struct {
	*rig
	pool    *db.Pool
	steward model.PersonID // holds events.manage site-wide
}

func migrateEvents(t testing.TB, pool *db.Pool) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{events.Set})
	wantNoErr(t, err)
}

// newIRig builds a linked rig (with the given providers) over a fresh database.
func newIRig(t *testing.T, ps []events.SetupProvider, mods ...func(*events.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrateEvents(t, pool)
	r := newRig(t, pool, mods...)
	r.link(ps...)
	ir := &irig{rig: r, pool: pool, steward: newPerson()}
	r.az.Grant(ir.steward, events.PermissionManage)
	return ir
}

func (r *irig) sctx() context.Context { return asPerson(ctxT(r.t), r.steward) }

// mk creates an event as the steward.
func (r *irig) mk(name string) events.Event {
	r.t.Helper()
	c, err := r.svc.Create(r.sctx(), validCreate(name))
	if err != nil {
		r.t.Fatalf("Create(%q): %v", name, err)
	}
	return c.Event
}

// mkClosed creates and closes an event.
func (r *irig) mkClosed(name string) events.Event {
	r.t.Helper()
	e := r.mk(name)
	c, err := r.svc.Close(r.sctx(), e.ID)
	if err != nil {
		r.t.Fatalf("Close: %v", err)
	}
	return c
}

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

// addItem adds a schedule item as the steward.
func (r *irig) addItem(ev model.EventID, in events.ScheduleInput) events.ScheduleItem {
	r.t.Helper()
	it, err := r.svc.AddScheduleItem(r.sctx(), ev, in)
	if err != nil {
		r.t.Fatalf("AddScheduleItem(%+v): %v", in, err)
	}
	return it
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

func (l *callLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func (p *fakeProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.reqs)
}

func advance(t testing.TB, c *clock.Fake, d time.Duration) {
	t.Helper()
	if err := c.Advance(d); err != nil {
		t.Fatalf("Advance: %v", err)
	}
}
