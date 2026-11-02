//go:build integration

package datarights_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func newRequestID() datarights.RequestID { return model.NewID[datarights.RequestID]() }

func TestRequestDeletion_CreatesAnOpenRequest(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
	wantNoErr(t, err)
	if req.ID.IsZero() || req.Person != me || req.Kind != "delete" || req.Status != datarights.RequestOpen || req.Note != mNote {
		t.Errorf("request = %+v", req)
	}
	if !req.CreatedAt.Equal(epoch) || !req.DecidedAt.IsZero() || !req.DecidedBy.IsZero() || req.DecisionNote != "" {
		t.Errorf("request = %+v, want created at the clock and undecided", req)
	}
	e := r.only("datarights.request.created")
	if e.Subject != me || e.Record.Kind != "people.request" || e.Record.ID != [16]byte(req.ID) {
		t.Errorf("entry = %+v", e)
	}
	sigs := r.published(sigRequested)
	if len(sigs) != 1 {
		t.Fatalf("published %d datarights.deletion-requested", len(sigs))
	}
	gotReq, err := signals.Get[datarights.RequestID](sigs[0].Payload, "request")
	wantNoErr(t, err)
	if gotReq != req.ID || payloadPerson(sigs[0]) != me {
		t.Errorf("payload = %v", sigs[0].Payload)
	}
	r.noLeak(mNote)
}

// F40
func TestRequestDeletion_Open_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	_, err := r.svc.RequestDeletion(ctx, "")
	wantNoErr(t, err)
	_, err = r.svc.RequestDeletion(ctx, "again")
	wantCode(t, err, errs.Conflict, "duplicate")
	if n := r.requestRows(me); n != 1 {
		t.Errorf("%d request rows, want 1", n)
	}
	if r.count("datarights.request.created") != 1 || len(r.published(sigRequested)) != 1 {
		t.Error("the refused request was recorded or published")
	}
}

func TestRequestDeletion_Concurrent_OnlyOneWins(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errs.Is(err, errs.Conflict):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || conflicts != 5 {
		t.Errorf("%d succeeded and %d conflicted, want 1 and 5", ok, conflicts)
	}
	if n := r.requestRows(me); n != 1 {
		t.Errorf("%d request rows, want 1", n)
	}
}

// F41
func TestRequestDeletion_LongNote_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	ctx := asPerson(ctxT(t), me)
	_, err := r.svc.RequestDeletion(ctx, repeat("n", datarights.MaxNoteBytes+1))
	wantField(t, err, "note", "too_long")
	if got := paramOf(err, "note", "too_long", "max"); got != "500" {
		t.Errorf("max param = %q, want 500", got)
	}
	_, err = r.svc.RequestDeletion(ctx, "bad\x07note")
	wantField(t, err, "note", "control_chars")
	if n := r.requestRows(me); n != 0 {
		t.Errorf("%d request rows after refused requests", n)
	}
	_, err = r.svc.RequestDeletion(ctx, repeat("n", datarights.MaxNoteBytes))
	wantNoErr(t, err)
}

func TestRequestDeletion_PersonWithoutARow_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, false)
	_, err := r.svc.RequestDeletion(asPerson(ctxT(t), newPerson()), "")
	wantKind(t, err, errs.NotFound)
}

func TestMyRequests_NewestFirst_OnlyMine(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	other := r.insertPerson(seed{Playa: "Pell Mallow"})
	ctx := asPerson(ctxT(t), me)
	got, err := r.svc.MyRequests(ctx)
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Fatalf("requests before any = %v", got)
	}
	first, err := r.svc.RequestDeletion(ctx, "first")
	wantNoErr(t, err)
	_, err = r.svc.RequestDeletion(asPerson(ctxT(t), other), "theirs")
	wantNoErr(t, err)
	wantNoErr(t, r.svc.WithdrawRequest(ctx, first.ID))
	wantNoErr(t, r.clk.Advance(time.Hour))
	second, err := r.svc.RequestDeletion(ctx, "second")
	wantNoErr(t, err)
	got, err = r.svc.MyRequests(ctx)
	wantNoErr(t, err)
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("MyRequests = %+v, want the second then the first", got)
	}
	if got[1].Status != datarights.RequestWithdrawn || got[0].Status != datarights.RequestOpen {
		t.Errorf("statuses = %v, %v", got[0].Status, got[1].Status)
	}
	for _, q := range got {
		if q.Person != me {
			t.Errorf("another person's request in MyRequests: %+v", q)
		}
	}
}

