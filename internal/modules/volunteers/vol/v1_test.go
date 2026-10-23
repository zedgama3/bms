package vol_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// Amendment V1 (D49): the leaf types. All people and IDs are invented.

func fieldCode(err error, field string) string {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field {
			return fe.Code
		}
	}
	return ""
}

// cursor builds a cursor the way the spec documents it: base64url of "<RFC3339Nano>|<id>".
func cursor(key, id string) vol.Cursor {
	return vol.Cursor(base64.RawURLEncoding.EncodeToString([]byte(key + "|" + id)))
}

// F149: the paging rules of Page, shared by the three long lists.
func TestPage_Validate_Table(t *testing.T) {
	id := model.NewID[vol.ShiftID]().String()
	at := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	tests := []struct {
		name  string
		page  vol.Page
		field string // "" = valid
		code  string
	}{
		{"zero is the first page of the default size", vol.Page{}, "", ""},
		{"limit 1", vol.Page{Limit: 1}, "", ""},
		{"limit MaxList", vol.Page{Limit: vol.MaxList}, "", ""},
		{"limit above MaxList", vol.Page{Limit: vol.MaxList + 1}, "limit", "range"},
		{"negative limit", vol.Page{Limit: -1}, "limit", "range"},
		{"a cursor of the documented shape", vol.Page{After: cursor(at, id)}, "", ""},
		{"not base64", vol.Page{After: "!!!not a cursor!!!"}, "after", "malformed"},
		{"no separator", vol.Page{After: vol.Cursor(base64.RawURLEncoding.EncodeToString([]byte("nothing here")))}, "after", "malformed"},
		{"bad time", vol.Page{After: cursor("yesterday", id)}, "after", "malformed"},
		{"bad id", vol.Page{After: cursor(at, "not-a-uuid")}, "after", "malformed"},
		{"empty id", vol.Page{After: cursor(at, "")}, "after", "malformed"},
		{"standard base64 padding is not what we gave out", vol.Page{After: vol.Cursor(base64.StdEncoding.EncodeToString([]byte(at + "|" + id + "x")))}, "after", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.page.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v (kind %v), want Invalid", err, errs.KindOf(err))
			}
			if got := fieldCode(err, tc.field); got != tc.code {
				t.Errorf("field %q code = %q, want %q (err %v)", tc.field, got, tc.code, err)
			}
		})
	}
}

func TestCursor_IsZero(t *testing.T) {
	if !vol.Cursor("").IsZero() {
		t.Error("the empty cursor is the first page / no next page")
	}
	if vol.Cursor("x").IsZero() {
		t.Error("a non-empty cursor is not zero")
	}
}

// The three queries embed the same Page: a bad cursor or limit is refused by each.
func TestQueries_EmbedPage_RefuseBadCursorAndLimit(t *testing.T) {
	dept := model.NewID[model.DepartmentID]()
	bad := vol.Page{After: "!!!"}
	tooBig := vol.Page{Limit: vol.MaxList + 1}
	for name, err := range map[string]error{
		"ShiftQuery cursor":    vol.ShiftQuery{Page: bad}.Validate(),
		"ShiftQuery limit":     vol.ShiftQuery{Page: tooBig}.Validate(),
		"ScheduleQuery cursor": vol.ScheduleQuery{Page: bad}.Validate(),
		"ScheduleQuery limit":  vol.ScheduleQuery{Page: tooBig}.Validate(),
		"MemberQuery cursor":   vol.MemberQuery{Department: dept, Page: bad}.Validate(),
		"MemberQuery limit":    vol.MemberQuery{Department: dept, Page: tooBig}.Validate(),
	} {
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("%s: Validate = %v, want Invalid", name, err)
		}
	}
	if err := (vol.MemberQuery{}).Validate(); !errs.Is(err, errs.Invalid) {
		t.Errorf("MemberQuery without a department: %v, want Invalid", err)
	}
}

// V1.8 (l): a window may have either bound alone; Start >= End is Invalid range.
func TestQueries_Within_Table(t *testing.T) {
	t0 := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		w    model.TimeRange
		ok   bool
	}{
		{"zero", model.TimeRange{}, true},
		{"start only", model.TimeRange{Start: t0}, true},
		{"end only", model.TimeRange{End: t0}, true},
		{"both, ordered", model.TimeRange{Start: t0, End: t0.Add(time.Hour)}, true},
		{"start equals end", model.TimeRange{Start: t0, End: t0}, false},
		{"start after end", model.TimeRange{Start: t0.Add(time.Hour), End: t0}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for name, err := range map[string]error{
				"ShiftQuery":    vol.ShiftQuery{Within: tc.w}.Validate(),
				"ScheduleQuery": vol.ScheduleQuery{Within: tc.w}.Validate(),
			} {
				if tc.ok && err != nil {
					t.Errorf("%s: Validate = %v, want nil", name, err)
				}
				if !tc.ok {
					if !errs.Is(err, errs.Invalid) || fieldCode(err, "within") != "range" {
						t.Errorf("%s: Validate = %v, want Invalid range on within", name, err)
					}
				}
			}
		})
	}
}

