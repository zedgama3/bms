//go:build integration

package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// F65 against a real database.
func TestLanguageOf_Table(t *testing.T) {
	r := newIRig(t, false)
	es := r.insertPerson(seed{Playa: mPlaya})
	r.exec("UPDATE core.person SET language = 'es-MX' WHERE id = $1", [16]byte(es))
	none := r.insertPerson(seed{Playa: mPlaya + " Two"})
	tests := []struct {
		name    string
		p       model.PersonID
		want    string
		wantErr errs.Kind
	}{
		{"saved", es, "es-MX", 0},
		{"none saved", none, "", 0},
		{"zero person", model.PersonID{}, "", errs.Invalid},
		{"no row", newPerson(), "", errs.NotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.svc.LanguageOf(ctxT(t), tc.p) // no actor in ctx: a system read
			if tc.wantErr != 0 {
				wantKind(t, err, tc.wantErr)
				return
			}
			wantNoErr(t, err)
			if got != tc.want {
				t.Errorf("LanguageOf = %q, want %q", got, tc.want)
			}
		})
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("a system read recorded %d audit entries", n)
	}
}

// F65 against a real database.
func TestContactEmailOf_Table(t *testing.T) {
	r := newIRig(t, false)
	has := r.insertPerson(seed{ContactEmail: mContactEmail})
	without := r.insertPerson(seed{Playa: mPlaya})
	tests := []struct {
		name    string
		p       model.PersonID
		want    model.Email
		wantErr errs.Kind
	}{
		{"has address", has, mContactEmail, 0},
		{"no address", without, "", errs.NotFound},
		{"no row", newPerson(), "", errs.NotFound},
		{"zero person", model.PersonID{}, "", errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.svc.ContactEmailOf(ctxT(t), tc.p)
			if tc.wantErr != 0 {
				wantKind(t, err, tc.wantErr)
				if got != "" {
					t.Error("an address came back with an error")
				}
				return
			}
			wantNoErr(t, err)
			if got != tc.want {
				t.Errorf("ContactEmailOf = %q, want %q", got, tc.want)
			}
		})
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("a system read recorded %d audit entries", n)
	}
	r.noLeak(mContactEmail) // the address is in no log, span or metric
}
