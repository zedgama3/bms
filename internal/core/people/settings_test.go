package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
)

func TestSettings_DeclarationsAreValidAndDefaultToDefaultPolicy(t *testing.T) {
	a := settings.Area{Name: "people", Core: true, Decls: people.Settings}
	if err := a.Validate(); err != nil {
		t.Fatalf("Area.Validate: %v", err)
	}
	def := people.DefaultPolicy()
	want := map[string]people.FieldMode{
		"legal_name": def.LegalName, "phone": def.Phone, "emergency_contacts": def.EmergencyContacts,
	}
	if len(people.Settings) != len(want) {
		t.Errorf("declares %d keys, want %d", len(people.Settings), len(want))
	}
	for _, d := range people.Settings {
		mode, err := people.ParseFieldMode(d.Default)
		if err != nil || mode != want[d.Key] {
			t.Errorf("%s default = %q, want %v", d.Key, d.Default, want[d.Key])
		}
		// Every option is a mode the policy accepts for that key.
		for _, o := range d.Options {
			m, err := people.ParseFieldMode(o)
			if err != nil {
				t.Errorf("%s option %q is not a field mode", d.Key, o)
				continue
			}
			pol := people.DefaultPolicy()
			switch d.Key {
			case "legal_name":
				pol.LegalName = m
			case "phone":
				pol.Phone = m
			case "emergency_contacts":
				pol.EmergencyContacts = m
			}
			if err := pol.Validate(); err != nil {
				t.Errorf("%s option %q: Policy.Validate: %v", d.Key, o, err)
			}
		}
	}
}

func TestSettings_EmergencyContactsOffersNoRequiredMode(t *testing.T) {
	for _, d := range people.Settings {
		if d.Key != "emergency_contacts" {
			continue
		}
		for _, o := range d.Options {
			if o == "required" {
				t.Errorf("emergency_contacts offers %q; Policy.Validate refuses it (Q-CORE7)", o)
			}
		}
		return
	}
	t.Fatal("people.Settings has no emergency_contacts")
}