// F148: NoExpiry together with an ExpiresOn is one_of on no_expiry, on both inputs.
func TestNoExpiry_WithDate_ReturnsInvalidOneOf(t *testing.T) {
	q, p := model.NewID[vol.QualificationID](), model.NewID[model.PersonID]()
	exp := vol.Date{Year: 2027, Month: 1, Day: 1}
	rec := vol.RecordInput{Qualification: q, Person: p, AcquiredOn: vol.Date{Year: 2026, Month: 1, Day: 1}, ExpiresOn: exp, NoExpiry: true}
	ver := vol.VerifyInput{ExpiresOn: exp, NoExpiry: true}
	for name, err := range map[string]error{"RecordInput": rec.Validate(), "VerifyInput": ver.Validate()} {
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("%s: Validate = %v, want Invalid", name, err)
			continue
		}
		if got := fieldCode(err, "no_expiry"); got != "one_of" {
			t.Errorf("%s: no_expiry code = %q, want one_of (err %v)", name, got, err)
		}
	}
	// Alone, NoExpiry is fine; so is a date alone.
	rec.ExpiresOn, ver.ExpiresOn = vol.Date{}, vol.Date{}
	if err := rec.Validate(); err != nil {
		t.Errorf("RecordInput NoExpiry alone: %v", err)
	}
	if err := ver.Validate(); err != nil {
		t.Errorf("VerifyInput NoExpiry alone: %v", err)
	}
	rec.NoExpiry, ver.NoExpiry, rec.ExpiresOn, ver.ExpiresOn = false, false, exp, exp
	if err := rec.Validate(); err != nil {
		t.Errorf("RecordInput date alone: %v", err)
	}
	if err := ver.Validate(); err != nil {
		t.Errorf("VerifyInput date alone: %v", err)
	}
	// The JSON name the form sends.
	var in vol.VerifyInput
	if err := json.Unmarshal([]byte(`{"no_expiry":true}`), &in); err != nil || !in.NoExpiry {
		t.Errorf(`{"no_expiry":true} decoded to %+v, %v`, in, err)
	}
}

// V1.8 (a): a lead's deletion nulls VerifiedBy; the record keeps counting while VerifiedAt stays.
func TestRecord_Verified_IsVerifiedAtNotVerifiedBy(t *testing.T) {
	r := vol.Record{VerifiedAt: time.Unix(1, 0)} // VerifiedBy zero: the verifier was deleted
	if !r.Verified() {
		t.Error("a record with VerifiedAt set and no VerifiedBy must stay verified")
	}
	if !r.ValidOn(vol.Date{Year: 2030, Month: 1, Day: 1}) {
		t.Error("an indefinite verified record whose verifier was deleted must stay valid")
	}
	pending := vol.Record{VerifiedBy: model.NewID[model.PersonID]()} // VerifiedBy without VerifiedAt is not verified
	if pending.Verified() {
		t.Error("Verified must follow VerifiedAt, not VerifiedBy")
	}
}

// F156: an empty list encodes as {"items":[]} for every wrapper; a hidden/absent next is omitted.
func TestListWrappers_EmptyEncodeEmptyItems(t *testing.T) {
	for name, v := range map[string]any{
		"ShiftList":      vol.ShiftList{Items: []vol.ShiftSummary{}},
		"ScheduleList":   vol.ScheduleList{Items: []vol.ScheduleEntry{}},
		"MemberViewList": vol.MemberViewList{Items: []vol.MemberView{}},
		"RecordList":     vol.RecordList{Items: []vol.Record{}},
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(b) != `{"items":[]}` {
			t.Errorf("%s encodes as %s, want {\"items\":[]}", name, b)
		}
	}
}

func TestListWrappers_NextAndSelfUploadWire(t *testing.T) {
	c := cursor(time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), model.NewID[vol.ShiftID]().String())
	b, err := json.Marshal(vol.ShiftList{Items: []vol.ShiftSummary{}, Next: c})
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	if string(top["next"]) != `"`+string(c)+`"` {
		t.Errorf("next = %s, want the cursor as a plain string", top["next"])
	}
	// self_upload false is a value (the upload is off), not an absent key; nil is absent.
	off := false
	b, err = json.Marshal(vol.RecordList{Items: []vol.Record{}, SelfUpload: &off})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"self_upload":false`) {
		t.Errorf("RecordList with SelfUpload=false encodes as %s", b)
	}
	b, err = json.Marshal(vol.RecordList{Items: []vol.Record{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "self_upload") {
		t.Errorf("RecordList for another person carries self_upload: %s", b)
	}
}

// V1.4 (b): the summary carries the two names under their wire keys, and Mates is absent when nil.
func TestShiftSummary_NamesAndAbsentMates(t *testing.T) {
	b, err := json.Marshal(vol.ShiftSummary{DepartmentName: "Rangers", PositionName: "Dirt crew"})
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	if top["department_name"] != "Rangers" || top["position_name"] != "Dirt crew" {
		t.Errorf("summary = %s, want department_name and position_name", b)
	}
	if _, ok := top["mates"]; ok {
		t.Errorf("a nil Mates is on the wire: %s", b)
	}
}

// F154: Signup carries who booked as a role word that survives the lead's deletion.
func TestSignup_BookedAs_IsARoleWord(t *testing.T) {
	su := vol.Signup{BookedAs: vol.BookedLead}
	if su.BookedAs != "lead" || vol.BookedSelf != "self" {
		t.Errorf("BookedAs words = %q/%q, want lead/self", su.BookedAs, vol.BookedSelf)
	}
	if !su.BookedBy.IsZero() {
		t.Error("BookedBy is independent of BookedAs and zero after the lead's deletion")
	}
}

// Decision 28's wire: the shift keeps Event on the input.
func TestShiftInput_Event_OnTheWire(t *testing.T) {
	ev := model.NewID[model.EventID]()
	b, err := json.Marshal(vol.ShiftInput{Event: ev})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"event":"`+ev.String()+`"`) {
		t.Errorf("ShiftInput = %s", b)
	}
}
