package privacy_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestCheck_WellTaggedType_Passes(t *testing.T) {
	if err := privacy.Check([]any{Record{}}, allGroups); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := privacy.Check([]any{&Record{}}, allGroups); err != nil {
		t.Fatalf("Check(pointer view): %v", err)
	}
	if err := privacy.Check(nil, nil); err != nil {
		t.Fatalf("Check(nil, nil): %v", err)
	}
	if err := privacy.Check([]any{}, allGroups); err != nil {
		t.Fatalf("Check(empty): %v", err)
	}
}

type untagged struct {
	Name  string `json:"name"`
	Other string `json:"other" privacy:"public"`
}

type untaggedNoJSONTag struct {
	Name string
}

func TestCheck_UntaggedField_ReturnsInvalid(t *testing.T) { // F1
	err := privacy.Check([]any{untagged{}}, nil)
	wantField(t, err, "views[0].Name", "required")
	if hasField(err, "views[0].Other", "required") {
		t.Error("a tagged field was reported")
	}
	// An exported field with no json tag is on the wire under its Go name.
	wantField(t, privacy.Check([]any{untaggedNoJSONTag{}}, nil), "views[0].Name", "required")
}

func TestCheck_ListsEveryProblemAcrossViews(t *testing.T) {
	type twoBad struct {
		A string `json:"a"`
		B string `json:"b" privacy:"bogus"`
	}
	err := privacy.Check([]any{twoBad{}, Record{}, untagged{}}, allGroups)
	wantField(t, err, "views[0].A", "required")
	wantField(t, err, "views[0].B", "one_of")
	wantField(t, err, "views[2].Name", "required")
}

func TestCheck_JSONDashField_Exempt(t *testing.T) { // F2
	type onlyDash struct {
		ID     string `json:"id" privacy:"public"`
		Person string `json:"-"`
	}
	if err := privacy.Check([]any{onlyDash{}}, nil); err != nil {
		t.Fatalf("a json:\"-\" field must be exempt: %v", err)
	}
	// json:"-," names the key "-": on the wire, so it needs a tag.
	type dashKey struct {
		Dash string `json:"-,"` //nolint:staticcheck // SA5008 is the point: the key "-" is on the wire
	}
	wantField(t, privacy.Check([]any{dashKey{}}, nil), "views[0].Dash", "required")
	// Unexported fields are not on the wire.
	type unexported struct {
		ID  string `json:"id" privacy:"public"`
		hid string //nolint:unused // the point of the test
	}
	if err := privacy.Check([]any{unexported{}}, nil); err != nil {
		t.Fatalf("an unexported field must be exempt: %v", err)
	}
}

func TestCheck_BadLevel_ReturnsInvalid(t *testing.T) { // F3
	cases := map[string]any{
		"unknown name": struct {
			F string `json:"f" privacy:"secret"`
		}{},
		"empty tag": struct {
			F string `json:"f" privacy:""`
		}{},
		"wrong case": struct {
			F string `json:"f" privacy:"Public"`
		}{},
		"only a group": struct {
			F *string `json:"f,omitempty" privacy:"group=contact"`
		}{},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			err := privacy.Check([]any{v}, allGroups)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid (%v)", errs.KindOf(err), err)
			}
		})
	}
	wantField(t, privacy.Check([]any{struct {
		F string `json:"f" privacy:"secret"`
	}{}}, nil), "views[0].F", "one_of")
}

func TestCheck_MalformedTag_ReturnsInvalid(t *testing.T) {
	cases := map[string]any{
		"empty group": struct {
			F *string `json:"f,omitempty" privacy:"personal,group="`
		}{},
		"unknown key": struct {
			F *string `json:"f,omitempty" privacy:"personal,grp=contact"`
		}{},
		"trailing comma": struct {
			F *string `json:"f,omitempty" privacy:"personal,"`
		}{},
		"two groups": struct {
			F *string `json:"f,omitempty" privacy:"personal,group=contact,group=nick"`
		}{},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			err := privacy.Check([]any{v}, allGroups)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid (%v)", errs.KindOf(err), err)
			}
		})
	}
}

