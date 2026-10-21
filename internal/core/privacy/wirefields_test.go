package privacy_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// unexportedBase is embedded by pointer and by value below; encoding/json promotes its
// exported fields although the embedded type is unexported.
type unexportedBase struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
}

type embedValUnexported struct {
	unexportedBase
	Pub string `json:"pub" privacy:"public"`
}

type embedNamedUnexported struct {
	unexportedBase `json:"base" privacy:"public"`
	Pub            string `json:"pub" privacy:"public"`
}

type embedNamedUnexportedInternal struct {
	unexportedBase `json:"base,omitzero" privacy:"internal"`
	Pub            string `json:"pub" privacy:"public"`
}

type neverListed struct {
	Phone *string `json:"phone" privacy:"personal,group=contact"`
}

func TestFilter_NamedUnexportedEmbeddedStruct_IsAFieldNotPromoted(t *testing.T) {
	// The wire has "base" (a named field), not "phone". Public is allowed: it is never hidden.
	rel := privacy.Relation{Subject: alice}
	v := embedNamedUnexported{unexportedBase: unexportedBase{Phone: sp("555-0100")}, Pub: "pub"}
	for name, viewer := range map[string]privacy.Viewer{"signed in": signedIn(bob), "anonymous": {}} {
		if _, err := privacy.Filter(&v, uniform(privacy.AudienceNobody), viewer, rel); err != nil {
			t.Fatalf("%s: a public named field needs no hiding: %v", name, err)
		}
	}
	body := marshalString(t, v)
	privacytest.AssertPresent(t, []byte(body), "base", "pub")
	// At any other level it cannot be hidden (it cannot be set): Check rejects it (F6d) and
	// Filter fails closed for every viewer (F17a).
	w := embedNamedUnexportedInternal{unexportedBase: unexportedBase{Phone: sp("555-0100")}, Pub: "pub"}
	wantField(t, privacy.Check([]any{w}, allGroups), "views[0].unexportedBase", "unsettable")
	for name, viewer := range map[string]privacy.Viewer{"signed in": signedIn(bob), "anonymous": {}} {
		if _, err := privacy.Filter(&w, uniform(privacy.AudienceNobody), viewer, rel); !errs.Is(err, errs.Internal) {
			t.Fatalf("%s: Filter = %v, want Internal", name, err)
		}
	}
}

func TestFilter_GroupedFieldWithoutOmitempty_ReturnsInternal(t *testing.T) {
	v := neverListed{Phone: sp("555-0100")}
	_, err := privacy.Filter(&v, uniform(privacy.AudienceNobody), signedIn(alice), privacy.Relation{Subject: alice})
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("Filter = %v, want Internal", err)
	}
	if v.Phone == nil {
		t.Fatal("*v was changed")
	}
}