// F42
func TestWithdrawRequest_Table(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	other := r.insertPerson(seed{Playa: "Pell Mallow"})
	ctx := asPerson(ctxT(t), me)
	otherCtx := asPerson(ctxT(t), other)
	mine, err := r.svc.RequestDeletion(ctx, "")
	wantNoErr(t, err)

	t.Run("someone else's request is not found", func(t *testing.T) {
		wantKind(t, r.svc.WithdrawRequest(otherCtx, mine.ID), errs.NotFound)
	})
	t.Run("unknown request is not found", func(t *testing.T) {
		wantKind(t, r.svc.WithdrawRequest(ctx, newRequestID()), errs.NotFound)
	})
	t.Run("own open request is withdrawn", func(t *testing.T) {
		wantNoErr(t, r.svc.WithdrawRequest(ctx, mine.ID))
		e := r.only("datarights.request.withdrawn")
		if e.Subject != me {
			t.Errorf("entry subject = %v", e.Subject)
		}
		got, err := r.svc.MyRequests(ctx)
		wantNoErr(t, err)
		if got[0].Status != datarights.RequestWithdrawn {
			t.Errorf("request = %+v", got[0])
		}
	})
	t.Run("not open any more", func(t *testing.T) {
		err := r.svc.WithdrawRequest(ctx, mine.ID)
		wantCode(t, err, errs.Conflict, "status")
	})
	t.Run("a withdrawn request lets the person ask again", func(t *testing.T) {
		_, err := r.svc.RequestDeletion(ctx, "")
		wantNoErr(t, err)
	})
}

// F44
func TestListRequests_NoPermission_ReturnsPermission(t *testing.T) {
	r := newIRig(t, false)
	for _, perms := range map[string][]authz.Permission{"nothing": nil, "export only": {permExport}, "manage only": {permManage}} {
		_, ctx := r.admin(perms...)
		_, err := r.svc.ListRequests(ctx, datarights.RequestQuery{})
		wantKind(t, err, errs.Permission)
		_, err = r.svc.DeclineRequest(ctx, newRequestID(), "")
		wantKind(t, err, errs.Permission)
	}
}

func TestListRequests_DefaultOpenOldestFirst_AndFilters(t *testing.T) {
	r := newIRig(t, false)
	p1 := r.insertPerson(seed{Playa: "Alpha"})
	p2 := r.insertPerson(seed{Playa: "Bravo"})
	p3 := r.insertPerson(seed{Playa: "Charlie"})
	_, ctx := r.admin(permDelete)

	ask := func(p model.PersonID) datarights.Request {
		req, err := r.svc.RequestDeletion(asPerson(ctxT(t), p), "")
		wantNoErr(t, err)
		wantNoErr(t, r.clk.Advance(time.Minute))
		return req
	}
	r1 := ask(p1)
	r2 := ask(p2)
	r3 := ask(p3)
	wantNoErr(t, r.svc.WithdrawRequest(asPerson(ctxT(t), p3), r3.ID))
	_, err := r.svc.DeclineRequest(ctx, r2.ID, "not now")
	wantNoErr(t, err)

	ids := func(q datarights.RequestQuery) []datarights.RequestID {
		got, err := r.svc.ListRequests(ctx, q)
		wantNoErr(t, err)
		var out []datarights.RequestID
		for _, x := range got {
			out = append(out, x.ID)
		}
		return out
	}
	eq := func(got []datarights.RequestID, want ...datarights.RequestID) bool { return slices.Equal(got, want) }
	if got := ids(datarights.RequestQuery{}); !eq(got, r1.ID) {
		t.Errorf("default (open) = %v, want only the first", got)
	}
	if got := ids(datarights.RequestQuery{Status: datarights.RequestDeclined}); !eq(got, r2.ID) {
		t.Errorf("declined = %v", got)
	}
	if got := ids(datarights.RequestQuery{Status: datarights.RequestWithdrawn}); !eq(got, r3.ID) {
		t.Errorf("withdrawn = %v", got)
	}
	if got := ids(datarights.RequestQuery{Person: p1}); !eq(got, r1.ID) {
		t.Errorf("person filter = %v", got)
	}
	if got := ids(datarights.RequestQuery{Person: p2}); len(got) != 0 {
		t.Errorf("person with no open request = %v", got)
	}
	// Oldest first.
	p4 := r.insertPerson(seed{Playa: "Delta"})
	r4 := ask(p4)
	if got := ids(datarights.RequestQuery{}); !eq(got, r1.ID, r4.ID) {
		t.Errorf("open requests = %v, want oldest first [%v %v]", got, r1.ID, r4.ID)
	}
}

