//go:build integration

package volunteers_test

// Amendment V4 (D57, D58): ShiftQuery.EligibleOnly, F178 to F181. Every person is invented.

import (
	"net/http"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

func idsOfShifts(items []vol.ShiftSummary) []vol.ShiftID {
	out := make([]vol.ShiftID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func sameShiftIDs(t *testing.T, what string, got []vol.ShiftSummary, want []vol.ShiftID) {
	t.Helper()
	g := idsOfShifts(got)
	if len(g) != len(want) {
		t.Errorf("%s: %d items, want %d", what, len(g), len(want))
		return
	}
	for i, id := range want {
		if g[i] != id {
			t.Errorf("%s: item %d differs", what, i)
			return
		}
	}
}

// mixedWorld has a department with 14 one-hour shifts two hours apart: even ones open with
// five slots (eligible), odd ones with one slot taken by another volunteer (full). The viewer
// holds a booking on the shift at index 5, which is full and so not eligible, yet is hers.
type mixedWorld struct {
	r      *rig
	dept   model.DepartmentID
	viewer model.PersonID
	shifts []vol.Shift
	mine   vol.ShiftID
}

func newMixedWorld(t *testing.T) mixedWorld {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	viewer := r.volunteer("Dandelion")
	other := r.volunteer("Thistle")
	w := mixedWorld{r: r, dept: d, viewer: viewer}
	for i := range 14 {
		slots := 5
		if i%2 == 1 {
			slots = 1
		}
		sh := r.shift(d, pos.ID, model.EventID{}, when(i*2), 1, slots)
		w.shifts = append(w.shifts, sh)
		switch i {
		case 5:
			r.book(viewer, sh.ID)
			w.mine = sh.ID
		default:
			if i%2 == 1 {
				r.book(other, sh.ID)
			}
		}
	}
	return w
}

func (w mixedWorld) list(t *testing.T, q vol.ShiftQuery) vol.ShiftList {
	t.Helper()
	q.Department = w.dept
	l, err := w.r.svc.ListShifts(w.r.as(w.viewer), q)
	wantNoErr(t, err)
	return l
}

// F178
func TestListShifts_EligibleOnly_KeepsEligibleAndMine(t *testing.T) {
	w := newMixedWorld(t)
	all := w.list(t, vol.ShiftQuery{Page: vol.Page{Limit: vol.MaxList}})
	var want []vol.ShiftID
	var mineNotEligible, dropped int
	for _, it := range all.Items {
		keep := it.Eligibility.Eligible || !it.Mine.IsZero()
		if keep {
			want = append(want, it.ID)
		} else {
			dropped++
		}
		if !it.Mine.IsZero() && !it.Eligibility.Eligible {
			mineNotEligible++
		}
	}
	if dropped == 0 || len(want) == 0 || mineNotEligible == 0 {
		t.Fatalf("fixture: dropped %d, kept %d, mine-but-not-eligible %d; each must be above zero", dropped, len(want), mineNotEligible)
	}
	got := w.list(t, vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{Limit: vol.MaxList}})
	sameShiftIDs(t, "EligibleOnly", got.Items, want)
	for _, it := range got.Items {
		if !it.Eligibility.Eligible && it.Mine.IsZero() {
			t.Errorf("shift %v is neither eligible nor mine", it.ID)
		}
	}
	found := false
	for _, it := range got.Items {
		if it.ID == w.mine {
			found = true
		}
	}
	if !found {
		t.Error("the actor's own booking, whose eligibility is false, was dropped")
	}
	if !got.Next.IsZero() {
		t.Errorf("Next = %q for a list that fits one page", got.Next)
	}
}

// F178: without the flag nothing is dropped (it only removes rows, never changes others).
func TestListShifts_EligibleOnlyFalse_ChangesNothing(t *testing.T) {
	w := newMixedWorld(t)
	a := w.list(t, vol.ShiftQuery{Page: vol.Page{Limit: vol.MaxList}})
	b := w.list(t, vol.ShiftQuery{EligibleOnly: false, Page: vol.Page{Limit: vol.MaxList}})
	sameShiftIDs(t, "unfiltered", b.Items, idsOfShifts(a.Items))
	if len(a.Items) != 14 {
		t.Errorf("unfiltered list holds %d shifts, want all 14", len(a.Items))
	}
}

// F178: a canceled shift is dropped unless Mine.
func TestListShifts_EligibleOnly_DropsCanceledShifts(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	lead := r.lead(d)
	r.az.Grant(lead, pSignUp)
	open := r.shift(d, pos.ID, model.EventID{}, when(0), 1, 5)
	gone := r.shift(d, pos.ID, model.EventID{}, when(3), 1, 5)
	wantNoErr(t, r.svc.CancelShift(r.as(lead), gone.ID))

	with, err := r.svc.ListShifts(r.as(lead), vol.ShiftQuery{Department: d, IncludeCanceled: true})
	wantNoErr(t, err)
	var seen bool
	for _, it := range with.Items {
		if it.ID == gone.ID {
			seen = true
		}
	}
	if !seen {
		t.Fatal("fixture: the canceled shift is not listed with IncludeCanceled")
	}
	got, err := r.svc.ListShifts(r.as(lead), vol.ShiftQuery{Department: d, IncludeCanceled: true, EligibleOnly: true})
	wantNoErr(t, err)
	for _, it := range got.Items {
		if it.ID == gone.ID {
			t.Error("a canceled shift the actor holds nothing on survived EligibleOnly")
		}
	}
	var keptOpen bool
	for _, it := range got.Items {
		if it.ID == open.ID {
			keptOpen = true
		}
	}
	if !keptOpen {
		t.Error("the open shift was dropped")
	}
}

// F179
func TestListShifts_EligibleOnly_PagesNeverSkip(t *testing.T) {
	w := newMixedWorld(t)
	full := w.list(t, vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{Limit: vol.MaxList}})
	want := idsOfShifts(full.Items)
	if len(want) < 8 {
		t.Fatalf("fixture: %d passing shifts", len(want))
	}
	for _, limit := range []int{1, 2, 3, 5, 7} {
		var got []vol.ShiftSummary
		q := vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{Limit: limit}}
		var pages int
		for range 40 {
			l := w.list(t, q)
			pages++
			if len(l.Items) > limit {
				t.Fatalf("limit %d: a page holds %d rows", limit, len(l.Items))
			}
			if pages == 1 && len(l.Items) != limit {
				t.Errorf("limit %d: the first page holds %d rows, want %d", limit, len(l.Items), limit)
			}
			if !l.Next.IsZero() && len(l.Items) == limit {
				last := l.Items[len(l.Items)-1]
				if l.Next != vol.NewCursor(last.Starts, last.ID) {
					t.Errorf("limit %d: Next is not the last returned row's cursor", limit)
				}
			}
			got = append(got, l.Items...)
			if l.Next.IsZero() {
				break
			}
			q.After = l.Next
		}
		if len(got) != len(want) {
			t.Errorf("limit %d: %d rows over %d pages, want %d", limit, len(got), pages, len(want))
			continue
		}
		seen := map[vol.ShiftID]bool{}
		for i, it := range got {
			if seen[it.ID] {
				t.Errorf("limit %d: row %d repeated", limit, i)
			}
			seen[it.ID] = true
			if it.ID != want[i] {
				t.Errorf("limit %d: row %d is out of order or skipped", limit, i)
			}
		}
	}
}

