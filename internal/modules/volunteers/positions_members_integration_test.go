//go:build integration

package volunteers_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// ---- positions (F44-F55) ----

// F44
func TestListPositions_ArchivedWithoutPermission_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	other := r.position(d, "Old crew", vol.AccessOpen)
	wantNoErr(t, r.svc.ArchivePosition(r.as(r.boss), other.ID))
	plain := r.as(r.volunteer("Tumbleweed"))

	got, err := r.svc.ListPositions(plain, d, false)
	wantNoErr(t, err)
	if len(got) != 1 || got[0].ID != pos.ID {
		t.Errorf("active list = %d positions, want only the active one", len(got))
	}
	_, err = r.svc.ListPositions(plain, d, true)
	wantKind(t, err, errs.Permission)
	all, err := r.svc.ListPositions(r.as(r.lead(d)), d, true)
	wantNoErr(t, err)
	if len(all) != 2 {
		t.Errorf("the lead sees %d positions with archived, want 2", len(all))
	}
}

// F45
func TestCreatePosition_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	in := vol.PositionInput{Name: "Dirt crew", Access: vol.AccessOpen}
	_, err := r.svc.CreatePosition(r.as(r.lead(other)), d, in)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreatePosition(r.as(r.volunteer("Tumbleweed")), d, in)
	wantKind(t, err, errs.Permission)
	got, err := r.svc.CreatePosition(r.as(r.lead(d)), d, in)
	wantNoErr(t, err)
	if got.Department != d || got.Access != vol.AccessOpen {
		t.Errorf("created = %+v", got)
	}
	if len(r.audited("volunteers.position.created")) != 1 {
		t.Error("no position.created audit entry")
	}
}

// F46
func TestCreatePosition_ArchivedDepartment_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	r.archiveDepartment(d)
	_, err := r.svc.CreatePosition(r.as(r.boss), d, vol.PositionInput{Name: "Dirt crew", Access: vol.AccessOpen})
	wantConflict(t, err, "archived")
}

// F47
func TestCreatePosition_InvalidInput_Table(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	tests := []struct {
		name string
		in   vol.PositionInput
	}{
		{"empty name", vol.PositionInput{Access: vol.AccessOpen}},
		{"name too long", vol.PositionInput{Name: strings.Repeat("n", vol.MaxNameBytes+1), Access: vol.AccessOpen}},
		{"control chars", vol.PositionInput{Name: "a\tb\x00", Access: vol.AccessOpen}},
		{"access not one_of", vol.PositionInput{Name: "Dirt crew", Access: "secret"}},
		{"access empty", vol.PositionInput{Name: "Dirt crew"}},
		{"description too long", vol.PositionInput{Name: "Dirt crew", Access: vol.AccessOpen, Description: strings.Repeat("d", vol.MaxDescriptionBytes+1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.CreatePosition(r.as(r.boss), d, tc.in)
			wantKind(t, err, errs.Invalid)
		})
	}
	_, err := r.svc.CreatePosition(r.as(r.boss), d, vol.PositionInput{Name: "Dirt crew", Access: "secret"})
	if got := codeOf(err); got != "one_of" {
		t.Errorf("access code = %q, want one_of", got)
	}
}

// F48
func TestCreatePosition_DuplicateName_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	r.position(d, "Dirt crew", vol.AccessOpen)
	_, err := r.svc.CreatePosition(r.as(r.boss), d, vol.PositionInput{Name: "DIRT CREW", Access: vol.AccessMembers})
	wantConflict(t, err, "taken")
	if _, err := r.svc.CreatePosition(r.as(r.boss), other, vol.PositionInput{Name: "Dirt crew", Access: vol.AccessOpen}); err != nil {
		t.Errorf("same name in another department: %v", err)
	}
}

// F49
func TestUpdatePosition_Unknown_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	in := vol.PositionInput{Name: "x", Access: vol.AccessOpen}
	_, err := r.svc.UpdatePosition(r.as(r.boss), model.NewID[vol.PositionID](), in)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.ArchivePosition(r.as(r.boss), model.NewID[vol.PositionID]()), errs.NotFound)
}

