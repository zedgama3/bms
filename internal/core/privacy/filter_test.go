package privacy_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestFilter_NotPointerToStruct_ReturnsInternal(t *testing.T) { // F16
	n := 1
	var nilRec *Record
	rec := newRecord()
	pp := &rec
	tests := map[string]any{
		"nil":                nil,
		"struct by value":    newRecord(),
		"nil pointer":        nilRec,
		"pointer to int":     &n,
		"pointer to slice":   &[]Record{},
		"pointer to pointer": &pp,
		"string":             "x",
	}
	for name, v := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := privacy.Filter(v, uniform(privacy.AudienceNobody), signedIn(bob), privacy.Relation{})
			wantKind(t, err, errs.Internal)
			if res.Audited() || len(res.Fields) != 0 {
				t.Errorf("nothing may be returned on error: %+v", res)
			}
		})
	}
}

type uncheckedUntagged struct {
	Pub  string  `json:"pub" privacy:"public"`
	Name *string `json:"name,omitempty"` // no tag
}

type uncheckedBadLevel struct {
	F string `json:"f,omitempty" privacy:"secret"`
}

type uncheckedBadTag struct {
	F *string `json:"f,omitempty" privacy:"personal,grp=contact"`
}

type uncheckedUngrouped struct {
	F string `json:"f,omitempty" privacy:"personal"`
}

// uncheckedDuplicate has two grouped fields with one JSON name (built by reflection: vet
// refuses the literal).
func uncheckedDuplicate() any {
	tag := reflect.StructTag(`json:"same,omitempty" privacy:"personal,group=contact"`)
	pt := reflect.TypeOf((*string)(nil))
	return reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: pt, Tag: tag}, {Name: "B", Type: pt, Tag: tag},
	})).Elem().Interface()
}

type uncheckedNamedUnexported struct {
	namedInner `json:"inner,omitzero" privacy:"internal"`
}

// ptrTo returns a pointer to a copy of v.
func ptrTo(v any) any {
	p := reflect.New(reflect.TypeOf(v))
	p.Elem().Set(reflect.ValueOf(v))
	return p.Interface()
}

func TestFilter_UncheckedType_ReturnsInternal(t *testing.T) { // F17 (A2.4): every Check code
	pol := privacy.Policy{"contact": privacy.AudienceNobody, "medical": privacy.AudienceNobody,
		"roster": privacy.AudienceNobody, "nick": privacy.AudienceNobody}
	long := strings.Repeat("a", audit.MaxFieldBytes+1)
	named := func(name string) any {
		return reflect.New(reflect.StructOf([]reflect.StructField{{
			Name: "F", Type: reflect.TypeOf((*string)(nil)),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" privacy:"personal,group=contact"`, name)),
		}})).Elem().Interface()
	}
	tests := []struct {
		name string
		v    any // a value; Filter gets a pointer to a copy
	}{
		{"required: untagged field", uncheckedUntagged{Pub: "p", Name: sp(marker)}},
		{"one_of: bad level", uncheckedBadLevel{F: marker}},
		{"malformed: bad tag", uncheckedBadTag{F: sp(marker)}},
		{"ungrouped: personal without group", uncheckedUngrouped{F: marker}},
		{"not_omittable: grouped pointer plain", M4View{Pub: "p", Phone: sp(marker)}},
		{"not_omittable: grouped string plain", struct {
			F string `json:"f" privacy:"personal,group=contact"`
		}{F: marker}},
		{"not_omittable: grouped struct omitempty", struct {
			F structVal `json:"f,omitempty" privacy:"personal,group=contact"`
		}{F: structVal{X: marker}}},
		{"not_omittable: grouped time.Time omitempty", struct {
			F time.Time `json:"f,omitempty" privacy:"personal,group=contact"`
		}{F: time.Unix(1, 0)}},
		{"not_omittable: grouped model.ID omitempty", struct {
			F model.PersonID `json:"f,omitempty" privacy:"personal,group=contact"`
		}{F: alice}},
		{"not_omittable: grouped array omitempty", struct {
			F [2]int `json:"f,omitempty" privacy:"personal,group=contact"`
		}{F: [2]int{1, 2}}},
		{"not_omittable: ungrouped internal scalar plain", struct {
			F string `json:"f" privacy:"internal"`
		}{F: marker}},
		{"not_omittable: ungrouped internal struct omitempty", struct {
			F structVal `json:"f,omitempty" privacy:"internal"`
		}{F: structVal{X: marker}}},
		{"duplicate: two grouped fields, one JSON name", uncheckedDuplicate()},
		{"too_many: more auditable fields than audit allows", manyFields(audit.MaxFields+1, "personal,group=contact")},
		{"too_long: auditable name over the limit", named(long)},
		{"malformed: auditable name outside audit grammar", named("legalName")},
		{"dynamic: interface field", struct {
			F any `json:"f,omitempty" privacy:"internal"`
		}{F: marker}},
		{"unsettable: named unexported embedded, internal", uncheckedNamedUnexported{namedInner: namedInner{X: marker}}},
		{"marshaler: a view that marshals itself", selfJSON{ID: marker}},
		{"nested required: element type has an untagged field", pRoster{Entries: []pEntry{{LegalName: marker}}}},
		{"nested not_omittable: element field lacks omit", pRosterPlain{Entries: []pEntryPlain{{Phone: sp(marker)}}}},
		{"nested dynamic: element has an interface field", pRosterDyn{Entries: []pEntryDyn{{Any: marker}}}},
		{"nested required: map value with an untagged field", pRosterMap{Entries: map[string]pEntry{"k": {LegalName: marker}}}},
		{"nested required: named struct field", pRosterOne{Entry: pEntry{LegalName: marker}}},
		{"nested marshaler: pointer-receiver marshaler as a value field", ptrMarshHolder{X: ptrMarsh{S: marker, N: marker}}},
	}
	for _, tc := range tests {
		for vname, viewer := range map[string]privacy.Viewer{"anonymous": {}, "signed in": signedIn(bob)} {
			t.Run(tc.name+"/"+vname, func(t *testing.T) {
				p := ptrTo(tc.v)
				before := reflect.ValueOf(ptrTo(tc.v)).Elem().Interface()
				res, err := privacy.Filter(p, pol, viewer, privacy.Relation{Subject: alice})
				wantKind(t, err, errs.Internal)
				if got := reflect.ValueOf(p).Elem().Interface(); !reflect.DeepEqual(got, before) {
					t.Errorf("*v must be untouched: %+v", got)
				}
				if res.Audited() || len(res.Fields) != 0 || res.Level != 0 {
					t.Errorf("nothing may be returned on error: %+v", res)
				}
				if strings.Contains(err.Error(), marker) {
					t.Error("the error echoes a field value")
				}
			})
		}
	}
	// An unknown group at run time is only detectable as "a group missing from pol":
	// F18 (TestFilter_PolicyMissingGroup_ReturnsInternal) covers it, since Filter is not
	// given the declared groups.
}

