//go:build integration

package volunteers_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/demo/demotest"
	"github.com/zedgama3/bms/internal/core/departments/departmentstest"
	"github.com/zedgama3/bms/internal/core/events/eventstest"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// conformance wires volunteers into moduletest.Conformance over a real, migrated database.
// Conformance insists on a *dbtest.Fake in Deps.DB (it makes the copy read-only through it),
// and a dbtest.Fake holds no rows, so the factory swaps in a fresh real database (read-only
// when the fake says so). The provision keeps the typed fakes for the seed.
type conformance struct {
	t  *testing.T
	mu sync.Mutex

	pending *rig // set by provision, consumed by the factory
	rigs    map[*volunteers.Service]*rig
}

func (c *conformance) provision(_ context.Context, m module.Manifest) (module.Deps, error) {
	r := &rig{
		t: c.t, real: true,
		az: authztest.NewFake(), ppl: peopletest.NewFake(), dept: departmentstest.NewFake(), evt: eventstest.NewFake(),
		aud: audittest.NewFake(m.Name), mail: notifytest.NewFake(m.Name), jobs: jobstest.NewFake(m.Name),
		fl: filestest.NewFake(m.Name), clk: clock.NewFake(now), set: settingstest.NewFake(m.Name, m.Settings),
	}
	sig, err := signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)
	if err != nil {
		return module.Deps{}, fmt.Errorf("signalstest.NewFake: %w", err)
	}
	r.sig = sig
	r.az.SetActor(actorFromSession)
	c.mu.Lock()
	c.pending = r
	c.mu.Unlock()
	return module.Deps{
		Clock: r.clk, DB: dbtest.NewFake(), Signals: sig, Audit: r.aud, Authz: r.az, People: r.ppl, Departments: r.dept,
		Events: r.evt, Settings: r.set, Notify: r.mail, Jobs: r.jobs, Files: r.fl,
	}, nil
}

func (c *conformance) factory(ctx context.Context, deps module.Deps) (module.Module, error) {
	c.mu.Lock()
	r := c.pending
	c.mu.Unlock()
	if r == nil {
		return nil, errors.New("the factory ran without a provision")
	}
	pool := realDB(c.t) //nolint:contextcheck // the helper takes the test, which carries its own deadlines
	if deps.DB.ReadOnly() {
		deps.DB = readOnlyDB{DB: pool}
	} else {
		deps.DB = pool
	}
	r.db = pool
	if err := insertDemoRows(ctx, pool); err != nil {
		return nil, err
	}
	mod, err := volunteers.New(ctx, deps)
	if err != nil {
		return nil, fmt.Errorf("volunteers.New: %w", err)
	}
	svc, ok := mod.(*volunteers.Service)
	if !ok {
		return nil, fmt.Errorf("volunteers.New returned %T, want *volunteers.Service", mod)
	}
	r.svc, r.mod = svc, mod
	c.mu.Lock()
	c.rigs[svc] = r
	c.mu.Unlock()
	return mod, nil
}

