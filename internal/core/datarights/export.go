package datarights

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

// commandActor is the audit actor of the export and delete commands.
var commandActor = audit.Actor{Kind: audit.ActorOperator}

// The JSON of the export document (ExportFormat 1). The person, contacts and consents are
// people's own rows, built by PeopleSource.ExportPerson and carried through as they come.
type exportDoc struct {
	Format     int                        `json:"format"`
	ExportedAt time.Time                  `json:"exported_at"`
	Person     json.RawMessage            `json:"person,omitempty"` // absent for an account holder with no profile row
	Contacts   json.RawMessage            `json:"emergency_contacts"`
	Consents   json.RawMessage            `json:"consents"`
	Requests   []exportRequest            `json:"requests"`
	Identity   json.RawMessage            `json:"identity"`
	Access     json.RawMessage            `json:"access"`
	Modules    map[string]json.RawMessage `json:"modules"`
}

type exportRequest struct {
	Kind         string    `json:"kind"`
	Status       string    `json:"status"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
	DecisionNote string    `json:"decision_note"`
}

// peopleDoc is the shape PeopleSource.ExportPerson answers; datarights lifts its three
// keys to the top of the document.
type peopleDoc struct {
	Person   json.RawMessage `json:"person"`
	Contacts json.RawMessage `json:"emergency_contacts"`
	Consents json.RawMessage `json:"consents"`
}

// Export returns one JSON document with everything bms holds about the person: the
// profile, emergency contacts, consents and requests, identity's account, authz's grants
// and each module's data (ExportFormat 1; audit rows are not included). The actor is the
// person (once per ExportCooldown) or holds people.data.export. Datarights records
// datarights.export (level sensitive) before the body is built. It runs in no transaction;
// a failing source fails the whole export. Fails errs.Unauthenticated, errs.Permission
// (F29), errs.NotFound (F11), errs.Internal before Link (F31), errs.Unavailable wrapping
// httpx.ErrRateLimited (F28), the failing source's kind wrapped with its name (F30) and
// store kinds.
func (s *Service) Export(ctx context.Context, person model.PersonID) (_ json.RawMessage, err error) {
	const op = "datarights.Service.Export"
	ctx, span := s.start(ctx, "export")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return nil, err
	}
	if person.IsZero() {
		return nil, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	self := a.Person == person
	if !self {
		if err := s.require(ctx, op, PermissionDataExport); err != nil {
			s.metrics.export(ctx, outcomeFailed)
			return nil, err
		}
	}
	if _, err := s.requireLinked(op); err != nil {
		return nil, err
	}
	var taken time.Time
	if self {
		taken = s.now()
		if !s.cooldown.take(person, taken) {
			s.metrics.export(ctx, outcomeLimited)
			return nil, errs.E(op, errs.Unavailable, httpx.ErrRateLimited)
		}
	}
	doc, recorded, err := s.exportDocument(ctx, op, person, audit.Actor{}, "")
	if err != nil {
		// Give the slot back only for a failure that left no trace: once datarights.export
		// is written the export was attempted, and a cancelled one may have been abandoned
		// halfway, so neither reopens the cooldown.
		if self && !recorded && !errs.Is(err, errs.Canceled) {
			s.cooldown.release(person, taken)
		}
		s.metrics.export(ctx, outcomeFailed)
		return nil, err
	}
	s.metrics.export(ctx, outcomeOK)
	return doc, nil
}

// ExportCommand is `bms export <person-id>`: it writes the export document and a newline to
// out, with audit.ActorOperator and no permission check (the operator has the database).
// Fails errs.Invalid (F50) for an id that is not a UUID, errs.NotFound, errs.Internal
// before Link and as Export does.
func (s *Service) ExportCommand(ctx context.Context, out io.Writer, id string) (err error) {
	const op = "datarights.Service.ExportCommand"
	ctx, span := s.start(ctx, "export-command")
	defer endSpan(span, &err)
	person, err := model.ParseID[model.PersonID](id)
	if err != nil {
		return fieldInvalid(op, "person", codeMalformed, "must be a UUID")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	if _, err := s.requireLinked(op); err != nil {
		return err
	}
	doc, _, err := s.exportDocument(ctx, op, person, commandActor, reasonCommand)
	if err != nil {
		s.metrics.export(ctx, outcomeFailed)
		return err
	}
	s.metrics.export(ctx, outcomeOK)
	if _, err := out.Write(append(doc[:len(doc):len(doc)], '\n')); err != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("write the export: %w", err))
	}
	return nil
}

// hasProfile says whether person has a profile row. A person with no row but an account (an
// account holder whose row was never made, people.md D37) still has identity, access and
// module data, so it answers false with no error; errs.NotFound (the row's own error) only
// when no account exists either.
func (s *Service) hasProfile(ctx context.Context, op string, person model.PersonID) (bool, error) {
	err := s.people.Exists(ctx, person)
	if err == nil {
		return true, nil
	}
	if !errs.Is(err, errs.NotFound) {
		return false, wrap(op, err)
	}
	if aerr := s.accountExists(ctx, op, person); aerr != nil {
		if errs.Is(aerr, errs.NotFound) {
			return false, wrap(op, err)
		}
		return false, aerr
	}
	return false, nil
}

// accountExists asks identity whether person has an account: nil when it does, errs.NotFound
// when it does not, and the failure's kind otherwise.
func (s *Service) accountExists(ctx context.Context, op string, person model.PersonID) error {
	_, err := s.accounts.EmailOf(ctx, person)
	return wrap(op, err)
}

// exportDocument builds the export of person. The profile row is looked for first
// (NotFound for an unknown person); then datarights.export is recorded; then everything
// else is read, each source in build order. recorded says whether the entry was written,
// which the caller needs to decide whether the self-export cooldown is given back.
func (s *Service) exportDocument(ctx context.Context, op string, person model.PersonID, act audit.Actor, reason string) (_ json.RawMessage, recorded bool, _ error) {
	st, err := s.requireLinked(op)
	if err != nil {
		return nil, false, err
	}
	hasRow, err := s.hasProfile(ctx, op, person)
	if err != nil {
		return nil, false, err
	}
	if err := s.record(ctx, audit.Entry{
		Action: actionExport, Outcome: audit.Succeeded, Actor: act, Subject: person,
		Record: audit.RefOf(kindPerson, person), Fields: []string{"all"}, Level: model.SensitivitySensitive, Reason: reason,
	}); err != nil {
		return nil, false, err
	}
	doc := exportDoc{
		Format: ExportFormat, ExportedAt: s.now(),
		Contacts: json.RawMessage(`[]`), Consents: json.RawMessage(`[]`),
		Modules: make(map[string]json.RawMessage, len(st.parts)),
	}
	if hasRow {
		if err := s.exportPeople(ctx, op, person, &doc); err != nil {
			return nil, true, err
		}
	}
	requests, err := s.store.ExportRequests(ctx, person)
	if err != nil {
		return nil, true, wrap(op, err)
	}
	doc.Requests = make([]exportRequest, len(requests))
	for i, r := range requests {
		doc.Requests[i] = exportRequest{Kind: r.Kind, Status: r.Status, Note: r.Note, CreatedAt: r.CreatedAt, DecisionNote: r.DecisionNote}
	}
	if doc.Identity, err = rawPart(ctx, op, "identity", person, s.accounts.ExportPerson); err != nil {
		return nil, true, err
	}
	if doc.Access, err = rawPart(ctx, op, "access", person, s.access.ExportPerson); err != nil {
		return nil, true, err
	}
	for _, p := range st.parts {
		data, err := p.export(ctx, person)
		if err != nil {
			return nil, true, fmt.Errorf("%s: module %s: %w", op, p.name, err)
		}
		if len(data) == 0 {
			data = json.RawMessage(`{}`)
		}
		if !json.Valid(data) {
			return nil, true, errs.E(op, errs.Internal, fmt.Errorf("module %s exported data that is not JSON", p.name))
		}
		doc.Modules[p.name] = data
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, true, errs.E(op, errs.Internal, fmt.Errorf("encode the export: %w", err))
	}
	return out, true, nil
}

// exportPeople reads people's own rows into doc.
func (s *Service) exportPeople(ctx context.Context, op string, person model.PersonID, doc *exportDoc) error {
	raw, err := rawPart(ctx, op, "people", person, s.people.ExportPerson)
	if err != nil {
		return err
	}
	var pd peopleDoc
	if err := json.Unmarshal(raw, &pd); err != nil {
		return errs.E(op, errs.Internal, fmt.Errorf("people exported data of an unexpected shape: %w", err))
	}
	doc.Person = pd.Person
	if len(pd.Contacts) > 0 {
		doc.Contacts = pd.Contacts
	}
	if len(pd.Consents) > 0 {
		doc.Consents = pd.Consents
	}
	return nil
}

// rawPart calls a core source's export and checks it is JSON.
func rawPart(ctx context.Context, op, name string, person model.PersonID, f func(context.Context, model.PersonID) (json.RawMessage, error)) (json.RawMessage, error) {
	raw, err := f(ctx, person)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", op, name, err)
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("%s exported data that is not JSON", name))
	}
	return raw, nil
}
