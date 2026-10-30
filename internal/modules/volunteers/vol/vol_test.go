package vol_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

func d(y int, m time.Month, day int) vol.Date { return vol.Date{Year: y, Month: m, Day: day} }

func TestDate_Validate_Table(t *testing.T) {
	tests := []struct {
		name string
		in   vol.Date
		ok   bool
	}{
		{"zero is none", vol.Date{}, true},
		{"ordinary", d(2026, time.October, 21), true},
		{"leap day in a leap year", d(2028, time.February, 29), true},
		{"leap day in a common year", d(2027, time.February, 29), false},
		{"month 13", vol.Date{Year: 2026, Month: 13, Day: 1}, false},
		{"day 0", vol.Date{Year: 2026, Month: 5, Day: 0}, false},
		{"day 32", vol.Date{Year: 2026, Month: 5, Day: 32}, false},
		{"april 31", d(2026, time.April, 31), false},
		{"year only", vol.Date{Year: 2026}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tc.ok {
				if !errs.Is(err, errs.Invalid) {
					t.Fatalf("Validate = %v, want Invalid (malformed)", err)
				}
			}
		})
	}
}

func TestDate_ZeroBeforeAddDays(t *testing.T) {
	if !(vol.Date{}).IsZero() {
		t.Error("the zero Date must report IsZero")
	}
	if d(2026, 1, 1).IsZero() {
		t.Error("a real date is not zero")
	}
	if !d(2026, 12, 31).Before(d(2027, 1, 1)) || d(2027, 1, 1).Before(d(2026, 12, 31)) || d(2026, 1, 1).Before(d(2026, 1, 1)) {
		t.Error("Before is wrong across a year boundary or on equality")
	}
	if got := d(2026, 12, 30).AddDays(3); got != d(2027, 1, 2) {
		t.Errorf("AddDays(3) = %+v, want 2027-01-02", got)
	}
	if got := d(2026, 3, 1).AddDays(-1); got != d(2026, 2, 28) {
		t.Errorf("AddDays(-1) = %+v, want 2026-02-28", got)
	}
	if got := d(2028, 2, 28).AddDays(1); got != d(2028, 2, 29) {
		t.Errorf("AddDays(1) in a leap year = %+v, want 2028-02-29", got)
	}
}

func TestDate_JSON_RoundTrip(t *testing.T) {
	b, err := json.Marshal(d(2026, time.March, 5))
	if err != nil || string(b) != `"2026-03-05"` {
		t.Fatalf("Marshal = %s, %v; want \"2026-03-05\"", b, err)
	}
	var got vol.Date
	if err := json.Unmarshal([]byte(`"2026-03-05"`), &got); err != nil || got != d(2026, 3, 5) {
		t.Fatalf("Unmarshal = %+v, %v", got, err)
	}
	for _, bad := range []string{`"2026-02-30"`, `"2026-3-5"`, `"yesterday"`, `20260305`} {
		var x vol.Date
		if err := json.Unmarshal([]byte(bad), &x); err == nil {
			t.Errorf("Unmarshal(%s) accepted a malformed date as %+v", bad, x)
		}
	}
}

func TestDateOf_UsesTheZone(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	// 03:00 UTC on the 8th is 21:00 on the 7th in Denver (UTC-6 in November).
	at := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	if got := vol.DateOf(at, denver); got != d(2026, 11, 7) {
		t.Errorf("DateOf in Denver = %+v, want 2026-11-07", got)
	}
	if got := vol.DateOf(at, time.UTC); got != d(2026, 11, 8) {
		t.Errorf("DateOf in UTC = %+v, want 2026-11-08", got)
	}
}

