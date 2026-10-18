//go:build integration

package people_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// gate is a module whose DeletePerson blocks, inside people's deletion transaction, until
// the test releases it: the deletion transaction is held open while a rival call starts.
type gate struct {
	src     *fakeSource
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGate(r *irig, name string) *gate {
	g := &gate{entered: make(chan struct{}), release: make(chan struct{})}
	g.src = &fakeSource{name: name, log: r.log}
	g.src.del = func(ctx context.Context, _ model.PersonID) (people.ModuleDeletion, error) {
		g.once.Do(func() { close(g.entered) })
		select {
		case <-g.release:
			return people.ModuleDeletion{}, nil
		case <-ctx.Done():
			return people.ModuleDeletion{}, errs.E("gate", errs.KindOf(ctx.Err()), ctx.Err())
		}
	}
	return g
}

// startDelete runs Delete in the background and returns once the gate is held.
func (g *gate) startDelete(t *testing.T, r *irig, ctx context.Context, p model.PersonID) <-chan delResult { //nolint:revive // ctx second: the rig is the subject
	t.Helper()
	done := make(chan delResult, 1)
	go func() {
		rep, err := r.svc.Delete(ctx, p)
		done <- delResult{rep, err}
	}()
	select {
	case <-g.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the deletion never reached the module")
	}
	return done
}

type delResult struct {
	rep people.DeletionReport
	err error
}

func waitFor[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for a concurrent call")
		panic("unreachable")
	}
}

