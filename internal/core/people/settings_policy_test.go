package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// boundaries step 5: people maps its "people.*" keys onto Policy itself, from
// Options.Settings, on every use case. The mapping is unexported, so these tests read it
// back through Me on a person with no row (an empty profile), which carries the Policy the
// use case read and needs no database row.

func meWith(t *testing.T, rd settings.Reader) (people.Profile, error) {
	t.Helper()
	r := newRig(t, dbtest.NewFake(), func(o *people.Options) { o.Settings = rd })
	return r.svc.Me(asPerson(ctxT(t), newPerson())) //nolint:wrapcheck // test helper
}

// Moved from app: TestPeoplePolicy_Defaults_AreDefaultPolicy.
func TestPeoplePolicy_Defaults_AreDefaultPolicy(t *testing.T) {
	prof, err := meWith(t, settingstest.NewFake("people", people.Settings))
	wantNoErr(t, err)
	if prof.Policy != people.DefaultPolicy() {
		t.Errorf("Policy = %+v, want %+v", prof.Policy, people.DefaultPolicy())
	}
}

// A reader of another area declares no people key: the use case fails Invalid, it does not
// fall back to defaults.
func TestPeoplePolicy_WrongArea_ReturnsInvalid(t *testing.T) {
	_, err := meWith(t, settingstest.NewFake("accounts", identity.Settings))
	wantKind(t, err, errs.Invalid)
}

// An unset key is settings' declared default, not people.DefaultPolicy: the declarations
// here carry other defaults and people must follow them.
func TestPolicy_UnsetKey_IsTheDeclaredDefault(t *testing.T) {
	decls := append([]settings.Decl(nil), people.Settings...)
	for i := range decls {
		switch decls[i].Key {
		case "legal_name":
			decls[i].Default = "required"
		case "phone":
			decls[i].Default = "hidden"
		}
	}
	prof, err := meWith(t, settingstest.NewFake("people", decls))
	wantNoErr(t, err)
	want := people.Policy{LegalName: people.FieldRequired, Phone: people.FieldHidden, EmergencyContacts: people.DefaultPolicy().EmergencyContacts}
	if prof.Policy != want {
		t.Errorf("Policy = %+v, want %+v", prof.Policy, want)
	}
}

// Never cached: a changed setting applies to the next use case.
func TestPolicy_ChangedSetting_AppliesToTheNextUseCase(t *testing.T) {
	f := settingstest.NewFake("people", people.Settings)
	r := newRig(t, dbtest.NewFake(), func(o *people.Options) { o.Settings = f })
	ctx := asPerson(ctxT(t), newPerson())
	prof, err := r.svc.Me(ctx)
	wantNoErr(t, err)
	if prof.Policy.Phone == people.FieldHidden {
		t.Fatal("phone should not start hidden")
	}
	f.Set("phone", "hidden")
	prof, err = r.svc.Me(ctx)
	wantNoErr(t, err)
	if prof.Policy.Phone != people.FieldHidden {
		t.Errorf("Policy.Phone = %v after the setting changed, want hidden (the policy was cached)", prof.Policy.Phone)
	}
}
