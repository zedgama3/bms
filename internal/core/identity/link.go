package identity

import (
	"context"
	"errors"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// EmailOf is Lookup.EmailOf: the login address of the person's account, for people's
// display. It checks no permission (people's seam; people checks its own) and records
// nothing. Fails Invalid for a zero ID, NotFound when the person has no account (F64) and
// store kinds.
func (s *Service) EmailOf(ctx context.Context, person model.PersonID) (model.Email, error) {
	return s.lookup.EmailOf(ctx, person) //nolint:wrapcheck // the Lookup names itself
}

// LinkAccount moves the account with this e-mail address to the given person and returns
// the account and the person it belonged to (people deletes the empty previous row). It
// checks no permission (people checked profiles.manage). Runs in the caller's Write
// transaction when there is one, and records {identity.account.linked, Succeeded, Subject:
// person, Record: account}; it publishes no signal. Its sessions follow at the next
// request, which re-reads the account row. Fails Invalid for a zero person or a malformed
// address, NotFound (field email, code unknown; it never says more, F65), Conflict
// (field email, code superuser, F66; field person, code has_account, F67), Permission (field
// account, code protected) for a shared demo account the Guard protects, Unavailable
// wrapping db.ErrReadOnly, store and audit kinds.
func (s *Service) LinkAccount(ctx context.Context, email model.Email, person model.PersonID) (_ model.AccountID, _ model.PersonID, err error) {
	const op = "identity.Service.LinkAccount"
	ctx, span := s.start(ctx, "link-account")
	defer endSpan(span, &err)

	if person.IsZero() {
		return model.AccountID{}, model.PersonID{}, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	if email.Validate() != nil {
		return model.AccountID{}, model.PersonID{}, fieldInvalid(op, "email", codeMalformed, "must be a valid e-mail address")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	var (
		acct     model.AccountID
		previous model.PersonID
	)
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		recs, err := s.store.ListByEmail(ctx, email)
		if err != nil {
			return wrap(op, err)
		}
		if len(recs) == 0 {
			return errs.E(op, errs.NotFound, &validate.FieldError{Field: "email", Code: codeUnknown, Reason: "no account has this address"})
		}
		rec := recs[0]
		if rec.Superuser {
			return errs.E(op, errs.Conflict, &validate.FieldError{Field: "email", Code: codeSuperuser, Reason: "this account cannot be linked"})
		}
		// Re-pointing a shared demo account to another person would lock everyone else out of
		// it (D53, Q-DEMO4): the Guard refuses, and fails closed when it cannot answer.
		if err := s.checkProtected(ctx, op, rec.ID); err != nil {
			return err
		}
		switch _, err := s.store.ExportByPerson(ctx, person); {
		case err == nil:
			return hasAccount(op)
		case !errs.Is(err, errs.NotFound):
			return wrap(op, err)
		}
		if previous, err = s.store.SetPerson(ctx, rec.ID, person, s.now()); err != nil {
			if isPersonTaken(err) {
				return hasAccount(op)
			}
			return wrap(op, err)
		}
		acct = rec.ID
		return s.record(ctx, audit.Entry{
			Action: actionLinked, Outcome: audit.Succeeded, Subject: person, Record: audit.RefOf(kindAccount, rec.ID),
		})
	})
	if err != nil {
		return model.AccountID{}, model.PersonID{}, wrap(op, err)
	}
	span.SetAttrs(telemetry.ID("account_id", acct))
	return acct, previous, nil
}

func hasAccount(op string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: "person", Code: codeHasAcct, Reason: "this person already has an account"})
}

// isPersonTaken reports a unique violation on the person: a second account for one person.
func isPersonTaken(err error) bool {
	var de *db.Error
	return errs.Is(err, errs.Conflict) && errors.As(err, &de) && de.Constraint == personKey
}