func TestDeclineRequest_RecordsTheDecision(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	admin, ctx := r.admin(permDelete)
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
	wantNoErr(t, err)
	wantNoErr(t, r.clk.Advance(time.Hour))
	got, err := r.svc.DeclineRequest(ctx, req.ID, mDecision)
	wantNoErr(t, err)
	if got.ID != req.ID || got.Status != datarights.RequestDeclined || got.DecidedBy != admin ||
		got.DecisionNote != mDecision || !got.DecidedAt.Equal(epoch.Add(time.Hour)) {
		t.Errorf("declined = %+v", got)
	}
	e := r.only("datarights.request.declined")
	if e.Subject != me || e.Record.Kind != "people.request" {
		t.Errorf("entry = %+v", e)
	}
	mine, err := r.svc.MyRequests(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if mine[0].Status != datarights.RequestDeclined || mine[0].DecisionNote != mDecision {
		t.Errorf("the person does not see the decision: %+v", mine[0])
	}
	// F43
	_, err = r.svc.DeclineRequest(ctx, req.ID, "")
	wantCode(t, err, errs.Conflict, "status")
	// The person may ask again after a decline.
	_, err = r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
	wantNoErr(t, err)
	r.noLeak(mNote, mDecision)
}

// F41
func TestDeclineRequest_LongNote_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDelete)
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
	wantNoErr(t, err)
	_, err = r.svc.DeclineRequest(ctx, req.ID, repeat("n", datarights.MaxNoteBytes+1))
	wantField(t, err, "note", "too_long")
	got, err := r.svc.MyRequests(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if got[0].Status != datarights.RequestOpen {
		t.Error("a refused decline changed the request")
	}
}

// F43
func TestDeclineRequest_NotOpen_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDelete)
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
	wantNoErr(t, err)
	wantNoErr(t, r.svc.WithdrawRequest(asPerson(ctxT(t), me), req.ID))
	_, err = r.svc.DeclineRequest(ctx, req.ID, "")
	wantCode(t, err, errs.Conflict, "status")
	_, err = r.svc.DeclineRequest(ctx, newRequestID(), "")
	wantKind(t, err, errs.NotFound)
}

