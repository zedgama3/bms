//go:build integration

package volunteers_test

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/demo/demotest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// All people here are invented. demoRig builds the module over a real database and
// registers the request's event, departments and people in the fixtures, with the
// permissions the starter roles would give the three actors the provider uses.
func demoRig(t *testing.T, grantLeadEverywhere, dated bool) (*rig, demoseed.Request) {
	t.Helper()
	r := newReal(t)
	req := demotest.NewFakeActors().Request()
	zone := "America/Chicago"
	loc, err := time.LoadLocation(zone)
	wantNoErr(t, err)
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, loc) // a Thursday
	end := time.Date(2026, 11, 23, 12, 0, 0, 0, loc)
	r.exec(`INSERT INTO core.event (id, name, kind, starts_at, ends_at, zone, created_at, updated_at)
		VALUES ($1, 'Ember Flats 2026', 'burn', $2, $3, $4, now(), now())`, [16]byte(req.Event), start, end, zone)
	info := events.Info{ID: req.Event, Name: "Ember Flats 2026", Kind: "burn", Dates: model.TimeRange{Start: start, End: end}, Zone: zone}
	if !dated {
		info.Dates = model.TimeRange{} // as moduletest's demoseed rule registers it
	}
	r.evt.Add(info)
	for key, id := range req.Departments {
		r.exec("INSERT INTO core.department (id, name, created_at, updated_at) VALUES ($1, $2, now(), now())", [16]byte(id), key)
		r.dept.Add(id, key)
	}
	for key, id := range req.People {
		r.exec("INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(id))
		r.ppl.Add(id, "Playa "+key)
	}
	lead := req.People[demoseed.ActorLead]
	for _, key := range []string{demoseed.DeptRangers, demoseed.DeptGate, demoseed.DeptGreeters, demoseed.DeptMedical, demoseed.DeptDPW} {
		if key != demoseed.DeptRangers && !grantLeadEverywhere {
			continue
		}
		for _, p := range []authz.Permission{pShifts, pRosters, pPositions, pMembers, pQualManage, pQualRead, pQualVerify} {
			r.az.GrantIn(lead, p, req.Departments[key])
		}
	}
	r.az.Grant(req.People[demoseed.ActorSteward], pQualManage)
	r.az.Grant(req.People[demoseed.ActorParticipant], pSignUp)
	return r, req
}

// seeder returns the module's demo provider.
func seeder(t *testing.T, r *rig) demoseed.Provider {
	t.Helper()
	c, ok := r.mod.(module.Contributor)
	if !ok {
		t.Fatalf("%T is not a module.Contributor", r.mod)
	}
	p, ok := c.Contributions()[demoseed.Point].(demoseed.Provider)
	if !ok {
		t.Fatalf("Contributions()[%q] is not a demoseed.Provider", demoseed.Point)
	}
	return p
}

func count(ctx context.Context, t *testing.T, r *rig, sql string) int {
	t.Helper()
	var n int
	if err := r.db.QueryRow(ctx, sql).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// TestDemoProvider_SeedsTheTable runs the provider and checks the rows it left behind.
func TestDemoProvider_SeedsTheTable(t *testing.T) {
	r, req := demoRig(t, true, true)
	ctx := ctxT(t)
	res, err := seeder(t, r).Seed(ctx, req)
	wantNoErr(t, err)
	for _, tc := range []struct {
		name, sql string
		want      int
	}{
		{"qualifications", `SELECT count(*) FROM volunteers.qualification`, 4},
		{"positions", `SELECT count(*) FROM volunteers.position`, 6},
		{"members", `SELECT count(*) FROM volunteers.member`, 12},
		{"records", `SELECT count(*) FROM volunteers.qualification_record`, 15},
		{"pending records", `SELECT count(*) FROM volunteers.qualification_record WHERE verified_by IS NULL`, 1},
		{"event shifts", `SELECT count(*) FROM volunteers.shift WHERE event_id IS NOT NULL`, 48},
		{"standing shifts", `SELECT count(*) FROM volunteers.shift WHERE event_id IS NULL`, 1},
		{"canceled shifts", `SELECT count(*) FROM volunteers.shift WHERE canceled_at IS NOT NULL`, 1},
		{"booked sign-ups", `SELECT count(*) FROM volunteers.signup WHERE status = 'booked'`, 28},
		{"waived sign-ups", `SELECT count(*) FROM volunteers.signup WHERE waived`, 2},
	} {
		if got := count(ctx, t, r, tc.sql); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, got, tc.want)
		}
	}
	// 4+6+12+15 records, 49 shifts, 28 sign-ups.
	if want := 4 + 6 + 12 + 15 + 49 + 28; res.Items != want {
		t.Errorf("Result.Items = %d, want %d", res.Items, want)
	}
}

// TestDemoProvider_LeadOnlyAtRangers_FailsWithPermissionAtGate shows the provider acts as the
// lead and nobody else (demo.md D1.1): with department-admin at Rangers alone the first step
// in another department fails, wrapped with the step, kind Permission.
func TestDemoProvider_LeadOnlyAtRangers_FailsWithPermissionAtGate(t *testing.T) {
	r, req := demoRig(t, false, true)
	_, err := seeder(t, r).Seed(ctxT(t), req)
	wantKind(t, err, errs.Permission)
}

// TestDemoProvider_EventWithoutDates_ReturnsInvalid seeds against an event whose directory
// entry has no dates: the provider carries no date rule of its own, so it fails Invalid on
// the field "event" (F175) before it creates anything.
func TestDemoProvider_EventWithoutDates_ReturnsInvalid(t *testing.T) {
	r, req := demoRig(t, true, false)
	ctx := ctxT(t)
	_, err := seeder(t, r).Seed(ctx, req)
	wantKind(t, err, errs.Invalid)
	if got := fieldOf(err); got != "event" {
		t.Errorf("field = %q, want %q", got, "event")
	}
	if got := codeOf(err); got != validate.CodeRequired {
		t.Errorf("code = %q, want %q", got, validate.CodeRequired)
	}
	if got := count(ctx, t, r, `SELECT count(*) FROM volunteers.qualification`); got != 0 {
		t.Errorf("%d qualifications were created before the failure, want 0", got)
	}
}
