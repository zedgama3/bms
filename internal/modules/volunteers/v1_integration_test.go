//go:build integration

package volunteers_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// Amendment V1 (D49) over a real database. People are invented; names are distinct.

// reminderHorizon is decision 25: a reminder due further ahead than jobs can schedule is
// parked at jobs.MaxDelay - 24h.
const reminderHorizon = jobs.MaxDelay - 24*time.Hour

// ---- doubles ----

// countingDepts counts NamesOf calls and can hide departments from it (an archived or
// vanished department the directory no longer returns).
type countingDepts struct {
	departments.Directory
	mu    sync.Mutex
	calls int
	sizes []int
	omit  map[model.DepartmentID]bool
}

func (c *countingDepts) NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error) {
	c.mu.Lock()
	c.calls++
	c.sizes = append(c.sizes, len(ids))
	omit := c.omit
	c.mu.Unlock()
	got, err := c.Directory.NamesOf(ctx, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // test wrapper
	}
	for id := range got {
		if omit[id] {
			delete(got, id)
		}
	}
	return got, nil
}

func (c *countingDepts) hide(d model.DepartmentID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.omit == nil {
		c.omit = map[model.DepartmentID]bool{}
	}
	c.omit[d] = true
}

func (c *countingDepts) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls, c.sizes = 0, nil
}

func newRealWithDepts(t *testing.T) (*rig, *countingDepts) {
	t.Helper()
	cd := &countingDepts{}
	r := newReal(t, func(o *opts) {
		o.wrapDepts = func(d departments.Directory) departments.Directory {
			cd.Directory = d
			return cd
		}
	})
	return r, cd
}

// omitPeople hides people from NamesOf, like a profile deleted since the booking.
type omitPeople struct {
	people.Directory
	mu   sync.Mutex
	omit map[model.PersonID]bool
}

func (o *omitPeople) NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	got, err := o.Directory.NamesOf(ctx, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // test wrapper
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for id := range o.omit {
		delete(got, id)
	}
	return got, nil
}

// cursorAt builds a cursor in the documented shape: base64url of "<RFC3339Nano>|<id>".
func cursorAt(key time.Time, id string) vol.Cursor {
	return vol.Cursor(base64.RawURLEncoding.EncodeToString([]byte(key.UTC().Format(time.RFC3339Nano) + "|" + id)))
}

// ---- decision 20: invisible is NotFound, visible but not allowed is Permission ----

// invisibleShift is a members-only shift in a department the actors below do not belong to.
func invisibleShift(t *testing.T) (*rig, vol.Shift, map[string]model.PersonID) {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	sh := r.shift(d, inner.ID, model.EventID{}, when(0), 2, 2)
	return r, sh, map[string]model.PersonID{
		"plain volunteer":            r.volunteer("Tumbleweed"),
		"lead of another department": r.lead(r.department("Greeters")),
		"person without permissions": r.person("Quillfeather"),
	}
}

// F77, F143
func TestUpdateShift_InvisibleShift_ReturnsNotFound(t *testing.T) {
	r, sh, actors := invisibleShift(t)
	for name, who := range actors {
		_, err := r.svc.UpdateShift(r.as(who), sh.ID, shiftInputOf(sh))
		if !errs.Is(err, errs.NotFound) {
			t.Errorf("%s: UpdateShift = %v (kind %v), want NotFound", name, err, errs.KindOf(err))
		}
	}
	_, err := r.svc.UpdateShift(r.as(r.boss), model.NewID[vol.ShiftID](), shiftInputOf(sh))
	wantKind(t, err, errs.NotFound)
}

// F143: an open-access shift, and a members-only shift of a department the actor belongs to,
// are visible: without shifts.manage@D the answer is Permission.
func TestUpdateShift_VisibleNotLead_ReturnsPermission(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	open := r.position(d, "Dirt crew", vol.AccessOpen)
	inner := r.position(d, "Inner circle", vol.AccessMembers)
	openShift := r.shift(d, open.ID, model.EventID{}, when(0), 2, 2)
	innerShift := r.shift(d, inner.ID, model.EventID{}, when(10), 2, 2)
	member := r.volunteer("Tumbleweed")
	r.member(d, member)
	stranger := r.volunteer("Dandelion")
	elsewhere := r.lead(r.department("Greeters"))
	for name, tc := range map[string]struct {
		who model.PersonID
		sh  vol.Shift
	}{
		"stranger, open shift":          {stranger, openShift},
		"other department's lead, open": {elsewhere, openShift},
		"member, members-only shift":    {member, innerShift},
	} {
		_, err := r.svc.UpdateShift(r.as(tc.who), tc.sh.ID, shiftInputOf(tc.sh))
		if !errs.Is(err, errs.Permission) {
			t.Errorf("%s: UpdateShift = %v (kind %v), want Permission", name, err, errs.KindOf(err))
		}
	}
}

// F81, F144
func TestCancelShift_InvisibleShift_ReturnsNotFound(t *testing.T) {
	r, sh, actors := invisibleShift(t)
	for name, who := range actors {
		err := r.svc.CancelShift(r.as(who), sh.ID)
		if !errs.Is(err, errs.NotFound) {
			t.Errorf("%s: CancelShift = %v (kind %v), want NotFound", name, err, errs.KindOf(err))
		}
	}
	got, err := r.svc.GetShift(r.as(r.boss), sh.ID)
	wantNoErr(t, err)
	if got.Canceled() {
		t.Error("the shift was cancelled by a caller who could not see it")
	}
}

// ---- F166, decision 28: a shift's event is immutable ----

func TestUpdateShift_ChangesEvent_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	s0, e0 := burnDates()
	ev1, ev2 := r.event(s0, e0, "UTC", false), r.event(s0.AddDate(1, 0, 0), e0.AddDate(1, 0, 0), "UTC", false)
	inEvent := r.shift(d, pos.ID, ev1, when(0), 2, 2)
	standing := r.shift(d, pos.ID, model.EventID{}, when(10), 2, 2)
	tests := []struct {
		name string
		sh   vol.Shift
		to   model.EventID
	}{
		{"to another event", inEvent, ev2},
		{"event removed (zero)", inEvent, model.EventID{}},
		{"standing work gets an event", standing, ev1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := shiftInputOf(tc.sh)
			in.Event = tc.to
			_, err := r.svc.UpdateShift(r.as(r.boss), tc.sh.ID, in)
			wantInvalid(t, err, "scope")
			if fieldOf(err) != "event" {
				t.Errorf("field = %q, want event", fieldOf(err))
			}
			got, gerr := r.svc.GetShift(r.as(r.boss), tc.sh.ID)
			wantNoErr(t, gerr)
			if got.Event != tc.sh.Event {
				t.Errorf("the event changed to %v despite the refusal", got.Event)
			}
		})
	}
	// The same event, and a change to anything else, still works.
	in := shiftInputOf(inEvent)
	in.Label = "Renamed"
	got, err := r.svc.UpdateShift(r.as(r.boss), inEvent.ID, in)
	wantNoErr(t, err)
	if got.Event != ev1 || got.Label != "Renamed" {
		t.Errorf("update = %+v", got)
	}
}