// everyKind holds one non-public field of each kind the encoder omits differently, plus
// an ungrouped internal one of each, for F6a.
type everyKind struct {
	Pub      string         `json:"pub" privacy:"public"`
	IntStr   string         `json:"int_str,omitempty" privacy:"internal"`
	IntNum   int            `json:"int_num,omitempty" privacy:"internal"`
	IntBool  bool           `json:"int_bool,omitempty" privacy:"internal"`
	IntPtr   *string        `json:"int_ptr,omitempty" privacy:"internal"`
	IntTime  time.Time      `json:"int_time,omitzero" privacy:"internal"`
	IntID    model.PersonID `json:"int_id,omitzero" privacy:"internal"`
	IntStr2  structVal      `json:"int_struct,omitzero" privacy:"internal"`
	IntArr   [2]int         `json:"int_arr,omitzero" privacy:"internal"`
	IntSlice []string       `json:"int_slice,omitempty" privacy:"internal"`
	IntMap   map[string]int `json:"int_map,omitempty" privacy:"internal"`
	GStr     string         `json:"g_str,omitempty" privacy:"personal,group=contact"`
	GNum     int            `json:"g_num,omitempty" privacy:"personal,group=contact"`
	GPtr     *string        `json:"g_ptr,omitempty" privacy:"personal,group=contact"`
	GTime    time.Time      `json:"g_time,omitzero" privacy:"personal,group=contact"`
	GID      model.PersonID `json:"g_id,omitzero" privacy:"personal,group=contact"`
	GStruct  structVal      `json:"g_struct,omitzero" privacy:"personal,group=contact"`
	GArr     [2]int         `json:"g_arr,omitzero" privacy:"personal,group=contact"`
	GSlice   []string       `json:"g_slice,omitempty" privacy:"personal,group=contact"`
	GMap     map[string]int `json:"g_map,omitempty" privacy:"personal,group=contact"`
}

var everyKindNames = []string{
	"int_str", "int_num", "int_bool", "int_ptr", "int_time", "int_id", "int_struct", "int_arr", "int_slice", "int_map",
	"g_str", "g_num", "g_ptr", "g_time", "g_id", "g_struct", "g_arr", "g_slice", "g_map",
}

func newEveryKind() everyKind {
	return everyKind{
		Pub: "pub", IntStr: "s", IntNum: 1, IntBool: true, IntPtr: sp("p"), IntTime: time.Unix(100, 0).UTC(),
		IntID: alice, IntStr2: structVal{X: "x"}, IntArr: [2]int{1, 2}, IntSlice: []string{"a"}, IntMap: map[string]int{"a": 1},
		GStr: "s", GNum: 1, GPtr: sp("p"), GTime: time.Unix(100, 0).UTC(), GID: alice, GStruct: structVal{X: "x"},
		GArr: [2]int{1, 2}, GSlice: []string{"a"}, GMap: map[string]int{"a": 1},
	}
}

func TestFilter_HiddenFieldEncodesAbsent(t *testing.T) { // F6a
	if err := privacy.Check([]any{everyKind{}}, allGroups); err != nil {
		t.Fatalf("Check: %v", err)
	}
	pol := uniform(privacy.AudienceNobody)
	t.Run("anonymous: every non-public field absent", func(t *testing.T) {
		v := newEveryKind()
		if _, err := privacy.Filter(&v, pol, privacy.Viewer{}, privacy.Relation{Subject: alice}); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertHidden(t, body, everyKindNames...)
		privacytest.AssertPresent(t, body, "pub")
		if string(body) != `{"pub":"pub"}` {
			t.Errorf("body = %s", body)
		}
	})
	t.Run("signed in, groups nobody: grouped absent, internal present", func(t *testing.T) {
		v := newEveryKind()
		if _, err := privacy.Filter(&v, pol, signedIn(bob), privacy.Relation{Subject: alice}); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertHidden(t, body, everyKindNames[10:]...)
		privacytest.AssertPresent(t, body, append([]string{"pub"}, everyKindNames[:10]...)...)
	})
	t.Run("visible: present", func(t *testing.T) {
		v := newEveryKind()
		if _, err := privacy.Filter(&v, uniform(privacy.AudienceSignedIn), signedIn(bob), privacy.Relation{Subject: alice}); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertPresent(t, body, append([]string{"pub"}, everyKindNames...)...)
	})
	t.Run("the zero values of a visible internal field are absent too", func(t *testing.T) {
		v := everyKind{Pub: "pub"}
		if _, err := privacy.Filter(&v, pol, signedIn(bob), privacy.Relation{}); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertHidden(t, body, everyKindNames...)
	})
}

// F17a: a hidden field that cannot be set fails closed, before anything changes.
type unsettableSibling struct {
	Note       string `json:"note,omitempty" privacy:"internal"`
	namedInner `json:"inner,omitzero" privacy:"internal"`
}