func TestCheck_UnknownGroup_ReturnsInvalid(t *testing.T) { // F4
	type v struct {
		F *string `json:"f,omitempty" privacy:"personal,group=nosuch"`
	}
	wantField(t, privacy.Check([]any{v{}}, allGroups), "views[0].F", "unknown")
	wantField(t, privacy.Check([]any{v{}}, nil), "views[0].F", "unknown")
	// A public or internal field may name a group, and the group must still exist.
	type w struct {
		F *string `json:"f,omitempty" privacy:"internal,group=nosuch"`
	}
	wantField(t, privacy.Check([]any{w{}}, allGroups), "views[0].F", "unknown")
}

func TestCheck_PersonalWithoutGroup_ReturnsInvalid(t *testing.T) { // F5
	tests := []struct {
		name string
		view any
		want bool
	}{
		{"personal", struct {
			F string `json:"f" privacy:"personal"`
		}{}, true},
		{"sensitive", struct {
			F string `json:"f" privacy:"sensitive"`
		}{}, true},
		{"public ungrouped", struct {
			F string `json:"f" privacy:"public"`
		}{}, false},
		{"internal ungrouped", struct {
			F string `json:"f,omitempty" privacy:"internal"`
		}{}, false},
		{"personal grouped", struct {
			F *string `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, false},
		{"public grouped", struct {
			F *string `json:"f,omitempty" privacy:"public,group=nick"`
		}{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := privacy.Check([]any{tc.view}, allGroups)
			if tc.want {
				wantField(t, err, "views[0].F", "ungrouped")
				return
			}
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
		})
	}
}

type structVal struct {
	X string `json:"x" privacy:"public"`
}

func TestCheck_NotOmittable_ReturnsInvalid(t *testing.T) { // F6 (A2.1, A2.2)
	tests := []struct {
		name string
		view any
		ok   bool
	}{
		{"string plain", struct {
			F string `json:"f" privacy:"personal,group=contact"`
		}{}, false},
		{"int plain", struct {
			F int `json:"f" privacy:"personal,group=contact"`
		}{}, false},
		{"struct plain", struct {
			F structVal `json:"f" privacy:"personal,group=contact"`
		}{}, false},
		{"string omitempty", struct {
			F string `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, true},
		{"struct omitempty", struct {
			F structVal `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, false},
		{"struct omitzero", struct {
			F structVal `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"time.Time omitempty", struct {
			F time.Time `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, false},
		{"time.Time omitzero", struct {
			F time.Time `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"model.ID omitempty", struct {
			F model.PersonID `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, false},
		{"model.ID omitzero", struct {
			F model.PersonID `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"array omitempty", struct {
			F [2]int `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, false},
		{"array omitzero", struct {
			F [2]int `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"pointer plain", struct {
			F *string `json:"f" privacy:"personal,group=contact"`
		}{}, false},
		{"slice plain", struct {
			F []string `json:"f" privacy:"personal,group=contact"`
		}{}, false},
		{"map plain", struct {
			F map[string]string `json:"f" privacy:"personal,group=contact"`
		}{}, false},
		{"pointer omitempty", struct {
			F *string `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, true},
		{"pointer omitzero", struct {
			F *string `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"slice omitempty", struct {
			F []string `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, true},
		{"slice omitzero", struct {
			F []string `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"map omitempty", struct {
			F map[string]string `json:"f,omitempty" privacy:"personal,group=contact"`
		}{}, true},
		{"map omitzero", struct {
			F map[string]string `json:"f,omitzero" privacy:"personal,group=contact"`
		}{}, true},
		{"public grouped pointer plain", struct {
			F *string `json:"f" privacy:"public,group=nick"`
		}{}, false},
		{"public grouped string plain", struct {
			F string `json:"f" privacy:"public,group=nick"`
		}{}, false},
		{"ungrouped internal pointer plain", struct {
			F *string `json:"f" privacy:"internal"`
		}{}, false},
		{"ungrouped internal scalar plain", struct {
			F string `json:"f" privacy:"internal"`
		}{}, false},
		{"ungrouped internal struct plain", struct {
			F structVal `json:"f" privacy:"internal"`
		}{}, false},
		{"ungrouped internal struct omitempty", struct {
			F structVal `json:"f,omitempty" privacy:"internal"`
		}{}, false},
		{"ungrouped internal scalar omitempty", struct {
			F string `json:"f,omitempty" privacy:"internal"`
		}{}, true},
		{"ungrouped internal struct omitzero", struct {
			F structVal `json:"f,omitzero" privacy:"internal"`
		}{}, true},
		{"ungrouped public scalar plain", struct {
			F string `json:"f" privacy:"public"`
		}{}, true},
		{"ungrouped public struct plain", struct {
			F structVal `json:"f" privacy:"public"`
		}{}, true},
		{"ungrouped public pointer plain", struct {
			F *string `json:"f" privacy:"public"`
		}{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := privacy.Check([]any{tc.view}, allGroups)
			if tc.ok {
				if err != nil {
					t.Fatalf("Check: %v", err)
				}
				return
			}
			wantField(t, err, "views[0].F", "not_omittable")
		})
	}
}

// F6b: a view that marshals itself (A2.5a).
type selfJSON struct {
	ID string `json:"id" privacy:"public"`
}

func (selfJSON) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

type selfText struct {
	ID string `json:"id" privacy:"public"`
}

func (selfText) MarshalText() ([]byte, error) { return []byte("x"), nil }

type promotedTime struct {
	time.Time
	ID string `json:"id" privacy:"public"`
}

func TestCheck_SelfMarshalingView_ReturnsInvalid(t *testing.T) { // F6b
	for name, v := range map[string]any{
		"MarshalJSON": selfJSON{}, "pointer to MarshalJSON": &selfJSON{},
		"MarshalText": selfText{}, "promoted from embedded time.Time": promotedTime{},
	} {
		t.Run(name, func(t *testing.T) {
			wantField(t, privacy.Check([]any{v}, nil), "views[0]", "marshaler")
		})
	}
	// A nested field of a marshaler type is a leaf and stays allowed (F9).
	type ok struct {
		At time.Time `json:"at,omitzero" privacy:"internal"`
	}
	if err := privacy.Check([]any{ok{}}, nil); err != nil {
		t.Fatalf("a marshaler field is a leaf: %v", err)
	}
}

func TestCheck_InterfaceField_ReturnsInvalid(t *testing.T) { // F6c
	tests := map[string]any{
		"any, public": struct {
			F any `json:"f" privacy:"public"`
		}{},
		"any, internal": struct {
			F any `json:"f,omitempty" privacy:"internal"`
		}{},
		"error-like interface": struct {
			F fmt.Stringer `json:"f,omitempty" privacy:"internal"`
		}{},
		"nested in a slice element": struct {
			Rows []struct {
				F any `json:"f,omitempty" privacy:"internal"`
			} `json:"rows,omitempty" privacy:"internal"`
		}{},
	}
	for name, v := range tests {
		t.Run(name, func(t *testing.T) {
			err := privacy.Check([]any{v}, nil)
			wantKind(t, err, errs.Invalid)
			found := false
			for _, fe := range fieldsOf(err) {
				if fe.Code == "dynamic" {
					found = true
				}
			}
			if !found {
				t.Errorf("no dynamic code in %v", err)
			}
		})
	}
	// json.RawMessage tagged as a whole is a leaf (a marshaler) and passes.
	type raw struct {
		R json.RawMessage `json:"r,omitempty" privacy:"internal"`
	}
	if err := privacy.Check([]any{raw{}}, nil); err != nil {
		t.Fatalf("json.RawMessage is a leaf: %v", err)
	}
}

type namedInner struct {
	X string `json:"x,omitempty" privacy:"public"`
}

func TestCheck_NamedUnexportedEmbedded_ReturnsInvalid(t *testing.T) { // F6d
	type internalNamed struct {
		namedInner `json:"inner,omitzero" privacy:"internal"`
		ID         string `json:"id" privacy:"public"`
	}
	type groupedNamed struct {
		namedInner `json:"inner,omitzero" privacy:"personal,group=contact"`
		ID         string `json:"id" privacy:"public"`
	}
	type publicNamed struct {
		namedInner `json:"inner" privacy:"public"`
		ID         string `json:"id" privacy:"public"`
	}
	wantField(t, privacy.Check([]any{internalNamed{}}, allGroups), "views[0].namedInner", "unsettable")
	wantField(t, privacy.Check([]any{groupedNamed{}}, allGroups), "views[0].namedInner", "unsettable")
	if err := privacy.Check([]any{publicNamed{}}, allGroups); err != nil {
		t.Fatalf("a public named unexported embedded field is never zeroed and is allowed: %v", err)
	}
}

type e6Inner struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
	Note  string  `json:"note" privacy:"internal"` // not omittable
}

type e6Val struct {
	e6Inner
	ID string `json:"id" privacy:"public"`
}

type e6Ptr struct {
	*e6Inner
	ID string `json:"id" privacy:"public"`
}

func TestCheck_UnexportedEmbeddedWalked(t *testing.T) { // F6e
	// encoding/json promotes the fields of an unexported embedded struct (and of a pointer
	// to one); Check must see the same fields.
	for name, v := range map[string]any{"value": e6Val{}, "pointer": e6Ptr{}} {
		t.Run(name, func(t *testing.T) {
			err := privacy.Check([]any{v}, allGroups)
			wantKind(t, err, errs.Invalid)
			if !fieldMentions(err, "Note") {
				t.Errorf("promoted field not checked; got %v", fieldsOf(err))
			}
			if fieldMentions(err, "Phone") {
				t.Errorf("a well-tagged promoted field was reported: %v", fieldsOf(err))
			}
		})
	}
}

func TestCheck_InterfaceOmitempty_NotReportedAsNotOmittable(t *testing.T) { // F6 row; F6c rejects it as dynamic
	err := privacy.Check([]any{struct {
		F any `json:"f,omitempty" privacy:"personal,group=contact"`
	}{}}, allGroups)
	if hasField(err, "views[0].F", "not_omittable") {
		t.Errorf("an interface with omitempty is omittable: %v", fieldsOf(err))
	}
	wantField(t, err, "views[0].F", "dynamic")
}

func TestCheck_NotStruct_ReturnsInvalid(t *testing.T) { // F7
	n := 3
	for name, v := range map[string]any{
		"int": 42, "string": "x", "slice": []string{"a"}, "map": map[string]int{}, "pointer to int": &n, "nil": nil,
	} {
		t.Run(name, func(t *testing.T) {
			wantField(t, privacy.Check([]any{v}, nil), "views[0]", "not_struct")
		})
	}
	wantField(t, privacy.Check([]any{Record{}, 7}, allGroups), "views[1]", "not_struct")
}

type badLeaf struct {
	LegalName string `json:"legal_name"`
}

type nestedContainer struct {
	ID      string    `json:"id" privacy:"public"`
	Entries []badLeaf `json:"entries,omitempty" privacy:"internal"`
}

type nestedNamed struct {
	Lead badLeaf `json:"lead,omitzero" privacy:"internal"`
}

type nestedPtr struct {
	Lead *badLeaf `json:"lead,omitempty" privacy:"internal"`
}

type nestedMap struct {
	ByDay map[string]badLeaf `json:"by_day,omitempty" privacy:"internal"`
}

type nestedPtrSlice struct {
	Entries []*badLeaf `json:"entries,omitempty" privacy:"internal"`
}

type embeddedBad struct {
	BadEmbedded
	ID string `json:"id" privacy:"public"`
}

// BadEmbedded is exported so that its fields are promoted onto the wire.
type BadEmbedded struct {
	LegalName string `json:"legal_name"`
}

func TestCheck_WalksNestedTypes(t *testing.T) { // F8
	tests := []struct {
		name string
		view any
		path string
	}{
		{"slice element", nestedContainer{}, "views[0].Entries.LegalName"},
		{"named struct", nestedNamed{}, "views[0].Lead.LegalName"},
		{"pointer to struct", nestedPtr{}, "views[0].Lead.LegalName"},
		{"map value", nestedMap{}, "views[0].ByDay.LegalName"},
		{"slice of pointers", nestedPtrSlice{}, "views[0].Entries.LegalName"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantField(t, privacy.Check([]any{tc.view}, nil), tc.path, "required")
		})
	}
	t.Run("embedded struct", func(t *testing.T) {
		err := privacy.Check([]any{embeddedBad{}}, nil)
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("kind = %v, want Invalid (%v)", errs.KindOf(err), err)
		}
	})
	t.Run("well tagged nested passes", func(t *testing.T) {
		type container struct {
			Entries []Record          `json:"entries,omitempty" privacy:"internal"`
			Lead    *Record           `json:"lead,omitempty" privacy:"internal"`
			ByDay   map[string]Record `json:"by_day,omitempty" privacy:"internal"`
		}
		if err := privacy.Check([]any{container{}}, allGroups); err != nil {
			t.Fatalf("Check: %v", err)
		}
	})
}

// customJSON implements json.Marshaler, so its untagged fields are not the wire shape.
type customJSON struct{ Raw string }

func (c customJSON) MarshalJSON() ([]byte, error) { return json.Marshal(c.Raw) } //nolint:wrapcheck // test fixture

type customText struct{ Raw string }

func (c customText) MarshalText() ([]byte, error) { return []byte(c.Raw), nil }

func TestCheck_MarshalerIsLeaf(t *testing.T) { // F9
	type v struct {
		At     time.Time         `json:"at,omitzero" privacy:"internal"`
		Who    model.PersonID    `json:"who,omitzero" privacy:"internal"`
		Level  model.Sensitivity `json:"level,omitempty" privacy:"internal"`
		Custom customJSON        `json:"custom,omitzero" privacy:"internal"`
		Text   customText        `json:"text,omitzero" privacy:"internal"`
		PtrAt  *time.Time        `json:"ptr_at,omitempty" privacy:"internal"`
		Many   []customJSON      `json:"many,omitempty" privacy:"internal"`
	}
	if err := privacy.Check([]any{v{}}, nil); err != nil {
		t.Fatalf("marshalers must be leaves: %v", err)
	}
	// A leaf still needs its own tag and the option its kind needs, grouped or not.
	type g struct {
		At time.Time `json:"at" privacy:"personal,group=contact"`
	}
	wantField(t, privacy.Check([]any{g{}}, allGroups), "views[0].At", "not_omittable")
	type h struct {
		At time.Time `json:"at,omitempty" privacy:"personal,group=contact"`
	}
	wantField(t, privacy.Check([]any{h{}}, allGroups), "views[0].At", "not_omittable")
	type u struct {
		At time.Time `json:"at" privacy:"internal"`
	}
	wantField(t, privacy.Check([]any{u{}}, nil), "views[0].At", "not_omittable")
}

// nestedDeep holds the group's highest level one type down.
type gl1 struct {
	Dx *string `json:"dx,omitempty" privacy:"sensitive,group=medical"`
}

type glOuter struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
	Inner []gl1   `json:"inner,omitempty" privacy:"internal"`
}

type glEmbedded struct {
	gl1
	Nick *string `json:"nick,omitempty" privacy:"public,group=nick"`
}

func TestGroupLevels_HighestAcrossViewsAndNested(t *testing.T) { // F9a
	got := privacy.GroupLevels([]any{glOuter{}, &glEmbedded{}, Record{}})
	want := map[string]model.Sensitivity{
		"contact": model.SensitivityPersonal,
		"medical": model.SensitivitySensitive,
		"roster":  model.SensitivityPersonal,
		"nick":    model.SensitivityInternal, // Record.Nick internal beats glEmbedded's public
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GroupLevels = %v, want %v", got, want)
	}
	// Highest wins regardless of order.
	got = privacy.GroupLevels([]any{Record{}, glEmbedded{}})
	if got["nick"] != model.SensitivityInternal || got["medical"] != model.SensitivitySensitive {
		t.Errorf("order mattered: %v", got)
	}
	// A level only appears through a nested type.
	only := privacy.GroupLevels([]any{struct {
		Rows []gl1 `json:"rows,omitempty" privacy:"internal"`
	}{}})
	if only["medical"] != model.SensitivitySensitive || len(only) != 1 {
		t.Errorf("nested-only = %v", only)
	}
}

func TestGroupLevels_SkipsNonStruct(t *testing.T) { // F9a
	n := 3
	got := privacy.GroupLevels([]any{nil, 42, "x", &n, []string{"a"}, glOuter{}})
	if len(got) != 2 || got["contact"] != model.SensitivityPersonal || got["medical"] != model.SensitivitySensitive {
		t.Errorf("GroupLevels = %v", got)
	}
	if got := privacy.GroupLevels(nil); got == nil || len(got) != 0 {
		t.Errorf("GroupLevels(nil) = %#v, want an empty map", got)
	}
}

func manyFields(n int, tag string) any {
	fields := make([]reflect.StructField, n)
	for i := range fields {
		fields[i] = reflect.StructField{
			Name: fmt.Sprintf("F%03d", i),
			Type: reflect.TypeOf((*string)(nil)),
			Tag:  reflect.StructTag(fmt.Sprintf(`json:"f%03d,omitempty" privacy:"%s"`, i, tag)),
		}
	}
	return reflect.New(reflect.StructOf(fields)).Elem().Interface()
}

func TestCheck_TooManyAuditable_ReturnsInvalid(t *testing.T) { // F10
	limit := audit.MaxFields
	if err := privacy.Check([]any{manyFields(limit, "personal,group=contact")}, allGroups); err != nil {
		t.Fatalf("exactly MaxFields auditable fields must pass: %v", err)
	}
	err := privacy.Check([]any{manyFields(limit+1, "personal,group=contact")}, allGroups)
	wantKind(t, err, errs.Invalid)
	found := false
	for _, fe := range fieldsOf(err) {
		if fe.Code == "too_many" {
			found = true
		}
	}
	if !found {
		t.Errorf("no too_many code in %v", err)
	}
	if err := privacy.Check([]any{manyFields(limit+1, "sensitive,group=medical")}, allGroups); !errs.Is(err, errs.Invalid) {
		t.Errorf("sensitive fields count too: %v", err)
	}
	// Non-auditable (internal) fields do not count.
	if err := privacy.Check([]any{manyFields(limit+10, "internal,group=nick")}, allGroups); err != nil {
		t.Errorf("internal fields are not audited: %v", err)
	}
}

func TestCheck_AuditableNameTooLong_ReturnsInvalid(t *testing.T) { // F10a
	mk := func(name, tag string) any {
		return reflect.New(reflect.StructOf([]reflect.StructField{{
			Name: "F", Type: reflect.TypeOf((*string)(nil)),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" privacy:"%s"`, name, tag)),
		}})).Elem().Interface()
	}
	atMax := strings.Repeat("a", audit.MaxFieldBytes)
	over := strings.Repeat("a", audit.MaxFieldBytes+1)
	for _, tag := range []string{"personal,group=contact", "sensitive,group=medical"} {
		if err := privacy.Check([]any{mk(atMax, tag)}, allGroups); err != nil {
			t.Errorf("%s: a name of exactly MaxFieldBytes must pass: %v", tag, err)
		}
		wantField(t, privacy.Check([]any{mk(over, tag)}, allGroups), "views[0].F", "too_long")
	}
	// An internal field is not audited, so its name is not bound by audit's limit.
	if err := privacy.Check([]any{mk(over, "internal,group=nick")}, allGroups); err != nil {
		t.Errorf("a non-auditable name is not limited: %v", err)
	}
}

func TestCheck_AuditableNameNotAuditGrammar_ReturnsInvalid(t *testing.T) { // F10b
	mk := func(name, tag string) any {
		return reflect.New(reflect.StructOf([]reflect.StructField{{
			Name: "F", Type: reflect.TypeOf((*string)(nil)),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" privacy:"%s"`, name, tag)),
		}})).Elem().Interface()
	}
	bad := []string{"legalName", "legal-name", "Legal", "1abc", "_abc", "a.b", "a b"}
	for _, name := range bad {
		for _, tag := range []string{"personal,group=contact", "sensitive,group=medical"} {
			t.Run(name+"/"+tag, func(t *testing.T) {
				wantField(t, privacy.Check([]any{mk(name, tag)}, allGroups), "views[0].F", "malformed")
				// The two grammars must not drift apart: audit refuses the same name.
				e := audit.Entry{Action: "hello.record.read", Outcome: audit.Succeeded,
					Fields: []string{name}, Level: model.SensitivityPersonal}
				if err := e.Validate(); err == nil {
					t.Errorf("audit.Entry accepts %q, so Check should not reject it", name)
				}
			})
		}
	}
	for _, name := range []string{"phone", "legal_name", "a1_b2", "x"} {
		if err := privacy.Check([]any{mk(name, "personal,group=contact")}, allGroups); err != nil {
			t.Errorf("%q fits audit's grammar and must pass: %v", name, err)
		}
		e := audit.Entry{Action: "hello.record.read", Outcome: audit.Succeeded,
			Fields: []string{name}, Level: model.SensitivityPersonal}
		if err := e.Validate(); err != nil {
			t.Errorf("audit rejects %q: %v", name, err)
		}
	}
	// Non-auditable names are not bound by audit's grammar.
	if err := privacy.Check([]any{mk("legalName", "internal,group=nick")}, allGroups); err != nil {
		t.Errorf("an internal field's name is free: %v", err)
	}
}