// 1. UpdateMe racing a Delete of the same person (real identity: the account goes too).
func TestUpdateMe_RacingDelete_NeverRecreatesTheRow(t *testing.T) {
	r := newIRig(t, true)
	g := newGate(r, "gated")
	r.link(g.src.source())
	u := r.signUp("racing.burner", true)
	_, err := r.svc.UpdateMe(u.ctx(t), people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantNoErr(t, err)
	_, actx := r.admin(permDelete)

	del := g.startDelete(t, r, actx, u.person)
	type upd struct{ err error }
	racing := make(chan upd, 1)
	go func() {
		_, err := r.svc.UpdateMe(u.ctx(t), people.ProfileInput{PlayaName: str("Racing Ember"), Pronouns: str("they/them")})
		racing <- upd{err}
	}()
	close(g.release)
	d := waitFor(t, del)
	wantNoErr(t, d.err)
	ru := waitFor(t, racing)
	if ru.err != nil && !errs.Is(ru.err, errs.Unauthenticated) {
		t.Errorf("the racing save failed with kind %v (%v), want success before the delete or Unauthenticated", errs.KindOf(ru.err), ru.err)
	}
	if r.rowExists(u.person) || r.accountRows(u.person) != 0 {
		t.Fatal("the person or the account is back after the deletion")
	}

	// After the delete committed, a save with the old session is Unauthenticated and
	// leaves nothing behind.
	entries, sigs := len(r.rec.Recorded()), len(r.bus.Published())
	_, err = r.svc.UpdateMe(u.ctx(t), people.ProfileInput{PlayaName: str("Back From The Dead")})
	wantKind(t, err, errs.Unauthenticated)
	if r.rowExists(u.person) {
		t.Error("UpdateMe re-created the deleted person's row")
	}
	if len(r.rec.Recorded()) != entries || len(r.bus.Published()) != sigs {
		t.Error("the refused save recorded or published something")
	}
}

// 2. LinkAccount racing a Delete of the same walk-on.
func TestLinkAccount_RacingDelete_NeverPointsAtADeletedPerson(t *testing.T) {
	r := newIRig(t, true)
	g := newGate(r, "gated")
	r.link(g.src.source())
	_, mctx := r.admin(permManage, permDelete)
	walkOn := r.create(mctx, "Walk On").Person.ID
	u := r.signUp("link.racer", true)

	del := g.startDelete(t, r, mctx, walkOn)
	type lk struct{ err error }
	racing := make(chan lk, 1)
	go func() {
		_, err := r.svc.LinkAccount(mctx, walkOn, u.email)
		racing <- lk{err}
	}()
	close(g.release)
	wantNoErr(t, waitFor(t, del).err)
	lr := waitFor(t, racing)
	if lr.err != nil && !errs.Is(lr.err, errs.NotFound) {
		t.Errorf("the racing link failed with kind %v (%v), want success or NotFound", errs.KindOf(lr.err), lr.err)
	}
	if r.rowExists(walkOn) {
		t.Fatal("the walk-on survives")
	}
	if n := r.scalarInt(`SELECT count(*) FROM core.account a WHERE NOT EXISTS (SELECT 1 FROM core.person p WHERE p.id = a.person_id)`); n != 0 {
		t.Errorf("%d accounts point at a person that does not exist", n)
	}
	if r.accountRows(walkOn) != 0 {
		t.Error("an account still points at the deleted walk-on")
	}
}

// 3. Deletion racing AddContact: the report counts every contact it deleted.
func TestDelete_RacingAddContact_ReportCountsEveryContact(t *testing.T) {
	r := newIRig(t, false)
	g := newGate(r, "gated")
	r.link(g.src.source())
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	r.insertContact(target, mECName, mECPhone, "", 1)
	_, actx := r.admin(permDelete)

	del := g.startDelete(t, r, actx, target)
	racing := make(chan error, 1)
	go func() {
		_, err := r.svc.AddContact(asPerson(ctxT(t), target), target, people.ContactInput{Name: "Late Addition", Phone: "+1 555 0100"})
		racing <- err
	}()
	close(g.release)
	d := waitFor(t, del)
	wantNoErr(t, d.err)
	addErr := waitFor(t, racing)
	if addErr != nil && !errs.Is(addErr, errs.NotFound) {
		t.Errorf("the racing add failed with kind %v (%v), want success or NotFound", errs.KindOf(addErr), addErr)
	}
	if r.rowExists(target) || r.contactRows(target) != 0 {
		t.Fatal("the person or a contact survives")
	}
	want := 2 // the person and the contact that was there
	if addErr == nil {
		want = 3
	}
	if d.rep.People != want {
		t.Errorf("People = %d, want %d: every deleted contact must be counted (add succeeded: %v)", d.rep.People, want, addErr == nil)
	}
}

// cancelAfterExport cancels a context as soon as people.person.exported is recorded.
type cancelAfterExport struct {
	audit.Recorder
	cancel context.CancelFunc
}

func (c cancelAfterExport) Record(ctx context.Context, es ...audit.Entry) error {
	err := c.Recorder.Record(ctx, es...)
	for _, e := range es {
		if string(e.Action) == "people.person.exported" {
			c.cancel()
		}
	}
	return err //nolint:wrapcheck // a test wrapper
}

// 4. A self-export cancelled after the export was recorded still starts the cooldown.
func TestExport_CancelledAfterRecording_StillStartsTheCooldown(t *testing.T) {
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	r := newIRig(t, false, func(o *people.Options) { o.Audit = cancelAfterExport{Recorder: o.Audit, cancel: cancel} })
	r.link()
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, err := r.svc.Export(asPerson(ctx, me), me)
	_ = err // success or Canceled: the entry was recorded either way
	if r.count("people.person.exported") != 1 {
		t.Fatalf("%d exported entries, want 1", r.count("people.person.exported"))
	}
	wantNoErr(t, r.clk.Advance(time.Minute))
	_, err = r.svc.Export(asPerson(ctxT(t), me), me)
	wantRateLimited(t, err)
	wantNoErr(t, r.clk.Advance(people.ExportCooldown))
	_, err = r.svc.Export(asPerson(ctxT(t), me), me)
	wantNoErr(t, err)
}

// 5. Dropping the orphan on link records people.person.deleted as the system.
func TestLinkAccount_OrphanDrop_RecordsDeletedAsTheSystem(t *testing.T) {
	f := newLinkFixture(t)
	u := f.signUp("orphan.audit", true)
	res, err := f.svc.LinkAccount(f.ctx, f.target, u.email)
	wantNoErr(t, err)
	if !res.OrphanDeleted {
		t.Fatal("setup: the orphan was kept")
	}
	var got []audit.Entry
	for _, e := range f.entries("people.person.deleted") {
		if e.Subject == u.person {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%d people.person.deleted entries about the orphan, want 1", len(got))
	}
	e := got[0]
	if e.Actor.Kind != audit.ActorSystem || !e.Actor.Person.IsZero() {
		t.Errorf("actor = %+v, want the system", e.Actor)
	}
	if e.Record.Kind != "people.person" || e.Record.ID != [16]byte(u.person) {
		t.Errorf("record = %+v", e.Record)
	}
}
