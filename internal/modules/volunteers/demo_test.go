package volunteers_test

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/demo/demotest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// demoProvider returns the module's demo seed provider through its public contribution.
func demoProvider(t *testing.T, r *rig) demoseed.Provider {
	t.Helper()
	v, ok := r.svc.Contributions()[demoseed.Point]
	if !ok {
		t.Fatalf("Contributions lacks %s", demoseed.Point)
	}
	p, ok := v.(demoseed.Provider)
	if !ok {
		t.Fatalf("the %s contribution is %T, not a demoseed.Provider", demoseed.Point, v)
	}
	return p
}

// fakeDemoRequest is a complete request over the fake actors with its event known to the rig.
func fakeDemoRequest(r *rig) (demoseed.Request, *demotest.FakeActors) {
	actors := demotest.NewFakeActors()
	req := actors.Request()
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	r.evt.Add(events.Info{
		ID: req.Event, Name: "Ember Flats", Kind: "burn",
		Dates: model.TimeRange{Start: start, End: start.Add(4 * 24 * time.Hour)}, Zone: "UTC",
	})
	return req, actors
}

// F172 (input side): a request that cannot be seeded from is Invalid, before any write.
func TestDemoSeed_IncompleteRequest_ReturnsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*demoseed.Request)
	}{
		{"no actors", func(q *demoseed.Request) { q.Actors = nil }},
		{"no event", func(q *demoseed.Request) { q.Event = model.EventID{} }},
		{"no rangers department", func(q *demoseed.Request) { delete(q.Departments, demoseed.DeptRangers) }},
		{"no dpw department", func(q *demoseed.Request) { delete(q.Departments, demoseed.DeptDPW) }},
		{"no lead person", func(q *demoseed.Request) { delete(q.People, demoseed.ActorLead) }},
		{"no participant person", func(q *demoseed.Request) { delete(q.People, demoseed.ActorParticipant) }},
		{"first walk-on missing", func(q *demoseed.Request) { delete(q.People, demoseed.WalkOnKey(1)) }},
		{"last walk-on missing", func(q *demoseed.Request) { delete(q.People, demoseed.WalkOnKey(demoseed.WalkOns)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, opts{})
			req, _ := fakeDemoRequest(r)
			tc.mutate(&req)
			_, err := demoProvider(t, r).Seed(ctxT(t), req)
			wantKind(t, err, errs.Invalid)
		})
	}
}

// F172: an event whose zone is not a time zone is Invalid, not a panic or a silent UTC.
func TestDemoSeed_BadEventZone_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	req, _ := fakeDemoRequest(r)
	r.evt.Add(events.Info{ID: req.Event, Name: "Ember Flats", Kind: "burn", Zone: "Not/AZone",
		Dates: model.TimeRange{Start: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)}})
	_, err := demoProvider(t, r).Seed(ctxT(t), req)
	wantKind(t, err, errs.Invalid)
}

// The event must be known to the directory.
func TestDemoSeed_UnknownEvent_ReturnsNotFound(t *testing.T) {
	r := newRig(t, opts{})
	actors := demotest.NewFakeActors()
	_, err := demoProvider(t, r).Seed(ctxT(t), actors.Request())
	wantKind(t, err, errs.NotFound)
}

// A cancelled context stops the seed with Canceled.
func TestDemoSeed_CancelledContext_ReturnsCanceled(t *testing.T) {
	r := newRig(t, opts{})
	req, _ := fakeDemoRequest(r)
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := demoProvider(t, r).Seed(ctx, req)
	if !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v (kind %v), want Canceled", err, errs.KindOf(err))
	}
}

// Actors.As failing is reported with its own kind.
func TestDemoSeed_ActorsFail_ReturnsTheirKind(t *testing.T) {
	r := newRig(t, opts{})
	req, _ := fakeDemoRequest(r)
	req.Actors = failingActors{}
	_, err := demoProvider(t, r).Seed(ctxT(t), req)
	wantKind(t, err, errs.Unavailable)
}

type failingActors struct{}

func (failingActors) As(context.Context, string) (context.Context, error) {
	return nil, errs.E("test.As", errs.Unavailable, context.DeadlineExceeded)
}

// F175: an event without dates is Invalid on "event" (required); the provider invents none.
func TestDemoSeed_EventWithoutDates_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	req, _ := fakeDemoRequest(r)
	r.evt.Add(events.Info{ID: req.Event, Name: "Ember Flats", Kind: "burn", Zone: "UTC"})
	_, err := demoProvider(t, r).Seed(ctxT(t), req)
	wantKind(t, err, errs.Invalid)
	if got := fieldCodeOf(err, "event"); got != "required" {
		t.Fatalf("field event code = %q, want required (err: %v)", got, err)
	}
}
