package notify

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// exportedMessage is one row of a person's export: no body and no address.
type exportedMessage struct {
	ID        MessageID `json:"id"`
	Template  Template  `json:"template"`
	Status    Status    `json:"status"`
	Language  Language  `json:"language"`
	CreatedAt time.Time `json:"created_at"`
	SentAt    time.Time `json:"sent_at,omitzero"`
	Subject   string    `json:"subject,omitempty"`
}

// ExportPerson returns {"messages": [...]}: the person's rows (id, template, status,
// language, created_at, sent_at and the subject while the row still waits), never a body or
// an address. It runs under people's permission checks and audits nothing itself. Fails
// errs.Invalid for the zero ID and store kinds.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (_ json.RawMessage, err error) {
	const op = "notify.Service.ExportPerson"
	ctx, span := s.start(ctx, "export_person")
	defer endSpan(span, &err)
	if person.IsZero() {
		return nil, errs.E(op, errs.Invalid, errors.New("the person ID is zero"))
	}
	rows, err := s.store.ExportPerson(ctx, person)
	if err != nil {
		return nil, wrap(op, err)
	}
	doc := struct {
		Messages []exportedMessage `json:"messages"`
	}{Messages: make([]exportedMessage, len(rows))}
	for i, r := range rows {
		doc.Messages[i] = exportedMessage{
			ID: r.ID, Template: Template(r.Template), Status: r.Status, Language: Language(r.Language),
			CreatedAt: r.CreatedAt, SentAt: r.SentAt, Subject: r.Subject,
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, errs.E(op, errs.Internal, err)
	}
	return raw, nil
}

// DeletePerson deletes the person's rows, whatever their status (queued ones are never
// sent), on the transaction ctx carries (people's), and returns how many. Fails errs.Invalid
// for the zero ID, errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (_ int, err error) {
	const op = "notify.Service.DeletePerson"
	ctx, span := s.start(ctx, "delete_person")
	defer endSpan(span, &err)
	if person.IsZero() {
		return 0, errs.E(op, errs.Invalid, errors.New("the person ID is zero"))
	}
	if s.db.ReadOnly() {
		return 0, readOnlyErr(op)
	}
	n, err := s.store.DeletePerson(ctx, person)
	if err != nil {
		return 0, wrap(op, err)
	}
	return int(n), nil
}