func TestFilter_UnsettableHiddenField_ReturnsInternal(t *testing.T) { // F17a
	// A named unexported embedded struct is rejected by Check (F6d); Filter backs that up
	// at run time. Public is allowed and never needs hiding, so it succeeds for everyone.
	type publicNamed struct {
		namedInner `json:"inner" privacy:"public"`
		Note       string `json:"note,omitempty" privacy:"internal"`
	}
	v := publicNamed{namedInner: namedInner{X: marker}, Note: "n"}
	if _, err := privacy.Filter(&v, nil, privacy.Viewer{}, privacy.Relation{}); err != nil {
		t.Fatalf("a public unsettable field is never hidden: %v", err)
	}
	if v.X != marker || v.Note != "" {
		t.Errorf("v = %+v", v)
	}
	// A hidden unsettable field fails, and the settable siblings are not changed first.
	w := unsettableSibling{Note: "n", namedInner: namedInner{X: marker}}
	before := w
	_, err := privacy.Filter(&w, nil, privacy.Viewer{}, privacy.Relation{})
	wantKind(t, err, errs.Internal)
	if !reflect.DeepEqual(w, before) {
		t.Errorf("*v changed on failure: %+v", w)
	}
}

func TestFilter_PolicyMissingGroup_ReturnsInternal(t *testing.T) { // F18
	for name, pol := range map[string]privacy.Policy{
		"nil":            nil,
		"empty":          {},
		"one of several": {"contact": privacy.AudienceNobody},
	} {
		t.Run(name, func(t *testing.T) {
			v := newRecord()
			res, err := privacy.Filter(&v, pol, signedIn(bob), privacy.Relation{})
			wantKind(t, err, errs.Internal)
			if res.Audited() {
				t.Errorf("result = %+v", res)
			}
			if v.Phone == nil || v.Dx == nil || v.Int != "int" {
				t.Errorf("*v must be untouched on error: %+v", v)
			}
		})
	}
	t.Run("type without groups needs none", func(t *testing.T) {
		type plain struct {
			A string `json:"a" privacy:"public"`
			B string `json:"b,omitempty" privacy:"internal"`
		}
		v := plain{A: "a", B: "b"}
		if _, err := privacy.Filter(&v, nil, signedIn(bob), privacy.Relation{}); err != nil {
			t.Fatalf("Filter: %v", err)
		}
		if v.A != "a" || v.B != "b" {
			t.Errorf("%+v", v)
		}
	})
}

// kept says which of the grouped fields survived.
type kept struct{ phone, dx, note, nick bool }

func keptOf(v Record) kept {
	return kept{phone: v.Phone != nil, dx: v.Dx != nil, note: v.Note != nil, nick: v.Nick != nil}
}

func TestFilter_Nobody_HidesFromSubject(t *testing.T) { // F19
	subject := alice
	viewers := map[string]privacy.Viewer{
		"subject":     holderSite(subject, permContacts, permMedical, permRoster),
		"site holder": holderSite(carol, permContacts, permMedical, permRoster),
		"anonymous":   {},
		"signed in":   signedIn(bob),
	}
	for name, v := range viewers {
		t.Run(name, func(t *testing.T) {
			rec := newRecord()
			res, err := privacy.Filter(&rec, uniform(privacy.AudienceNobody), v, privacy.Relation{Subject: subject, Department: deptA, Also: []privacy.Audience{"booked"}})
			if err != nil {
				t.Fatal(err)
			}
			if k := keptOf(rec); k != (kept{}) {
				t.Errorf("kept = %+v, want none", k)
			}
			if res.Audited() {
				t.Errorf("hidden fields must not be audited: %+v", res)
			}
			if rec.Pub != "pub" {
				t.Error("public field must stay")
			}
		})
	}
}

