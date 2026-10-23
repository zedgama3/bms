package volunteers_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/departments/departmentstest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/events/eventstest"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// All people, departments and names in these tests are invented.

const testTimeout = 60 * time.Second

// now is the fixed instant of every rig's clock.
var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// Permissions, as the spec names them (typed here once).
const (
	pSignUp      = authz.Permission(volunteers.PermissionSignUp)
	pShifts      = authz.Permission(volunteers.PermissionShiftsManage)
	pRosters     = authz.Permission(volunteers.PermissionRostersRead)
	pPositions   = authz.Permission(volunteers.PermissionPositionsManage)
	pMembers     = authz.Permission(volunteers.PermissionMembersManage)
	pQualManage  = authz.Permission(volunteers.PermissionQualificationsManage)
	pQualRead    = authz.Permission(volunteers.PermissionQualificationsRead)
	pQualVerify  = authz.Permission(volunteers.PermissionQualificationsVerify)
	pContactRead = authz.Permission("people.contacts.read")
)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// ---- error helpers ----

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if !errs.Is(err, kind) {
		t.Fatalf("error = %v (kind %v), want kind %v", err, errs.KindOf(err), kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- wrappers that inject failures ----

// countingPeople counts the calls to people and can fail them.
type countingPeople struct {
	people.Directory
	mu       sync.Mutex
	names    int
	nameIDs  [][]model.PersonID // the IDs of each NamesOf call, in order
	legal    int
	legalIDs []model.PersonID
	namesErr error
	legalErr error
}

func (c *countingPeople) NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	c.mu.Lock()
	c.names++
	c.nameIDs = append(c.nameIDs, slices.Clone(ids))
	err := c.namesErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.Directory.NamesOf(ctx, ids) //nolint:wrapcheck // test wrapper
}

func (c *countingPeople) LegalNamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	c.mu.Lock()
	c.legal++
	c.legalIDs = append(c.legalIDs, ids...)
	err := c.legalErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.Directory.LegalNamesOf(ctx, ids) //nolint:wrapcheck // test wrapper
}

// ---- the rig ----

type opts struct {
	db         db.DB // nil = a dbtest.Fake (no rows)
	real       bool  // db is a migrated real database: departments and events are inserted
	wrapPeople func(people.Directory) people.Directory
	wrapEvents func(events.Directory) events.Directory
	wrapDepts  func(departments.Directory) departments.Directory
	wrapNotify func(notify.Notifier) notify.Notifier
	wrapBus    func(signals.Bus) signals.Bus
	clk        *clock.Fake    // nil = a fresh clock at now
	jobsReg    jobs.Registrar // nil = the rig's jobstest.Fake; else the module is given this one (the real runner)
	noStart    bool
	share      *rig // reuse another rig's directories, grants and settings (a second module over the same data)
}

type rig struct {
	t    *testing.T
	real bool

	mod  module.Module
	svc  *volunteers.Service
	db   db.DB
	az   *authztest.Fake
	ppl  *peopletest.Fake
	cpl  *countingPeople
	dept *departmentstest.Fake
	evt  *eventstest.Fake
	aud  *audittest.Fake
	sig  *signalstest.Fake
	set  *settingstest.Fake
	mail *notifytest.Fake
	jobs *jobstest.Fake
	fl   *filestest.Fake
	clk  *clock.Fake
	cap  *telemetrytest.Capture

	boss model.PersonID // a superuser used to set fixtures up

	// names hands out playa names. People refuses a duplicate playa name installation-wide
	// (Amendment P1), so a fixture that asks for a name already given gets a distinct
	// variant ("Dandelion", then "Dandelion 2"); the first holder keeps the plain name.
	names *nameBook
}

type nameBook struct {
	mu   sync.Mutex
	used map[string]int
}

// unique returns playa unchanged the first time and a distinct variant after that.
func (b *nameBook) unique(playa string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used[playa]++
	if n := b.used[playa]; n > 1 {
		return fmt.Sprintf("%s %d", playa, n)
	}
	return playa
}

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