// F158
func TestGetShift_PastVisible_Returns(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.advance(s.sh.Ends.Add(time.Hour).Sub(now))
	got, err := r.svc.GetShift(r.as(p), s.sh.ID)
	wantNoErr(t, err)
	if got.ID != s.sh.ID {
		t.Errorf("GetShift of an ended shift = %+v", got.Shift)
	}
	// The list's default is "from now on": the ended shift is not in it, but not secret either.
	list, err := shiftItems(r.svc.ListShifts(r.as(p), vol.ShiftQuery{Department: s.dept}))
	wantNoErr(t, err)
	for _, it := range list {
		if it.ID == s.sh.ID {
			t.Error("the default list shows an ended shift")
		}
	}
}

// ---- F147, F148, F148a ----

func TestRecordsOf_Self_CarriesSelfUpload(t *testing.T) {
	r := newReal(t)
	p := r.volunteer("Dandelion")
	got, err := r.svc.RecordsOf(r.as(p), p)
	wantNoErr(t, err)
	if got.SelfUpload == nil || !*got.SelfUpload {
		t.Errorf("SelfUpload = %v, want present and true (the default)", got.SelfUpload)
	}
	r.set.Set(volunteers.SettingSelfUpload, "false") // read now, never cached
	got, err = r.svc.RecordsOf(r.as(p), p)
	wantNoErr(t, err)
	if got.SelfUpload == nil || *got.SelfUpload {
		t.Errorf("SelfUpload = %v, want present and false after the setting changed", got.SelfUpload)
	}
	if !strings.Contains(string(mustJSON(t, got)), `"self_upload":false`) {
		t.Errorf("body = %s, want self_upload:false on the wire", mustJSON(t, got))
	}
}

func TestRecordsOf_Other_OmitsSelfUpload(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	p := r.person("Dandelion")
	reader := r.lead(d) // qualifications.read@D
	got, err := r.svc.RecordsOf(r.as(reader), p)
	wantNoErr(t, err)
	if got.SelfUpload != nil {
		t.Errorf("SelfUpload = %v for another person's records, want absent", *got.SelfUpload)
	}
	if strings.Contains(string(mustJSON(t, got)), "self_upload") {
		t.Errorf("body = %s carries self_upload", mustJSON(t, got))
	}
}

// V1.8 (g): the service returns a non-nil slice so an empty list is [] on the wire.
func TestLists_Empty_ItemsAreNonNil(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	p := r.volunteer("Dandelion")
	shifts, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{})
	wantNoErr(t, err)
	sched, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{})
	wantNoErr(t, err)
	past, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{Past: true})
	wantNoErr(t, err)
	members, err := r.svc.ListMembers(r.as(r.lead(d)), vol.MemberQuery{Department: d})
	wantNoErr(t, err)
	records, err := r.svc.RecordsOf(r.as(p), p)
	wantNoErr(t, err)
	for name, b := range map[string][]byte{
		"ListShifts": mustJSON(t, shifts), "MySchedule": mustJSON(t, sched), "MySchedule past": mustJSON(t, past),
		"ListMembers": mustJSON(t, members), "RecordsOf": mustJSON(t, records),
	} {
		if !strings.Contains(string(b), `"items":[]`) {
			t.Errorf("%s encodes as %s, want items:[]", name, b)
		}
	}
	if !shifts.Next.IsZero() || !sched.Next.IsZero() || !members.Next.IsZero() {
		t.Error("an empty list carries a Next cursor")
	}
}

// F148
func TestRecordQualification_NoExpiryWithDate_ReturnsInvalid(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	p := r.person("Dandelion")
	_, err := r.svc.RecordQualification(r.as(r.lead(d)), vol.RecordInput{
		Qualification: q.ID, Person: p, AcquiredOn: jan1, ExpiresOn: vol.Date{Year: 2027, Month: 1, Day: 1}, NoExpiry: true,
	})
	wantInvalid(t, err, "one_of")
	if fieldOf(err) != "no_expiry" {
		t.Errorf("field = %q, want no_expiry", fieldOf(err))
	}
}

// F148
func TestVerifyRecord_NoExpiryWithDate_ReturnsInvalid(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	_, err := r.svc.VerifyRecord(r.as(r.lead(d)), rec.ID, vol.VerifyInput{ExpiresOn: vol.Date{Year: 2027, Month: 1, Day: 1}, NoExpiry: true})
	wantInvalid(t, err, "one_of")
	if fieldOf(err) != "no_expiry" {
		t.Errorf("field = %q, want no_expiry", fieldOf(err))
	}
	// Refused means unchanged: still pending.
	recs, rerr := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, rerr)
	if len(recs) != 1 || recs[0].Verified() {
		t.Errorf("records = %+v, want the one pending record untouched", recs)
	}
}

// F148a: a lead clears the expiry a volunteer entered for a certificate that never expires.
func TestVerifyRecord_NoExpiry_ClearsExpiry(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{
		Qualification: q.ID, AcquiredOn: jan1, ExpiresOn: vol.Date{Year: 2026, Month: 12, Day: 31}, Upload: upload("radio.pdf", pdf()),
	})
	wantNoErr(t, err)
	if rec.ExpiresOn.IsZero() {
		t.Fatal("the fixture's pending record has no expiry")
	}
	got, err := r.svc.VerifyRecord(r.as(r.lead(d)), rec.ID, vol.VerifyInput{NoExpiry: true})
	wantNoErr(t, err)
	if !got.Verified() || !got.ExpiresOn.IsZero() {
		t.Fatalf("record = %+v, want verified with no expiry", got)
	}
	if !got.ValidOn(vol.Date{Year: 2099, Month: 1, Day: 1}) {
		t.Error("a never-expiring record is not valid in 2099")
	}
	// The stored row says the same, and the record now satisfies a shift years ahead.
	recs, err := recordItems(r.svc.RecordsOf(r.as(p), p))
	wantNoErr(t, err)
	if len(recs) != 1 || !recs[0].ExpiresOn.IsZero() {
		t.Errorf("stored records = %+v", recs)
	}
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	far := r.shift(d, pos.ID, model.EventID{}, time.Date(2028, 3, 1, 9, 0, 0, 0, time.UTC), 2, 1)
	r.book(p, far.ID)
}

// F148 and the catalogue's default validity: NoExpiry wins over it, a zero ExpiresOn takes it.
func TestRecordQualification_NoExpiry_OverridesCatalogueValidity(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q, err := r.svc.CreateQualification(r.as(r.boss), vol.QualificationInput{Department: d, Name: "First aid", ValidityDays: 365})
	wantNoErr(t, err)
	lead := r.lead(d)
	withDefault, err := r.svc.RecordQualification(r.as(lead), vol.RecordInput{Qualification: q.ID, Person: r.person("Dandelion"), AcquiredOn: jan1})
	wantNoErr(t, err)
	if want := jan1.AddDays(365); withDefault.ExpiresOn != want {
		t.Errorf("ExpiresOn = %+v, want the catalogue default %+v", withDefault.ExpiresOn, want)
	}
	never, err := r.svc.RecordQualification(r.as(lead), vol.RecordInput{Qualification: q.ID, Person: r.person("Tumbleweed"), AcquiredOn: jan1, NoExpiry: true})
	wantNoErr(t, err)
	if !never.ExpiresOn.IsZero() {
		t.Errorf("ExpiresOn = %+v with NoExpiry, want indefinite", never.ExpiresOn)
	}
}

