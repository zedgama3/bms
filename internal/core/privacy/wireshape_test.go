package privacy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Wire-shape parity with encoding/json: nested rows (F17), pointer-receiver marshalers
// (F9b), invalid json names and ambiguous embedded marshalers (C1), and F6e's exact path (C2).

type pEntry struct {
	LegalName string `json:"legal_name"` // untagged
}

type pRoster struct {
	Entries []pEntry `json:"entries,omitempty" privacy:"internal"`
}

type pRosterMap struct {
	Entries map[string]pEntry `json:"entries,omitempty" privacy:"internal"`
}

type pRosterOne struct {
	Entry pEntry `json:"entry,omitzero" privacy:"internal"`
}

type pEntryPlain struct {
	Phone *string `json:"phone" privacy:"personal,group=contact"` // no omit option
}

type pRosterPlain struct {
	Entries []pEntryPlain `json:"entries,omitempty" privacy:"internal"`
}

type pEntryDyn struct {
	Any any `json:"any,omitempty" privacy:"internal"`
}

type pRosterDyn struct {
	Entries []pEntryDyn `json:"entries,omitempty" privacy:"internal"`
}

func TestCheck_NestedProblems_AreReported(t *testing.T) { // F8, F17
	wantField(t, privacy.Check([]any{pRoster{}}, allGroups), "views[0].Entries.LegalName", "required")
	wantField(t, privacy.Check([]any{pRosterMap{}}, allGroups), "views[0].Entries.LegalName", "required")
	wantField(t, privacy.Check([]any{pRosterPlain{}}, allGroups), "views[0].Entries.Phone", "not_omittable")
	wantField(t, privacy.Check([]any{pRosterDyn{}}, allGroups), "views[0].Entries.Any", "dynamic")
}

// ptrMarsh marshals itself through a pointer receiver. encoding/json calls that method only
// for an addressable value, so a value held in a field of a struct passed by value, or in a
// map, encodes as its plain fields (tags and all) and the type is not a safe leaf.
type ptrMarsh struct {
	S string `json:"s,omitempty" privacy:"internal"`
	N string `json:"n"` // untagged
}

func (*ptrMarsh) MarshalJSON() ([]byte, error) { return []byte(`"m"`), nil }

type ptrMarshHolder struct {
	X ptrMarsh `json:"x,omitzero" privacy:"internal"`
}

type ptrMarshMap struct {
	M map[string]ptrMarsh `json:"m,omitempty" privacy:"internal"`
}

type ptrMarshPtr struct {
	X *ptrMarsh `json:"x,omitempty" privacy:"internal"`
}

type valMarsh struct {
	S string `json:"s,omitempty" privacy:"internal"`
	N string `json:"n"` // untagged, hidden behind the marshaler
}

func (valMarsh) MarshalJSON() ([]byte, error) { return []byte(`"m"`), nil }

type valMarshHolder struct {
	X valMarsh `json:"x,omitzero" privacy:"internal"`
}

func hasCode(err error, code string) bool {
	for _, fe := range fieldsOf(err) {
		if fe.Code == code {
			return true
		}
	}
	return false
}

func TestCheck_PointerReceiverMarshaler_ReturnsInvalid(t *testing.T) { // F9b
	// The hazard, proved: by value the pointer method is not used and the raw fields leak.
	b, err := json.Marshal(ptrMarshHolder{X: ptrMarsh{S: "s", N: marker}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), marker) {
		t.Fatalf("test premise broken: encoding/json used the pointer marshaler on a value field: %s", b)
	}
	b, err = json.Marshal(ptrMarshMap{M: map[string]ptrMarsh{"k": {N: marker}}})
	if err != nil || !strings.Contains(string(b), marker) {
		t.Fatalf("test premise broken for map values: %s %v", b, err)
	}

	for name, v := range map[string]any{"value field": ptrMarshHolder{}, "map value": ptrMarshMap{}} {
		t.Run(name, func(t *testing.T) {
			err := privacy.Check([]any{v}, allGroups)
			wantKind(t, err, errs.Invalid)
			if !hasCode(err, "marshaler") {
				t.Errorf("want a marshaler problem; got %v", fieldsOf(err))
			}
		})
	}
}

