package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// deliveryAccounts and deliveryProfiles answer the two address reads with fixed results.
type deliveryAccounts struct {
	to  model.Email
	err error
}

func (a deliveryAccounts) EmailOf(context.Context, model.PersonID) (model.Email, error) {
	return a.to, a.err
}

type deliveryProfiles struct {
	to  model.Email
	err error
}

func (deliveryProfiles) PlayaNameOf(context.Context, model.PersonID) (string, error) { return "", nil }
func (deliveryProfiles) LanguageOf(context.Context, model.PersonID) (string, error)  { return "", nil }
func (p deliveryProfiles) ContactEmailOf(context.Context, model.PersonID) (model.Email, error) {
	return p.to, p.err
}

// The address rule that moved in from app (boundaries step 8): the login address first,
// else the contact address, else NotFound; any other kind is not hidden.
func TestAddressOfPerson_Table(t *testing.T) {
	notFound := errs.E("test", errs.NotFound, errors.New("none"))
	down := errs.E("test", errs.Unavailable, errors.New("down"))
	slow := errs.E("test", errs.Timeout, errors.New("slow"))
	for _, tc := range []struct {
		name     string
		acct     deliveryAccounts
		contact  deliveryProfiles
		want     model.Email
		wantKind errs.Kind // 0 = success
	}{
		{"account found", deliveryAccounts{to: "login@example.org"}, deliveryProfiles{to: "contact@example.org"}, "login@example.org", 0},
		{"no account uses the contact", deliveryAccounts{err: notFound}, deliveryProfiles{to: "contact@example.org"}, "contact@example.org", 0},
		{"account without an address uses the contact", deliveryAccounts{}, deliveryProfiles{to: "contact@example.org"}, "contact@example.org", 0},
		{"neither is NotFound", deliveryAccounts{err: notFound}, deliveryProfiles{err: notFound}, "", errs.NotFound},
		{"account empty and no profile is NotFound", deliveryAccounts{}, deliveryProfiles{err: notFound}, "", errs.NotFound},
		{"an account failure keeps its kind", deliveryAccounts{err: down}, deliveryProfiles{to: "contact@example.org"}, "", errs.Unavailable},
		{"a contact failure keeps its kind", deliveryAccounts{err: notFound}, deliveryProfiles{err: slow}, "", errs.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{accounts: tc.acct, profiles: tc.contact}
			got, err := s.addressOfPerson(t.Context(), model.NewID[model.PersonID]())
			if tc.wantKind != 0 {
				if !errs.Is(err, tc.wantKind) {
					t.Fatalf("err = %v, want %v", err, tc.wantKind)
				}
				if got != "" {
					t.Errorf("address %q returned with an error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("addressOfPerson = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}