// V1.8 (a): the verifier's deletion does not stop a record counting (see also the persondata tests).
func TestRecord_VerifiedByDeletedLead_StillCounts(t *testing.T) {
	w := newDataWorld(t)
	_, err := w.r.mod.DeletePerson(ctxT(t), w.lead)
	wantNoErr(t, err)
	pos := w.pos
	sh := w.r.shift(w.d, pos.ID, model.EventID{}, when(50), 2, 1)
	w.r.book(w.subject, sh.ID) // the position requires the Radio qualification the deleted lead verified
}

// ---- paging: F149, F149a, F149b ----

// pageAll follows Next from the first page and returns the pages' item counts and every item.
func pageShifts(t *testing.T, r *rig, who model.PersonID, q vol.ShiftQuery) (sizes []int, all []vol.ShiftSummary, nexts []vol.Cursor) {
	t.Helper()
	for range 50 {
		l, err := r.svc.ListShifts(r.as(who), q)
		wantNoErr(t, err)
		sizes, all, nexts = append(sizes, len(l.Items)), append(all, l.Items...), append(nexts, l.Next)
		if l.Next.IsZero() {
			return sizes, all, nexts
		}
		q.After = l.Next
	}
	t.Fatal("ListShifts never reached a last page")
	return nil, nil, nil
}

// F149a: Next never skips a row; a short last page has no Next; (starts, id) is the order.
func TestListShifts_Pages_NeverSkips(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	// Two pairs share a start so a page boundary can fall between equals.
	for _, h := range []int{0, 0, 3, 3, 6, 9, 12} {
		r.shift(d, pos.ID, model.EventID{}, when(h), 1, 1)
	}
	p := r.volunteer("Dandelion")
	full, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{Department: d, Page: vol.Page{Limit: vol.MaxList}})
	wantNoErr(t, err)
	if len(full.Items) != 7 || !full.Next.IsZero() {
		t.Fatalf("unpaged list = %d items, next %q; want 7 and none", len(full.Items), full.Next)
	}
	for i := 1; i < len(full.Items); i++ {
		a, b := full.Items[i-1], full.Items[i]
		ab, bb := [16]byte(a.ID), [16]byte(b.ID)
		if b.Starts.Before(a.Starts) || (b.Starts.Equal(a.Starts) && bytes.Compare(ab[:], bb[:]) >= 0) {
			t.Fatalf("items %d,%d are out of (starts, id) order", i-1, i)
		}
	}
	for _, limit := range []int{1, 2, 3, 4, 6, 7} {
		sizes, all, nexts := pageShifts(t, r, p, vol.ShiftQuery{Department: d, Page: vol.Page{Limit: limit}})
		if len(all) != 7 {
			t.Errorf("limit %d: %d items over the pages %v, want all 7", limit, len(all), sizes)
			continue
		}
		for i := range all {
			if all[i].ID != full.Items[i].ID {
				t.Errorf("limit %d: item %d differs from the unpaged list (a row was skipped or repeated)", limit, i)
			}
		}
		last := len(nexts) - 1
		for i, n := range nexts {
			if (i == last) != n.IsZero() {
				t.Errorf("limit %d: page %d next=%q (the last page, and only it, has none)", limit, i, n)
			}
		}
		for i, s := range sizes[:last] {
			if s != limit {
				t.Errorf("limit %d: page %d holds %d, want a full page", limit, i, s)
			}
		}
	}
	// Exactly a multiple of the limit: the second page is full and has no Next (Limit+1 rows decide).
	sizes, _, nexts := pageShifts(t, r, p, vol.ShiftQuery{Department: d, Within: model.TimeRange{Start: when(0), End: when(7)}, Page: vol.Page{Limit: 3}})
	if len(sizes) != 2 || sizes[0] != 3 || sizes[1] != 2 { // shifts at 0,0,3,3,6 fall in the window: 5 rows
		t.Errorf("window pages = %v, want [3 2]", sizes)
	}
	if nexts[0].IsZero() || !nexts[1].IsZero() {
		t.Errorf("nexts = %q, want Next on the first page only", nexts)
	}
}

func TestListShifts_ExactMultiple_LastPageHasNoNext(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	for h := range 6 {
		r.shift(d, pos.ID, model.EventID{}, when(h*3), 1, 1)
	}
	p := r.volunteer("Dandelion")
	sizes, all, nexts := pageShifts(t, r, p, vol.ShiftQuery{Department: d, Page: vol.Page{Limit: 3}})
	if len(all) != 6 || len(sizes) != 2 || !nexts[1].IsZero() || nexts[0].IsZero() {
		t.Errorf("pages %v, nexts %q; want two full pages, Next only on the first", sizes, nexts)
	}
}

// F149a: a department the directory no longer returns drops its rows after the fetch; the
// page is short but Next still comes from the fetched rows, so following it loses nothing else.
func TestListShifts_ArchivedDepartment_ShortPageKeepsNext(t *testing.T) {
	r, cd := newRealWithDepts(t)
	gone, kept := r.department("Rangers"), r.department("Greeters")
	posGone, posKept := r.position(gone, "Dirt crew", vol.AccessOpen), r.position(kept, "Door crew", vol.AccessOpen)
	for _, h := range []int{0, 3, 6} {
		r.shift(gone, posGone.ID, model.EventID{}, when(h), 1, 1)
	}
	var keptShifts []vol.Shift
	for _, h := range []int{9, 12} {
		keptShifts = append(keptShifts, r.shift(kept, posKept.ID, model.EventID{}, when(h), 1, 1))
	}
	cd.hide(gone)
	p := r.volunteer("Dandelion")
	first, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{Page: vol.Page{Limit: 3}})
	wantNoErr(t, err)
	if len(first.Items) != 0 {
		t.Fatalf("first page holds %d items, want none (all three belong to the hidden department)", len(first.Items))
	}
	if first.Next.IsZero() {
		t.Fatal("a short page lost its Next: the rows after it would never be reached")
	}
	second, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{Page: vol.Page{Limit: 3, After: first.Next}})
	wantNoErr(t, err)
	if len(second.Items) != 2 || second.Items[0].ID != keptShifts[0].ID || second.Items[1].ID != keptShifts[1].ID || !second.Next.IsZero() {
		t.Errorf("second page = %d items, next %q; want the two Greeters shifts and no Next", len(second.Items), second.Next)
	}
	for _, it := range append(first.Items, second.Items...) {
		if it.Department == gone {
			t.Error("a shift of the hidden department was returned")
		}
	}
}

// Decision 22: a cursor from elsewhere is just a position: rows after that key, no error.
func TestListShifts_ForeignCursor_YieldsRowsAfterKey(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	var shifts []vol.Shift
	for _, h := range []int{0, 3, 6, 9} {
		shifts = append(shifts, r.shift(d, pos.ID, model.EventID{}, when(h), 1, 1))
	}
	p := r.volunteer("Dandelion")
	c := cursorAt(when(3), model.NewID[vol.ShiftID]().String()) // key between the second and third shift
	got, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{Department: d, Page: vol.Page{After: c}})
	wantNoErr(t, err)
	var ids []vol.ShiftID
	for _, it := range got.Items {
		if it.Starts.Before(when(3)) {
			t.Errorf("a row at %v precedes the cursor key", it.Starts)
		}
		ids = append(ids, it.ID)
	}
	for _, later := range shifts[2:] {
		if !slices.Contains(ids, later.ID) {
			t.Errorf("shift at %v after the cursor key is missing", later.Starts)
		}
	}
	if slices.Contains(ids, shifts[0].ID) {
		t.Error("a shift well before the cursor key was returned")
	}
}

