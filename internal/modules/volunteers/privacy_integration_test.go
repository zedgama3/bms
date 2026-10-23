//go:build integration

package volunteers_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// ---- shift mates (F126-F134) ----

type matesWorld struct {
	r        *rig
	dept     model.DepartmentID
	pos      vol.Position
	shift    vol.Shift
	lead     model.PersonID
	alden    model.PersonID // listed, playa "Dandelion"
	briar    model.PersonID // listed, playa "Tumbleweed"
	shy      model.PersonID // booked, opted out of the list
	nameless model.PersonID // booked, no playa name yet
}

const (
	legalAlden = "Alden Quillfeather"
	legalBriar = "Briar Marlowe-Test"
	legalShy   = "Shay Underbough"
	legalNone  = "Nell Nameless-Test"
)

func newMatesWorld(t *testing.T, hide bool) matesWorld {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	pos, err := r.svc.CreatePosition(r.as(r.boss), d, vol.PositionInput{Name: "Dirt crew", Access: vol.AccessOpen, HideShiftMates: hide})
	wantNoErr(t, err)
	w := matesWorld{r: r, dept: d, pos: pos, lead: r.lead(d)}
	w.shift = r.shift(d, pos.ID, model.EventID{}, when(0), 4, 10)
	w.alden, w.briar = r.volunteer("Dandelion"), r.volunteer("Tumbleweed")
	w.shy, w.nameless = r.volunteer("Shy Violet"), r.volunteer("Nell Nameless")
	for p, legal := range map[model.PersonID]string{w.alden: legalAlden, w.briar: legalBriar, w.shy: legalShy, w.nameless: legalNone} {
		r.ppl.AddLegalName(p, legal)
	}
	for _, p := range []model.PersonID{w.alden, w.briar, w.shy, w.nameless} {
		r.book(p, w.shift.ID)
	}
	wantNoErr(t, r.svc.SetListed(r.as(w.shy), false))
	// Decision 26 refuses a booking without a playa name, so the nameless person was named
	// when they booked; the name is gone now (F129: only the fake can produce this).
	r.ppl.Add(w.nameless, "")
	return w
}

// F126
func TestGetShift_ShiftMatesNobody_OmitsMates(t *testing.T) {
	w := newMatesWorld(t, false)
	w.r.set.Set(volunteers.SettingVisibilityShiftMates, "nobody")
	viewer := w.r.volunteer("Quillfeather")
	w.r.cpl.resetCounts()
	got, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates != nil {
		t.Errorf("mates = %+v, want absent under nobody", got.Mates)
	}
	body := mustJSON(t, got)
	privacytest.AssertHidden(t, body, "mates", "names", "unlisted")
	if names, _ := w.r.cpl.counts(); names != 0 {
		t.Errorf("NamesOf was called %d times although the list is hidden", names)
	}
}

type viewerKind int

const (
	plainHolder viewerKind = iota // holds shifts.signup, not booked
	bookedHolder
	bookedNonHolder // assigned by a lead, holds no shifts.signup
	nonHolder       // signed in, may not volunteer, not booked
)

func (k viewerKind) String() string {
	return [...]string{"holder not booked", "holder booked", "booked but not a holder", "non-holder not booked"}[k]
}

// TestShiftMates_Table is the audience x position flag x booked x shifts.signup rule (F127, F128, F134).
func TestShiftMates_Table(t *testing.T) {
	audiences := []string{"nobody", "booked", "holders", "signed_in"}
	for _, hide := range []bool{false, true} {
		for _, aud := range audiences {
			for _, vk := range []viewerKind{plainHolder, bookedHolder, bookedNonHolder, nonHolder} {
				name := fmt.Sprintf("hide=%v/%s/%s", hide, aud, vk)
				t.Run(name, func(t *testing.T) {
					w := newMatesWorld(t, hide)
					w.r.set.Set(volunteers.SettingVisibilityShiftMates, aud)
					var viewer model.PersonID
					switch vk {
					case plainHolder:
						viewer = w.r.volunteer("Quillfeather")
					case bookedHolder:
						viewer = w.r.volunteer("Quillfeather")
						w.r.book(viewer, w.shift.ID)
					case bookedNonHolder:
						viewer = w.r.person("Quillfeather")
						_, err := w.r.svc.Assign(w.r.as(w.lead), w.shift.ID, vol.AssignInput{Person: viewer})
						wantNoErr(t, err)
					case nonHolder:
						viewer = w.r.person("Quillfeather")
					}
					booked := vk == bookedHolder || vk == bookedNonHolder
					holder := vk == plainHolder || vk == bookedHolder
					want := !hide && ((aud == "booked" && booked) || (aud == "holders" && holder) || aud == "signed_in")
					got, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
					wantNoErr(t, err)
					body := mustJSON(t, got)
					if want {
						privacytest.AssertPresent(t, body, "mates")
						if got.Mates == nil {
							t.Fatal("mates is nil although the policy shows it")
						}
					} else {
						privacytest.AssertHidden(t, body, "mates", "names")
						if got.Mates != nil {
							t.Fatalf("mates = %+v although the policy hides it", got.Mates)
						}
					}
				})
			}
		}
	}
}