// F180: the end of the data. A page that returns the last passing row may still carry Next
// (rows that did not pass lie beyond), but following it ends with no Next and no row lost;
// a list with nothing beyond has no Next.
func TestListShifts_EligibleOnly_LastPageHasNoNext(t *testing.T) {
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	p := r.volunteer("Dandelion")
	var ids []vol.ShiftID
	for _, h := range []int{0, 3, 6} {
		ids = append(ids, r.shift(d, pos.ID, model.EventID{}, when(h), 1, 5).ID)
	}
	q := vol.ShiftQuery{Department: d, EligibleOnly: true, Page: vol.Page{Limit: 3}}
	l, err := r.svc.ListShifts(r.as(p), q)
	wantNoErr(t, err)
	sameShiftIDs(t, "exactly Limit rows, nothing beyond", l.Items, ids)
	if !l.Next.IsZero() {
		t.Errorf("Next = %q, want none: nothing lies beyond the last row", l.Next)
	}
	q.Limit = 10
	l, err = r.svc.ListShifts(r.as(p), q)
	wantNoErr(t, err)
	if len(l.Items) != 3 || !l.Next.IsZero() {
		t.Errorf("short list: %d rows, next %q; want 3 and none", len(l.Items), l.Next)
	}
	// An empty department: an empty list, no Next, no error.
	d2 := r.department("Greeters")
	l, err = r.svc.ListShifts(r.as(p), vol.ShiftQuery{Department: d2, EligibleOnly: true})
	wantNoErr(t, err)
	if len(l.Items) != 0 || !l.Next.IsZero() {
		t.Errorf("empty department: %d rows, next %q", len(l.Items), l.Next)
	}
}