func TestRecord_VerifiedAndValidOn_Table(t *testing.T) {
	who := model.NewID[model.PersonID]()
	tests := []struct {
		name     string
		rec      vol.Record
		on       vol.Date
		verified bool
		valid    bool
	}{
		{"pending is never valid", vol.Record{}, d(2026, 6, 1), false, false},
		{"verified indefinite", vol.Record{VerifiedBy: who, VerifiedAt: time.Unix(1, 0)}, d(2030, 1, 1), true, true},
		{"verified, expires later", vol.Record{VerifiedBy: who, VerifiedAt: time.Unix(1, 0), ExpiresOn: d(2026, 12, 31)}, d(2026, 6, 1), true, true},
		{"verified, expires that very day", vol.Record{VerifiedBy: who, VerifiedAt: time.Unix(1, 0), ExpiresOn: d(2026, 6, 1)}, d(2026, 6, 1), true, true},
		{"verified, expired the day before", vol.Record{VerifiedBy: who, VerifiedAt: time.Unix(1, 0), ExpiresOn: d(2026, 5, 31)}, d(2026, 6, 1), true, false},
		{"pending with a future expiry is still not valid", vol.Record{ExpiresOn: d(2030, 1, 1)}, d(2026, 6, 1), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rec.Verified(); got != tc.verified {
				t.Errorf("Verified = %v, want %v", got, tc.verified)
			}
			if got := tc.rec.ValidOn(tc.on); got != tc.valid {
				t.Errorf("ValidOn = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestShift_CanceledAndTimes(t *testing.T) {
	s := vol.Shift{Starts: time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC), Ends: time.Date(2026, 11, 7, 13, 0, 0, 0, time.UTC)}
	if s.Canceled() {
		t.Error("a shift without CanceledAt is not canceled")
	}
	s.CanceledAt = time.Unix(1, 0)
	if !s.Canceled() {
		t.Error("a shift with CanceledAt is canceled")
	}
	tr := s.Times()
	if !tr.Start.Equal(s.Starts) || !tr.End.Equal(s.Ends) {
		t.Errorf("Times = %+v, want the shift's own start and end", tr)
	}
	adjacent := vol.Shift{Starts: s.Ends, Ends: s.Ends.Add(time.Hour)}
	if s.Times().Overlaps(adjacent.Times()) {
		t.Error("back-to-back shifts must not overlap (half-open ranges)")
	}
}

func TestConstants_AreTheSpecs(t *testing.T) {
	got := map[string]int{
		"MaxNameBytes": vol.MaxNameBytes, "MaxDescriptionBytes": vol.MaxDescriptionBytes, "MaxLabelBytes": vol.MaxLabelBytes,
		"MaxLocationBytes": vol.MaxLocationBytes, "MaxSlots": vol.MaxSlots, "MaxValidityDays": vol.MaxValidityDays,
		"MaxRequirements": vol.MaxRequirements, "MaxList": vol.MaxList, "MaxIDs": vol.MaxIDs,
	}
	want := map[string]int{
		"MaxNameBytes": 100, "MaxDescriptionBytes": 2000, "MaxLabelBytes": 100, "MaxLocationBytes": 200, "MaxSlots": 200,
		"MaxValidityDays": 3650, "MaxRequirements": 20, "MaxList": 500, "MaxIDs": 200,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %d, want %d", k, got[k], w)
		}
	}
	if vol.MaxShiftDuration != 48*time.Hour {
		t.Errorf("MaxShiftDuration = %v, want 48h", vol.MaxShiftDuration)
	}
}

func TestQualificationInput_Validate_Table(t *testing.T) {
	tests := []struct {
		name string
		in   vol.QualificationInput
		code string // "" = valid
	}{
		{"valid", vol.QualificationInput{Name: "Ranger training"}, ""},
		{"valid with validity", vol.QualificationInput{Name: "First aid", ValidityDays: 730}, ""},
		{"empty name", vol.QualificationInput{}, "required"},
		{"blank name", vol.QualificationInput{Name: "   "}, "required"},
		{"name too long", vol.QualificationInput{Name: strings.Repeat("x", vol.MaxNameBytes+1)}, "too_long"},
		{"name at the limit", vol.QualificationInput{Name: strings.Repeat("x", vol.MaxNameBytes)}, ""},
		{"control character", vol.QualificationInput{Name: "bad\x07name"}, "control_chars"},
		{"description too long", vol.QualificationInput{Name: "x", Description: strings.Repeat("d", vol.MaxDescriptionBytes+1)}, "too_long"},
		{"negative validity", vol.QualificationInput{Name: "x", ValidityDays: -1}, "range"},
		{"validity above the limit", vol.QualificationInput{Name: "x", ValidityDays: vol.MaxValidityDays + 1}, "range"},
		{"validity at the limit", vol.QualificationInput{Name: "x", ValidityDays: vol.MaxValidityDays}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.code == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) || fe.Code != tc.code {
				t.Fatalf("Validate = %v, want a field error with code %q", err, tc.code)
			}
		})
	}
}

