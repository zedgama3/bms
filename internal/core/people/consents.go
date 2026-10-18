package people

import (
	"context"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/model"
)

// SetConsent records whether person consents to purpose (granted or revoked), as the
// person or, for a paper form, as a holder of people.profiles.manage; RecordedBy names
// the actor. Records people.consent.changed in the transaction. Fails errs.Invalid (F23),
// errs.Permission, errs.NotFound (unknown person), errs.Unavailable wrapping db.ErrReadOnly
// and store kinds.
func (s *Service) SetConsent(ctx context.Context, person model.PersonID, purpose Purpose, granted bool) (_ Consent, err error) {
	const op = "people.Service.SetConsent"
	ctx, span := s.start(ctx, "set-consent")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Consent{}, err
	}
	if person.IsZero() {
		return Consent{}, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	if err := purpose.Validate(); err != nil {
		return Consent{}, wrap(op, err)
	}
	span.SetAttrs(telemetry.ID("person_id", person), telemetry.Bool("granted", granted))
	if _, err := s.selfOrManage(ctx, op, a, person); err != nil {
		return Consent{}, err
	}
	var out rows.Consent
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		var err error
		out, err = s.store.SetConsent(ctx, rows.Consent{
			Person: person, Purpose: string(purpose), Granted: granted, RecordedBy: a.Person, ChangedAt: s.now(),
		})
		if err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionConsentChanged, Outcome: audit.Succeeded, Subject: person,
			Record: audit.RefOf(kindPerson, person), Fields: []string{"purpose"}, Level: model.SensitivityInternal,
		})
	})
	if err != nil {
		return Consent{}, wrap(op, err)
	}
	return consentOf(out), nil
}
