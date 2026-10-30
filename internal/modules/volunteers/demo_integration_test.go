//go:build integration

package volunteers_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/demo/demotest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// demoWorld builds a real database holding the departments, event and people a demo request
// names (all invented), and the playa names the walk-ons and actors need. skipName, when
// not empty, is an actor or walk-on key left without a playa name.
func demoWorld(t *testing.T, skipName string) (*rig, demoseed.Request, *demotest.FakeActors) {
	t.Helper()
	r := newReal(t)
	actors := demotest.NewFakeActors()
	req := actors.Request()
	for key, d := range req.Departments {
		r.exec("INSERT INTO core.department (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", [16]byte(d), "Demo "+key)
		r.dept.Add(d, "Demo "+key)
	}
	for key, p := range req.People {
		r.exec("INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(p))
		if key != skipName {
			r.ppl.Add(p, r.names.unique("Demo "+key))
		}
	}
	// Thursday to Sunday, in the future of the rig's clock.
	loc, err := time.LoadLocation("America/Los_Angeles")
	wantNoErr(t, err)
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, loc)
	end := time.Date(2026, 11, 9, 12, 0, 0, 0, loc)
	r.exec(`INSERT INTO core.event (id, name, kind, starts_at, ends_at, zone, created_at, updated_at)
		VALUES ($1, 'Ember Flats', 'burn', $2, $3, $4, now(), now())`, [16]byte(req.Event), start, end, loc.String())
	r.evt.Add(events.Info{ID: req.Event, Name: "Ember Flats", Kind: "burn", Dates: model.TimeRange{Start: start, End: end}, Zone: loc.String()})
	return r, req, actors
}

// F171: an actor lacking the permission for its step fails the seed with Permission.
func TestDemoSeed_ActorLacksPermission_ReturnsPermission(t *testing.T) {
	cases := []struct {
		name    string
		granted []string // actors that hold every permission
		leadNo  string   // when set, the lead holds the lead permissions at every department but this one
	}{
		{"nobody holds anything", nil, ""},
		{"steward only", []string{demoseed.ActorSteward}, ""},
		{"steward and lead only", []string{demoseed.ActorSteward, demoseed.ActorLead}, ""},
		{"lead lacks Gate (D1.1)", []string{demoseed.ActorSteward, demoseed.ActorParticipant}, demoseed.DeptGate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, req, actors := demoWorld(t, "")
			for _, k := range tc.granted {
				p, _ := actors.Person(k)
				r.az.Superuser(p)
			}
			if tc.leadNo != "" {
				lead, _ := actors.Person(demoseed.ActorLead)
				for _, key := range []string{demoseed.DeptRangers, demoseed.DeptGate, demoseed.DeptGreeters, demoseed.DeptMedical, demoseed.DeptDPW} {
					if key == tc.leadNo {
						continue
					}
					for _, perm := range []authz.Permission{pShifts, pRosters, pPositions, pMembers, pQualManage, pQualRead, pQualVerify, pSignUp} {
						r.az.GrantIn(lead, perm, req.Departments[key])
					}
				}
			}
			// The participant needs sign-up and read rights to finish; "steward and lead only"
			// leaves the participant with none, so the seed must stop at the participant's step.
			_, err := demoProvider(t, r).Seed(ctxT(t), req)
			wantKind(t, err, errs.Permission)
		})
	}
}

// F173: a booked person without a playa name fails the seed with Conflict "unnamed".
func TestDemoSeed_EveryBookedPersonIsNamed(t *testing.T) {
	r, req, actors := demoWorld(t, demoseed.ActorParticipant)
	for _, k := range []string{demoseed.ActorSteward, demoseed.ActorLead, demoseed.ActorParticipant} {
		p, _ := actors.Person(k)
		r.az.Superuser(p)
	}
	_, err := demoProvider(t, r).Seed(ctxT(t), req)
	wantConflict(t, err, "unnamed")
}