func TestPositionInput_Validate_Table(t *testing.T) {
	tests := []struct {
		name string
		in   vol.PositionInput
		ok   bool
	}{
		{"open", vol.PositionInput{Name: "Dirt crew", Access: vol.AccessOpen}, true},
		{"members", vol.PositionInput{Name: "Dirt crew", Access: vol.AccessMembers}, true},
		{"hide shift mates", vol.PositionInput{Name: "Peer support", Access: vol.AccessMembers, HideShiftMates: true}, true},
		{"unknown access", vol.PositionInput{Name: "Dirt crew", Access: "secret"}, false},
		{"empty access", vol.PositionInput{Name: "Dirt crew"}, false},
		{"empty name", vol.PositionInput{Access: vol.AccessOpen}, false},
		{"name too long", vol.PositionInput{Name: strings.Repeat("n", vol.MaxNameBytes+1), Access: vol.AccessOpen}, false},
		{"control chars", vol.PositionInput{Name: "a\nb", Access: vol.AccessOpen}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tc.ok && !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
		})
	}
}

func TestRecordInput_Validate_Table(t *testing.T) {
	q := model.NewID[vol.QualificationID]()
	p := model.NewID[model.PersonID]()
	ok := vol.RecordInput{Qualification: q, Person: p, AcquiredOn: d(2026, 1, 1)}
	tests := []struct {
		name string
		mod  func(*vol.RecordInput)
		ok   bool
	}{
		{"valid", func(*vol.RecordInput) {}, true},
		{"expires after acquired", func(r *vol.RecordInput) { r.ExpiresOn = d(2027, 1, 1) }, true},
		{"expires the day it was acquired", func(r *vol.RecordInput) { r.ExpiresOn = d(2026, 1, 1) }, true},
		{"expires before acquired", func(r *vol.RecordInput) { r.ExpiresOn = d(2025, 12, 31) }, false},
		{"malformed acquired", func(r *vol.RecordInput) { r.AcquiredOn = vol.Date{Year: 2026, Month: 2, Day: 30} }, false},
		{"malformed expires", func(r *vol.RecordInput) { r.ExpiresOn = vol.Date{Year: 2027, Month: 13, Day: 1} }, false},
		{"no qualification", func(r *vol.RecordInput) { r.Qualification = vol.QualificationID{} }, false},
		{"no person", func(r *vol.RecordInput) { r.Person = model.PersonID{} }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := ok
			tc.mod(&in)
			err := in.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tc.ok && !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
		})
	}
}

func TestDate_Long_Table(t *testing.T) {
	cases := []struct {
		name string
		in   vol.Date
		want string
	}{
		{"zero", vol.Date{}, ""},
		{"end of August", vol.Date{Year: 2028, Month: time.August, Day: 31}, "31 August 2028"},
		{"single digit day has no padding", vol.Date{Year: 2027, Month: time.March, Day: 5}, "5 March 2027"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Long(); got != tc.want {
				t.Errorf("Long() = %q, want %q", got, tc.want)
			}
		})
	}
}
