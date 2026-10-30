package identity

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// AccountResult is what EnsureAccount reports.
type AccountResult struct {
	Account model.AccountID
	Person  model.PersonID
	Created bool // false: the address existed and its password was replaced
}

// EnsureAccount is the writer-only bootstrap for commands (the sibling of EnsureSuperuser):
// no actor, no permission check, no sign-up mode check and no limiter. The password is
// validated as SignUp validates it (the policy's minimum length, the maximum, the
// common-password list). In one Write transaction it creates the account and publishes
// SignalAccountCreated, recording identity.account.created, or, when a participant account
// holds the address, sets the password, re-enables the account and ends its sessions
// (session generation bumped), recording identity.password.changed. Both entries carry
// ActorOperator. It is not guarded: it is how the demo seed sets the shared password.
//
// Fails Invalid (field email or password) before any write (F99), Conflict (field email,
// code superuser) when the superuser's account holds the address (F100), Unavailable
// wrapping db.ErrReadOnly on a read-only copy (F101), and store, signal and audit kinds.
// Consumer: core/demo's seed, through app.
func (s *Service) EnsureAccount(ctx context.Context, c Credentials) (_ AccountResult, err error) {
	const op = "identity.Service.EnsureAccount"
	ctx, span := s.start(ctx, "ensure-account")
	defer endSpan(span, &err)

	pol, err := s.policy(ctx)
	if err != nil {
		return AccountResult{}, err
	}
	v := validate.Errors{Op: op}
	email := emailField(&v, c.Email)
	addPasswordProblems(&v, "password", c.Password, pol.PasswordMinLength, s.common)
	if err := v.Err(); err != nil {
		return AccountResult{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	hash, err := s.hash(ctx, c.Password)
	if err != nil {
		return AccountResult{}, err
	}

	var got AccountResult
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		fresh, freshPerson := model.NewID[model.AccountID](), model.NewID[model.PersonID]()
		res, err := s.store.EnsureAccount(ctx, fresh, freshPerson, email, hash, s.now())
		if err != nil {
			if errs.Is(err, errs.Conflict) {
				return errs.E(op, errs.Conflict, &validate.FieldError{
					Field: "email", Code: codeSuperuser, Reason: "this address belongs to the superuser account",
				})
			}
			return wrap(op, err)
		}
		got = AccountResult{Account: res.ID, Person: res.Person, Created: res.Created}
		action := actionPasswordSet
		if res.Created {
			action = actionCreated
			if err := s.publishCreated(ctx, res.ID, res.Person); err != nil {
				return err
			}
		}
		return s.record(ctx, audit.Entry{
			Action: action, Outcome: audit.Succeeded, Actor: audit.Actor{Kind: audit.ActorOperator},
			Subject: res.Person, Record: audit.RefOf(kindAccount, res.ID),
		})
	})
	if err != nil {
		return AccountResult{}, fmt.Errorf("%s: %w", op, err)
	}
	span.SetAttrs(telemetry.ID("account_id", got.Account))
	return got, nil
}
