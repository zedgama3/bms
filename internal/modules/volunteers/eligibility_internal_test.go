package volunteers

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// TestEligibility_Table is the pure part of the spec's eligibility rule: each reason on and
// off, expiry judged on the shift's start date in its zone.
func TestEligibility_Table(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	dept := model.NewID[model.DepartmentID]()
	other := model.NewID[model.DepartmentID]()
	pos := model.NewID[vol.PositionID]()
	radio := model.NewID[vol.QualificationID]()
	ev := model.NewID[model.EventID]()
	starts := now.Add(24 * time.Hour)
	base := vol.ShiftRow{
		Shift:          vol.Shift{ID: model.NewID[vol.ShiftID](), Department: dept, Position: pos, Starts: starts, Ends: starts.Add(4 * time.Hour), Zone: "UTC", Slots: 2},
		PositionAccess: vol.AccessOpen,
	}
	verified := func(exp vol.Date) vol.Record {
		return vol.Record{Qualification: radio, AcquiredOn: vol.Date{Year: 2026, Month: 1, Day: 1}, ExpiresOn: exp, VerifiedAt: now}
	}
	env := func(mut func(*eligEnv)) *eligEnv {
		e := &eligEnv{
			now: now, members: map[model.DepartmentID]bool{}, active: map[model.DepartmentID]bool{dept: true, other: true},
			closed: map[model.EventID]bool{}, reqs: map[vol.PositionID][]vol.QualificationID{},
		}
		if mut != nil {
			mut(e)
		}
		return e
	}
	tests := []struct {
		name         string
		row          func(vol.ShiftRow) vol.ShiftRow
		env          *eligEnv
		wantEligible bool
		wantReason   string
		wantMissing  int
	}{
		{"open shift, no requirements", nil, env(nil), true, "", 0},
		{"canceled", func(r vol.ShiftRow) vol.ShiftRow { r.CanceledAt = now; return r }, env(nil), false, vol.ReasonCanceled, 0},
		{"started", func(r vol.ShiftRow) vol.ShiftRow { r.Starts = now.Add(-time.Minute); return r }, env(nil), false, vol.ReasonStarted, 0},
		{"archived department", nil, env(func(e *eligEnv) { e.active[dept] = false }), false, vol.ReasonArchived, 0},
		{"closed event", func(r vol.ShiftRow) vol.ShiftRow { r.Event = ev; return r }, env(func(e *eligEnv) { e.closed[ev] = true }), false, vol.ReasonClosed, 0},
		{"members only, not a member", func(r vol.ShiftRow) vol.ShiftRow { r.PositionAccess = vol.AccessMembers; return r }, env(nil), false, vol.ReasonNotMember, 0},
		{"members only, member", func(r vol.ShiftRow) vol.ShiftRow { r.PositionAccess = vol.AccessMembers; return r }, env(func(e *eligEnv) { e.members[dept] = true }), true, "", 0},
		{"full", func(r vol.ShiftRow) vol.ShiftRow { r.Booked = 2; return r }, env(nil), false, vol.ReasonFull, 0},
		{"overlap", nil, env(func(e *eligEnv) {
			e.windows = []vol.BookedWindow{{Shift: model.NewID[vol.ShiftID](), Starts: starts.Add(time.Hour), Ends: starts.Add(5 * time.Hour)}}
		}), false, vol.ReasonOverlap, 0},
		{"back to back is not an overlap", nil, env(func(e *eligEnv) {
			e.windows = []vol.BookedWindow{{Shift: model.NewID[vol.ShiftID](), Starts: starts.Add(4 * time.Hour), Ends: starts.Add(8 * time.Hour)}}
		}), true, "", 0},
		{"missing requirement", nil, env(func(e *eligEnv) { e.reqs[pos] = []vol.QualificationID{radio} }), false, "", 1},
		{"pending record does not count", nil, env(func(e *eligEnv) {
			e.reqs[pos] = []vol.QualificationID{radio}
			r := verified(vol.Date{})
			r.VerifiedAt = time.Time{}
			e.records = []vol.Record{r}
		}), false, "", 1},
		{"valid record", nil, env(func(e *eligEnv) {
			e.reqs[pos] = []vol.QualificationID{radio}
			e.records = []vol.Record{verified(vol.Date{})}
		}), true, "", 0},
		{"expired the day before the shift", nil, env(func(e *eligEnv) {
			e.reqs[pos] = []vol.QualificationID{radio}
			e.records = []vol.Record{verified(vol.Date{Year: 2026, Month: 10, Day: 21})}
		}), false, "", 1},
		{"expires on the shift's day", nil, env(func(e *eligEnv) {
			e.reqs[pos] = []vol.QualificationID{radio}
			e.records = []vol.Record{verified(vol.Date{Year: 2026, Month: 10, Day: 22})}
		}), true, "", 0},
		{"already booked", func(r vol.ShiftRow) vol.ShiftRow { r.Mine = model.NewID[vol.SignupID](); return r }, env(nil), false, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := base
			if tc.row != nil {
				row = tc.row(row)
			}
			got := tc.env.of(row)
			if got.Eligible != tc.wantEligible || got.Reason != tc.wantReason || len(got.Missing) != tc.wantMissing {
				t.Errorf("of = %+v, want eligible=%v reason=%q missing=%d", got, tc.wantEligible, tc.wantReason, tc.wantMissing)
			}
		})
	}
}

// TestEligibility_ExpiryUsesTheShiftZone: 03:00 UTC on 22 October is still 21 October in
// Denver, so a record that expires on the 21st is valid for it.
func TestEligibility_ExpiryUsesTheShiftZone(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	dept := model.NewID[model.DepartmentID]()
	pos := model.NewID[vol.PositionID]()
	radio := model.NewID[vol.QualificationID]()
	starts := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	row := vol.ShiftRow{Shift: vol.Shift{ID: model.NewID[vol.ShiftID](), Department: dept, Position: pos, Starts: starts, Ends: starts.Add(2 * time.Hour), Zone: "America/Denver", Slots: 1}}
	e := &eligEnv{
		now: now, members: map[model.DepartmentID]bool{}, active: map[model.DepartmentID]bool{dept: true},
		closed: map[model.EventID]bool{}, reqs: map[vol.PositionID][]vol.QualificationID{pos: {radio}},
		records: []vol.Record{{Qualification: radio, AcquiredOn: vol.Date{Year: 2026, Month: 1, Day: 1}, ExpiresOn: vol.Date{Year: 2026, Month: 10, Day: 21}, VerifiedAt: now}},
	}
	if got := e.of(row); !got.Eligible {
		t.Errorf("Denver evening shift: %+v, want eligible", got)
	}
	row.Zone = "UTC"
	if got := e.of(row); got.Eligible {
		t.Errorf("UTC shift on the 22nd: %+v, want the record expired", got)
	}
}