// newRig builds the module over fakes (and a real database when o.real).
func newRig(t *testing.T, o opts) *rig {
	t.Helper()
	clk := o.clk
	if clk == nil {
		clk = clock.NewFake(now)
	}
	r := &rig{
		t: t, real: o.real, db: o.db,
		az: authztest.NewFake(), ppl: peopletest.NewFake(), dept: departmentstest.NewFake(), evt: eventstest.NewFake(),
		aud: audittest.NewFake(volunteers.Manifest.Name), mail: notifytest.NewFake(volunteers.Manifest.Name),
		jobs: jobstest.NewFake(volunteers.Manifest.Name, jobstest.WithClock(clk)), fl: filestest.NewFake(volunteers.Manifest.Name),
		clk: clk, cap: telemetrytest.New(t), names: &nameBook{used: map[string]int{}},
		set: settingstest.NewFake(volunteers.Manifest.Name, volunteers.Manifest.Settings),
	}
	if r.db == nil {
		r.db = dbtest.NewFake()
	}
	if o.share != nil {
		r.az, r.ppl, r.dept, r.evt, r.set, r.boss, r.names = o.share.az, o.share.ppl, o.share.dept, o.share.evt, o.share.set, o.share.boss, o.share.names
	} else {
		r.boss = r.person("Boss Gadget")
	}
	r.az.SetActor(actorFromSession)
	r.az.Superuser(r.boss)
	sig, err := signalstest.NewFake(volunteers.Manifest.Name, volunteers.Manifest.Publishes, volunteers.Manifest.Subscribes)
	wantNoErr(t, err)
	r.sig = sig
	r.cpl = &countingPeople{Directory: r.ppl}
	var pd people.Directory = r.cpl
	if o.wrapPeople != nil {
		pd = o.wrapPeople(r.cpl)
	}
	var ev events.Directory = r.evt
	if o.wrapEvents != nil {
		ev = o.wrapEvents(r.evt)
	}
	var nt notify.Notifier = r.mail
	if o.wrapNotify != nil {
		nt = o.wrapNotify(r.mail)
	}
	var bus signals.Bus = sig
	if o.wrapBus != nil {
		bus = o.wrapBus(sig)
	}
	var dd departments.Directory = r.dept
	if o.wrapDepts != nil {
		dd = o.wrapDepts(r.dept)
	}
	var jr jobs.Registrar = r.jobs
	if o.jobsReg != nil {
		jr = o.jobsReg
	}
	deps := module.Deps{
		Logger: r.cap.Logger(), Tracer: r.cap.Tracer("module.volunteers"), Clock: r.clk,
		DB: r.db, Signals: bus, Audit: r.aud, Authz: r.az, People: pd,
		Departments: dd, Events: ev, Settings: r.set, Notify: nt, Jobs: jr, Files: r.fl,
	}
	mod, err := volunteers.New(ctxT(t), deps)
	if err != nil {
		t.Fatalf("volunteers.New: %v", err)
	}
	r.mod = mod
	svc, ok := mod.(*volunteers.Service)
	if !ok {
		t.Fatalf("New returned %T, want *volunteers.Service", mod)
	}
	r.svc = svc
	if !o.noStart {
		wantNoErr(t, mod.Start(ctxT(t)))
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			if err := mod.Stop(ctx); err != nil {
				t.Errorf("Stop: %v", err)
			}
		})
	}
	return r
}

func actorFromSession(ctx context.Context) (authz.Actor, bool) {
	p, ok := identity.PersonFrom(ctx)
	return authz.Actor{Person: p}, ok
}

// as returns ctx carrying a session for p.
func (r *rig) as(p model.PersonID) context.Context {
	return asPerson(ctxT(r.t), p)
}

func asPerson(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, identity.Session{
		ID: model.NewID[identity.SessionID](), Account: model.NewID[model.AccountID](), Person: p,
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
}

func (r *rig) exec(sql string, args ...any) {
	r.t.Helper()
	if !r.real {
		return
	}
	if _, err := r.db.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("exec: %v", err)
	}
}

// person creates a person with a playa name ("" = no playa name yet).
func (r *rig) person(playa string) model.PersonID {
	r.t.Helper()
	p := newPerson()
	r.exec("INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(p))
	if playa != "" {
		r.ppl.Add(p, r.names.unique(playa))
	}
	return p
}

func (r *rig) department(name string) model.DepartmentID {
	r.t.Helper()
	d := model.NewID[model.DepartmentID]()
	r.exec("INSERT INTO core.department (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", [16]byte(d), name)
	r.dept.Add(d, name)
	return d
}

// lead gives a person every lead permission at d, plus the verify permission.
func (r *rig) lead(d model.DepartmentID) model.PersonID {
	p := newPerson()
	r.exec("INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(p))
	for _, perm := range []authz.Permission{pShifts, pRosters, pPositions, pMembers, pQualManage, pQualRead, pQualVerify} {
		r.az.GrantIn(p, perm, d)
	}
	return p
}

// volunteer is a person who may volunteer (holds shifts.signup, as the base role does).
func (r *rig) volunteer(playa string) model.PersonID {
	p := r.person(playa)
	r.az.Grant(p, pSignUp)
	return p
}

// ---- fixtures through the service, as the superuser ----

// when is a start time n hours after 2026-11-07 09:00 UTC (always in the future of now).
func when(n int) time.Time {
	return time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Hour)
}

// ---- audit and wire helpers ----

var _ = departments.SignalArchived

func typeName(v any) string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.PkgPath() + "." + t.Name()
}

const errsInvalid = errs.Invalid

// fieldCodeOf returns the code of the first field problem on field in err, "" when none.
func fieldCodeOf(err error, field string) string {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field {
			return fe.Code
		}
	}
	return ""
}

func isReadOnly(err error) bool { return errors.Is(err, db.ErrReadOnly) }

// errOf keeps the error of a call whose other results do not matter to the test.
func errOf[T any](_ T, err error) error { return err }

// errOf3 is errOf for a call with two other results.
func errOf3[A, B any](_ A, _ B, err error) error { return err }

// ---- list wrappers (V1): most tests only want the items ----

func shiftItems(l vol.ShiftList, err error) ([]vol.ShiftSummary, error)        { return l.Items, err }
func scheduleItems(l vol.ScheduleList, err error) ([]vol.ScheduleEntry, error) { return l.Items, err }
func recordItems(l vol.RecordList, err error) ([]vol.Record, error)            { return l.Items, err }
func memberItems(l vol.MemberViewList, err error) ([]vol.MemberView, error)    { return l.Items, err }