// F180 with a tail: kept rows followed by dropped ones. The last page ends the chain.
func TestListShifts_EligibleOnly_TrailingDroppedRows_ChainEnds(t *testing.T) {
	w := newMixedWorld(t) // index 13 is full: the last fetched row is dropped
	q := vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{Limit: 100}}
	l := w.list(t, q)
	if !l.Next.IsZero() {
		t.Errorf("Next = %q with the whole day on one page", l.Next)
	}
	// Ask for exactly as many as pass: the page is full; following Next (if any) must be empty.
	n := len(l.Items)
	q.Limit = n
	l = w.list(t, q)
	if len(l.Items) != n {
		t.Fatalf("page holds %d, want %d", len(l.Items), n)
	}
	if !l.Next.IsZero() {
		q.After = l.Next
		more := w.list(t, q)
		if len(more.Items) != 0 || !more.Next.IsZero() {
			t.Errorf("following Next gave %d rows and next %q, want an empty, final page", len(more.Items), more.Next)
		}
	}
}

// scanWorld: the viewer holds a 24-hour shift (hers); 501 one-hour shifts with one slot lie
// inside it, so each overlaps her booking and none is eligible for her.
type scanWorld struct {
	r      *rig
	dept   model.DepartmentID
	viewer model.PersonID
	long   vol.Shift
}

func newScanWorld(t *testing.T) scanWorld {
	t.Helper()
	r := newReal(t)
	d := r.department("Rangers")
	pos := r.position(d, "Dirt crew", vol.AccessOpen)
	p := r.volunteer("Dandelion")
	long := r.shift(d, pos.ID, model.EventID{}, when(0), 24, 5)
	r.book(p, long.ID)
	for range vol.MaxList + 1 {
		r.shift(d, pos.ID, model.EventID{}, when(1), 1, 1)
	}
	return scanWorld{r: r, dept: d, viewer: p, long: long}
}

func (w scanWorld) list(t *testing.T, q vol.ShiftQuery) vol.ShiftList {
	t.Helper()
	q.Department = w.dept
	l, err := w.r.svc.ListShifts(w.r.as(w.viewer), q)
	wantNoErr(t, err)
	return l
}

