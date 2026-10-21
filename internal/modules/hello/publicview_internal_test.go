package hello

import (
	"encoding/json"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// publicViewFixture is a test-only response type: a public field and an internal one that
// is omittable. The public route must never ship the internal one.
type publicViewFixture struct {
	Greeting string `json:"greeting" privacy:"public"`
	Note     string `json:"note,omitempty" privacy:"internal"`
}

// F38 (A2.7): publicView hides a non-public field, and the key is absent from the body.
func TestPublicView_HidesNonPublicField(t *testing.T) {
	v := publicViewFixture{Greeting: "hi", Note: "for signed-in eyes"}
	if err := publicView(&v); err != nil {
		t.Fatalf("publicView: %v", err)
	}
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	privacytest.AssertHidden(t, body, "note")
	privacytest.AssertPresent(t, body, "greeting")
	if string(body) != `{"greeting":"hi"}` {
		t.Errorf("body = %s", body)
	}
}

// publicView on the real public types keeps every public field.
func TestPublicView_KeepsPublicFields(t *testing.T) {
	v := PublicGreeting{Greeting: "hi", Banners: []Banner{{Module: "m", Key: "k"}}}
	if err := publicView(&v); err != nil {
		t.Fatalf("publicView: %v", err)
	}
	if v.Greeting != "hi" || len(v.Banners) != 1 || v.Banners[0].Key != "k" {
		t.Errorf("v = %+v", v)
	}
}

// publicView on a type that skipped Check fails closed (Internal) and changes nothing.
func TestPublicView_UncheckedType_ReturnsInternal(t *testing.T) {
	type unchecked struct {
		Greeting string `json:"greeting" privacy:"public"`
		Note     string `json:"note" privacy:"internal"` // not omittable: a placeholder if hidden
	}
	v := unchecked{Greeting: "hi", Note: "n"}
	if err := publicView(&v); !errs.Is(err, errs.Internal) {
		t.Fatalf("publicView = %v, want Internal", err)
	}
	if v.Note != "n" {
		t.Errorf("*v changed on failure: %+v", v)
	}
}
