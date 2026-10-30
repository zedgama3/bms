package people

import (
	"context"
	"log/slog"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// handleAccountCreated is the identity.account-created subscriber: it creates the empty
// person row behind a new account, idempotently, and publishes people.person-created only
// when it inserted one, and only while the account still exists (it takes the person's
// deletion lock first). It runs as the system (no actor). Fails errs.Invalid for a payload
// without person (F47), errs.Unavailable wrapping db.ErrReadOnly (F46) and store kinds;
// signals logs and counts the failure, and the person's first UpdateMe repairs the gap.
func (s *Service) handleAccountCreated(ctx context.Context, sig signals.Signal) (err error) {
	const op = "people.Service.handleAccountCreated"
	ctx, span := s.start(ctx, "account-created")
	defer endSpan(span, &err)
	person, err := signals.Get[model.PersonID](sig.Payload, identity.PayloadPerson)
	if err != nil {
		return wrap(op, err)
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		// A deletion of this person in flight finishes first, and the row is made only while
		// the account is still there: a late delivery must not leave an orphan row behind a
		// deletion that succeeded without one (D37, the same rule as UpdateMe's repair).
		if err := s.store.LockDeletion(ctx, person); err != nil {
			return wrap(op, err)
		}
		if _, err := s.accounts.EmailOf(ctx, person); err != nil {
			if errs.Is(err, errs.NotFound) {
				return nil
			}
			return wrap(op, err)
		}
		_, inserted, err := s.store.InsertPerson(ctx, rows.Person{ID: person}, s.now())
		if err != nil {
			return wrap(op, err)
		}
		if !inserted {
			return nil
		}
		return s.recordSave(ctx, person, true, nil, false)
	})
	return wrap(op, err)
}

// LinkAccount joins the account with the given login address to a person who has none (a
// walk-on a manager created). Needs people.profiles.manage. Transaction 1 re-points the
// account through identity and clears the walk-on's contact_email (the account's address replaces it) and records
// people.person.linked; transaction 2, best effort,
// deletes the empty row sign-up minted for the account (OrphanDeleted) and publishes
// people.person-deleted for it. A non-empty or referenced orphan is kept. Fails
// errs.Permission (F14), errs.NotFound (F11; field email, code unknown when no account has
// the address), errs.Conflict (has_account, superuser: F25, F26; field account, code protected for a shared
// demo account) and store kinds.
func (s *Service) LinkAccount(ctx context.Context, person model.PersonID, email model.Email) (_ LinkResult, err error) {
	const op = "people.Service.LinkAccount"
	ctx, span := s.start(ctx, "link-account")
	defer endSpan(span, &err)
	if _, err := s.actorPerson(ctx, op); err != nil {
		return LinkResult{}, err
	}
	if err := s.require(ctx, op, PermissionProfilesManage); err != nil {
		return LinkResult{}, err
	}
	if person.IsZero() {
		return LinkResult{}, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	if err := email.Validate(); err != nil {
		return LinkResult{}, fieldInvalid(op, "email", codeMalformed, "must be a valid e-mail address")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	var res LinkResult
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		// First statement: a deletion of this person in flight finishes before the link
		// looks at the person, and the link then finds nothing to link.
		if err := s.store.LockDeletion(ctx, person); err != nil {
			return wrap(op, err)
		}
		cur, err := s.store.GetPerson(ctx, person)
		if err != nil {
			return wrap(op, err)
		}
		switch _, err := s.accounts.EmailOf(ctx, person); {
		case err == nil:
			return fieldConflict(op, "person", codeHasAccount, nil, "the person already has an account")
		case !errs.Is(err, errs.NotFound):
			return wrap(op, err)
		}
		acct, orphan, err := s.accounts.LinkAccount(ctx, email, person)
		if err != nil {
			if isProtected(err) {
				return fieldConflict(op, "account", codeProtected, nil, "this is a shared demo account and cannot be linked to another person")
			}
			return wrap(op, err)
		}
		res = LinkResult{Account: acct, Orphan: orphan}
		entry := audit.Entry{
			Action: actionLinked, Outcome: audit.Succeeded, Subject: person, Record: audit.RefOf(kindPerson, person),
		}
		// The account's address replaces the walk-on's contact address (D37, F56).
		if cur.ContactEmail != "" {
			empty := ""
			if _, err := s.store.UpdatePerson(ctx, rows.PersonPatch{ID: person, ContactEmail: &empty}, s.now()); err != nil {
				return wrap(op, err)
			}
			entry.Fields, entry.Level = []string{FieldContactEmail}, model.SensitivityPersonal
		}
		return s.record(ctx, entry)
	})
	if err != nil {
		return LinkResult{}, wrap(op, err)
	}
	if !res.Orphan.IsZero() && res.Orphan != person {
		res.OrphanDeleted = s.dropOrphan(ctx, res.Orphan)
	}
	return res, nil
}

// dropOrphan is transaction 2 of LinkAccount, best effort: it deletes the empty person row
// sign-up minted and reports whether it did. A failure is logged with the person's ID and
// leaves the row (the admin is told it was kept). It runs outside a transaction on purpose:
// a foreign key that refuses the delete must not poison one.
func (s *Service) dropOrphan(ctx context.Context, orphan model.PersonID) bool {
	deleted, err := s.store.DeleteOrphan(ctx, orphan)
	if err != nil {
		s.warnOrphan(ctx, orphan, err)
		return false
	}
	if !deleted {
		return false
	}
	// The row is gone, so a failure from here on is logged and does not undo it.
	if err := s.record(ctx, audit.Entry{
		Action: actionDeleted, Outcome: audit.Succeeded, Actor: audit.Actor{Kind: audit.ActorSystem},
		Subject: orphan, Record: audit.RefOf(kindPerson, orphan), Reason: reasonLink,
	}); err != nil {
		s.warnOrphan(ctx, orphan, err)
	}
	if err := s.publishPerson(ctx, SignalPersonDeleted, orphan); err != nil {
		s.warnOrphan(ctx, orphan, err) // subscribers will clear their caches later
	}
	return true
}

func (s *Service) warnOrphan(ctx context.Context, orphan model.PersonID, err error) {
	s.logger.WarnContext(ctx, "the empty profile of a linked account was not fully removed",
		slog.String("kind", errs.KindOf(err).String()), slog.Any("person", Person{ID: orphan}))
}
