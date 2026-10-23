package volunteers_test

import (
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// Amendment V1 (D49), tests that need no database: input validation runs before the permission
// check and before any read (Behavior: actor, input Validate, permission, then state).

func badCursors() map[string]vol.Cursor {
	at := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	enc := func(s string) vol.Cursor { return vol.Cursor(base64.RawURLEncoding.EncodeToString([]byte(s))) }
	id := model.NewID[vol.ShiftID]().String()
	return map[string]vol.Cursor{
		"not base64":   "!!!",
		"no separator": enc("nothing"),
		"bad time":     enc("yesterday|" + id),
		"bad id":       enc(at + "|not-an-id"),
		"empty id":     enc(at + "|"),
	}
}

// F149
func TestListShifts_BadCursor_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	ctx := r.as(r.volunteer("Dandelion"))
	for name, c := range badCursors() {
		t.Run(name, func(t *testing.T) {
			_, err := r.svc.ListShifts(ctx, vol.ShiftQuery{Page: vol.Page{After: c}})
			wantKind(t, err, errsInvalid)
			if got := fieldCodeOf(err, "after"); got != "malformed" {
				t.Errorf("after code = %q, want malformed (err %v)", got, err)
			}
		})
	}
}

// F149
func TestMySchedule_BadCursor_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	ctx := r.as(r.volunteer("Dandelion"))
	for name, c := range badCursors() {
		t.Run(name, func(t *testing.T) {
			for _, past := range []bool{false, true} {
				_, err := r.svc.MySchedule(ctx, vol.ScheduleQuery{Past: past, Page: vol.Page{After: c}})
				wantKind(t, err, errsInvalid)
				if got := fieldCodeOf(err, "after"); got != "malformed" {
					t.Errorf("past=%v: after code = %q, want malformed (err %v)", past, got, err)
				}
			}
		})
	}
}

// F149: the lead does not even need the permission for the input to be refused first.
func TestListMembers_BadCursor_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	d := r.department("Rangers")
	ctx := r.as(r.lead(d))
	for name, c := range badCursors() {
		t.Run(name, func(t *testing.T) {
			_, err := r.svc.ListMembers(ctx, vol.MemberQuery{Department: d, Page: vol.Page{After: c}})
			wantKind(t, err, errsInvalid)
			if got := fieldCodeOf(err, "after"); got != "malformed" {
				t.Errorf("after code = %q, want malformed (err %v)", got, err)
			}
		})
	}
}

// F149: limit out of range is range on limit, on all three lists.
func TestLists_LimitOutOfRange_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	d := r.department("Rangers")
	ctx := r.as(r.lead(d))
	for _, limit := range []int{-1, vol.MaxList + 1} {
		_, err := r.svc.ListShifts(ctx, vol.ShiftQuery{Page: vol.Page{Limit: limit}})
		wantKind(t, err, errsInvalid)
		_, err = r.svc.MySchedule(ctx, vol.ScheduleQuery{Page: vol.Page{Limit: limit}})
		wantKind(t, err, errsInvalid)
		_, err = r.svc.ListMembers(ctx, vol.MemberQuery{Department: d, Page: vol.Page{Limit: limit}})
		wantKind(t, err, errsInvalid)
		if got := fieldCodeOf(errOf(r.svc.ListShifts(ctx, vol.ShiftQuery{Page: vol.Page{Limit: limit}})), "limit"); got != "range" {
			t.Errorf("limit %d: code = %q, want range", limit, got)
		}
	}
}

// F149: a window with Start >= End is Invalid range, on both lists that take one.
func TestLists_BadWindow_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	ctx := r.as(r.volunteer("Dandelion"))
	bad := model.TimeRange{Start: when(5), End: when(5)}
	_, err := r.svc.ListShifts(ctx, vol.ShiftQuery{Within: bad})
	wantKind(t, err, errsInvalid)
	_, err = r.svc.MySchedule(ctx, vol.ScheduleQuery{Within: bad})
	wantKind(t, err, errsInvalid)
}

// F149 over HTTP: the handler hands `after` on without decoding it and the service answers 400
// malformed on "after"; limit out of range is 400 range on "limit".
func TestHandlers_BadPaging_Return400(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	d := model.NewID[model.DepartmentID]().String()
	for _, tc := range []struct{ path, field, code string }{
		{"/shifts?after=!!!", "after", "malformed"},
		{"/me/schedule?after=!!!", "after", "malformed"},
		{"/me/schedule?past=true&after=!!!", "after", "malformed"},
		{"/departments/" + d + "/members?after=!!!", "after", "malformed"},
		{"/shifts?limit=501", "limit", "range"},
		{"/shifts?limit=-1", "limit", "range"},
		{"/me/schedule?limit=501", "limit", "range"},
		{"/departments/" + d + "/members?limit=501", "limit", "range"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			wantField(t, wantProblem(t, r.call(p, "GET", tc.path, nil), http.StatusBadRequest, "invalid"), tc.field, tc.code)
		})
	}
	// An unparsable limit never reaches the service.
	wantField(t, wantProblem(t, r.call(p, "GET", "/departments/"+d+"/members?limit=lots", nil), http.StatusBadRequest, "invalid"), "limit", "malformed")
}