func TestUpdatePosition_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	in := vol.PositionInput{Name: "Dirt crew 2", Access: vol.AccessOpen}
	_, err := r.svc.UpdatePosition(r.as(r.lead(other)), pos.ID, in)
	wantKind(t, err, errs.Permission)
	wantKind(t, r.svc.ArchivePosition(r.as(r.lead(other)), pos.ID), errs.Permission)
	got, err := r.svc.UpdatePosition(r.as(r.lead(d)), pos.ID, vol.PositionInput{Name: "Dirt crew 2", Access: vol.AccessMembers, HideShiftMates: true})
	wantNoErr(t, err)
	if got.Name != "Dirt crew 2" || got.Access != vol.AccessMembers || !got.HideShiftMates {
		t.Errorf("updated = %+v", got)
	}
}

// F50
func TestArchivePosition_FutureShifts_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	wantConflict(t, r.svc.ArchivePosition(r.as(r.boss), pos.ID), "in_use")
	// A canceled shift does not hold the position.
	wantNoErr(t, r.svc.CancelShift(r.as(r.boss), sh.ID))
	wantNoErr(t, r.svc.ArchivePosition(r.as(r.boss), pos.ID))
}

// F51
func TestArchivePosition_Twice_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	wantNoErr(t, r.svc.ArchivePosition(r.as(r.boss), pos.ID))
	wantConflict(t, r.svc.ArchivePosition(r.as(r.boss), pos.ID), "archived")
}

// F52
func TestSetRequirements_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	q := r.qual(d, "Radio")
	_, err := r.svc.SetRequirements(r.as(r.lead(other)), pos.ID, []vol.QualificationID{q.ID})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.SetRequirements(r.as(r.boss), model.NewID[vol.PositionID](), nil)
	wantKind(t, err, errs.NotFound)
}

func TestSetRequirements_ReplacesTheSetSorted(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	a, b, org := r.qual(d, "Radio"), r.qual(d, "First aid"), r.qual(model.DepartmentID{}, "Over 18")
	lead := r.as(r.lead(d))
	got, err := r.svc.SetRequirements(lead, pos.ID, []vol.QualificationID{a.ID, b.ID, org.ID})
	wantNoErr(t, err)
	if len(got.Requirements) != 3 {
		t.Fatalf("requirements = %d, want 3", len(got.Requirements))
	}
	for i := 1; i < len(got.Requirements); i++ {
		if got.Requirements[i-1].String() > got.Requirements[i].String() {
			t.Error("requirements are not sorted")
		}
	}
	got, err = r.svc.SetRequirements(lead, pos.ID, []vol.QualificationID{org.ID})
	wantNoErr(t, err)
	if len(got.Requirements) != 1 || got.Requirements[0] != org.ID {
		t.Errorf("the set was not replaced: %v", got.Requirements)
	}
	got, err = r.svc.SetRequirements(lead, pos.ID, nil)
	wantNoErr(t, err)
	if len(got.Requirements) != 0 {
		t.Errorf("clearing left %d requirements", len(got.Requirements))
	}
	if len(r.audited("volunteers.position.requirements-changed")) != 3 {
		t.Errorf("want 3 requirements-changed entries, got %d", len(r.audited("volunteers.position.requirements-changed")))
	}
}

// F53
func TestSetRequirements_OtherDepartmentQualification_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	foreign := r.qual(other, "Door duty")
	_, err := r.svc.SetRequirements(r.as(r.boss), pos.ID, []vol.QualificationID{foreign.ID})
	wantInvalid(t, err, "scope")
}

// F54
func TestSetRequirements_ArchivedQualification_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	q := r.qual(d, "Radio")
	wantNoErr(t, r.svc.ArchiveQualification(r.as(r.boss), q.ID))
	_, err := r.svc.SetRequirements(r.as(r.boss), pos.ID, []vol.QualificationID{q.ID})
	wantConflict(t, err, "archived")
	_, err = r.svc.SetRequirements(r.as(r.boss), pos.ID, []vol.QualificationID{model.NewID[vol.QualificationID]()})
	wantInvalid(t, err, "unknown")
}

