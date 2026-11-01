package people

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

// countingLegalStore counts the legal-name reads that reach the store.
type countingLegalStore struct {
	memberStore
	reads int
}

func (c *countingLegalStore) LegalNamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	c.reads++
	return c.memberStore.LegalNamesOf(ctx, ids)
}

// policyReader is a settings.Reader for the "people" area that answers one policy or one error.
type policyReader struct {
	p   Policy
	err error
}

func (f policyReader) fake() (*settingstest.Fake, error) {
	if f.err != nil {
		return nil, f.err
	}
	fk := settingstest.NewFake("people", Settings)
	fk.Set("legal_name", f.p.LegalName.String())
	fk.Set("phone", f.p.Phone.String())
	fk.Set("emergency_contacts", f.p.EmergencyContacts.String())
	return fk, nil
}

func (f policyReader) Bool(ctx context.Context, k string) (bool, error) {
	fk, err := f.fake()
	if err != nil {
		return false, err
	}
	return fk.Bool(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (f policyReader) Int(ctx context.Context, k string) (int64, error) {
	fk, err := f.fake()
	if err != nil {
		return 0, err
	}
	return fk.Int(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (f policyReader) Duration(ctx context.Context, k string) (time.Duration, error) {
	fk, err := f.fake()
	if err != nil {
		return 0, err
	}
	return fk.Duration(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (f policyReader) Text(ctx context.Context, k string) (string, error) {
	fk, err := f.fake()
	if err != nil {
		return "", err
	}
	return fk.Text(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (f policyReader) Secret(ctx context.Context, k string) (secret.Secret, error) {
	fk, err := f.fake()
	if err != nil {
		return secret.Secret{}, err
	}
	return fk.Secret(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

// people.md Settings and Visibility: hidden "hides and stops accepting the field; stored
// values stay", and hidden-mode fields are absent for everyone. Nothing is read or audited.
func TestLegalNamesOf_LegalNameHidden_ReturnsNoneAndAuditsNothing(t *testing.T) {
	someone := model.NewID[model.PersonID]()
	tests := []struct {
		name     string
		mode     FieldMode
		wantName bool
	}{
		{"hidden", FieldHidden, false},
		{"optional (control)", FieldOptional, true},
		{"required (control)", FieldRequired, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newWB(t, PermissionContactsRead)
			st := &countingLegalStore{memberStore: memberStore{
				fakeStore: fakeStore{person: rows.Person{ID: r.me}},
				legal:     map[model.PersonID]string{someone: "Odalys Brindle"},
			}}
			r.svc.store = st
			pol := DefaultPolicy()
			pol.LegalName = tc.mode
			r.svc.settings = policyReader{p: pol}
			got, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{someone})
			if err != nil {
				t.Fatalf("LegalNamesOf: %v", err)
			}
			if got == nil {
				t.Error("map is nil, want empty")
			}
			if tc.wantName {
				if got[someone] != "Odalys Brindle" || st.reads != 1 || len(r.rec.Recorded()) != 1 {
					t.Errorf("got %v, store reads %d, audit entries %d; want the name, one read, one entry", got, st.reads, len(r.rec.Recorded()))
				}
				return
			}
			if len(got) != 0 {
				t.Errorf("hidden mode returned %v", got)
			}
			if st.reads != 0 {
				t.Errorf("store reads = %d, want 0", st.reads)
			}
			if n := len(r.rec.Recorded()); n != 0 {
				t.Errorf("audit entries = %d, want 0", n)
			}
		})
	}
}

func TestLegalNamesOf_LegalNameHidden_StillNeedsContactsRead(t *testing.T) {
	r := newWB(t)
	r.svc.settings = policyReader{p: Policy{LegalName: FieldHidden, Phone: FieldOptional, EmergencyContacts: FieldOptional}}
	_, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{model.NewID[model.PersonID]()})
	if !errs.Is(err, errs.Permission) {
		t.Errorf("kind = %v, want Permission", errs.KindOf(err))
	}
}

func TestLegalNamesOf_PolicyFails_ReturnsItsKind(t *testing.T) {
	r := newWB(t, PermissionContactsRead)
	r.svc.settings = policyReader{err: errs.E("settings.Policy", errs.Unavailable, context.DeadlineExceeded)}
	_, err := r.svc.LegalNamesOf(r.ctx, []model.PersonID{model.NewID[model.PersonID]()})
	if !errs.Is(err, errs.Unavailable) {
		t.Errorf("kind = %v, want Unavailable", errs.KindOf(err))
	}
}
