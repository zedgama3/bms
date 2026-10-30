package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ExportPerson returns {"account": {...}} (the Account without the hash) or {"account":
// null} when the person has none. people (M2/M4) calls it through app. Works on a
// read-only copy. Not audited here: people records people.person.exported. Fails Invalid
// for a zero ID (F47) and store kinds.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (_ json.RawMessage, err error) {
	const op = "identity.Service.ExportPerson"
	ctx, span := s.start(ctx, "export-person")
	defer endSpan(span, &err)

	if person.IsZero() {
		return nil, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	body := struct {
		Account *Account `json:"account"`
	}{}
	rec, err := s.store.ExportByPerson(ctx, person)
	switch {
	case errs.Is(err, errs.NotFound):
		// the person has no account: {"account": null}
	case err != nil:
		return nil, fmt.Errorf("%s: %w", op, err)
	default:
		a := accountOf(rec)
		body.Account = &a
		span.SetAttrs(telemetry.ID("account_id", rec.ID))
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("encode the export: %w", err))
	}
	return out, nil
}

// DeletePerson deletes the person's account row (0 or 1) and records
// identity.account.deleted in the caller's Write transaction (Transact joins it); its
// sessions fail at the next request (NotFound becomes Unauthenticated). Idempotent: no
// account, no entry, 0. Fails Invalid for a zero ID (F47), Permission for a superuser
// account (F68) or an account the Guard protects (field account, code protected, F105; people
// maps it to Conflict protected), the Guard's kind when it fails (F108), Unavailable wrapping
// db.ErrReadOnly (F48), store and audit kinds.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (_ int, err error) {
	const op = "identity.Service.DeletePerson"
	ctx, span := s.start(ctx, "delete-person")
	defer endSpan(span, &err)

	if person.IsZero() {
		return 0, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	var n int
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		// A superuser account is never changed through the API (decision 7, F68).
		switch rec, err := s.store.ExportByPerson(ctx, person); {
		case err == nil && rec.Superuser:
			return errs.E(op, errs.Permission, errors.New("the superuser account cannot be deleted"))
		case err != nil && !errs.Is(err, errs.NotFound):
			return wrap(op, err)
		case err == nil:
			if err := s.checkProtected(ctx, op, rec.ID); err != nil {
				return err
			}
		}
		id, deleted, err := s.store.DeleteByPerson(ctx, person)
		if err != nil {
			return wrap(op, err)
		}
		n = deleted
		if deleted == 0 {
			return nil
		}
		span.SetAttrs(telemetry.ID("account_id", id))
		return s.record(ctx, audit.Entry{
			Action: actionDeleted, Outcome: audit.Succeeded, Subject: person, Record: audit.RefOf(kindAccount, id),
		})
	})
	if err != nil {
		return 0, wrap(op, err)
	}
	return n, nil
}
