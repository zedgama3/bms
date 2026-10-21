package privacy_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// These tests pin that Check and Filter see exactly the fields encoding/json puts on the
// wire (privacy-tags, Behavior; code review M1, M2, M4). Each asserts through json.Marshal
// of the filtered value, never through the plan.

var contactGroups = []privacy.Group{gContact}

// hiddenPolicy shows the contact group to holders only; the zero Viewer is no holder.
var hiddenPolicy = privacy.Policy{"contact": privacy.AudienceHolders}

func marshalString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

// fieldMentions reports whether any field error path contains frag.
func fieldMentions(err error, frag string) bool {
	for _, fe := range fieldsOf(err) {
		if strings.Contains(fe.Field, frag) {
			return true
		}
	}
	return false
}

// M1: an embedded pointer to an unexported struct is promoted by encoding/json (marshal
// works; only unmarshal fails), so its fields are on the wire.
type m1Inner struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
	Label string  `json:"label" privacy:"public"`
}

type M1View struct {
	*m1Inner
	ID string `json:"id" privacy:"public"`
}

type m1BadInner struct {
	Loose string `json:"loose"` // untagged
}

type M1BadView struct {
	*m1BadInner
	ID string `json:"id" privacy:"public"`
}

func TestCheck_EmbeddedPointerToUnexportedStruct_SeesInnerFields(t *testing.T) {
	// Precondition: encoding/json really emits the inner field.
	if s := marshalString(t, M1BadView{m1BadInner: &m1BadInner{Loose: marker}, ID: "x"}); !strings.Contains(s, marker) {
		t.Fatalf("test premise broken: encoding/json omitted the embedded field: %s", s)
	}
	err := privacy.Check([]any{M1BadView{}}, nil)
	wantKind(t, err, errs.Invalid)
	if !fieldMentions(err, "Loose") {
		t.Errorf("untagged inner field not reported; got %v", fieldsOf(err))
	}
	if err := privacy.Check([]any{M1View{}}, contactGroups); err != nil {
		t.Errorf("a well-tagged embedded pointer view: %v", err)
	}
}

// M2: an unexported embedded struct with a json name is an ordinary named field.
type m2Inner struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
	Label string  `json:"label" privacy:"public"`
}

type M2View struct {
	m2Inner `json:"inner" privacy:"public"`
	ID      string `json:"id" privacy:"public"`
}

type m2BadInner struct {
	Loose string `json:"loose"`
}

type M2BadView struct {
	m2BadInner `json:"inner" privacy:"public"`
	ID         string `json:"id" privacy:"public"`
}

func TestCheck_UnexportedEmbeddedWithJSONName_WalksAsNamedField(t *testing.T) {
	if s := marshalString(t, M2BadView{m2BadInner: m2BadInner{Loose: marker}, ID: "x"}); !strings.Contains(s, `"inner":{`) {
		t.Fatalf("test premise broken: encoding/json did not emit a named inner object: %s", s)
	}
	err := privacy.Check([]any{M2BadView{}}, nil)
	wantKind(t, err, errs.Invalid)
	if !fieldMentions(err, "Loose") {
		t.Errorf("untagged nested field not reported; got %v", fieldsOf(err))
	}
	if err := privacy.Check([]any{M2View{}}, contactGroups); err != nil {
		t.Errorf("well-tagged view: %v", err)
	}
}

// Spec F29 / decision 4: Filter does not enter a named struct field, and an embedded struct
// with a json name is one; the caller filters the nested record first. Filter must still
// succeed and decide the container's own fields. (The brief asked for the nested field to be
// hidden; the approved spec says untouched, so this pins the spec.)
func TestFilter_UnexportedEmbeddedWithJSONName_NotEntered(t *testing.T) {
	v := M2View{m2Inner: m2Inner{Phone: sp(marker), Label: "shown"}, ID: "1"}
	if _, err := privacy.Filter(&v, hiddenPolicy, privacy.Viewer{}, privacy.Relation{}); err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if v.Phone == nil || *v.Phone != marker {
		t.Errorf("Filter entered a named struct field: %+v", v)
	}
	// The caller's own call on the nested record hides it on the wire.
	if _, err := privacy.Filter(&v.m2Inner, hiddenPolicy, privacy.Viewer{}, privacy.Relation{}); err != nil {
		t.Fatalf("Filter(nested): %v", err)
	}
	body := marshalString(t, v)
	privacytest.AssertHidden(t, []byte(body), "phone")
	privacytest.AssertPresent(t, []byte(body), "id", "inner")
}

// M4: Filter refuses a type Check rejects for not_omittable and leaves *v untouched.
type M4View struct {
	Pub   string  `json:"pub" privacy:"public"`
	Phone *string `json:"phone" privacy:"personal,group=contact"` // no omitempty
}

func TestFilter_GroupedFieldNotOmittable_ReturnsInternalAndLeavesValueUntouched(t *testing.T) {
	wantField(t, privacy.Check([]any{M4View{}}, contactGroups), "views[0].Phone", "not_omittable")
	v := M4View{Pub: "p", Phone: sp(marker)}
	before := v
	_, err := privacy.Filter(&v, hiddenPolicy, privacy.Viewer{}, privacy.Relation{})
	wantKind(t, err, errs.Internal)
	if !reflect.DeepEqual(v, before) || v.Phone == nil || *v.Phone != marker {
		t.Errorf("*v changed on failure: %+v", v)
	}
}
