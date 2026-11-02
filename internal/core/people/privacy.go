package people

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// This file is people's answer to datarights: its own rows, as one more source of the
// per-person export and deletion (datarights.PeopleSource, boundaries.md decision 5).
// datarights authorizes, audits and orders; people only knows its rows.

// The JSON of people's part of the export document (ExportFormat 1). The three keys sit at
// the top of the document datarights builds; "person" is absent when there is no row.
type exportDoc struct {
	Person   *exportPerson   `json:"person,omitempty"`
	Contacts []exportContact `json:"emergency_contacts"`
	Consents []exportConsent `json:"consents"`
}

type exportPerson struct {
	ID           model.PersonID `json:"id"`
	PlayaName    string         `json:"playa_name"`
	Pronouns     string         `json:"pronouns"`
	Language     string         `json:"language"`
	LegalName    string         `json:"legal_name"`
	Phone        string         `json:"phone"`
	ContactEmail string         `json:"contact_email"`
	CreatedAt    time.Time      `json:"created_at,omitzero"`
	UpdatedAt    time.Time      `json:"updated_at,omitzero"`
	// NamePromptedAt is when the person was last asked for a playa name (decision 34);
	// absent when never.
	NamePromptedAt time.Time `json:"name_prompted_at,omitzero"`
}

type exportContact struct {
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Relationship string `json:"relationship"`
	Position     int    `json:"position"`
}

type exportConsent struct {
	Purpose   string    `json:"purpose"`
	Granted   bool      `json:"granted"`
	ChangedAt time.Time `json:"changed_at"`
}

// ExportPerson returns {"person": {...}, "emergency_contacts": [...], "consents": [...]}:
// every row people holds about the person, personal fields included. It runs in no
// transaction, checks no permission and records no audit entry: datarights has authorized
// the export and records it. Works on a read-only copy. Fails errs.Invalid for a zero
// person, errs.NotFound when there is no profile row (datarights asks Exists first and
// leaves people out for an account holder who never made one) and store kinds.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (_ json.RawMessage, err error) {
	const op = "people.Service.ExportPerson"
	ctx, span := s.start(ctx, "export-person")
	defer endSpan(span, &err)
	if person.IsZero() {
		return nil, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	row, err := s.store.ExportPerson(ctx, person)
	if err != nil {
		return nil, wrap(op, err)
	}
	doc := exportDoc{Person: &exportPerson{
		ID: row.ID, PlayaName: row.PlayaName, Pronouns: row.Pronouns, Language: row.Language, LegalName: row.LegalName,
		Phone: row.Phone, ContactEmail: row.ContactEmail, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		NamePromptedAt: row.NamePromptedAt,
	}}
	contacts, err := s.store.ExportContacts(ctx, person)
	if err != nil {
		return nil, wrap(op, err)
	}
	doc.Contacts = make([]exportContact, len(contacts))
	for i, c := range contacts {
		doc.Contacts[i] = exportContact{Name: c.Name, Phone: c.Phone, Relationship: c.Relationship, Position: c.Position}
	}
	consents, err := s.store.ExportConsents(ctx, person)
	if err != nil {
		return nil, wrap(op, err)
	}
	doc.Consents = make([]exportConsent, len(consents))
	for i, c := range consents {
		doc.Consents[i] = exportConsent{Purpose: c.Purpose, Granted: c.Granted, ChangedAt: c.ChangedAt}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("encode the export: %w", err))
	}
	return out, nil
}

// DeletePerson deletes the person's consents, emergency contacts and profile row, and
// reports how many rows went. It runs inside the caller's Write transaction (datarights'
// deletion, after every module and core source) and publishes people.person-deleted in it,
// delivered after commit. It checks no permission and records no audit entry: datarights
// has authorized the deletion and records it. The row is locked first, so a contact added
// concurrently is deleted and counted. Fails errs.Invalid for a zero person, errs.Internal
// outside a Write transaction, errs.NotFound when there is no profile row, errs.Conflict
// (with *db.Error.Constraint) when a record elsewhere still refers to the person (a module
// that forgot a table: a programming error, which datarights reports as in_use),
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy and store kinds.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (_ int, err error) {
	const op = "people.Service.DeletePerson"
	ctx, span := s.start(ctx, "delete-person")
	defer endSpan(span, &err)
	if person.IsZero() {
		return 0, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	if mode, inTx := db.TxMode(ctx); !inTx || mode != db.Write {
		return 0, errs.E(op, errs.Internal, errors.New("called outside a Write transaction: a person is deleted with every source"))
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	if _, err := s.store.GetForUpdate(ctx, person); err != nil {
		return 0, wrap(op, err)
	}
	total := 0
	for _, del := range []func(context.Context, model.PersonID) (int, error){s.store.DeleteConsentsOf, s.store.DeleteContactsOf} {
		n, err := del(ctx, person)
		if err != nil {
			return 0, wrap(op, err)
		}
		total += n
	}
	n, err := s.store.DeletePerson(ctx, person)
	if err != nil {
		return 0, wrap(op, err)
	}
	total += n
	if err := s.publishPerson(ctx, SignalPersonDeleted, person); err != nil {
		return 0, err
	}
	return total, nil
}

// Exists reports whether the person row exists: nil when it does and errs.NotFound when it
// does not. It reads with no lock, in the caller's transaction when there is one, and
// works on a read-only copy. Fails errs.Invalid for a zero person and store kinds.
func (s *Service) Exists(ctx context.Context, person model.PersonID) error {
	const op = "people.Service.Exists"
	if person.IsZero() {
		return fieldInvalid(op, "person", codeRequired, "must be set")
	}
	if _, err := s.store.GetPerson(ctx, person); err != nil {
		return wrap(op, err)
	}
	return nil
}