// seed books the subject on a shift (whose notes carry the free-text marker), adds a
// membership, an opt-out and a verified record with a certificate whose name carries the
// legal-name marker, as the spec's Test plan asks.
func (c *conformance) seed(ctx context.Context, mod module.Module, subject moduletest.Subject) error {
	svc, ok := mod.(*volunteers.Service)
	if !ok {
		return errors.New("the module under test is not a *volunteers.Service")
	}
	c.mu.Lock()
	r := c.rigs[svc]
	c.mu.Unlock()
	if r == nil {
		return errors.New("no rig for the module under test")
	}
	step := func(what string, err error) error {
		if err != nil {
			return fmt.Errorf("seed %s: %w", what, err)
		}
		return nil
	}
	r.boss = newPerson()
	r.az.Superuser(r.boss)
	for _, p := range []model.PersonID{r.boss, subject.Person} {
		if _, err := r.db.Exec(ctx, "INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now()) ON CONFLICT DO NOTHING", [16]byte(p)); err != nil {
			return step("person", err)
		}
	}
	r.ppl.Add(subject.Person, "Marker Playa")
	r.ppl.AddLegalName(subject.Person, subject.LegalName)
	r.az.Grant(subject.Person, pSignUp)
	r.set.Set(volunteers.SettingReminderLead, "0") // the seed runs before Start: no job handler is registered yet
	d := model.NewID[model.DepartmentID]()
	if _, err := r.db.Exec(ctx, "INSERT INTO core.department (id, name, created_at, updated_at) VALUES ($1, 'Seeded', now(), now())", [16]byte(d)); err != nil {
		return step("department", err)
	}
	r.dept.Add(d, "Seeded")
	boss := asPerson(ctx, r.boss)
	q, err := svc.CreateQualification(boss, vol.QualificationInput{Department: d, Name: "Seeded qualification"})
	if err != nil {
		return step("qualification", err)
	}
	pos, err := svc.CreatePosition(boss, d, vol.PositionInput{Name: "Seeded position", Access: vol.AccessOpen})
	if err != nil {
		return step("position", err)
	}
	sh, err := svc.CreateShift(boss, d, vol.ShiftInput{
		Position: pos.ID, Label: "Seeded shift", Starts: when(0), Ends: when(4), Zone: "UTC", Slots: 3, Location: "Ranger HQ", Notes: subject.Note,
	})
	if err != nil {
		return step("shift", err)
	}
	if _, err := svc.AddMember(boss, d, subject.Person); err != nil {
		return step("member", err)
	}
	if _, err := svc.SignUp(asPerson(ctx, subject.Person), sh.ID); err != nil {
		return step("signup", err)
	}
	if err := svc.SetListed(asPerson(ctx, subject.Person), false); err != nil {
		return step("listing", err)
	}
	rec, err := svc.UploadCertificate(asPerson(ctx, subject.Person), vol.UploadInput{
		Qualification: q.ID, AcquiredOn: jan1, Upload: upload("certificate.pdf", pdf()),
	})
	if err != nil {
		return step("certificate", err)
	}
	if _, err := svc.VerifyRecord(boss, rec.ID, vol.VerifyInput{}); err != nil {
		return step("verify", err)
	}
	return nil
}

// TestConformance_RealDatabase runs moduletest.Conformance with the module built by New over a
// real database (the seed books, verifies and uploads for real).
func TestConformance_RealDatabase(t *testing.T) {
	c := &conformance{t: t, rigs: map[*volunteers.Service]*rig{}}
	moduletest.Conformance(t, volunteers.Manifest, c.factory, c.provision, moduletest.WithSeed(c.seed), moduletest.WithDemoSeed())
}

// insertDemoRows inserts the persons, departments and event of demotest's fixed request, so
// the demo seed check (WithDemoSeed) finds the rows its foreign keys point at. The IDs are
// fixed, so the rows are the ones that check's request names.
func insertDemoRows(ctx context.Context, pool db.DB) error {
	req := demotest.NewFakeActors().Request()
	for _, p := range req.People {
		if _, err := pool.Exec(ctx, "INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now()) ON CONFLICT DO NOTHING", [16]byte(p)); err != nil {
			return fmt.Errorf("demo person: %w", err)
		}
	}
	for k, d := range req.Departments {
		if _, err := pool.Exec(ctx, "INSERT INTO core.department (id, name, created_at, updated_at) VALUES ($1, $2, now(), now()) ON CONFLICT DO NOTHING", [16]byte(d), "Demo "+k); err != nil {
			return fmt.Errorf("demo department: %w", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO core.event (id, name, kind, starts_at, ends_at, zone, created_at, updated_at)
		VALUES ($1, 'Demo event', 'burn', now(), now() + interval '4 days', 'UTC', now(), now()) ON CONFLICT DO NOTHING`, [16]byte(req.Event)); err != nil {
		return fmt.Errorf("demo event: %w", err)
	}
	return nil
}
