//go:build integration

package volunteers_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// realDB returns a throw-away migrated database holding the core tables volunteers'
// foreign keys point at and the volunteers schema itself (BMS_TEST_DB_DSN; skipped when unset).
func realDB(t *testing.T) *db.Pool {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{
		identity.Set, people.Set, departments.Set, events.Set,
		{Name: volunteers.Manifest.Name, Schema: volunteers.Manifest.Name, Files: volunteers.Manifest.Migrations},
	})
	wantNoErr(t, err)
	return pool
}

// newReal builds the module over a real, migrated database.
func newReal(t *testing.T, mod ...func(*opts)) *rig {
	t.Helper()
	o := opts{db: realDB(t), real: true}
	for _, m := range mod {
		m(&o)
	}
	return newRig(t, o)
}

// codeOf returns the code of the first validate.FieldError in err, "" when none.
func codeOf(err error) string {
	var fe *validate.FieldError
	if errors.As(err, &fe) {
		return fe.Code
	}
	return ""
}

func fieldOf(err error) string {
	var fe *validate.FieldError
	if errors.As(err, &fe) {
		return fe.Field
	}
	return ""
}

func wantConflict(t testing.TB, err error, code string) {
	t.Helper()
	wantKind(t, err, errs.Conflict)
	if got := codeOf(err); got != code {
		t.Fatalf("conflict code = %q, want %q (err: %v)", got, code, err)
	}
}

func wantInvalid(t testing.TB, err error, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if code != "" {
		if got := codeOf(err); got != code {
			t.Fatalf("invalid code = %q, want %q (err: %v)", got, code, err)
		}
	}
}

func (c *countingPeople) counts() (names, legal int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names, c.legal
}

func (c *countingPeople) legalCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.legal
}

func (c *countingPeople) resetCounts() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names, c.legal, c.legalIDs, c.nameIDs = 0, 0, nil, nil
}

// failEvents makes InfoOf fail.
type failEvents struct {
	events.Directory
	err error
}

func (f failEvents) InfoOf(context.Context, []model.EventID) (map[model.EventID]events.Info, error) {
	return nil, f.err
}

// selectiveNotify fails Send for the listed recipients only.
type selectiveNotify struct {
	notify.Notifier
	mu   sync.Mutex
	fail map[model.PersonID]error
}

func (s *selectiveNotify) Send(ctx context.Context, m notify.Message) error {
	s.mu.Lock()
	for _, p := range m.To {
		if err := s.fail[p]; err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()
	return s.Notifier.Send(ctx, m) //nolint:wrapcheck // test wrapper
}

// busFn lets a test refuse Publish.
type busFn struct {
	signals.Bus
	publish func(name signals.Name) error
}

func (b busFn) Publish(ctx context.Context, name signals.Name, p signals.Payload) error {
	if b.publish != nil {
		if err := b.publish(name); err != nil {
			return err
		}
	}
	return b.Bus.Publish(ctx, name, p) //nolint:wrapcheck // test wrapper
}

// readOnlyDB wraps a real DB and refuses every write transaction like a replica.
type readOnlyDB struct{ db.DB }

func (r readOnlyDB) ReadOnly() bool { return true }
func (r readOnlyDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	if mode == db.Write {
		return errs.E("test.readOnlyDB", errs.Unavailable, db.ErrReadOnly)
	}
	return r.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test wrapper
}

func (r *rig) archiveDepartment(d model.DepartmentID) {
	r.t.Helper()
	r.exec("UPDATE core.department SET archived_at = now() WHERE id = $1", [16]byte(d))
	r.dept.Archive(d)
}

func (r *rig) event(starts, ends time.Time, zone string, closed bool) model.EventID {
	r.t.Helper()
	e := model.NewID[model.EventID]()
	var closedAt any
	if closed {
		closedAt = now
	}
	r.exec(`INSERT INTO core.event (id, name, kind, starts_at, ends_at, zone, closed_at, created_at, updated_at)
		VALUES ($1, $2, 'burn', $3, $4, $5, $6, now(), now())`, [16]byte(e), "Test burn "+e.String()[:6], starts, ends, zone, closedAt)
	r.evt.Add(events.Info{ID: e, Name: "Test burn", Kind: "burn", Dates: model.TimeRange{Start: starts, End: ends}, Zone: zone, Closed: closed})
	return e
}

func (r *rig) closeEvent(e model.EventID) {
	r.t.Helper()
	r.exec("UPDATE core.event SET closed_at = now() WHERE id = $1", [16]byte(e))
	infos, err := r.evt.InfoOf(ctxT(r.t), []model.EventID{e})
	wantNoErr(r.t, err)
	info := infos[e]
	info.Closed = true
	r.evt.Add(info)
}

func (r *rig) qual(d model.DepartmentID, name string) vol.Qualification {
	r.t.Helper()
	q, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: d, Name: name})
	if err != nil {
		r.t.Fatalf("CreateQualification: %v", err)
	}
	return q
}