// F55
func TestSetRequirements_TooMany_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	var many []vol.QualificationID
	for i := 0; i <= vol.MaxRequirements; i++ {
		many = append(many, r.qual(d, "Course "+string(rune('A'+i))).ID)
	}
	_, err := r.svc.SetRequirements(r.as(r.boss), pos.ID, many)
	wantInvalid(t, err, "too_many")
	// At the limit it works.
	if _, err := r.svc.SetRequirements(r.as(r.boss), pos.ID, many[:vol.MaxRequirements]); err != nil {
		t.Errorf("exactly MaxRequirements failed: %v", err)
	}
	// Duplicates are refused.
	_, err = r.svc.SetRequirements(r.as(r.boss), pos.ID, []vol.QualificationID{many[0], many[0]})
	wantKind(t, err, errs.Invalid)
}

// Existing bookings are not re-checked when requirements change.
func TestSetRequirements_DoesNotCancelExistingBookings(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Tumbleweed")
	r.book(p, s.sh.ID)
	q := r.qual(s.dept, "Radio")
	_, err := r.svc.SetRequirements(r.as(s.lead), s.pos.ID, []vol.QualificationID{q.ID})
	wantNoErr(t, err)
	ro, err := r.svc.Roster(r.as(s.lead), s.sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 1 || ro.Entries[0].Eligible {
		t.Errorf("roster = %+v, want the booking kept and flagged not eligible", ro.Entries)
	}
}

// ---- members (F56-F65) ----

// F56
func TestListMembers_NoPermission_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	_, err := memberItems(r.svc.ListMembers(r.as(r.lead(other)), vol.MemberQuery{Department: d}))
	wantKind(t, err, errs.Permission)
	_, err = memberItems(r.svc.ListMembers(r.as(r.volunteer("Tumbleweed")), vol.MemberQuery{Department: d}))
	wantKind(t, err, errs.Permission)
}

func TestListMembers_ShowsPlayaNamesAndEligiblePositions(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	open := r.position(d, "Dirt crew", vol.AccessOpen)
	needs := r.position(d, "Radio op", vol.AccessMembers, q.ID)
	archived := r.position(d, "Old crew", vol.AccessOpen)
	wantNoErr(t, r.svc.ArchivePosition(r.as(r.boss), archived.ID))
	qualified, plain := r.person("Quillfeather"), r.person("Dandelion")
	r.member(d, qualified)
	r.member(d, plain)
	r.verified(qualified, q.ID)
	got, err := memberItems(r.svc.ListMembers(r.as(r.lead(d)), vol.MemberQuery{Department: d}))
	wantNoErr(t, err)
	if len(got) != 2 {
		t.Fatalf("%d members, want 2", len(got))
	}
	for _, m := range got {
		set := map[vol.PositionID]bool{}
		for _, id := range m.Eligible {
			set[id] = true
		}
		switch m.Person {
		case qualified:
			if m.PlayaName != "Quillfeather" || !set[open.ID] || !set[needs.ID] || set[archived.ID] {
				t.Errorf("qualified member = %+v", m)
			}
		case plain:
			if m.PlayaName != "Dandelion" || !set[open.ID] || set[needs.ID] {
				t.Errorf("plain member = %+v", m)
			}
		default:
			t.Errorf("unexpected member %v", m.Person)
		}
	}
}

// F57
func TestListMembers_NamesFail_ReturnsError(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	r.member(d, r.person("Dandelion"))
	r.cpl.namesErr = errs.E("test.people", errs.Unavailable, errPeopleDown)
	_, err := memberItems(r.svc.ListMembers(r.as(r.lead(d)), vol.MemberQuery{Department: d}))
	wantKind(t, err, errs.Unavailable)
}

// F58
func TestAddMember_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	p := r.person("Dandelion")
	_, err := r.svc.AddMember(r.as(r.lead(other)), d, p)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.AddMember(r.as(p), d, p) // joining oneself is not a thing yet (join requests are Later)
	wantKind(t, err, errs.Permission)
	lead := r.lead(d)
	m, err := r.svc.AddMember(r.as(lead), d, p)
	wantNoErr(t, err)
	if m.Department != d || m.Person != p || m.AddedBy != lead {
		t.Errorf("member = %+v", m)
	}
	if len(r.audited("volunteers.member.created")) != 1 {
		t.Error("no member.created audit entry")
	}
}