// DepartmentName and PositionName come with the summary; one Departments.NamesOf per page.
func TestListShifts_CarriesNames_OneDirectoryCallPerPage(t *testing.T) {
	r, cd := newRealWithDepts(t)
	a, b := r.department("Rangers"), r.department("Greeters")
	pa, pb := r.position(a, "Dirt crew", vol.AccessOpen), r.position(b, "Door crew", vol.AccessOpen)
	r.shift(a, pa.ID, model.EventID{}, when(0), 1, 1)
	r.shift(b, pb.ID, model.EventID{}, when(3), 1, 1)
	r.shift(a, pa.ID, model.EventID{}, when(6), 1, 1)
	p := r.volunteer("Dandelion")
	cd.reset()
	got, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{})
	wantNoErr(t, err)
	if len(got.Items) != 3 {
		t.Fatalf("%d items", len(got.Items))
	}
	for _, it := range got.Items {
		wantD, wantP := "Rangers", "Dirt crew"
		if it.Department == b {
			wantD, wantP = "Greeters", "Door crew"
		}
		if it.DepartmentName != wantD || it.PositionName != wantP {
			t.Errorf("shift names = %q/%q, want %q/%q", it.DepartmentName, it.PositionName, wantD, wantP)
		}
	}
	if cd.calls != 1 {
		t.Errorf("%d Departments.NamesOf calls for one page, want 1", cd.calls)
	}
	one, err := r.svc.GetShift(r.as(p), got.Items[0].ID)
	wantNoErr(t, err)
	if one.DepartmentName == "" || one.PositionName == "" {
		t.Errorf("GetShift names = %q/%q", one.DepartmentName, one.PositionName)
	}
}

// F149b and F153: upcoming is soonest first with removals marked; past is newest first, booked only.
func TestMySchedule_Pages_UpcomingSoonestFirst_PastNewestFirst(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	p := r.volunteer("Dandelion")
	var shifts []vol.Shift
	for i := range 6 {
		sh := r.shift(d, pos.ID, model.EventID{}, when(i*10), 2, 2)
		shifts = append(shifts, sh)
		r.book(p, sh.ID)
	}
	// The sixth is removed by a lead: it never counts as worked.
	wantNoErr(t, r.svc.CancelSignup(r.as(lead), r.bookingOf(p, shifts[5].ID)))

	// Upcoming, pages of 2: ascending, the removed booking is listed with its reason.
	var up []vol.ScheduleEntry
	q := vol.ScheduleQuery{Page: vol.Page{Limit: 2}}
	for range 10 {
		l, err := r.svc.MySchedule(r.as(p), q)
		wantNoErr(t, err)
		up = append(up, l.Items...)
		if l.Next.IsZero() {
			break
		}
		q.After = l.Next
	}
	if len(up) != 6 {
		t.Fatalf("upcoming lists %d entries over its pages, want 6 (five booked, one removed)", len(up))
	}
	for i, e := range up {
		if e.Shift.ID != shifts[i].ID {
			t.Errorf("upcoming[%d] is the wrong shift: not soonest first", i)
		}
	}
	if up[5].Status != vol.SignupCanceled || up[5].CancelReason != vol.CancelLead {
		t.Errorf("the removed booking = %+v, want canceled with reason lead", up[5].Signup)
	}

	// Everything ends; past mode is booked rows, newest first, paged.
	r.advance(shifts[5].Ends.Add(time.Hour).Sub(now))
	var past []vol.ScheduleEntry
	pages := 0
	q = vol.ScheduleQuery{Past: true, Page: vol.Page{Limit: 2}}
	for range 10 {
		l, err := r.svc.MySchedule(r.as(p), q)
		wantNoErr(t, err)
		pages++
		past = append(past, l.Items...)
		if l.Next.IsZero() {
			break
		}
		q.After = l.Next
	}
	if len(past) != 5 || pages != 3 {
		t.Fatalf("past mode: %d entries in %d pages, want 5 in 3 (the removed booking is not 'worked')", len(past), pages)
	}
	for i, e := range past {
		if want := shifts[4-i].ID; e.Shift.ID != want || e.Status != vol.SignupBooked {
			t.Errorf("past[%d] = shift %v status %v, want newest first and booked", i, e.Shift.ID, e.Status)
		}
	}
	// Upcoming is now empty: nothing has a future end and a removed booking of an ended shift is gone.
	l, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{})
	wantNoErr(t, err)
	if len(l.Items) != 0 {
		t.Errorf("upcoming after every shift ended = %d entries, want 0", len(l.Items))
	}
}

// F149: MySchedule's cursor is the person's own position; a cursor naming a later key skips to it.
func TestMySchedule_Window_NarrowsBothModes(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	p := r.volunteer("Dandelion")
	var shifts []vol.Shift
	for i := range 4 {
		sh := r.shift(d, pos.ID, model.EventID{}, when(i*10), 2, 2)
		shifts = append(shifts, sh)
		r.book(p, sh.ID)
	}
	l, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{Within: model.TimeRange{Start: when(10), End: when(25)}})
	wantNoErr(t, err)
	if len(l.Items) != 2 || l.Items[0].Shift.ID != shifts[1].ID || l.Items[1].Shift.ID != shifts[2].ID {
		t.Errorf("window result = %d entries, want shifts 1 and 2", len(l.Items))
	}
	l, err = r.svc.MySchedule(r.as(p), vol.ScheduleQuery{Within: model.TimeRange{Start: when(20)}})
	wantNoErr(t, err)
	if len(l.Items) != 2 {
		t.Errorf("start-only window = %d entries, want 2", len(l.Items))
	}
}

func TestListMembers_Pages_NeverSkips(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	lead := r.lead(d)
	var people []model.PersonID
	for _, n := range []string{"Aspen", "Birch", "Cedar", "Dogwood", "Elm"} {
		p := r.volunteer(n)
		people = append(people, p)
		r.member(d, p)
		r.advance(time.Minute) // distinct created_at where the store uses the clock
	}
	full, err := r.svc.ListMembers(r.as(lead), vol.MemberQuery{Department: d, Page: vol.Page{Limit: vol.MaxList}})
	wantNoErr(t, err)
	if len(full.Items) != 5 || !full.Next.IsZero() {
		t.Fatalf("unpaged = %d items, next %q", len(full.Items), full.Next)
	}
	for _, limit := range []int{1, 2, 3, 5} {
		var got []vol.MemberView
		var nexts []vol.Cursor
		q := vol.MemberQuery{Department: d, Page: vol.Page{Limit: limit}}
		for range 10 {
			l, err := r.svc.ListMembers(r.as(lead), q)
			wantNoErr(t, err)
			got, nexts = append(got, l.Items...), append(nexts, l.Next)
			if l.Next.IsZero() {
				break
			}
			q.After = l.Next
		}
		if len(got) != 5 {
			t.Errorf("limit %d: %d members over the pages, want 5", limit, len(got))
			continue
		}
		for i := range got {
			if got[i].Person != full.Items[i].Person {
				t.Errorf("limit %d: member %d differs from the unpaged order", limit, i)
			}
		}
		for i, n := range nexts {
			if (i == len(nexts)-1) != n.IsZero() {
				t.Errorf("limit %d: page %d next=%q", limit, i, n)
			}
		}
	}
	for _, m := range full.Items {
		if m.PlayaName == "" {
			t.Errorf("member %v has no playa name in the view", m.Person)
		}
	}
	_ = people
}