// F174: a normal run creates the described data, and Items says so.
func TestDemoSeed_CreatesTheDescribedData(t *testing.T) {
	r, req, actors := demoWorld(t, "")
	for _, k := range []string{demoseed.ActorSteward, demoseed.ActorLead, demoseed.ActorParticipant} {
		p, _ := actors.Person(k)
		r.az.Superuser(p)
	}
	res, err := demoProvider(t, r).Seed(ctxT(t), req)
	wantNoErr(t, err)
	if res.Items <= 0 {
		t.Fatalf("Result.Items = %d, want > 0", res.Items)
	}
	ctx := r.as(r.boss)

	quals, err := r.svc.ListQualifications(ctx, vol.QualificationQuery{})
	wantNoErr(t, err)
	names := map[string]bool{}
	for _, q := range quals {
		names[q.Name] = true
	}
	for _, want := range []string{"ICS-100", "Over 18", "Ranger training", "First aid"} {
		if !names[want] {
			t.Errorf("qualification %q was not created (have %v)", want, names)
		}
	}

	positions := map[string]vol.Position{}
	for _, key := range []string{demoseed.DeptRangers, demoseed.DeptGate, demoseed.DeptGreeters, demoseed.DeptMedical, demoseed.DeptDPW} {
		ps, err := r.svc.ListPositions(ctx, req.Departments[key], false)
		wantNoErr(t, err)
		for _, p := range ps {
			positions[p.Name] = p
		}
	}
	for name, wantReqs := range map[string]int{"Dirt Ranger": 2, "Khaki": 3, "Gate Crew": 0, "Greeter": 0, "Medic": 1, "Inventory Crew": 0} {
		p, ok := positions[name]
		if !ok {
			t.Errorf("position %q was not created", name)
			continue
		}
		if len(p.Requirements) != wantReqs {
			t.Errorf("position %q has %d requirements, want %d", name, len(p.Requirements), wantReqs)
		}
	}
	if positions["Khaki"].Access != vol.AccessMembers || positions["Medic"].Access != vol.AccessMembers || positions["Dirt Ranger"].Access != vol.AccessOpen {
		t.Errorf("access: Khaki=%q Medic=%q Dirt Ranger=%q", positions["Khaki"].Access, positions["Medic"].Access, positions["Dirt Ranger"].Access)
	}

	for key, want := range map[string]int{demoseed.DeptRangers: 8, demoseed.DeptMedical: 4} {
		ml, err := r.svc.ListMembers(ctx, vol.MemberQuery{Department: req.Departments[key], Page: vol.Page{Limit: vol.MaxList}})
		wantNoErr(t, err)
		if len(ml.Items) != want {
			t.Errorf("%s has %d members, want %d", key, len(ml.Items), want)
		}
	}

	part := req.People[demoseed.ActorParticipant]
	recs, err := r.svc.RecordsOf(ctx, part)
	wantNoErr(t, err)
	verified, pendingOwn := 0, 0
	for _, rec := range recs.Items {
		if rec.Verified() {
			verified++
		} else {
			pendingOwn++
		}
	}
	if verified != 2 || pendingOwn != 1 {
		t.Errorf("the participant has %d verified and %d pending records, want 2 (Ranger training, Over 18) and 1 (ICS-100)", verified, pendingOwn)
	}
	leadRecs, err := r.svc.RecordsOf(ctx, req.People[demoseed.ActorLead])
	wantNoErr(t, err)
	if len(leadRecs.Items) != 0 {
		t.Errorf("the lead has %d records, want none (D1.2: a lead cannot verify their own)", len(leadRecs.Items))
	}
	pending, err := r.svc.PendingRecords(ctx, vol.PendingQuery{})
	wantNoErr(t, err)
	if len(pending) != 1 || pending[0].Person != part {
		t.Errorf("pending records = %d, want exactly one, the participant's ICS-100", len(pending))
	}

	list, err := r.svc.ListShifts(ctx, vol.ShiftQuery{Event: req.Event, IncludeCanceled: true, Page: vol.Page{Limit: vol.MaxList}})
	wantNoErr(t, err)
	perPos := map[string]int{}
	canceled := 0
	for _, sh := range list.Items {
		perPos[sh.PositionName]++
		if sh.Canceled() {
			canceled++
		}
	}
	// Thursday to Sunday: four days of each pattern.
	for name, want := range map[string]int{"Dirt Ranger": 12, "Khaki": 8, "Gate Crew": 12, "Greeter": 8, "Medic": 8} {
		if perPos[name] != want {
			t.Errorf("%s has %d shifts, want %d", name, perPos[name], want)
		}
	}
	if canceled != 1 {
		t.Errorf("%d canceled shifts, want 1 (Sunday 20-02 Dirt Ranger)", canceled)
	}
	standing, err := r.svc.ListShifts(ctx, vol.ShiftQuery{Department: req.Departments[demoseed.DeptDPW], Page: vol.Page{Limit: vol.MaxList}})
	wantNoErr(t, err)
	if len(standing.Items) != 1 || !standing.Items[0].Event.IsZero() || standing.Items[0].PositionName != "Inventory Crew" || standing.Items[0].Slots != 6 {
		t.Errorf("DPW shifts = %+v, want one standing Inventory Crew shift with 6 slots and no event", standing.Items)
	}

	sched, err := r.svc.MySchedule(r.as(part), vol.ScheduleQuery{Page: vol.Page{Limit: vol.MaxList}})
	wantNoErr(t, err)
	if len(sched.Items) != 2 {
		t.Errorf("the participant's schedule has %d entries, want 2", len(sched.Items))
	}
	lead := r.as(req.People[demoseed.ActorLead])
	ls, err := r.svc.MySchedule(lead, vol.ScheduleQuery{Page: vol.Page{Limit: vol.MaxList}})
	wantNoErr(t, err)
	if len(ls.Items) != 1 {
		t.Errorf("the lead's schedule has %d entries, want 1 (Khaki on Friday, by self-waiver)", len(ls.Items))
	}
}