// F127 (named): booked shows only once the viewer holds a booking on this shift.
func TestGetShift_ShiftMatesBooked_Table(t *testing.T) {
	w := newMatesWorld(t, false)
	w.r.set.Set(volunteers.SettingVisibilityShiftMates, "booked")
	other := w.r.shift(w.dept, w.pos.ID, model.EventID{}, when(30), 2, 5)
	viewer := w.r.volunteer("Quillfeather")
	got, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates != nil {
		t.Error("shown to a viewer with no booking")
	}
	w.r.book(viewer, other.ID) // a booking on another shift does not count
	got, err = w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates != nil {
		t.Error("shown because of a booking on a different shift")
	}
	w.r.book(viewer, w.shift.ID)
	got, err = w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates == nil {
		t.Fatal("not shown once the viewer holds a booking on the shift")
	}
	// A canceled booking no longer counts.
	wantNoErr(t, w.r.svc.CancelSignup(w.r.as(viewer), w.r.bookingOf(viewer, w.shift.ID)))
	got, err = w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates != nil {
		t.Error("shown after the viewer canceled their booking")
	}
}

// F128
func TestGetShift_HiddenPosition_OmitsMates(t *testing.T) {
	w := newMatesWorld(t, true)
	for _, aud := range []string{"booked", "holders", "signed_in"} {
		w.r.set.Set(volunteers.SettingVisibilityShiftMates, aud)
		viewer := w.r.volunteer("Quillfeather")
		w.r.book(viewer, w.shift.ID)
		got, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
		wantNoErr(t, err)
		if got.Mates != nil {
			t.Errorf("under %s a position with HideShiftMates still shows mates", aud)
		}
		privacytest.AssertHidden(t, mustJSON(t, got), "mates")
		wantNoErr(t, w.r.svc.CancelSignup(w.r.as(viewer), w.r.bookingOf(viewer, w.shift.ID)))
	}
	// The flag can only hide: leads still see the roster.
	ro, err := w.r.svc.Roster(w.r.as(w.lead), w.shift.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 4 {
		t.Errorf("the lead's roster has %d entries, want 4", len(ro.Entries))
	}
}

// F134
func TestGetShift_ShiftMatesHolders_Table(t *testing.T) {
	w := newMatesWorld(t, false) // default audience: holders
	holder, nonHolder := w.r.volunteer("Quillfeather"), w.r.person("Driftwood")
	got, err := w.r.svc.GetShift(w.r.as(holder), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates == nil {
		t.Error("the default audience must show mates to a holder of shifts.signup before booking (D43)")
	}
	got, err = w.r.svc.GetShift(w.r.as(nonHolder), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates != nil {
		t.Error("a signed-in person who may not volunteer saw mates under holders")
	}
	w.r.set.Set(volunteers.SettingVisibilityShiftMates, "signed_in")
	got, err = w.r.svc.GetShift(w.r.as(nonHolder), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates == nil {
		t.Error("signed_in must show mates to anyone who can see the shift")
	}
}

// The spec's content rules: playa names only, sorted, never repeated (people keeps playa names
// unique installation-wide, Amendment P1), the withheld counted.
func TestGetShift_Mates_PlayaNamesOnly(t *testing.T) {
	w := newMatesWorld(t, false)
	early := w.r.volunteer("Aspen") // sorts before the others
	w.r.book(early, w.shift.ID)
	viewer := w.r.volunteer("Quillfeather")
	w.r.ppl.AddLegalName(viewer, "Viv Viewer-Test")
	got, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates == nil {
		t.Fatal("no mates")
	}
	want := []string{"Aspen", "Dandelion", "Tumbleweed"}
	if !slices.Equal(got.Mates.Names, want) {
		t.Errorf("Names = %v, want %v (sorted)", got.Mates.Names, want)
	}
	if !slices.IsSorted(got.Mates.Names) || len(slices.Compact(slices.Clone(got.Mates.Names))) != len(got.Mates.Names) {
		t.Errorf("Names = %v, want sorted and without repeats", got.Mates.Names)
	}
	if got.Mates.Unlisted != 2 {
		t.Errorf("Unlisted = %d, want 2 (one opted out, one with no playa name)", got.Mates.Unlisted)
	}
	body := mustJSON(t, got.Mates)
	privacytest.AssertHidden(t, body, "person", "person_id", "id", "legal_name", "pronouns", "email", "phone")
	whole := string(mustJSON(t, got))
	for _, secret := range []string{legalAlden, legalBriar, legalShy, legalNone, "Viv Viewer-Test"} {
		if strings.Contains(whole, secret) {
			t.Errorf("the shift summary body contains a legal name (%q)", secret)
		}
	}
	for _, p := range []model.PersonID{w.alden, w.briar, w.shy, w.nameless, viewer} {
		if strings.Contains(whole, p.String()) && p != w.lead {
			// the shift itself may name its creator, never a booked person
			t.Errorf("the shift summary body contains the person ID %s", p)
		}
	}
	if w.r.cpl.legalCalls() != 0 {
		t.Error("GetShift asked people for legal names")
	}
}

// F129
func TestGetShift_NoPlayaName_CountsUnlisted_NeverLegalName(t *testing.T) {
	w := newMatesWorld(t, false)
	viewer := w.r.volunteer("Quillfeather")
	got, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	for _, n := range got.Mates.Names {
		if n == "" || n == legalNone {
			t.Errorf("Names contains %q for a person with no playa name", n)
		}
	}
	if got.Mates.Unlisted < 1 {
		t.Error("the nameless person is not counted in Unlisted")
	}
	// The nameless person is absent from people's NamesOf map altogether (no profile yet); a
	// deleted profile is the same case. Both are counted, never named.
	if got.Mates.Unlisted != 2 {
		t.Errorf("Unlisted = %d, want 2 (the nameless person and the opted-out one)", got.Mates.Unlisted)
	}
	if strings.Contains(string(mustJSON(t, got)), legalNone) {
		t.Error("a legal name reached the shift body")
	}
}

// F130
func TestGetShift_OptedOut_CountsUnlisted(t *testing.T) {
	w := newMatesWorld(t, false)
	viewer := w.r.volunteer("Quillfeather")
	before, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if slices.Contains(before.Mates.Names, "Shy Violet") {
		t.Fatal("an opted-out person is named")
	}
	// Opting out takes effect from the next read; opting in again names the person.
	wantNoErr(t, w.r.svc.SetListed(w.r.as(w.shy), true))
	after, err := w.r.svc.GetShift(w.r.as(viewer), w.shift.ID)
	wantNoErr(t, err)
	if !slices.Contains(after.Mates.Names, "Shy Violet") || after.Mates.Unlisted != before.Mates.Unlisted-1 {
		t.Errorf("after opting back in: %+v (before %+v)", after.Mates, before.Mates)
	}
	// The roster, bookings and the opted-out person's own view are unaffected.
	ro, err := w.r.svc.Roster(w.r.as(w.lead), w.shift.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 4 {
		t.Errorf("roster has %d entries, want all 4", len(ro.Entries))
	}
}

func TestSetListed_OptedOutRosterStillNamesThem(t *testing.T) {
	w := newMatesWorld(t, false)
	ro, err := w.r.svc.Roster(w.r.as(w.lead), w.shift.ID)
	wantNoErr(t, err)
	found := false
	for _, e := range ro.Entries {
		if e.Person == w.shy {
			found = true
			if e.PlayaName != "Shy Violet" {
				t.Errorf("roster playa name = %q", e.PlayaName)
			}
		}
	}
	if !found {
		t.Error("the opted-out person is missing from the lead's roster")
	}
	got, err := w.r.svc.GetShift(w.r.as(w.shy), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates == nil || len(got.Mates.Names) != 2 {
		t.Errorf("the opted-out person's own view = %+v, want to see the others", got.Mates)
	}
}

// F131
func TestGetShift_Mates_ExcludesActorAndCanceled(t *testing.T) {
	w := newMatesWorld(t, false)
	wantNoErr(t, w.r.svc.CancelSignup(w.r.as(w.briar), w.r.bookingOf(w.briar, w.shift.ID)))
	got, err := w.r.svc.GetShift(w.r.as(w.alden), w.shift.ID)
	wantNoErr(t, err)
	if got.Mates == nil {
		t.Fatal("no mates")
	}
	if slices.Contains(got.Mates.Names, "Dandelion") {
		t.Error("the actor appears in their own mates list")
	}
	if slices.Contains(got.Mates.Names, "Tumbleweed") {
		t.Error("a canceled sign-up is listed")
	}
	if got.Mates.Unlisted != 2 { // shy + nameless; the actor and the canceled are in neither count
		t.Errorf("Unlisted = %d, want 2", got.Mates.Unlisted)
	}
	if got.Booked != 3 || got.Mine.IsZero() {
		t.Errorf("Booked/Mine = %d/%v", got.Booked, got.Mine)
	}
}

// F132
func TestGetShift_NamesFail_ReturnsError(t *testing.T) {
	w := newMatesWorld(t, false)
	w.r.cpl.namesErr = errs.E("test.people", errs.Unavailable, errors.New("people down"))
	got, err := w.r.svc.GetShift(w.r.as(w.r.volunteer("Quillfeather")), w.shift.ID)
	wantKind(t, err, errs.Unavailable)
	if got.ID != (vol.ShiftID{}) {
		t.Error("a shift was returned together with the error")
	}
}

// F133
func TestListShifts_NeverFillsMates(t *testing.T) {
	w := newMatesWorld(t, false)
	for _, aud := range []string{"nobody", "booked", "holders", "signed_in"} {
		w.r.set.Set(volunteers.SettingVisibilityShiftMates, aud)
		viewer := w.r.volunteer("Quillfeather")
		w.r.book(viewer, w.shift.ID)
		w.r.cpl.resetCounts() // booking asks people for the booker's name (decision 26); the list must not
		got, err := shiftItems(w.r.svc.ListShifts(w.r.as(viewer), vol.ShiftQuery{Department: w.dept}))
		wantNoErr(t, err)
		if names, _ := w.r.cpl.counts(); names != 0 {
			t.Errorf("%s: ListShifts asked people for names %d times", aud, names)
		}
		if len(got) == 0 {
			t.Fatal("no shifts")
		}
		for _, s := range got {
			if s.Mates != nil {
				t.Errorf("%s: ListShifts filled Mates", aud)
			}
		}
		privacytest.AssertHidden(t, mustJSON(t, got), "mates", "names")
		wantNoErr(t, w.r.svc.CancelSignup(w.r.as(viewer), w.r.bookingOf(viewer, w.shift.ID)))
	}
}

// F142
func TestSetListed_Idempotent(t *testing.T) {
	r := newReal(t)
	p := r.person("Dandelion")
	ctx := r.as(p)
	if ok, err := r.svc.Listed(ctx); err != nil || !ok {
		t.Fatalf("Listed = %v, %v; want true by default", ok, err)
	}
	wantNoErr(t, r.svc.SetListed(ctx, true)) // already listed: nothing changes
	if n := len(r.audited("volunteers.listing.changed")); n != 0 {
		t.Errorf("%d audit entries for a no-op", n)
	}
	wantNoErr(t, r.svc.SetListed(ctx, false))
	wantNoErr(t, r.svc.SetListed(ctx, false))
	entries := r.audited("volunteers.listing.changed")
	if len(entries) != 1 || entries[0].Reason != "unlisted" || entries[0].Subject != p {
		t.Fatalf("audit = %+v, want exactly one entry, reason unlisted, about the actor", entries)
	}
	if ok, err := r.svc.Listed(ctx); err != nil || ok {
		t.Errorf("Listed = %v, %v; want false", ok, err)
	}
	wantNoErr(t, r.svc.SetListed(ctx, true))
	wantNoErr(t, r.svc.SetListed(ctx, true))
	entries = r.audited("volunteers.listing.changed")
	if len(entries) != 2 || entries[1].Reason != "listed" {
		t.Errorf("audit = %+v, want a second entry with reason listed and no more", entries)
	}
	if ok, err := r.svc.Listed(r.as(r.person("Tumbleweed"))); err != nil || !ok {
		t.Error("another person's choice leaked into Listed")
	}
}

// ---- roster legal names (F135-F140) ----

type rosterWorld struct {
	r          *rig
	dept       model.DepartmentID
	shift      vol.Shift
	plainLead  model.PersonID // rosters.read@D only
	siteHolder model.PersonID // rosters.read@D + people.contacts.read site-wide (the coordinator)
	deptHolder model.PersonID // rosters.read@D + people.contacts.read@D
	onRoster   model.PersonID // rosters.read@D, booked on the shift, no contacts.read
	a, b, c    model.PersonID // booked; a and b have legal names, c has none
}

func newRosterWorld(t *testing.T, mod ...func(*opts)) rosterWorld {
	t.Helper()
	r := newReal(t, mod...)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	w := rosterWorld{r: r, dept: d, shift: r.shift(d, pos.ID, model.EventID{}, when(0), 4, 10)}
	mk := func(perms ...func(p model.PersonID)) model.PersonID {
		p := r.volunteer("Reader " + model.NewID[model.PersonID]().String()[:4])
		r.az.GrantIn(p, pRosters, d)
		for _, f := range perms {
			f(p)
		}
		return p
	}
	w.plainLead = mk()
	w.siteHolder = mk(func(p model.PersonID) { r.az.Grant(p, pContactRead) })
	w.deptHolder = mk(func(p model.PersonID) { r.az.GrantIn(p, pContactRead, d) })
	w.onRoster = mk()
	w.a, w.b, w.c = r.volunteer("Dandelion"), r.volunteer("Tumbleweed"), r.volunteer("Quillfeather")
	r.ppl.AddLegalName(w.a, legalAlden)
	r.ppl.AddLegalName(w.b, legalBriar)
	r.ppl.AddLegalName(w.onRoster, legalShy)
	for _, p := range []model.PersonID{w.a, w.b, w.c, w.onRoster} {
		r.book(p, w.shift.ID)
	}
	return w
}

func legalOf(w rosterWorld, p model.PersonID) string {
	switch p {
	case w.a:
		return legalAlden
	case w.b:
		return legalBriar
	case w.onRoster:
		return legalShy
	}
	return ""
}

// F135
func TestRoster_LegalNameNobody_NeverAsksPeople(t *testing.T) {
	w := newRosterWorld(t)
	w.r.set.Set(volunteers.SettingVisibilityLegalName, "nobody")
	w.r.cpl.resetCounts()
	for _, viewer := range []model.PersonID{w.plainLead, w.siteHolder, w.deptHolder, w.onRoster} {
		ro, err := w.r.svc.Roster(w.r.as(viewer), w.shift.ID)
		wantNoErr(t, err)
		body := mustJSON(t, ro)
		privacytest.AssertHidden(t, body, "legal_name")
		for _, name := range []string{legalAlden, legalBriar, legalShy} {
			if strings.Contains(string(body), name) {
				t.Errorf("a legal name is in the roster body under nobody")
			}
		}
	}
	if _, legal := w.r.cpl.counts(); legal != 0 {
		t.Errorf("LegalNamesOf was called %d times under nobody", legal)
	}
	if n := len(w.r.audited("volunteers.roster.read")); n != 0 {
		t.Errorf("%d roster.read entries under nobody", n)
	}
}

// F136
func TestRoster_LegalNameHolder_ShowsAndAudits(t *testing.T) {
	for name, pick := range map[string]func(rosterWorld) model.PersonID{
		"site-wide contacts.read":  func(w rosterWorld) model.PersonID { return w.siteHolder },
		"department contacts.read": func(w rosterWorld) model.PersonID { return w.deptHolder },
	} {
		t.Run(name, func(t *testing.T) {
			w := newRosterWorld(t)
			viewer := pick(w)
			ro, err := w.r.svc.Roster(w.r.as(viewer), w.shift.ID)
			wantNoErr(t, err)
			if len(ro.Entries) != 4 {
				t.Fatalf("%d entries", len(ro.Entries))
			}
			shown := map[model.PersonID]bool{}
			for _, e := range ro.Entries {
				body := mustJSON(t, e)
				if want := legalOf(w, e.Person); want != "" {
					privacytest.AssertPresent(t, body, "legal_name")
					if e.LegalName == nil || *e.LegalName != want {
						t.Errorf("entry %v LegalName = %v, want %q", e.Person, e.LegalName, want)
					}
					shown[e.Person] = true
				} else {
					privacytest.AssertHidden(t, body, "legal_name") // people knows none: nil, never ""
					if e.LegalName != nil {
						t.Errorf("entry %v has LegalName %q although people holds none", e.Person, *e.LegalName)
					}
				}
			}
			audits := w.r.audited("volunteers.roster.read")
			if len(audits) != len(shown) {
				t.Fatalf("%d roster.read entries, want one per shown legal name (%d)", len(audits), len(shown))
			}
			for _, e := range audits {
				if !shown[e.Subject] || e.Level != model.SensitivityPersonal || len(e.Fields) != 1 || e.Fields[0] != "legal_name" {
					t.Errorf("audit entry = %+v", e)
				}
				if e.Record.ID != [16]byte(w.shift.ID) {
					t.Errorf("audit record = %+v, want the shift", e.Record)
				}
			}
			if _, legal := w.r.cpl.counts(); legal != 1 {
				t.Errorf("LegalNamesOf called %d times, want exactly once", legal)
			}
		})
	}
}

// F137
func TestRoster_LeadWithoutContactsRead_NoLegalName(t *testing.T) {
	w := newRosterWorld(t)
	w.r.cpl.resetCounts()
	ro, err := w.r.svc.Roster(w.r.as(w.plainLead), w.shift.ID)
	wantNoErr(t, err)
	body := mustJSON(t, ro)
	privacytest.AssertHidden(t, body, "legal_name")
	if len(ro.Entries) != 4 {
		t.Errorf("the lead's roster is incomplete: %d entries", len(ro.Entries))
	}
	if _, legal := w.r.cpl.counts(); legal != 0 {
		t.Errorf("LegalNamesOf was called %d times for a viewer without people.contacts.read", legal)
	}
	if n := len(w.r.audited("volunteers.roster.read")); n != 0 {
		t.Errorf("%d roster.read entries", n)
	}
}

// F138 (Q-VOL8: no): a plain lead who is on their own roster and holds no people.contacts.read
// anywhere gets no legal name, their own entry included, and people is never asked.
func TestRoster_PlainLeadOwnEntry_NoLegalName(t *testing.T) {
	w := newRosterWorld(t)
	w.r.cpl.resetCounts()
	ro, err := w.r.svc.Roster(w.r.as(w.onRoster), w.shift.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 4 {
		t.Fatalf("%d entries, want 4", len(ro.Entries))
	}
	var sawOwn bool
	for _, e := range ro.Entries {
		sawOwn = sawOwn || e.Person == w.onRoster
		privacytest.AssertHidden(t, mustJSON(t, e), "legal_name")
		if e.LegalName != nil {
			t.Errorf("entry %v carries a legal name for a viewer without people.contacts.read", e.Person)
		}
	}
	if !sawOwn {
		t.Error("the viewer is not on their own roster; the fixture is wrong")
	}
	if _, legal := w.r.cpl.counts(); legal != 0 {
		t.Errorf("LegalNamesOf was called %d times for a non-holder", legal)
	}
	if n := len(w.r.audited("volunteers.roster.read")); n != 0 {
		t.Errorf("%d roster.read entries, want none", n)
	}
}

// TestRosterLegalName_Table is audience x contacts.read extent x self (no "self" audience since V1, decision 27).
func TestRosterLegalName_Table(t *testing.T) {
	type cell struct{ any, own bool }
	// expected visibility of (other people's legal names, the viewer's own)
	want := map[string]map[string]cell{
		"nobody": {"plain lead": {false, false}, "site holder": {false, false}, "dept holder": {false, false}, "on roster": {false, false}},
		// holders of people.contacts.read see names; a non-holder never does, their own entry included
		// (Q-VOL8: no: people answers only a holder, so the viewer is not even asked for)
		"holders": {"plain lead": {false, false}, "site holder": {true, true}, "dept holder": {true, true}, "on roster": {false, false}},
	}
	for aud, byViewer := range want {
		for vname, exp := range byViewer {
			t.Run(aud+"/"+vname, func(t *testing.T) {
				w := newRosterWorld(t)
				w.r.set.Set(volunteers.SettingVisibilityLegalName, aud)
				viewer := map[string]model.PersonID{"plain lead": w.plainLead, "site holder": w.siteHolder, "dept holder": w.deptHolder, "on roster": w.onRoster}[vname]
				ro, err := w.r.svc.Roster(w.r.as(viewer), w.shift.ID)
				wantNoErr(t, err)
				for _, e := range ro.Entries {
					body := mustJSON(t, e)
					legal := legalOf(w, e.Person)
					shouldShow := legal != "" && ((e.Person == viewer && exp.own) || (e.Person != viewer && exp.any))
					if shouldShow {
						privacytest.AssertPresent(t, body, "legal_name")
					} else {
						privacytest.AssertHidden(t, body, "legal_name")
					}
				}
			})
		}
	}
}

// F139
func TestRoster_PeoplePermission_HidesQuietly(t *testing.T) {
	w := newRosterWorld(t)
	w.r.cpl.legalErr = errs.E("test.people", errs.Permission, errors.New("people disagrees"))
	ro, err := w.r.svc.Roster(w.r.as(w.siteHolder), w.shift.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 4 {
		t.Errorf("the roster has %d entries, want all 4", len(ro.Entries))
	}
	privacytest.AssertHidden(t, mustJSON(t, ro), "legal_name")
	if n := len(w.r.audited("volunteers.roster.read")); n != 0 {
		t.Errorf("%d roster.read entries although no legal name was returned", n)
	}
}

func TestRoster_PeopleUnavailable_ReturnsError(t *testing.T) {
	w := newRosterWorld(t)
	w.r.cpl.legalErr = errs.E("test.people", errs.Unavailable, errors.New("people down"))
	ro, err := w.r.svc.Roster(w.r.as(w.siteHolder), w.shift.ID)
	wantKind(t, err, errs.Unavailable)
	if len(ro.Entries) != 0 {
		t.Error("entries were returned together with the error")
	}
}

// F140
func TestRoster_AuditFails_ReturnsError(t *testing.T) {
	w := newRosterWorld(t)
	w.r.aud.Fail(errs.E("test.audit", errs.Unavailable, errors.New("audit down")))
	ro, err := w.r.svc.Roster(w.r.as(w.siteHolder), w.shift.ID)
	wantKind(t, err, errs.Unavailable)
	if len(ro.Entries) != 0 {
		t.Errorf("a roster with legal names was returned although the audit failed: %+v", ro.Entries)
	}
}

// Nothing personal reaches logs, spans, notifications or signals.
func TestPrivacy_NoNamesInLogsNotificationsOrSignals(t *testing.T) {
	w := newRosterWorld(t)
	_, err := w.r.svc.Roster(w.r.as(w.siteHolder), w.shift.ID)
	wantNoErr(t, err)
	_, err = w.r.svc.GetShift(w.r.as(w.a), w.shift.ID)
	wantNoErr(t, err)
	wantNoErr(t, w.r.svc.CancelShift(w.r.as(w.r.boss), w.shift.ID))
	for _, marker := range []string{legalAlden, legalBriar, legalShy, "Dandelion", "Tumbleweed", "Quillfeather"} {
		if w.r.cap.Contains(marker) {
			t.Errorf("%q reached a log line or span", marker)
		}
		for _, m := range w.r.mail.Sent() {
			for k, v := range m.Vars {
				if strings.Contains(v, marker) {
					t.Errorf("notification var %q carries %q", k, marker)
				}
			}
			if strings.Contains(m.Key+m.Link, marker) {
				t.Errorf("notification key/link carries %q", marker)
			}
		}
		for _, req := range w.r.jobs.Enqueued() {
			if strings.Contains(string(mustJSON(t, req)), marker) {
				t.Errorf("a job request carries %q", marker)
			}
		}
	}
	for _, m := range w.r.mail.Sent() {
		if len(m.To) != 1 {
			t.Errorf("a notification went to %d recipients, want 1 (vars carry the recipient's data only)", len(m.To))
		}
	}
}

// The view types pass privacy.Check and Filter never has to fail at run time (F141).
func TestPrivacy_ViewsCheckCleanly(t *testing.T) {
	wantNoErr(t, privacy.Check(volunteers.Manifest.Views, volunteers.Manifest.Groups))
}