// The wire: ?limit and ?after, Next as "next", absent on the last page.
func TestHTTP_ShiftsPaging_FollowsNext(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	for h := range 5 {
		r.shift(d, pos.ID, model.EventID{}, when(h*3), 1, 1)
	}
	p := r.volunteer("Dandelion")
	path := "/shifts?department=" + d.String() + "&limit=2"
	var seen, pages int
	for range 10 {
		rec := r.call(p, "GET", path, nil)
		wantStatus(t, rec, http.StatusOK)
		var body struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v (%q)", err, rec.Body.String())
		}
		pages++
		seen += len(body.Items)
		if body.Next == "" {
			if strings.Contains(rec.Body.String(), `"next"`) {
				t.Errorf("the last page carries a next key: %q", rec.Body.String())
			}
			break
		}
		path = "/shifts?department=" + d.String() + "&limit=2&after=" + body.Next
	}
	if seen != 5 || pages != 3 {
		t.Errorf("followed next over %d pages to %d shifts, want 3 pages and 5 shifts", pages, seen)
	}
}

// ---- F150: CopyShifts keeps wall-clock times in the destination zone ----

func localClock(t time.Time, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		panic(err)
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func copyOne(t *testing.T, r *rig, d model.DepartmentID, pos vol.PositionID, from, to model.EventID, starts, ends time.Time) vol.ShiftSummary {
	t.Helper()
	src, err := r.svc.CreateShift(r.as(r.boss), d, vol.ShiftInput{Position: pos, Event: from, Label: "Gate", Starts: starts, Ends: ends, Slots: 2})
	wantNoErr(t, err)
	n, err := r.svc.CopyShifts(r.as(r.boss), d, from, to)
	wantNoErr(t, err)
	if n != 1 {
		t.Fatalf("copied %d shifts, want 1", n)
	}
	got, err := shiftItems(r.svc.ListShifts(r.as(r.boss), vol.ShiftQuery{Department: d, Event: to, Within: model.TimeRange{Start: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)}}))
	wantNoErr(t, err)
	if len(got) != 1 || got[0].CopiedFrom != src.ID {
		t.Fatalf("copies = %+v", got)
	}
	return got[0]
}

func TestCopyShifts_AcrossDST_KeepsLocalTimes(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Gate", vol.AccessOpen)
	// Denver leaves daylight time on 2026-11-01: the source week is MDT (UTC-6), the target MST (UTC-7).
	from := r.event(time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC), "America/Denver", false)
	to := r.event(time.Date(2026, 11, 5, 7, 0, 0, 0, time.UTC), time.Date(2026, 11, 8, 7, 0, 0, 0, time.UTC), "America/Denver", false)
	starts := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC) // 08:00 MDT
	got := copyOne(t, r, d, pos.ID, from, to, starts, starts.Add(6*time.Hour))
	if s, e := localClock(got.Starts, "America/Denver"), localClock(got.Ends, "America/Denver"); s != "2026-11-06 08:00" || e != "2026-11-06 14:00" {
		t.Errorf("copy runs %s to %s Denver time, want 08:00 to 14:00 on 2026-11-06 (an instant shift would give 07:00)", s, e)
	}
	if want := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC); !got.Starts.Equal(want) {
		t.Errorf("Starts = %v, want %v (08:00 MST)", got.Starts, want)
	}
	if got.Zone != "America/Denver" {
		t.Errorf("Zone = %q", got.Zone)
	}
}

func TestCopyShifts_DifferentZone_KeepsLocalTimes(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Gate", vol.AccessOpen)
	from := r.event(time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC), "America/Denver", false)
	to := r.event(time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC), time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC), "America/Los_Angeles", false)
	starts := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC) // 08:00 MDT
	got := copyOne(t, r, d, pos.ID, from, to, starts, starts.Add(6*time.Hour))
	if s, e := localClock(got.Starts, "America/Los_Angeles"), localClock(got.Ends, "America/Los_Angeles"); s != "2026-11-06 08:00" || e != "2026-11-06 14:00" {
		t.Errorf("copy runs %s to %s Los Angeles time, want 08:00 to 14:00 on 2026-11-06", s, e)
	}
	if got.Zone != "America/Los_Angeles" {
		t.Errorf("Zone = %q, want the destination event's", got.Zone)
	}
}

// A shift inside the hour that does not exist on the target day still ends after it starts,
// and keeps the source duration.
func TestCopyShifts_SkippedHour_KeepsDuration(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Gate", vol.AccessOpen)
	// Denver springs forward at 02:00 on 2027-03-14; the source week is MST.
	from := r.event(time.Date(2027, 3, 7, 7, 0, 0, 0, time.UTC), time.Date(2027, 3, 10, 7, 0, 0, 0, time.UTC), "America/Denver", false)
	to := r.event(time.Date(2027, 3, 14, 7, 0, 0, 0, time.UTC), time.Date(2027, 3, 17, 6, 0, 0, 0, time.UTC), "America/Denver", false)
	starts := time.Date(2027, 3, 7, 9, 0, 0, 0, time.UTC) // 02:00 MST
	dur := 30 * time.Minute
	got := copyOne(t, r, d, pos.ID, from, to, starts, starts.Add(dur))
	if !got.Ends.After(got.Starts) {
		t.Fatalf("copy ends %v, not after it starts %v", got.Ends, got.Starts)
	}
	if got.Ends.Sub(got.Starts) != dur {
		t.Errorf("copy lasts %v, want the source duration %v", got.Ends.Sub(got.Starts), dur)
	}
	if day := localClock(got.Starts, "America/Denver")[:10]; day != "2027-03-14" {
		t.Errorf("copy starts on %s, want the same day a week later", day)
	}
}

// ---- F151: the reminder horizon ----

func reminderFor(r *rig, key string) (jobs.Request, bool) {
	var out jobs.Request
	ok := false
	for _, req := range r.jobs.Enqueued() {
		if string(req.Name) == volunteers.JobShiftReminder && req.Key == key {
			out, ok = req, true // the later request for a key replaces the earlier (reschedule semantics)
		}
	}
	return out, ok
}

func TestSignUp_FarFuture_ParksReminderAtHorizon(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	tests := []struct {
		name string
		due  time.Duration // how far ahead Starts - lead (24h) is
	}{
		{"due well inside the horizon", 30 * 24 * time.Hour},
		{"due just inside the horizon", reminderHorizon - time.Hour},
		{"due just beyond the horizon", reminderHorizon + time.Hour},
		{"due years ahead", 3 * 365 * 24 * time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := r.volunteer("Dandelion")
			starts := r.clk.Now().Add(tc.due + 24*time.Hour) // the default lead is 24h
			sh := r.shift(d, pos.ID, model.EventID{}, starts, 2, 1)
			su := r.book(p, sh.ID)
			req, ok := reminderFor(r, su.ID.String())
			if !ok {
				t.Fatal("no reminder was enqueued for the booking (a far booking must be parked, never dropped)")
			}
			want := r.clk.Now().Add(min(tc.due, reminderHorizon))
			if !req.RunAt.Equal(want) {
				t.Errorf("RunAt = %v, want %v", req.RunAt, want)
			}
			if req.RunAt.After(r.clk.Now().Add(jobs.MaxDelay)) {
				t.Errorf("RunAt %v is beyond what jobs can schedule", req.RunAt)
			}
		})
	}
}

