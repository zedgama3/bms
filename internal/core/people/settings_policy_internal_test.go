package people

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/settings/settingstest"
)

// Moved from app: TestPeoplePolicy_ReadsEveryKey. The whole Policy.
func TestPeoplePolicy_ReadsEveryKey(t *testing.T) {
	r := settingstest.NewFake("people", Settings)
	r.Set("legal_name", "required")
	r.Set("phone", "hidden")
	r.Set("emergency_contacts", "hidden")
	got, err := policyFrom(context.Background(), r)
	if err != nil {
		t.Fatalf("policyFrom: %v", err)
	}
	want := Policy{LegalName: FieldRequired, Phone: FieldHidden, EmergencyContacts: FieldHidden}
	if got != want {
		t.Errorf("Policy = %+v, want %+v", got, want)
	}
}