func TestFilter_Self_Table(t *testing.T) { // F20
	tests := []struct {
		name   string
		viewer privacy.Viewer
		rel    privacy.Relation
		want   bool
	}{
		{"viewer is the subject", signedIn(alice), privacy.Relation{Subject: alice}, true},
		{"viewer is another person", signedIn(bob), privacy.Relation{Subject: alice}, false},
		{"anonymous, record has a subject", privacy.Viewer{}, privacy.Relation{Subject: alice}, false},
		{"anonymous, record has no subject (zero never equals zero)", privacy.Viewer{}, privacy.Relation{}, false},
		{"signed in, record has no subject", signedIn(bob), privacy.Relation{}, false},
		{"holder who is not the subject", holderSite(carol, permContacts), privacy.Relation{Subject: alice}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecord()
			if _, err := privacy.Filter(&rec, uniform(privacy.AudienceSelf), tc.viewer, tc.rel); err != nil {
				t.Fatal(err)
			}
			if got := rec.Phone != nil; got != tc.want {
				t.Errorf("phone kept = %v, want %v", got, tc.want)
			}
			if got := rec.Dx != nil; got != tc.want {
				t.Errorf("dx kept = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilter_Holders_Table(t *testing.T) { // F21
	rel := privacy.Relation{Subject: alice, Department: deptA}
	tests := []struct {
		name   string
		viewer privacy.Viewer
		rel    privacy.Relation
		want   bool
	}{
		{"held site-wide", holderSite(carol, permContacts), rel, true},
		{"held at the record's department", holderIn(carol, deptA, permContacts), rel, true},
		{"held at another department", holderIn(carol, deptB, permContacts), rel, false},
		{"not held", signedIn(carol), rel, false},
		{"held at a department, record has zero department", holderIn(carol, deptA, permContacts), privacy.Relation{Subject: alice}, false},
		{"held site-wide, record has zero department", holderSite(carol, permContacts), privacy.Relation{Subject: alice}, true},
		{"a different permission held site-wide", holderSite(carol, permMedical), rel, false},
		{"empty extent entry", privacy.Viewer{Person: carol, Extents: map[string]authz.Extent{"contact": {}}}, rel, false},
		{"held at two departments, one covers", privacy.Viewer{Person: carol, Extents: map[string]authz.Extent{"contact": {Departments: sortedDepts(deptA, deptB)}}}, rel, true},
		{"anonymous", privacy.Viewer{}, rel, false},
		{"the subject who holds nothing sees their own", signedIn(alice), rel, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecord()
			pol := policy(privacy.AudienceHolders, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
			if _, err := privacy.Filter(&rec, pol, tc.viewer, tc.rel); err != nil {
				t.Fatal(err)
			}
			if got := rec.Phone != nil; got != tc.want {
				t.Errorf("phone kept = %v, want %v", got, tc.want)
			}
		})
	}
}

func sortedDepts(ds ...model.DepartmentID) []model.DepartmentID {
	out := slices.Clone(ds)
	slices.SortFunc(out, func(a, b model.DepartmentID) int { return strings.Compare(string(a[:]), string(b[:])) })
	return out
}

func TestFilter_SignedIn_Table(t *testing.T) { // F22
	tests := []struct {
		name   string
		viewer privacy.Viewer
		want   bool
	}{
		{"signed in, no permission", signedIn(bob), true},
		{"signed in, record has a subject who is someone else", signedIn(bob), true},
		{"anonymous", privacy.Viewer{}, false},
		{"anonymous with extents", privacy.Viewer{Extents: map[string]authz.Extent{"contact": {Site: true}}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecord()
			pol := policy(privacy.AudienceSignedIn, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceSignedIn)
			if _, err := privacy.Filter(&rec, pol, tc.viewer, privacy.Relation{Subject: alice, Department: deptA}); err != nil {
				t.Fatal(err)
			}
			if got := rec.Phone != nil; got != tc.want {
				t.Errorf("phone kept = %v, want %v", got, tc.want)
			}
			if got := rec.Nick != nil; got != tc.want {
				t.Errorf("nick kept = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilter_Extra_Table(t *testing.T) { // F23
	tests := []struct {
		name   string
		viewer privacy.Viewer
		rel    privacy.Relation
		pol    privacy.Audience
		want   bool
	}{
		{"audience asserted in Also", signedIn(bob), privacy.Relation{Subject: alice, Also: []privacy.Audience{"booked"}}, "booked", true},
		{"audience absent from Also", signedIn(bob), privacy.Relation{Subject: alice}, "booked", false},
		{"Also names another extra", signedIn(bob), privacy.Relation{Subject: alice, Also: []privacy.Audience{"other"}}, "booked", false},
		{"Also names holders, viewer is not a holder: ignored", signedIn(bob), privacy.Relation{Subject: alice, Also: []privacy.Audience{privacy.AudienceHolders}}, privacy.AudienceHolders, false},
		{"Also names self: ignored", signedIn(bob), privacy.Relation{Subject: alice, Also: []privacy.Audience{privacy.AudienceSelf}}, privacy.AudienceNobody, false},
		{"Also names signed_in, viewer anonymous: ignored", privacy.Viewer{}, privacy.Relation{Subject: alice, Also: []privacy.Audience{privacy.AudienceSignedIn}}, privacy.AudienceNobody, false},
		{"Also asserted but policy nobody", signedIn(bob), privacy.Relation{Subject: alice, Also: []privacy.Audience{"booked"}}, privacy.AudienceNobody, false},
		{"Also asserted, among others", signedIn(bob), privacy.Relation{Subject: alice, Also: []privacy.Audience{"other", "booked"}}, "booked", true},
		{"the subject sees their own under an extra policy without Also", signedIn(alice), privacy.Relation{Subject: alice}, "booked", true},
		// F40 and Behavior 10: the zero Viewer keeps public fields only, whatever Also says.
		{"anonymous with Also gets nothing non-public (F40)", privacy.Viewer{}, privacy.Relation{Subject: alice, Also: []privacy.Audience{"booked"}}, "booked", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecord()
			pol := policy(privacy.AudienceNobody, privacy.AudienceNobody, tc.pol, privacy.AudienceNobody)
			if _, err := privacy.Filter(&rec, pol, tc.viewer, tc.rel); err != nil {
				t.Fatal(err)
			}
			if got := rec.Note != nil; got != tc.want {
				t.Errorf("note kept = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilter_ExtraNotPromotingToHolders(t *testing.T) {
	rec := newRecord()
	pol := policy(privacy.AudienceHolders, privacy.AudienceHolders, privacy.AudienceHolders, privacy.AudienceNobody)
	rel := privacy.Relation{Subject: alice, Department: deptA, Also: []privacy.Audience{privacy.AudienceHolders}}
	if _, err := privacy.Filter(&rec, pol, signedIn(bob), rel); err != nil {
		t.Fatal(err)
	}
	if k := keptOf(rec); k.phone || k.dx || k.note {
		t.Errorf("a module cannot promote a viewer to holders through Also: %+v", k)
	}
}

func TestFilter_SubjectSeesOwn_NotAudited(t *testing.T) { // F24
	for _, a := range []privacy.Audience{privacy.AudienceSelf, privacy.AudienceHolders, privacy.AudienceSignedIn} {
		t.Run(string(a), func(t *testing.T) {
			rec := newRecord()
			pol := policy(a, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
			res, err := privacy.Filter(&rec, pol, signedIn(alice), privacy.Relation{Subject: alice, Department: deptA})
			if err != nil {
				t.Fatal(err)
			}
			if rec.Phone == nil {
				t.Fatal("the subject must see their own personal field")
			}
			if len(res.Fields) != 0 || res.Audited() {
				t.Errorf("own personal read must not be audited: %+v", res)
			}
		})
	}
}

func TestFilter_OtherPersonal_Audited(t *testing.T) { // F25
	rec := newRecord()
	pol := policy(privacy.AudienceHolders, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
	res, err := privacy.Filter(&rec, pol, holderIn(carol, deptA, permContacts), privacy.Relation{Subject: alice, Department: deptA})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Phone == nil {
		t.Fatal("holder must see the field")
	}
	if !slices.Equal(res.Fields, []string{"phone"}) {
		t.Errorf("Fields = %v, want [phone]", res.Fields)
	}
	if res.Level != model.SensitivityPersonal {
		t.Errorf("Level = %v, want personal", res.Level)
	}
	if !res.Audited() {
		t.Error("Audited must be true")
	}
}

func TestFilter_SensitiveSelf_Audited(t *testing.T) { // F26
	rec := newRecord()
	pol := policy(privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceNobody, privacy.AudienceNobody)
	res, err := privacy.Filter(&rec, pol, signedIn(alice), privacy.Relation{Subject: alice})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Dx == nil {
		t.Fatal("the subject must see their own sensitive field")
	}
	if !slices.Equal(res.Fields, []string{"dx"}) || res.Level != model.SensitivitySensitive {
		t.Errorf("result = %+v, want dx / sensitive", res)
	}
}

func TestFilter_Result_FieldsAndLevel(t *testing.T) {
	t.Run("mixed levels: order of fields, highest level", func(t *testing.T) {
		rec := newRecord()
		pol := policy(privacy.AudienceSignedIn, privacy.AudienceSignedIn, privacy.AudienceNobody, privacy.AudienceSignedIn)
		res, err := privacy.Filter(&rec, pol, signedIn(bob), privacy.Relation{Subject: alice})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(res.Fields, []string{"phone", "dx"}) {
			t.Errorf("Fields = %v, want [phone dx] in field order, without internal or hidden fields", res.Fields)
		}
		if res.Level != model.SensitivitySensitive {
			t.Errorf("Level = %v", res.Level)
		}
	})
	t.Run("hidden fields are not reported", func(t *testing.T) {
		rec := newRecord()
		pol := policy(privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceSignedIn)
		res, err := privacy.Filter(&rec, pol, signedIn(bob), privacy.Relation{Subject: alice})
		if err != nil {
			t.Fatal(err)
		}
		if res.Audited() || len(res.Fields) != 0 {
			t.Errorf("only an internal grouped field was returned: %+v", res)
		}
	})
	t.Run("a returned field that is absent (nil) is still only reported when present", func(t *testing.T) {
		rec := newRecord()
		rec.Phone = nil
		pol := policy(privacy.AudienceSignedIn, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
		res, err := privacy.Filter(&rec, pol, signedIn(bob), privacy.Relation{Subject: alice})
		if err != nil {
			t.Fatal(err)
		}
		_ = res // spec: "the returned fields"; whether an empty one is reported is not specified
	})
	t.Run("no subject: personal about someone else", func(t *testing.T) {
		rec := newRecord()
		pol := policy(privacy.AudienceSignedIn, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
		res, err := privacy.Filter(&rec, pol, signedIn(bob), privacy.Relation{})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(res.Fields, []string{"phone"}) || res.Level != model.SensitivityPersonal {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("Fields fit an audit entry", func(t *testing.T) {
		rec := newRecord()
		res, err := privacy.Filter(&rec, uniform(privacy.AudienceSignedIn), signedIn(bob), privacy.Relation{Subject: alice})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, f := range res.Fields {
			if seen[f] {
				t.Errorf("duplicate field %q", f)
			}
			seen[f] = true
		}
	})
}

func TestFilter_Anonymous_PublicOnly(t *testing.T) { // F27
	rec := newRecord()
	res, err := privacy.Filter(&rec, uniform(privacy.AudienceNobody), privacy.Viewer{}, privacy.Relation{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Int != "" {
		t.Errorf("internal ungrouped field must be zeroed for an anonymous viewer: %q", rec.Int)
	}
	if rec.Pub != "pub" {
		t.Errorf("public field must stay: %q", rec.Pub)
	}
	if res.Audited() {
		t.Errorf("result = %+v", res)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	privacytest.AssertHidden(t, body, "int", "phone", "dx", "note", "nick")
	privacytest.AssertPresent(t, body, "pub")
	// Signed in: internal stays.
	rec = newRecord()
	if _, err := privacy.Filter(&rec, uniform(privacy.AudienceNobody), signedIn(bob), privacy.Relation{}); err != nil {
		t.Fatal(err)
	}
	if rec.Int != "int" {
		t.Errorf("internal ungrouped field must stay for a signed-in viewer: %q", rec.Int)
	}
}

func TestFilter_HiddenIsAbsentOnTheWire(t *testing.T) {
	rec := newRecord()
	if _, err := privacy.Filter(&rec, uniform(privacy.AudienceNobody), signedIn(bob), privacy.Relation{Subject: alice}); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"phone", "dx", "note", "nick"} {
		if _, ok := m[k]; ok {
			t.Errorf("hidden field %q is present in %s", k, body)
		}
	}
	if _, ok := m["pub"]; !ok {
		t.Error("public field missing")
	}
}

func TestFilter_JSONDashFieldUntouched(t *testing.T) {
	rec := newRecord()
	if _, err := privacy.Filter(&rec, uniform(privacy.AudienceNobody), privacy.Viewer{}, privacy.Relation{}); err != nil {
		t.Fatal(err)
	}
	if rec.Hidden != "kept" {
		t.Errorf("a json:\"-\" field is exempt and must not be zeroed: %q", rec.Hidden)
	}
}

// Embedded.
type ContactBlock struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
	Dx    *string `json:"dx,omitempty" privacy:"sensitive,group=medical"`
}

type WithEmbedded struct {
	ContactBlock
	Name string `json:"name,omitempty" privacy:"internal"`
}

type WithEmbeddedPtr struct {
	*ContactBlock
	Name string `json:"name,omitempty" privacy:"internal"`
}

func TestFilter_EmbeddedWalked(t *testing.T) { // F28
	if err := privacy.Check([]any{WithEmbedded{}}, allGroups); err != nil {
		t.Fatalf("Check: %v", err)
	}
	pol := policy(privacy.AudienceHolders, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
	t.Run("hidden", func(t *testing.T) {
		v := WithEmbedded{ContactBlock: ContactBlock{Phone: sp("1"), Dx: sp("2")}, Name: "n"}
		res, err := privacy.Filter(&v, pol, signedIn(bob), privacy.Relation{Subject: alice, Department: deptA})
		if err != nil {
			t.Fatal(err)
		}
		if v.Phone != nil || v.Dx != nil || v.Name != "n" || res.Audited() {
			t.Errorf("v = %+v res = %+v", v, res)
		}
	})
	t.Run("kept and audited", func(t *testing.T) {
		v := WithEmbedded{ContactBlock: ContactBlock{Phone: sp("1"), Dx: sp("2")}, Name: "n"}
		res, err := privacy.Filter(&v, pol, holderSite(carol, permContacts), privacy.Relation{Subject: alice, Department: deptA})
		if err != nil {
			t.Fatal(err)
		}
		if v.Phone == nil || v.Dx != nil {
			t.Errorf("v = %+v", v)
		}
		if !slices.Equal(res.Fields, []string{"phone"}) {
			t.Errorf("Fields = %v", res.Fields)
		}
	})
	t.Run("embedded pointer, nil", func(t *testing.T) {
		v := WithEmbeddedPtr{Name: "n"}
		if _, err := privacy.Filter(&v, pol, signedIn(bob), privacy.Relation{}); err != nil {
			t.Fatalf("a nil embedded pointer must not fail or panic: %v", err)
		}
	})
	t.Run("embedded pointer, set", func(t *testing.T) {
		v := WithEmbeddedPtr{ContactBlock: &ContactBlock{Phone: sp("1")}, Name: "n"}
		if _, err := privacy.Filter(&v, pol, signedIn(bob), privacy.Relation{}); err != nil {
			t.Fatal(err)
		}
		if v.ContactBlock != nil && v.Phone != nil {
			t.Errorf("hidden embedded field survived: %+v", v.ContactBlock)
		}
	})
}

func TestFilter_EmbeddedWalked_Unexported(t *testing.T) { // F28 (A2): unexported embedded types
	nobody := hiddenPolicy
	t.Run("unexported value, hidden", func(t *testing.T) {
		v := embedValUnexported{unexportedBase: unexportedBase{Phone: sp(marker)}, Pub: "pub"}
		if _, err := privacy.Filter(&v, nobody, privacy.Viewer{}, privacy.Relation{Subject: alice}); err != nil {
			t.Fatalf("Filter: %v", err)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertHidden(t, body, "phone")
		if string(body) != `{"pub":"pub"}` {
			t.Errorf("wire = %s", body)
		}
	})
	t.Run("unexported value, kept and audited", func(t *testing.T) {
		v := embedValUnexported{unexportedBase: unexportedBase{Phone: sp("555-0100")}, Pub: "pub"}
		res, err := privacy.Filter(&v, nobody, holderSite(carol, permContacts), privacy.Relation{Subject: alice, Department: deptA})
		if err != nil {
			t.Fatal(err)
		}
		if v.Phone == nil || !slices.Equal(res.Fields, []string{"phone"}) {
			t.Errorf("v = %+v res = %+v", v, res)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertPresent(t, body, "phone", "pub")
	})
	t.Run("unexported pointer, set, hidden", func(t *testing.T) {
		v := M1View{m1Inner: &m1Inner{Phone: sp(marker), Label: "shown"}, ID: "1"}
		if _, err := privacy.Filter(&v, nobody, privacy.Viewer{}, privacy.Relation{}); err != nil {
			t.Fatalf("Filter: %v", err)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertHidden(t, body, "phone")
		privacytest.AssertPresent(t, body, "id", "label")
		if strings.Contains(string(body), marker) {
			t.Errorf("hidden value on the wire: %s", body)
		}
	})
	t.Run("unexported pointer, nil", func(t *testing.T) {
		v := M1View{ID: "1"}
		res, err := privacy.Filter(&v, nobody, privacy.Viewer{}, privacy.Relation{})
		if err != nil {
			t.Fatalf("a nil embedded pointer must not fail or panic: %v", err)
		}
		if res.Audited() {
			t.Errorf("res = %+v", res)
		}
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		privacytest.AssertHidden(t, body, "phone", "label")
		if v.m1Inner != nil {
			t.Error("Filter must not allocate a nil embedded pointer")
		}
	})
}

func TestFilter_EmbeddedPointer_WritesThrough(t *testing.T) { // F28a
	// A shared pointee is changed for every holder of the pointer: documented, and pinned
	// here so a change to copy-on-write is a conscious one.
	shared := &ContactBlock{Phone: sp("1"), Dx: sp("2")}
	a := WithEmbeddedPtr{ContactBlock: shared, Name: "a"}
	b := WithEmbeddedPtr{ContactBlock: shared, Name: "b"}
	if _, err := privacy.Filter(&a, uniform(privacy.AudienceNobody), signedIn(bob), privacy.Relation{Subject: alice}); err != nil {
		t.Fatal(err)
	}
	if a.ContactBlock != shared {
		t.Error("Filter replaced the embedded pointer instead of writing through it")
	}
	if shared.Phone != nil || shared.Dx != nil {
		t.Errorf("the pointee was not zeroed: %+v", shared)
	}
	if b.Phone != nil || b.Dx != nil {
		t.Errorf("the second holder of the pointee still sees the values: %+v", b.ContactBlock)
	}
}

// Containers: pointers, slices, maps and named structs holding grouped fields.
type Roster struct {
	Title   string            `json:"title,omitempty" privacy:"internal"`
	Entries []Record          `json:"entries,omitempty" privacy:"internal"`
	Lead    Record            `json:"lead,omitzero" privacy:"internal"`
	Boss    *Record           `json:"boss,omitempty" privacy:"internal"`
	ByDay   map[string]Record `json:"by_day,omitempty" privacy:"internal"`
}

func newRoster() Roster {
	b := newRecord()
	return Roster{Title: "t", Entries: []Record{newRecord(), newRecord()}, Lead: newRecord(), Boss: &b, ByDay: map[string]Record{"mon": newRecord()}}
}

func TestFilter_NestedNotEntered(t *testing.T) { // F29
	if err := privacy.Check([]any{Roster{}}, allGroups); err != nil {
		t.Fatalf("Check: %v", err)
	}
	r := newRoster()
	res, err := privacy.Filter(&r, uniform(privacy.AudienceNobody), signedIn(bob), privacy.Relation{})
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range r.Entries {
		if keptOf(e) != (kept{true, true, true, true}) {
			t.Errorf("entry %d was filtered: %+v", i, keptOf(e))
		}
	}
	if keptOf(r.Lead) != (kept{true, true, true, true}) {
		t.Error("named struct field was filtered")
	}
	if r.Boss == nil || keptOf(*r.Boss) != (kept{true, true, true, true}) {
		t.Error("pointer field was filtered")
	}
	if keptOf(r.ByDay["mon"]) != (kept{true, true, true, true}) {
		t.Error("map value was filtered")
	}
	if res.Audited() {
		t.Errorf("nested values are not reported by the container's call: %+v", res)
	}
	// The container's own ungrouped fields are still decided.
	r2 := newRoster()
	if _, err := privacy.Filter(&r2, uniform(privacy.AudienceNobody), privacy.Viewer{}, privacy.Relation{}); err != nil {
		t.Fatal(err)
	}
	if r2.Title != "" {
		t.Error("the container's internal field must be zeroed for an anonymous viewer")
	}
}

func TestFilter_ElementsThenContainer_CallerPattern(t *testing.T) {
	r := newRoster()
	pol := policy(privacy.AudienceHolders, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
	viewer := holderIn(carol, deptA, permContacts)
	subjects := []model.PersonID{alice, bob}
	var audited []string
	for i := range r.Entries {
		res, err := privacy.Filter(&r.Entries[i], pol, viewer, privacy.Relation{Subject: subjects[i], Department: deptB})
		if err != nil {
			t.Fatal(err)
		}
		audited = append(audited, res.Fields...)
	}
	if len(audited) != 0 || r.Entries[0].Phone != nil {
		t.Errorf("department B record must be hidden from a department A holder: %v", audited)
	}
	if _, err := privacy.Filter(&r, pol, viewer, privacy.Relation{Department: deptA}); err != nil {
		t.Fatal(err)
	}
}

func TestFilter_HiddenValueNeverLogged(t *testing.T) { // F30
	c := telemetrytest.New(t)
	old := slog.Default()
	slog.SetDefault(c.Logger())
	t.Cleanup(func() { slog.SetDefault(old) })

	rec := newRecord()
	rec.Phone, rec.Dx, rec.Note, rec.Nick = sp(marker), sp(marker), sp(marker), sp(marker)
	rec.Int, rec.Hidden = marker, "unrelated"
	res, err := privacy.Filter(&rec, uniform(privacy.AudienceNobody), privacy.Viewer{}, privacy.Relation{Subject: alice})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Phone != nil || rec.Dx != nil || rec.Note != nil || rec.Nick != nil || rec.Int != "" {
		t.Errorf("hidden values remain in memory: %+v", rec)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), marker) {
		t.Errorf("zeroed memory still carries the marker: %s", body)
	}
	if strings.Contains(strings.Join(res.Fields, " "), marker) {
		t.Error("Result carries a value")
	}
	if c.Contains(marker) {
		t.Error("the marker reached the logs or spans")
	}

	// A failing call does not echo values either.
	bad := uncheckedUntagged{Name: sp(marker)}
	_, err = privacy.Filter(&bad, privacy.Policy{}, privacy.Viewer{}, privacy.Relation{})
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), marker) {
		t.Error("error message carries a field value")
	}
}

func TestFilter_SuperuserIsHoldersNeverSelf(t *testing.T) {
	az := authztest.NewFake()
	az.Superuser(carol)
	ctx := actorCtx(carol)
	viewer, err := privacy.ViewerOf(ctx, az, carol, allGroups)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("holders policy, a record about someone else: shown and audited", func(t *testing.T) {
		rec := newRecord()
		pol := policy(privacy.AudienceHolders, privacy.AudienceHolders, privacy.AudienceNobody, privacy.AudienceNobody)
		res, err := privacy.Filter(&rec, pol, viewer, privacy.Relation{Subject: alice, Department: deptB})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Phone == nil || rec.Dx == nil {
			t.Error("the superuser holds every permission everywhere")
		}
		if !slices.Equal(res.Fields, []string{"phone", "dx"}) {
			t.Errorf("Fields = %v (superuser reads are audited as anyone's)", res.Fields)
		}
	})
	t.Run("self policy, a record about someone else: hidden", func(t *testing.T) {
		rec := newRecord()
		if _, err := privacy.Filter(&rec, uniform(privacy.AudienceSelf), viewer, privacy.Relation{Subject: alice, Department: deptA}); err != nil {
			t.Fatal(err)
		}
		if rec.Phone != nil || rec.Dx != nil {
			t.Error("the superuser is not self anywhere")
		}
	})
	t.Run("extra policy without Also: hidden", func(t *testing.T) {
		rec := newRecord()
		pol := policy(privacy.AudienceNobody, privacy.AudienceNobody, "booked", privacy.AudienceNobody)
		if _, err := privacy.Filter(&rec, pol, viewer, privacy.Relation{Subject: alice}); err != nil {
			t.Fatal(err)
		}
		if rec.Note != nil {
			t.Error("the superuser is not in a module's extra audience")
		}
	})
	t.Run("the superuser as subject", func(t *testing.T) {
		rec := newRecord()
		if _, err := privacy.Filter(&rec, uniform(privacy.AudienceSelf), viewer, privacy.Relation{Subject: carol}); err != nil {
			t.Fatal(err)
		}
		if rec.Phone == nil {
			t.Error("the subject sees their own")
		}
	})
}

func TestShows_MatchesFilter(t *testing.T) {
	auds := []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders, privacy.AudienceSignedIn}
	viewers := []privacy.Viewer{
		{}, signedIn(alice), signedIn(bob), holderSite(carol, permContacts), holderIn(carol, deptA, permContacts), holderIn(carol, deptB, permContacts),
	}
	rels := []privacy.Relation{
		{}, {Subject: alice}, {Subject: alice, Department: deptA}, {Subject: bob, Department: deptB},
	}
	for _, a := range auds {
		for vi, v := range viewers {
			for ri, rel := range rels {
				pol := policy(a, privacy.AudienceNobody, privacy.AudienceNobody, privacy.AudienceNobody)
				rec := newRecord()
				if _, err := privacy.Filter(&rec, pol, v, rel); err != nil {
					t.Fatal(err)
				}
				if got, want := pol.Shows("contact", v, rel), rec.Phone != nil; got != want {
					t.Errorf("audience %s viewer %d relation %d: Shows = %v, Filter kept = %v", a, vi, ri, got, want)
				}
			}
		}
	}
	// Extra audience parity.
	pol := privacy.Policy{"roster": "booked"}
	for _, rel := range []privacy.Relation{{}, {Also: []privacy.Audience{"booked"}}, {Subject: alice}} {
		for _, v := range []privacy.Viewer{{}, signedIn(alice), signedIn(bob)} {
			rec := newRecord()
			full := policy(privacy.AudienceNobody, privacy.AudienceNobody, "booked", privacy.AudienceNobody)
			if _, err := privacy.Filter(&rec, full, v, rel); err != nil {
				t.Fatal(err)
			}
			if got, want := pol.Shows("roster", v, rel), rec.Note != nil; got != want {
				t.Errorf("extra: Shows = %v, Filter kept = %v (%+v %+v)", got, want, v, rel)
			}
		}
	}
}

func TestShows_UnknownGroup_False(t *testing.T) { // F37
	pol := policy(privacy.AudienceSignedIn, privacy.AudienceSignedIn, privacy.AudienceSignedIn, privacy.AudienceSignedIn)
	if pol.Shows("nosuch", signedIn(alice), privacy.Relation{Subject: alice}) {
		t.Error("an unknown group must be false, even for the subject")
	}
	var nilPol privacy.Policy
	if nilPol.Shows("contact", signedIn(alice), privacy.Relation{Subject: alice}) {
		t.Error("a nil policy shows nothing")
	}
	if !pol.Shows("contact", signedIn(bob), privacy.Relation{}) {
		t.Error("a known group with signed_in must show to a signed-in viewer")
	}
}

// coldNest and coldView are used by no other test, so the plan cache is cold when
// TestPlan_ParsedOnce_ConcurrentUse starts and the goroutines race on the first parse.
type coldNest struct {
	Tel *string `json:"tel,omitempty" privacy:"personal,group=contact"`
}

type coldView struct {
	ID      string     `json:"id" privacy:"public"`
	Phone   *string    `json:"phone,omitempty" privacy:"personal,group=contact"`
	Nested  []coldNest `json:"nested,omitempty" privacy:"internal"`
	Dx      *string    `json:"dx,omitempty" privacy:"sensitive,group=medical"`
	Created time.Time  `json:"created,omitzero" privacy:"internal"`
}

func TestPlan_ParsedOnce_ConcurrentUse(t *testing.T) {
	// The plan cache is shared by Check and Filter and must be safe under -race; the types
	// here are parsed by no other test, so concurrent first parses are exercised.
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 50; i++ {
				if err := privacy.Check([]any{coldView{}}, allGroups); err != nil {
					t.Error(err)
					return
				}
				v := coldView{ID: "i", Phone: sp("555-0100"), Dx: sp("asthma")}
				res, err := privacy.Filter(&v, uniform(privacy.AudienceSignedIn), signedIn(bob), privacy.Relation{Subject: alice})
				if err != nil {
					t.Error(err)
					return
				}
				if v.Phone == nil || len(res.Fields) != 2 { // phone, dx
					t.Errorf("v = %+v res = %+v", v, res)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func FuzzFilter(f *testing.F) {
	f.Add(uint8(0), uint8(1), uint8(2), uint8(3), true, true, true, "booked")
	f.Add(uint8(4), uint8(9), uint8(255), uint8(0), false, false, false, "")
	f.Add(uint8(2), uint8(2), uint8(2), uint8(2), true, false, true, "holders")
	auds := []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders, privacy.AudienceSignedIn, "booked", "", "weird"}
	f.Fuzz(func(t *testing.T, a, b, c, d uint8, signed, subject, siteHolder bool, also string) {
		pol := policy(auds[int(a)%len(auds)], auds[int(b)%len(auds)], auds[int(c)%len(auds)], auds[int(d)%len(auds)])
		var v privacy.Viewer
		if signed {
			v.Person = bob
		}
		rel := privacy.Relation{Department: deptA, Also: []privacy.Audience{privacy.Audience(also)}}
		if subject {
			rel.Subject = bob
		}
		if siteHolder {
			v.Extents = map[string]authz.Extent{"contact": {Site: true}}
		}
		rec := newRecord()
		res, err := privacy.Filter(&rec, pol, v, rel)
		if err != nil {
			if !errs.Is(err, errs.Internal) {
				t.Fatalf("unexpected kind %v", errs.KindOf(err))
			}
			return
		}
		if rec.Pub != "pub" {
			t.Fatal("public field zeroed")
		}
		for _, f := range res.Fields {
			if f != "phone" && f != "dx" && f != "note" {
				t.Fatalf("unexpected audited field %q", f)
			}
		}
	})
}