func TestFulfilRequest_Table(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDelete)
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
	wantNoErr(t, err)

	t.Run("no permission", func(t *testing.T) {
		_, c := r.admin(permExport, permManage)
		_, err := r.svc.FulfilRequest(c, req.ID)
		wantKind(t, err, errs.Permission)
	})
	t.Run("unknown request", func(t *testing.T) {
		_, err := r.svc.FulfilRequest(ctx, newRequestID())
		wantKind(t, err, errs.NotFound)
	})
	t.Run("the request's own person cannot fulfil it", func(t *testing.T) {
		r.grant(me, permDelete)
		_, err := r.svc.FulfilRequest(asPerson(ctxT(t), me), req.ID)
		wantField(t, err, "person", "self")
		if !r.rowExists(me) {
			t.Error("the person deleted themselves through their own request")
		}
	})
	t.Run("an admin fulfils it", func(t *testing.T) {
		rep, err := r.svc.FulfilRequest(ctx, req.ID)
		wantNoErr(t, err)
		if rep.Person != me || rep.People < 1 {
			t.Errorf("report = %+v", rep)
		}
		if r.rowExists(me) || r.requestRows(me) != 0 { //nolint:contextcheck // test helpers derive their own bounded context
			t.Error("the person or the request survives")
		}
		f := r.only("datarights.request.fulfilled")
		if f.Subject != me {
			t.Errorf("fulfilled entry subject = %v", f.Subject)
		}
		d := r.only("datarights.delete")
		if d.Subject != me || d.Reason != "request:"+req.ID.String() {
			t.Errorf("deleted entry = %+v, want Reason request:<id>", d)
		}
	})
	t.Run("a second fulfil finds nothing", func(t *testing.T) {
		_, err := r.svc.FulfilRequest(ctx, req.ID)
		wantKind(t, err, errs.NotFound)
	})
}

func TestFulfilRequest_NotOpen_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDelete)
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
	wantNoErr(t, err)
	_, err = r.svc.DeclineRequest(ctx, req.ID, "")
	wantNoErr(t, err)
	_, err = r.svc.FulfilRequest(ctx, req.ID)
	wantCode(t, err, errs.Conflict, "status")
	if !r.rowExists(me) {
		t.Error("a declined request deleted the person")
	}
}

// F57
func TestListRequests_WithNote_RecordsRead(t *testing.T) {
	r := newIRig(t, false)
	withNote := r.insertPerson(seed{Playa: "Alpha"})
	without := r.insertPerson(seed{Playa: "Bravo"})
	_, err := r.svc.RequestDeletion(asPerson(ctxT(t), withNote), mNote)
	wantNoErr(t, err)
	_, err = r.svc.RequestDeletion(asPerson(ctxT(t), without), "")
	wantNoErr(t, err)
	_, ctx := r.admin(permDelete)

	got, err := r.svc.ListRequests(ctx, datarights.RequestQuery{})
	wantNoErr(t, err)
	if len(got) != 2 {
		t.Fatalf("%d requests, want 2", len(got))
	}
	e := r.only("datarights.request.read")
	if e.Subject != withNote || e.Level != model.SensitivityPersonal || !slices.Equal(e.Fields, []string{"note"}) || e.Record.Kind != "people.request" {
		t.Errorf("entry = %+v, want one read of the note, about the person who wrote it", e)
	}
	r.noLeak(mNote)

	// A list with no notes records nothing; so does the requester reading their own.
	before := r.count("datarights.request.read")
	_, err = r.svc.ListRequests(ctx, datarights.RequestQuery{Person: without})
	wantNoErr(t, err)
	if r.count("datarights.request.read") != before {
		t.Error("a list without notes was recorded")
	}

	// The requester reading their own request (they may also hold the permission) records nothing.
	selfie := r.insertPerson(seed{Playa: "Charlie"})
	_, err = r.svc.RequestDeletion(asPerson(ctxT(t), selfie), "my own words")
	wantNoErr(t, err)
	r.grant(selfie, permDelete)
	before = r.count("datarights.request.read")
	_, err = r.svc.ListRequests(asPerson(ctxT(t), selfie), datarights.RequestQuery{Person: selfie})
	wantNoErr(t, err)
	if r.count("datarights.request.read") != before {
		t.Error("a requester reading their own request was recorded")
	}

	// A recorder failure fails the read: no audit, no data.
	r.rec.Fail(errs.E("test", errs.Unavailable, errors.New("audit down")))
	_, err = r.svc.ListRequests(ctx, datarights.RequestQuery{})
	wantKind(t, err, errs.Unavailable)
}