func (r *rig) position(d model.DepartmentID, name string, access vol.Access, reqs ...vol.QualificationID) vol.Position {
	r.t.Helper()
	p, err := r.svc.CreatePosition(r.as(r.boss), d, vol.PositionInput{Name: name, Access: access})
	if err != nil {
		r.t.Fatalf("CreatePosition: %v", err)
	}
	if len(reqs) > 0 {
		p, err = r.svc.SetRequirements(r.as(r.boss), p.ID, reqs)
		if err != nil {
			r.t.Fatalf("SetRequirements: %v", err)
		}
	}
	return p
}

func (r *rig) shift(d model.DepartmentID, pos vol.PositionID, ev model.EventID, starts time.Time, hours, slots int) vol.Shift {
	r.t.Helper()
	in := vol.ShiftInput{
		Position: pos, Event: ev, Label: "Morning dirt", Starts: starts, Ends: starts.Add(time.Duration(hours) * time.Hour),
		Slots: slots, Location: "Ranger HQ",
	}
	if ev.IsZero() {
		in.Zone = "UTC"
	}
	s, err := r.svc.CreateShift(r.as(r.boss), d, in)
	if err != nil {
		r.t.Fatalf("CreateShift: %v", err)
	}
	return s
}

// verified records a verified qualification for person, valid from 2026-01-01, never expiring.
func (r *rig) verified(person model.PersonID, q vol.QualificationID) vol.Record {
	r.t.Helper()
	rec, err := r.svc.RecordQualification(r.as(r.boss), vol.RecordInput{
		Qualification: q, Person: person, AcquiredOn: vol.Date{Year: 2026, Month: time.January, Day: 1},
	})
	if err != nil {
		r.t.Fatalf("RecordQualification: %v", err)
	}
	return rec
}

func (r *rig) member(d model.DepartmentID, p model.PersonID) {
	r.t.Helper()
	if _, err := r.svc.AddMember(r.as(r.boss), d, p); err != nil {
		r.t.Fatalf("AddMember: %v", err)
	}
}

// book signs a volunteer into a shift and fails the test on error.
func (r *rig) book(p model.PersonID, s vol.ShiftID) vol.Signup {
	r.t.Helper()
	su, err := r.svc.SignUp(r.as(p), s)
	if err != nil {
		r.t.Fatalf("SignUp: %v", err)
	}
	return su
}

// scenario is the usual small world: one department, an open position, a shift.
type scenario struct {
	dept model.DepartmentID
	lead model.PersonID
	pos  vol.Position
	sh   vol.Shift
}

func (r *rig) scenario() scenario {
	r.t.Helper()
	d := r.department("Rangers " + model.NewID[model.DepartmentID]().String()[:6])
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	return scenario{dept: d, lead: r.lead(d), pos: pos, sh: r.shift(d, pos.ID, model.EventID{}, when(0), 4, 3)}
}

func (r *rig) audited(action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range r.aud.Recorded() {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

func (r *rig) published(name string) []signals.Signal {
	var out []signals.Signal
	for _, s := range r.sig.Published() {
		if string(s.Name) == name {
			out = append(out, s)
		}
	}
	return out
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

type eventsDirectory = events.Directory
type (
	signalsBus  = signals.Bus
	signalsName = signals.Name
)

// advance moves the rig's clock forward and fails the test if the fake clock refuses.
func (r *rig) advance(d time.Duration) {
	r.t.Helper()
	wantNoErr(r.t, r.clk.Advance(d))
}