// F59
func TestAddMember_ArchivedDepartment_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	r.archiveDepartment(d)
	_, err := r.svc.AddMember(r.as(r.boss), d, r.person("Dandelion"))
	wantConflict(t, err, "archived")
}

// F60
func TestAddMember_UnknownPerson_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	_, err := r.svc.AddMember(r.as(r.boss), d, model.NewID[model.PersonID]())
	wantInvalid(t, err, "unknown")
}

// F61
func TestAddMember_Twice_ReturnsConflict(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	p := r.person("Dandelion")
	r.member(d, p)
	_, err := r.svc.AddMember(r.as(r.boss), d, p)
	wantConflict(t, err, "duplicate")
}

// F62
func TestAddMember_ZeroPerson_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	_, err := r.svc.AddMember(r.as(r.boss), d, model.PersonID{})
	wantInvalid(t, err, "required")
}

// F63
func TestRemoveMember_NotMember_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	wantKind(t, r.svc.RemoveMember(r.as(r.boss), d, r.person("Dandelion")), errs.NotFound)
}

func TestRemoveMember_KeepsBookings(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.member(s.dept, p)
	r.book(p, s.sh.ID)
	wantNoErr(t, r.svc.RemoveMember(r.as(s.lead), s.dept, p))
	ro, err := r.svc.Roster(r.as(s.lead), s.sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 1 {
		t.Errorf("roster has %d entries after the member was removed, want the booking kept", len(ro.Entries))
	}
	mine, err := r.svc.MyDepartments(r.as(p))
	wantNoErr(t, err)
	if len(mine) != 0 {
		t.Error("the person is still a member")
	}
	if len(r.audited("volunteers.member.deleted")) != 1 {
		t.Error("no member.deleted audit entry")
	}
}

// F64
func TestRemoveMember_NotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d, other := r.department("Rangers"), r.department("Greeters")
	p := r.person("Dandelion")
	r.member(d, p)
	wantKind(t, r.svc.RemoveMember(r.as(r.lead(other)), d, p), errs.Permission)
}

// F65
func TestLeave_NotMember_ReturnsNotFound(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	wantKind(t, r.svc.Leave(r.as(r.person("Dandelion")), d), errs.NotFound)
}

func TestLeave_CancelsFutureMembersOnlyBookings(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	members := r.position(d, "Inner circle", vol.AccessMembers)
	open := r.position(d, "Dirt crew", vol.AccessOpen)
	p := r.volunteer("Dandelion")
	r.member(d, p)
	inner := r.shift(d, members.ID, model.EventID{}, when(0), 2, 2)
	outer := r.shift(d, open.ID, model.EventID{}, when(5), 2, 2)
	r.book(p, inner.ID)
	r.book(p, outer.ID)
	wantNoErr(t, r.svc.Leave(r.as(p), d))

	sched, err := scheduleItems(r.svc.MySchedule(r.as(p), vol.ScheduleQuery{}))
	wantNoErr(t, err)
	if len(sched) != 1 || sched[0].Shift.ID != outer.ID {
		t.Fatalf("schedule = %d entries, want only the open-position booking to survive", len(sched))
	}
	cancels := r.published(volunteers.SignalSignupCanceled)
	if len(cancels) != 1 {
		t.Errorf("%d signup-canceled signals, want 1", len(cancels))
	}
	ro, err := r.svc.Roster(r.as(r.lead(d)), inner.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 0 {
		t.Errorf("the members-only roster still lists %d people", len(ro.Entries))
	}
}

func TestMyDepartments_ListsOwnMemberships(t *testing.T) {
	r := newReal(t)
	a, b := r.department("Rangers"), r.department("Greeters")
	p, other := r.person("Dandelion"), r.person("Tumbleweed")
	r.member(a, p)
	r.member(b, p)
	r.member(a, other)
	got, err := r.svc.MyDepartments(r.as(p))
	wantNoErr(t, err)
	if len(got) != 2 {
		t.Fatalf("%d memberships, want 2", len(got))
	}
	for _, m := range got {
		if m.Person != p {
			t.Errorf("MyDepartments returned another person's membership: %+v", m)
		}
	}
}