// F180: fewer than Limit passing rows among MaxList scanned and a row beyond: the page is
// short and Next is the MaxList-th scanned row's cursor.
func TestListShifts_EligibleOnly_ScanBoundSetsNext(t *testing.T) {
	w := newScanWorld(t)
	raw := w.list(t, vol.ShiftQuery{Page: vol.Page{Limit: vol.MaxList}})
	if len(raw.Items) != vol.MaxList || raw.Next.IsZero() {
		t.Fatalf("fixture: unfiltered page = %d rows, next %q", len(raw.Items), raw.Next)
	}
	if raw.Items[0].ID != w.long.ID {
		t.Fatal("fixture: the long shift is not first")
	}
	for _, it := range raw.Items[1:] {
		if it.Eligibility.Eligible {
			t.Fatalf("fixture: shift %v is eligible; every inner shift must overlap the booking", it.ID)
		}
	}
	scanned := raw.Items[vol.MaxList-1]

	l := w.list(t, vol.ShiftQuery{EligibleOnly: true})
	sameShiftIDs(t, "first page", l.Items, []vol.ShiftID{w.long.ID})
	if l.Next != vol.NewCursor(scanned.Starts, scanned.ID) {
		t.Errorf("Next = %q, want the cursor of the %dth scanned row", l.Next, vol.MaxList)
	}
	// Following it loses nothing and ends: the two rows beyond are dropped.
	next := w.list(t, vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{After: l.Next}})
	if len(next.Items) != 0 || !next.Next.IsZero() {
		t.Errorf("second page = %d rows, next %q; want empty and final", len(next.Items), next.Next)
	}
}

// F180/F184: a page can be empty with Next set.
func TestListShifts_EligibleOnly_EmptyPageWithNext(t *testing.T) {
	w := newScanWorld(t)
	after := vol.NewCursor(w.long.Starts, w.long.ID)
	raw := w.list(t, vol.ShiftQuery{Page: vol.Page{Limit: vol.MaxList, After: after}})
	if len(raw.Items) != vol.MaxList || raw.Next.IsZero() {
		t.Fatalf("fixture: unfiltered page = %d rows, next %q", len(raw.Items), raw.Next)
	}
	scanned := raw.Items[vol.MaxList-1]
	l := w.list(t, vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{After: after}})
	if len(l.Items) != 0 {
		t.Fatalf("page holds %d rows, want an empty one", len(l.Items))
	}
	if l.Next != vol.NewCursor(scanned.Starts, scanned.ID) {
		t.Errorf("an empty page's Next = %q, want the cursor of the last scanned row", l.Next)
	}
	if l.Items == nil {
		t.Error("Items is nil; an empty page must serialize as []")
	}
}

// Boundaries the spec implies: EligibleOnly keeps the existing validation.
func TestListShifts_EligibleOnly_BadInputs_ReturnInvalid(t *testing.T) {
	r := newReal(t)
	p := r.volunteer("Dandelion")
	_, err := r.svc.ListShifts(r.as(p), vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{Limit: vol.MaxList + 1}})
	wantKind(t, err, errs.Invalid)
	_, err = r.svc.ListShifts(r.as(p), vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{After: "not-a-cursor"}})
	wantKind(t, err, errs.Invalid)
	_, err = r.svc.ListShifts(r.as(p), vol.ShiftQuery{EligibleOnly: true, Page: vol.Page{Limit: -1}})
	wantKind(t, err, errs.Invalid)
	_, err = r.svc.ListShifts(r.as(p), vol.ShiftQuery{EligibleOnly: true, Within: model.TimeRange{Start: when(5), End: when(1)}})
	wantKind(t, err, errs.Invalid)
}

// F181 over the route with a real service: eligible=true filters.
func TestShiftsRoute_Eligible_True_Filters(t *testing.T) {
	w := newMixedWorld(t)
	all := w.r.call(w.viewer, "GET", "/shifts?department="+w.dept.String()+"&limit=500", nil)
	wantStatus(t, all, http.StatusOK)
	only := w.r.call(w.viewer, "GET", "/shifts?department="+w.dept.String()+"&limit=500&eligible=true", nil)
	wantStatus(t, only, http.StatusOK)
	a, o := decodeBody[vol.ShiftList](t, all), decodeBody[vol.ShiftList](t, only)
	if len(o.Items) == 0 || len(o.Items) >= len(a.Items) {
		t.Errorf("eligible=true returned %d of %d shifts", len(o.Items), len(a.Items))
	}
}