func TestUpdateShift_MovedBeyondHorizon_ParksReminder(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	first, ok := reminderFor(r, su.ID.String())
	if !ok || !first.RunAt.Equal(s.sh.Starts.Add(-24*time.Hour)) {
		t.Fatalf("first reminder = %+v", first)
	}
	in := shiftInputOf(s.sh)
	in.Starts, in.Ends = now.Add(3*reminderHorizon), now.Add(3*reminderHorizon+4*time.Hour)
	_, err := r.svc.UpdateShift(r.as(s.lead), s.sh.ID, in)
	wantNoErr(t, err)
	moved, ok := reminderFor(r, su.ID.String())
	if !ok {
		t.Fatal("the reminder disappeared")
	}
	if want := r.clk.Now().Add(reminderHorizon); !moved.RunAt.Equal(want) {
		t.Errorf("RunAt after moving the shift years ahead = %v, want it parked at now + horizon (%v); the old row would fire early", moved.RunAt, want)
	}
}

// F112a with decision 25: the parked reminder carries itself forward until Starts - lead.
func TestReminder_ParkedAtHorizon_CarriesItselfForward(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	starts := now.Add(5*reminderHorizon/2 + 24*time.Hour) // due = now + 2.5 horizons
	sh := r.shift(d, pos.ID, model.EventID{}, starts, 2, 1)
	su := r.book(r.volunteer("Dandelion"), sh.ID)
	r.advance(reminderHorizon)
	before := len(r.jobs.Enqueued())
	wantNoErr(t, r.run(volunteers.JobShiftReminder, jobWith(signals.Ref("signup", su.ID))))
	if n := len(r.sentTo("volunteers.shift-reminder")); n != 0 {
		t.Fatalf("%d reminders sent a year and a half early", n)
	}
	again := r.jobs.Enqueued()[before:]
	if len(again) != 1 || again[0].Key != su.ID.String() {
		t.Fatalf("re-enqueued = %+v, want one row with the sign-up's Key", again)
	}
	if want := r.clk.Now().Add(reminderHorizon); !again[0].RunAt.Equal(want) {
		t.Errorf("RunAt = %v, want min(due, now + horizon) = %v", again[0].RunAt, want)
	}
}

// ---- F152, F152a: a lead may assign themself, with a recorded waiver ----

func TestAssign_SelfWaive_RecordsWaiver(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 4, 2)
	lead := r.lead(d)
	r.ppl.Add(lead, r.names.unique("Lead Waiver"))
	su, err := r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: lead, Waive: true})
	wantNoErr(t, err)
	if su.Person != lead || su.WaivedBy != lead || su.BookedBy != lead || su.BookedAs != vol.BookedLead {
		t.Errorf("signup = %+v, want person, WaivedBy and BookedBy all the actor, BookedAs lead", su)
	}
	entries := r.audited("volunteers.signup.created")
	if len(entries) != 1 || entries[0].Reason != "waived" || entries[0].Subject != lead {
		t.Fatalf("audit = %+v, want one entry, reason waived, about the lead", entries)
	}
	// The recorder resolves the actor from the context: when it is set it is the same person.
	if a := entries[0].Actor; a.Kind != 0 && a.Person != lead {
		t.Errorf("audit actor = %+v, want the lead (actor = subject is the record of a self-waiver)", a)
	}
	ro, err := r.svc.Roster(r.as(lead), sh.ID)
	wantNoErr(t, err)
	if len(ro.Entries) != 1 || ro.Entries[0].Person != lead || ro.Entries[0].WaivedBy != lead || ro.Entries[0].Eligible {
		t.Errorf("roster = %+v, want the lead flagged as waived and not eligible", ro.Entries)
	}
	// Without Waive the same lead is refused like anyone: the waiver is the only door.
	_, err = r.svc.Assign(r.as(lead), r.shift(d, pos.ID, model.EventID{}, when(20), 4, 2).ID, vol.AssignInput{Person: lead})
	wantConflict(t, err, "unqualified")
}

func TestAssign_SelfEligible_Succeeds(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	r.ppl.Add(s.lead, r.names.unique("Lead Eligible"))
	for _, waive := range []bool{false, true} {
		sh := r.shift(s.dept, s.pos.ID, model.EventID{}, when(20*b2i(waive)), 4, 2)
		su, err := r.svc.Assign(r.as(s.lead), sh.ID, vol.AssignInput{Person: s.lead, Waive: waive})
		wantNoErr(t, err)
		if su.BookedAs != vol.BookedLead || !su.WaivedBy.IsZero() || su.BookedBy != s.lead {
			t.Errorf("waive=%v: signup = %+v, want BookedAs lead, no waiver", waive, su)
		}
	}
	for _, e := range r.audited("volunteers.signup.created") {
		if e.Reason == "waived" {
			t.Error("a waiver was audited for an eligible lead")
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// F154's source: who booked is a role word kept on the sign-up.
func TestSignUp_BookedAs_SelfAndLead(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	mine := r.book(r.volunteer("Dandelion"), s.sh.ID)
	if mine.BookedAs != vol.BookedSelf || !mine.BookedBy.IsZero() {
		t.Errorf("self sign-up = %+v, want BookedAs self and BookedBy zero", mine)
	}
	other, err := r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: r.person("Tumbleweed")})
	wantNoErr(t, err)
	if other.BookedAs != vol.BookedLead || other.BookedBy != s.lead {
		t.Errorf("assigned = %+v, want BookedAs lead and BookedBy the lead", other)
	}
}

// ---- F161, F161a, F164: decision 26 ----

type nameCase struct {
	name  string
	setup func(r *rig) model.PersonID
}

func unnamedPeople() []nameCase {
	return []nameCase{
		{"playa name empty", func(r *rig) model.PersonID {
			p := r.volunteer("Placeholder")
			r.ppl.Add(p, "")
			return p
		}},
		{"unknown to people", func(r *rig) model.PersonID {
			p := r.person("")
			r.az.Grant(p, pSignUp)
			return p
		}},
	}
}

func assertNothingBooked(t *testing.T, r *rig, lead model.PersonID, sh vol.ShiftID) {
	t.Helper()
	ro, err := r.svc.Roster(r.as(lead), sh)
	wantNoErr(t, err)
	if len(ro.Entries) != 0 {
		t.Errorf("%d people booked by a refused call", len(ro.Entries))
	}
	if n := len(r.audited("volunteers.signup.created")); n != 0 {
		t.Errorf("%d signup.created audit entries for a refused booking", n)
	}
	if n := len(r.published(volunteers.SignalSignupBooked)); n != 0 {
		t.Errorf("%d signup-booked signals for a refused booking", n)
	}
	for _, req := range r.jobs.Enqueued() {
		if string(req.Name) == volunteers.JobShiftReminder {
			t.Errorf("a reminder was enqueued for a refused booking")
		}
	}
}

// shiftsOfBothKinds returns a standing-work shift and an event shift of one position.
func shiftsOfBothKinds(r *rig, d model.DepartmentID, pos vol.PositionID) map[string]vol.Shift {
	s0, e0 := burnDates()
	ev := r.event(s0, e0, "UTC", false)
	return map[string]vol.Shift{
		"standing shift": r.shift(d, pos, model.EventID{}, when(0), 2, 5),
		"event shift":    r.shift(d, pos, ev, when(10), 2, 5),
	}
}

// F161: SignUp, any shift, with or without the name.
func TestSignUp_NoPlayaName_ReturnsUnnamed(t *testing.T) {
	for _, pc := range unnamedPeople() {
		for _, kind := range []string{"standing shift", "event shift"} {
			t.Run(pc.name+"/"+kind, func(t *testing.T) {
				r := newReal(t)
				d := r.department("Rangers")
				pos := r.position(d, "Dirt crew", vol.AccessOpen)
				sh := shiftsOfBothKinds(r, d, pos.ID)[kind]
				p := pc.setup(r)
				_, err := r.svc.SignUp(r.as(p), sh.ID)
				wantConflict(t, err, "unnamed")
				assertNothingBooked(t, r, r.boss, sh.ID)
				// Once the person has a name the same call books.
				r.ppl.Add(p, r.names.unique("Fennel"))
				_, err = r.svc.SignUp(r.as(p), sh.ID)
				wantNoErr(t, err)
			})
		}
	}
}

// F161: Assign, table: shift kind x Waive on and off. Waive covers a missing requirement, never the name.
func TestAssign_NoPlayaName_ReturnsUnnamed(t *testing.T) {
	for _, pc := range unnamedPeople() {
		for _, kind := range []string{"standing shift", "event shift"} {
			for _, waive := range []bool{false, true} {
				name := pc.name + "/" + kind
				if waive {
					name += "/waive"
				}
				t.Run(name, func(t *testing.T) {
					r := newReal(t)
					d := r.department("Rangers")
					pos := r.position(d, "Dirt crew", vol.AccessOpen)
					sh := shiftsOfBothKinds(r, d, pos.ID)[kind]
					lead := r.lead(d)
					p := pc.setup(r)
					_, err := r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: p, Waive: waive})
					wantConflict(t, err, "unnamed")
					assertNothingBooked(t, r, lead, sh.ID)
				})
			}
		}
	}
}