func TestCheck_MarshalerLeaves_Pass(t *testing.T) {
	// A pointer field of a pointer-receiver marshaler is always called through the pointer,
	// and a value-receiver marshaler is called in every position: both are leaves.
	for name, v := range map[string]any{"pointer field": ptrMarshPtr{}, "value receiver": valMarshHolder{}} {
		t.Run(name, func(t *testing.T) {
			if err := privacy.Check([]any{v}, allGroups); err != nil {
				t.Fatalf("a safe marshaler leaf was rejected: %v", err)
			}
		})
	}
	b, err := json.Marshal(valMarshHolder{X: valMarsh{N: marker}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), marker) {
		t.Fatalf("test premise broken: %s", b)
	}
}

// C1: an invalid json name is ignored by encoding/json (the Go name is used, and an
// embedded struct is promoted); Check must follow.
type badNameInner struct {
	Loose string `json:"loose"` // untagged
}

type badNameEmbed struct {
	badNameInner `json:"a\\b"` //nolint:staticcheck // the invalid name is the point
	ID           string        `json:"id" privacy:"public"`
}

type badNameField struct {
	Phone *string `json:"a\\b,omitempty" privacy:"personal,group=contact"` //nolint:staticcheck // the invalid name is the point
}

func TestCheck_InvalidJSONName_BehavesLikeEncodingJSON(t *testing.T) { // C1
	b, err := json.Marshal(badNameEmbed{badNameInner{Loose: marker}, "i"})
	if err != nil || !strings.Contains(string(b), `"Loose"`) && !strings.Contains(string(b), `"loose"`) {
		t.Fatalf("test premise broken: encoding/json did not promote the embedded struct: %s %v", b, err)
	}
	if err := privacy.Check([]any{badNameEmbed{}}, allGroups); !fieldMentions(err, "Loose") {
		t.Errorf("the promoted untagged field was not reported: %v", fieldsOf(err))
	}
	// The wire name is the Go name "Phone", which breaks the audit grammar.
	b, err = json.Marshal(badNameField{Phone: sp(marker)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"Phone"`) {
		t.Fatalf("test premise broken: %s", b)
	}
	wantField(t, privacy.Check([]any{badNameField{}}, allGroups), "views[0].Phone", "malformed")
}

type mA struct {
	Y string `json:"y,omitempty" privacy:"internal"`
}

func (mA) MarshalJSON() ([]byte, error) { return []byte(`"a"`), nil }

type mB struct {
	Z string `json:"z,omitempty" privacy:"internal"`
}

func (mB) MarshalJSON() ([]byte, error) { return []byte(`"b"`), nil }

type twoMarshalers struct {
	mA
	mB
	ID string `json:"id" privacy:"public"`
}

func TestCheck_AmbiguousEmbeddedMarshalers_ReturnsInvalid(t *testing.T) { // F9c
	// Both MarshalJSON methods are ambiguous, so the outer type has none and encoding/json
	// emits the fields of the struct; Check must still not treat that as a clean view.
	err := privacy.Check([]any{twoMarshalers{}}, allGroups)
	wantKind(t, err, errs.Invalid)
	if !hasCode(err, "marshaler") {
		t.Errorf("want a marshaler problem; got %v", fieldsOf(err))
	}
}

// C2 / F6e: the exact path of a promoted field of an unexported embedded struct.
type inner struct {
	Name string `json:"name"` // untagged
}

type innerHolder struct {
	inner
	ID string `json:"id" privacy:"public"`
}

type innerPtrHolder struct {
	*inner
	ID string `json:"id" privacy:"public"`
}

func TestCheck_UnexportedEmbeddedUntagged_ExactPath(t *testing.T) { // F6e, C2
	wantField(t, privacy.Check([]any{innerHolder{}}, allGroups), "views[0].inner.Name", "required")
	wantField(t, privacy.Check([]any{innerPtrHolder{}}, allGroups), "views[0].inner.Name", "required")
}