// F161: a nameless person who is also unqualified is told about the name first (the name is
// checked with the person lock, before the shift is read), and Waive does not change that.
func TestAssign_NoPlayaNameAndUnqualified_ReturnsUnnamed(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	q := r.qual(d, "Radio")
	pos := r.position(d, "Radio op", vol.AccessOpen, q.ID)
	sh := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 5)
	lead := r.lead(d)
	p := r.person("")
	for _, waive := range []bool{false, true} {
		_, err := r.svc.Assign(r.as(lead), sh.ID, vol.AssignInput{Person: p, Waive: waive})
		wantConflict(t, err, "unnamed")
	}
	assertNothingBooked(t, r, lead, sh.ID)
}

// F161a: a named person books; the module asks people once, for that one person, and compares
// no names: two people people would never allow to share a name book the same shift here.
func TestSignUp_Named_NoNameComparison(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	a := r.volunteer("Dandelion")
	b := r.person("")
	r.az.Grant(b, pSignUp)
	r.ppl.Add(b, "Dandelion") // the state people P1 makes impossible, seeded to prove no check is made here
	r.cpl.resetCounts()
	r.book(a, s.sh.ID)
	names, _ := r.cpl.counts()
	if names != 1 || len(r.cpl.nameIDs) != 1 || !slices.Equal(r.cpl.nameIDs[0], []model.PersonID{a}) {
		t.Errorf("NamesOf calls = %d with IDs %v, want one call with exactly the booking person", names, r.cpl.nameIDs)
	}
	su, err := r.svc.SignUp(r.as(b), s.sh.ID)
	wantNoErr(t, err)
	if su.Person != b {
		t.Errorf("signup = %+v", su)
	}
	other := r.shift(s.dept, s.pos.ID, model.EventID{}, when(30), 2, 5)
	if _, err := r.svc.SignUp(r.as(b), other.ID); err != nil {
		t.Errorf("a second shift: %v", err)
	}
}

// F164
func TestSignUp_NamesFailDuringNameCheck_RollsBack(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.cpl.namesErr = errs.E("test.people", errs.Unavailable, errors.New("people down"))
	_, err := r.svc.SignUp(r.as(p), s.sh.ID)
	wantKind(t, err, errs.Unavailable)
	_, err = r.svc.Assign(r.as(s.lead), s.sh.ID, vol.AssignInput{Person: p})
	wantKind(t, err, errs.Unavailable)
	assertNothingBooked(t, r, s.lead, s.sh.ID)
	r.cpl.namesErr = nil
	r.book(p, s.sh.ID) // and nothing is left holding a lock or a half-written row
}

// ---- F153, Q-VOL9 ----

func TestMySchedule_OtherCanceled_ShownWithReason(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	p := r.volunteer("Dandelion")
	byLead := r.shift(d, pos.ID, model.EventID{}, when(0), 2, 2)
	byShift := r.shift(d, pos.ID, model.EventID{}, when(10), 2, 2)
	bySelf := r.shift(d, pos.ID, model.EventID{}, when(20), 2, 2)
	kept := r.shift(d, pos.ID, model.EventID{}, when(30), 2, 2)
	for _, sh := range []vol.Shift{byLead, byShift, bySelf, kept} {
		r.book(p, sh.ID)
	}
	wantNoErr(t, r.svc.CancelSignup(r.as(lead), r.bookingOf(p, byLead.ID)))
	wantNoErr(t, r.svc.CancelShift(r.as(lead), byShift.ID))
	wantNoErr(t, r.svc.CancelSignup(r.as(p), r.bookingOf(p, bySelf.ID)))

	list, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{})
	wantNoErr(t, err)
	byShiftID := map[vol.ShiftID]vol.ScheduleEntry{}
	for _, e := range list.Items {
		byShiftID[e.Shift.ID] = e
	}
	if e, ok := byShiftID[byLead.ID]; !ok || e.Status != vol.SignupCanceled || e.CancelReason != vol.CancelLead {
		t.Errorf("lead-canceled booking = %+v, want listed, canceled, reason lead", e.Signup)
	}
	if e, ok := byShiftID[byShift.ID]; !ok || e.Status != vol.SignupCanceled || e.CancelReason != vol.CancelShiftCanceled {
		t.Errorf("booking of a canceled shift = %+v, want listed, canceled, reason shift_canceled", e.Signup)
	}
	if e, ok := byShiftID[kept.ID]; !ok || e.Status != vol.SignupBooked {
		t.Errorf("kept booking = %+v", e.Signup)
	}
	if _, ok := byShiftID[bySelf.ID]; ok {
		t.Error("the person's own cancellation is listed: they did it, they know")
	}
	if len(list.Items) != 3 {
		t.Errorf("%d entries, want 3", len(list.Items))
	}
	// Until the shift ends: once it has, the removal is no longer shown in upcoming mode,
	// and past mode never lists a canceled row.
	r.advance(byLead.Ends.Add(time.Minute).Sub(now))
	list, err = r.svc.MySchedule(r.as(p), vol.ScheduleQuery{})
	wantNoErr(t, err)
	for _, e := range list.Items {
		if e.Shift.ID == byLead.ID {
			t.Error("a removed booking of an ended shift is still in the upcoming list")
		}
	}
	past, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{Past: true})
	wantNoErr(t, err)
	if len(past.Items) != 0 {
		t.Errorf("past mode lists %d entries, want none (the only ended shift's booking was removed)", len(past.Items))
	}
}

func TestMySchedule_SelfCanceled_Hidden(t *testing.T) {
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	su := r.book(p, s.sh.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(p), su.ID))
	list, err := r.svc.MySchedule(r.as(p), vol.ScheduleQuery{})
	wantNoErr(t, err)
	if len(list.Items) != 0 {
		t.Errorf("a self-cancelled booking is listed: %+v", list.Items)
	}
}

// ---- F154, F155: export ----

// strOf is v as a string, or "" when v is absent or not a string.
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func signupsOf(t *testing.T, r *rig, p model.PersonID) map[string]map[string]any {
	t.Helper()
	exp, err := r.mod.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	var doc struct {
		Signups []map[string]any `json:"signups"`
	}
	wantNoErr(t, json.Unmarshal(exp.Data, &doc))
	out := map[string]map[string]any{}
	for _, s := range doc.Signups {
		out[strOf(s["label"])] = s
	}
	return out
}

func TestExportPerson_AfterLeadDeleted_KeepsRoles(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	other := r.lead(d) // stays; cancels as a lead
	p := r.volunteer("Dandelion")
	mk := func(label string, h int) vol.Shift {
		sh, err := r.svc.CreateShift(r.as(r.boss), d, vol.ShiftInput{Position: pos.ID, Label: label, Starts: when(h), Ends: when(h + 2), Zone: "UTC", Slots: 3})
		wantNoErr(t, err)
		return sh
	}
	assigned, selfBooked, selfCanceled, leadCanceled, shiftCanceled := mk("assigned", 0), mk("self", 10), mk("selfcancel", 20), mk("leadcancel", 30), mk("shiftcancel", 40)
	_, err := r.svc.Assign(r.as(lead), assigned.ID, vol.AssignInput{Person: p})
	wantNoErr(t, err)
	r.book(p, selfBooked.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(p), r.book(p, selfCanceled.ID).ID))
	su := r.book(p, leadCanceled.ID)
	wantNoErr(t, r.svc.CancelSignup(r.as(lead), su.ID)) // cancelled by the lead who will be deleted
	r.book(p, shiftCanceled.ID)
	wantNoErr(t, r.svc.CancelShift(r.as(other), shiftCanceled.ID))

	check := func(when string) {
		t.Helper()
		got := signupsOf(t, r, p)
		want := []struct{ label, bookedBy, canceledBy string }{
			{"assigned", "lead", ""}, {"self", "self", ""}, {"selfcancel", "self", "self"},
			{"leadcancel", "self", "lead"}, {"shiftcancel", "self", "system"},
		}
		for _, w := range want {
			s, ok := got[w.label]
			if !ok {
				t.Errorf("%s: signup %q missing from the export", when, w.label)
				continue
			}
			if s["booked_by"] != w.bookedBy {
				t.Errorf("%s: %s booked_by = %v, want the role word %q", when, w.label, s["booked_by"], w.bookedBy)
			}
			if cb := strOf(s["canceled_by"]); cb != w.canceledBy {
				t.Errorf("%s: %s canceled_by = %q, want %q", when, w.label, cb, w.canceledBy)
			}
		}
	}
	check("before the lead's deletion")
	_, err = r.mod.DeletePerson(ctxT(t), lead)
	wantNoErr(t, err)
	check("after the lead's deletion")
	exp, err := r.mod.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	if strings.Contains(string(exp.Data), lead.String()) || strings.Contains(string(exp.Data), other.String()) {
		t.Error("the export names another person")
	}
}

// F155
func TestExportPerson_Unknown_IsEmpty(t *testing.T) {
	r := newReal(t)
	for name, p := range map[string]model.PersonID{
		"a person the module never saw": r.person("Dandelion"),
		"an ID nobody has":              model.NewID[model.PersonID](),
	} {
		exp, err := r.mod.ExportPerson(ctxT(t), p)
		wantNoErr(t, err)
		var doc map[string]json.RawMessage
		if len(exp.Data) == 0 {
			t.Errorf("%s: no document, want {}", name)
			continue
		}
		wantNoErr(t, json.Unmarshal(exp.Data, &doc))
		if _, ok := doc["listed"]; ok {
			t.Errorf("%s: export carries a listed key: %s", name, exp.Data)
		}
		if len(doc) != 0 {
			t.Errorf("%s: export = %s, want {}", name, exp.Data)
		}
	}
}

// ---- F157: Sections chunks the directory call ----

func TestSections_ManyDepartments_ChunksNamesOf(t *testing.T) {
	r, cd := newRealWithDepts(t)
	p := r.person("Dandelion")
	n := vol.MaxIDs + 5
	names := make([]string, 0, n)
	for i := range n {
		name := fmt.Sprintf("Dept %03d %s", i, model.NewID[model.DepartmentID]().String()[:8])
		d := r.department(name)
		names = append(names, name)
		r.member(d, p)
	}
	cd.reset()
	got, err := r.svc.Sections(r.as(r.boss), []model.PersonID{p})
	wantNoErr(t, err)
	secs := got[p]
	if len(secs) != 1 || secs[0].Failed {
		t.Fatalf("sections = %+v", secs)
	}
	var all strings.Builder
	for _, it := range secs[0].Items {
		all.WriteString(it.Value)
		all.WriteString("\n")
	}
	for _, name := range names {
		if !strings.Contains(all.String(), name) {
			t.Errorf("department %q is not named in the section", name)
		}
	}
	if len(cd.sizes) < 2 {
		t.Errorf("NamesOf called %d times for %d departments, want it chunked", len(cd.sizes), n)
	}
	for _, sz := range cd.sizes {
		if sz > vol.MaxIDs {
			t.Errorf("a NamesOf call carried %d IDs, over MaxIDs (%d)", sz, vol.MaxIDs)
		}
	}
}

// ---- F129 variant: a booked person people no longer knows ----

func TestGetShift_PersonMissingFromNamesOf_CountsUnlisted(t *testing.T) {
	op := &omitPeople{omit: map[model.PersonID]bool{}}
	r := newReal(t, func(o *opts) {
		o.wrapPeople = func(d people.Directory) people.Directory {
			op.Directory = d
			return op
		}
	})
	s := r.scenario()
	gone, stays := r.volunteer("Marigold"), r.volunteer("Fennel")
	r.book(gone, s.sh.ID)
	r.book(stays, s.sh.ID)
	op.mu.Lock()
	op.omit[gone] = true
	op.mu.Unlock()
	got, err := r.svc.GetShift(r.as(r.volunteer("Dandelion")), s.sh.ID)
	wantNoErr(t, err)
	if got.Mates == nil || !slices.Equal(got.Mates.Names, []string{"Fennel"}) || got.Mates.Unlisted != 1 {
		t.Errorf("mates = %+v, want only Fennel named and one unlisted", got.Mates)
	}
}

// ---- the spec's Test plan: a certificate's file name never reaches a log or span ----

func TestOpenCertificate_NameNotLogged(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	const marker = "zzmarkername"
	rec, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{Qualification: q.ID, AcquiredOn: jan1, Upload: upload(marker+".pdf", pdf())})
	wantNoErr(t, err)
	lead := r.lead(d)
	_, content, err := r.svc.OpenCertificate(r.as(lead), rec.ID)
	wantNoErr(t, err)
	if content != nil {
		wantNoErr(t, content.Close())
	}
	_, err = r.svc.VerifyRecord(r.as(lead), rec.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	if r.cap.Contains(marker) {
		t.Error("the certificate's file name reached a log line or span")
	}
}
