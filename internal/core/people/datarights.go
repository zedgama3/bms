package people

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// The reasons recorded on the export and deletion entries: tokens, never free text
// (decision 16).
const (
	reasonAdmin   = "admin"
	reasonCommand = "command"
	reasonLink    = "link" // the empty row sign-up minted, dropped by LinkAccount

	reasonGranted   = "granted"   // a consent change: the person agreed (SetConsent)
	reasonWithdrawn = "withdrawn" // a consent change: the person took their agreement back
)

// commandActor is the audit actor of the export and delete commands.
var commandActor = audit.Actor{Kind: audit.ActorOperator}

// ---- export -----------------------------------------------------------------------------

// The JSON of the export document (ExportFormat 1).
type exportDoc struct {
	Format     int                        `json:"format"`
	ExportedAt time.Time                  `json:"exported_at"`
	Person     *exportPerson              `json:"person,omitempty"` // absent for an account holder with no profile row
	Contacts   []exportContact            `json:"emergency_contacts"`
	Consents   []exportConsent            `json:"consents"`
	Requests   []exportRequest            `json:"requests"`
	Identity   json.RawMessage            `json:"identity"`
	Access     json.RawMessage            `json:"access"`
	Modules    map[string]json.RawMessage `json:"modules"`
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

type exportRequest struct {
	Kind         string    `json:"kind"`
	Status       string    `json:"status"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
	DecisionNote string    `json:"decision_note"`
}

// Export returns one JSON document with everything bms holds about the person: the
// profile, emergency contacts, consents and requests, identity's account, authz's grants
// and each module's data (ExportFormat 1; audit rows are not included). The actor is the
// person (once per ExportCooldown) or holds people.data.export. People records
// people.person.exported (level sensitive) before the body is built. It runs in no
// transaction; a failing source fails the whole export. Fails errs.Unauthenticated,
// errs.Permission (F29), errs.NotFound (F11), errs.Internal before Link (F31), errs.Unavailable
// wrapping httpx.ErrRateLimited (F28), the failing source's kind wrapped with its name
// (F30) and store kinds.
func (s *Service) Export(ctx context.Context, person model.PersonID) (_ json.RawMessage, err error) {
	const op = "people.Service.Export"
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
		// Give the slot back only for a failure that left no trace: once people.person.exported
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
	const op = "people.Service.ExportCommand"
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

// exportDocument builds the export of person. The profile row is read first (NotFound for
// an unknown person); then people.person.exported is recorded; then everything else is
// read, each source in build order. recorded says whether the entry was written, which the
// caller needs to decide whether the self-export cooldown is given back.
func (s *Service) exportDocument(ctx context.Context, op string, person model.PersonID, act audit.Actor, reason string) (_ json.RawMessage, recorded bool, _ error) {
	st, err := s.requireLinked(op)
	if err != nil {
		return nil, false, err
	}
	row, err := s.store.ExportPerson(ctx, person)
	hasRow := err == nil
	if err != nil {
		if !errs.Is(err, errs.NotFound) {
			return nil, false, wrap(op, err)
		}
		// An account holder whose profile row was never made (D37): the identity, access
		// and module parts still hold data about them. NotFound only when no account exists.
		if aerr := s.accountExists(ctx, op, person); aerr != nil {
			if errs.Is(aerr, errs.NotFound) {
				return nil, false, wrap(op, err)
			}
			return nil, false, aerr
		}
	}
	if err := s.record(ctx, audit.Entry{
		Action: actionExported, Outcome: audit.Succeeded, Actor: act, Subject: person,
		Record: audit.RefOf(kindPerson, person), Fields: []string{"all"}, Level: model.SensitivitySensitive, Reason: reason,
	}); err != nil {
		return nil, false, err
	}
	doc := exportDoc{
		Format: ExportFormat, ExportedAt: s.now(),
		Modules: make(map[string]json.RawMessage, len(st.sources)),
	}
	if hasRow {
		doc.Person = &exportPerson{
			ID: row.ID, PlayaName: row.PlayaName, Pronouns: row.Pronouns, Language: row.Language, LegalName: row.LegalName,
			Phone: row.Phone, ContactEmail: row.ContactEmail, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	contacts, err := s.store.ExportContacts(ctx, person)
	if err != nil {
		return nil, true, wrap(op, err)
	}
	doc.Contacts = make([]exportContact, len(contacts))
	for i, c := range contacts {
		doc.Contacts[i] = exportContact{Name: c.Name, Phone: c.Phone, Relationship: c.Relationship, Position: c.Position}
	}
	consents, err := s.store.ExportConsents(ctx, person)
	if err != nil {
		return nil, true, wrap(op, err)
	}
	doc.Consents = make([]exportConsent, len(consents))
	for i, c := range consents {
		doc.Consents[i] = exportConsent{Purpose: c.Purpose, Granted: c.Granted, ChangedAt: c.ChangedAt}
	}
	requests, err := s.store.ExportRequests(ctx, person)
	if err != nil {
		return nil, true, wrap(op, err)
	}
	doc.Requests = make([]exportRequest, len(requests))
	for i, r := range requests {
		doc.Requests[i] = exportRequest{Kind: r.Kind, Status: r.Status, Note: r.Note, CreatedAt: r.CreatedAt, DecisionNote: r.DecisionNote}
	}
	if doc.Identity, err = s.rawPart(ctx, op, "identity", person, s.accounts.ExportPerson); err != nil {
		return nil, true, err
	}
	if doc.Access, err = s.rawPart(ctx, op, "access", person, s.access.ExportPerson); err != nil {
		return nil, true, err
	}
	for _, src := range st.sources {
		e, err := src.Exporter.ExportPerson(ctx, person)
		if err != nil {
			return nil, true, fmt.Errorf("%s: module %s: %w", op, src.Name, err)
		}
		data := e.Data
		if len(data) == 0 {
			data = json.RawMessage(`{}`)
		}
		if !json.Valid(data) {
			return nil, true, errs.E(op, errs.Internal, fmt.Errorf("module %s exported data that is not JSON", src.Name))
		}
		doc.Modules[src.Name] = data
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, true, errs.E(op, errs.Internal, fmt.Errorf("encode the export: %w", err))
	}
	return out, true, nil
}

// accountExists asks identity whether person has an account: nil when it does, errs.NotFound
// when it does not, and the failure's kind otherwise.
func (s *Service) accountExists(ctx context.Context, op string, person model.PersonID) error {
	_, err := s.accounts.EmailOf(ctx, person)
	return wrap(op, err)
}

// rawPart calls identity's or authz's export and checks it is JSON.
func (*Service) rawPart(ctx context.Context, op, name string, person model.PersonID, f func(context.Context, model.PersonID) (json.RawMessage, error)) (json.RawMessage, error) {
	raw, err := f(ctx, person)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", op, name, err)
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("%s exported data that is not JSON", name))
	}
	return raw, nil
}

// ---- deletion ---------------------------------------------------------------------------

// Delete removes the person and everything about them, in one transaction: every module's
// PersonData in build order, then authz's grants, then identity's account, then people's
// own rows. Any failure rolls everything back. Audit rows stay and are counted in the
// report as Kept "audit.entry". Needs people.data.delete; not for yourself. Records
// people.person.deleted and publishes people.person-deleted. Fails errs.Permission (F32),
// errs.Invalid (F33), errs.NotFound (F11), errs.Internal before Link (F31),
// errs.Conflict (last_access_admin: F34; in_use: F37), identity's errs.Permission for a
// superuser account (F35), errs.Conflict (field account, code protected: F89) for a
// shared demo account that identity's Guard protects, a module's kind wrapped with its name (F36),
// errs.Unavailable wrapping db.ErrReadOnly (F38) and the recorder's kind (F39).
func (s *Service) Delete(ctx context.Context, person model.PersonID) (_ DeletionReport, err error) {
	const op = "people.Service.Delete"
	ctx, span := s.start(ctx, "delete")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return DeletionReport{}, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return DeletionReport{}, err
	}
	if person.IsZero() {
		return DeletionReport{}, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	if a.Person == person {
		return DeletionReport{}, fieldInvalid(op, "person", codeSelf, "you cannot delete yourself; ask another administrator")
	}
	return s.deleteWith(ctx, span, op, deletion{person: person, reason: reasonAdmin})
}

// DeleteCommand is `bms delete <person-id>`: it deletes as Delete does with
// audit.ActorOperator and no permission check, and writes the report, one line per part,
// to out. Fails errs.Invalid (F50) for an id that is not a UUID and as Delete does.
func (s *Service) DeleteCommand(ctx context.Context, out io.Writer, id string) (err error) {
	const op = "people.Service.DeleteCommand"
	ctx, span := s.start(ctx, "delete-command")
	defer endSpan(span, &err)
	person, err := model.ParseID[model.PersonID](id)
	if err != nil {
		return fieldInvalid(op, "person", codeMalformed, "must be a UUID")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	rep, err := s.deleteWith(ctx, span, op, deletion{person: person, reason: reasonCommand, actor: commandActor})
	if err != nil {
		return err
	}
	if err := writeReport(out, rep); err != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("write the report: %w", err))
	}
	return nil
}

// writeReport prints the report: one "name value" line per part, modules sorted by name.
func writeReport(out io.Writer, r DeletionReport) error {
	lines := []string{
		fmt.Sprintf("person %s", r.Person),
		fmt.Sprintf("people %d", r.People),
		fmt.Sprintf("identity %d", r.Identity),
		fmt.Sprintf("access %d", r.Access),
	}
	names := make([]string, 0, len(r.Modules))
	for n := range r.Modules {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m := r.Modules[n]
		lines = append(lines, fmt.Sprintf("module %s deleted=%d anonymized=%d", n, m.Deleted, m.Anonymized))
		for _, k := range m.Kept {
			lines = append(lines, fmt.Sprintf("module %s kept %s %d", n, k.Kind, k.Count))
		}
	}
	for _, k := range r.Kept {
		lines = append(lines, fmt.Sprintf("kept %s %d", k.Kind, k.Count))
	}
	for _, l := range lines {
		if _, err := io.WriteString(out, l+"\n"); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
	return nil
}

// deletion is one deletion to run.
type deletion struct {
	person  model.PersonID
	reason  string      // the audit Reason token
	actor   audit.Actor // zero: the recorder resolves it from ctx
	request RequestID   // the request being fulfilled, or zero
}

// isProtected reports identity's refusal of a shared demo account: Permission carrying the
// field "account" with the code "protected" (identity.md F105). The shape is read from the
// error, so people does not import identity's types for it.
func isProtected(err error) bool {
	if !errs.Is(err, errs.Permission) {
		return false
	}
	for _, fe := range validate.Fields(err) {
		if fe.Field == "account" && fe.Code == codeProtected {
			return true
		}
	}
	return false
}

// deleteWith is the deletion of people.md, "Deletion, step by step", in one Write
// transaction.
func (s *Service) deleteWith(ctx context.Context, span telemetry.Span, op string, d deletion) (DeletionReport, error) {
	st, err := s.requireLinked(op)
	if err != nil {
		return DeletionReport{}, err
	}
	var rep DeletionReport
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		var err error
		rep, err = s.deleteInTx(ctx, op, st, d)
		return err
	})
	if err != nil {
		s.metrics.deletion(ctx, outcomeFailed)
		return DeletionReport{}, wrap(op, err)
	}
	span.SetAttrs(telemetry.Int("people", rep.People), telemetry.Int("identity", rep.Identity), telemetry.Int("access", rep.Access),
		telemetry.Int("modules", len(rep.Modules)))
	s.metrics.deletion(ctx, outcomeOK)
	return rep, nil
}

func (s *Service) deleteInTx(ctx context.Context, op string, st *linkState, d deletion) (DeletionReport, error) {
	person := d.person
	if err := s.store.LockDeletion(ctx, person); err != nil {
		return DeletionReport{}, wrap(op, err)
	}
	if !d.request.IsZero() {
		r, err := s.store.RequestForUpdate(ctx, d.request)
		if err != nil {
			return DeletionReport{}, wrap(op, err)
		}
		if r.Status != rows.StatusOpen {
			return DeletionReport{}, fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
	}
	// Lock the row: a contact added concurrently would otherwise be dropped by the cascade
	// without being counted. An account holder with no row (D37) still has an account and
	// grants: the module, access and identity steps run, people's own deletes are no-ops
	// and no people.person-deleted is published. NotFound only when no account exists.
	hasRow := true
	if _, err := s.store.GetForUpdate(ctx, person); err != nil {
		if !errs.Is(err, errs.NotFound) || !d.request.IsZero() {
			return DeletionReport{}, wrap(op, err)
		}
		if aerr := s.accountExists(ctx, op, person); aerr != nil {
			if errs.Is(aerr, errs.NotFound) {
				return DeletionReport{}, wrap(op, err)
			}
			return DeletionReport{}, aerr
		}
		hasRow = false
	}
	rep := DeletionReport{Person: person, Modules: make(map[string]ModuleDeletion, len(st.sources))}
	for _, src := range st.sources {
		md, err := src.Exporter.DeletePerson(ctx, person)
		if err != nil {
			return DeletionReport{}, fmt.Errorf("%s: module %s: %w", op, src.Name, err)
		}
		rep.Modules[src.Name] = md
	}
	var err error
	if rep.Access, err = s.access.DeletePerson(ctx, person); err != nil {
		return DeletionReport{}, fmt.Errorf("%s: access: %w", op, err)
	}
	if rep.Identity, err = s.accounts.DeletePerson(ctx, person); err != nil {
		if isProtected(err) {
			return DeletionReport{}, fieldConflict(op, "account", codeProtected, nil, "this is a shared demo account and cannot be deleted")
		}
		return DeletionReport{}, fmt.Errorf("%s: identity: %w", op, err)
	}
	if hasRow {
		for _, del := range []func(context.Context, model.PersonID) (int, error){
			s.store.DeleteRequestsOf, s.store.DeleteConsentsOf, s.store.DeleteContactsOf,
		} {
			n, err := del(ctx, person)
			if err != nil {
				return DeletionReport{}, wrap(op, err)
			}
			rep.People += n
		}
		n, err := s.store.DeletePerson(ctx, person)
		if err != nil {
			if errs.Is(err, errs.Conflict) {
				return DeletionReport{}, fieldConflict(op, "person", codeInUse,
					map[string]string{"constraint": constraintOf(err)}, "a record elsewhere still refers to this person")
			}
			return DeletionReport{}, wrap(op, err)
		}
		rep.People += n
	}
	kept, err := s.auditCount.CountAbout(ctx, person)
	if err != nil {
		return DeletionReport{}, fmt.Errorf("%s: count the audit entries: %w", op, err)
	}
	if kept > 0 {
		rep.Kept = append(rep.Kept, Kept{Kind: auditEntryKind, Count: kept})
	}
	entries := []audit.Entry{{
		Action: actionDeleted, Outcome: audit.Succeeded, Actor: d.actor, Subject: person,
		Record: audit.RefOf(kindPerson, person), Reason: d.reason,
	}}
	if !d.request.IsZero() {
		entries = append(entries, audit.Entry{
			Action: actionRequestFulfilled, Outcome: audit.Succeeded, Actor: d.actor, Subject: person,
			Record: audit.RefOf(kindRequest, d.request), Reason: d.reason,
		})
	}
	if err := s.record(ctx, entries...); err != nil {
		return DeletionReport{}, err
	}
	if !hasRow {
		return rep, nil
	}
	if err := s.publishPerson(ctx, SignalPersonDeleted, person); err != nil {
		return DeletionReport{}, err
	}
	return rep, nil
}

// ---- deletion requests ------------------------------------------------------------------

func requestOf(r rows.Request) Request {
	return Request{
		ID: r.ID, Person: r.Person, Kind: r.Kind, Status: statusOf(r.Status), Note: r.Note, CreatedAt: r.CreatedAt,
		DecidedAt: r.DecidedAt, DecidedBy: r.DecidedBy, DecisionNote: r.DecisionNote,
	}
}

// statusOf maps a stored status word to the RequestStatus; a word outside the three (the
// table's CHECK forbids it) is the zero status.
func statusOf(word string) RequestStatus {
	for st := RequestOpen; st <= RequestDeclined; st++ {
		if requestStatusWords[st] == word {
			return st
		}
	}
	return 0
}

func requestsOf(rs []rows.Request) []Request {
	out := make([]Request, len(rs))
	for i, r := range rs {
		out[i] = requestOf(r)
	}
	return out
}

// RequestDeletion files the actor's own request to be deleted, which a holder of
// people.data.delete fulfils or declines. At most one request is open at a time. Records
// people.request.created and publishes people.deletion-requested in the transaction, then
// e-mails the holders of people.data.delete that a request is waiting (Options.Notify and
// Options.Holders; nil, nobody to tell, or e-mail not configured means no notice and no
// failure). Fails errs.Unauthenticated, errs.Invalid (F41), errs.Conflict (field request,
// code duplicate: F40; nothing is sent), errs.NotFound (no profile row yet), the kind of a
// failed HoldersOf or Send other than Unsupported (the request is rolled back: F62) and
// store kinds.
func (s *Service) RequestDeletion(ctx context.Context, note string) (_ Request, err error) {
	const op = "people.Service.RequestDeletion"
	ctx, span := s.start(ctx, "request-deletion")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Request{}, err
	}
	span.SetAttrs(telemetry.ID("person_id", a.Person))
	note, err = checkNote(op, "note", note)
	if err != nil {
		return Request{}, err
	}
	id := model.NewID[RequestID]()
	var out rows.Request
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockPerson(ctx, a.Person); err != nil {
			return wrap(op, err)
		}
		var err error
		out, err = s.store.InsertRequest(ctx, id, a.Person, note, s.now())
		if err != nil {
			if errs.Is(err, errs.Conflict) {
				return fieldConflict(op, "request", codeDuplicate, nil, "you already have an open request")
			}
			return wrap(op, err)
		}
		if err := s.record(ctx, audit.Entry{
			Action: actionRequestCreated, Outcome: audit.Succeeded, Subject: a.Person, Record: audit.RefOf(kindRequest, id),
		}); err != nil {
			return err
		}
		if err := s.publish(ctx, SignalDeletionRequested, requestRef(id), personRef(a.Person)); err != nil {
			return err
		}
		return s.notifyDeletionRequested(ctx, op, id)
	})
	if err != nil {
		return Request{}, wrap(op, err)
	}
	return requestOf(out), nil
}

// MyRequests returns the actor's own deletion requests, newest first. Fails
// errs.Unauthenticated and store kinds.
func (s *Service) MyRequests(ctx context.Context) (_ []Request, err error) {
	const op = "people.Service.MyRequests"
	ctx, span := s.start(ctx, "my-requests")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return nil, err
	}
	rs, err := s.store.MyRequests(ctx, a.Person, maxRequestsListed)
	if err != nil {
		return nil, wrap(op, err)
	}
	return requestsOf(rs), nil
}

// WithdrawRequest withdraws the actor's own open request. Records people.request.withdrawn.
// Fails errs.NotFound (not theirs: F42), errs.Conflict (status: not open) and store kinds.
func (s *Service) WithdrawRequest(ctx context.Context, id RequestID) (err error) {
	const op = "people.Service.WithdrawRequest"
	ctx, span := s.start(ctx, "withdraw-request")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return err
	}
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		_, decided, err := s.store.WithdrawRequest(ctx, id, a.Person, s.now())
		if err != nil {
			return wrap(op, err)
		}
		if !decided {
			cur, err := s.store.GetRequest(ctx, id)
			if err != nil {
				return wrap(op, err)
			}
			if cur.Person != a.Person {
				return notFoundRequest(op)
			}
			return fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
		return s.record(ctx, audit.Entry{
			Action: actionRequestWithdraw, Outcome: audit.Succeeded, Subject: a.Person, Record: audit.RefOf(kindRequest, id),
		})
	})
	return wrap(op, err)
}

func notFoundRequest(op string) error {
	return errs.E(op, errs.NotFound, errors.New("no such request"))
}

// ListRequests returns the deletion requests with the status (open when zero), oldest
// first, optionally for one person. Needs people.data.delete. Reading a request that has a
// note, someone else's, records people.request.read (F57). Fails errs.Permission (F44),
// errs.Invalid (status), the recorder's kind and store kinds.
func (s *Service) ListRequests(ctx context.Context, q RequestQuery) (_ []Request, err error) {
	const op = "people.Service.ListRequests"
	ctx, span := s.start(ctx, "list-requests")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return nil, err
	}
	status := q.Status
	if status == 0 {
		status = RequestOpen
	}
	if !status.valid() {
		return nil, fieldInvalid(op, "status", codeOneOf, "must be one of: open, withdrawn, declined")
	}
	rs, err := s.store.ListRequests(ctx, status.String(), q.Person, maxRequestsListed)
	if err != nil {
		return nil, wrap(op, err)
	}
	// A note is the person's own words and may say anything: reading someone else's is
	// recorded after loading and before returning, one entry per request with a note (D37).
	var reads []audit.Entry
	for _, r := range rs {
		if r.Note == "" || r.Person == a.Person {
			continue
		}
		reads = append(reads, audit.Entry{
			Action: actionRequestRead, Outcome: audit.Succeeded, Subject: r.Person, Record: audit.RefOf(kindRequest, r.ID),
			Fields: []string{"note"}, Level: model.SensitivityPersonal,
		})
	}
	if len(reads) > 0 {
		if err := s.record(ctx, reads...); err != nil {
			return nil, err
		}
	}
	return requestsOf(rs), nil
}

// DeclineRequest declines an open request with a note for the person. Needs
// people.data.delete. Records people.request.declined. Fails errs.Permission (F44),
// errs.Invalid (F41), errs.NotFound, errs.Conflict (status: F43) and store kinds.
func (s *Service) DeclineRequest(ctx context.Context, id RequestID, note string) (_ Request, err error) {
	const op = "people.Service.DeclineRequest"
	ctx, span := s.start(ctx, "decline-request")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Request{}, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return Request{}, err
	}
	note, err = checkNote(op, "note", note)
	if err != nil {
		return Request{}, err
	}
	var out rows.Request
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		r, decided, err := s.store.DeclineRequest(ctx, id, a.Person, note, s.now())
		if err != nil {
			return wrap(op, err)
		}
		if !decided {
			if _, err := s.store.GetRequest(ctx, id); err != nil {
				return wrap(op, err)
			}
			return fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
		out = r
		return s.record(ctx, audit.Entry{
			Action: actionRequestDeclined, Outcome: audit.Succeeded, Subject: r.Person, Record: audit.RefOf(kindRequest, id),
		})
	})
	if err != nil {
		return Request{}, wrap(op, err)
	}
	return requestOf(out), nil
}

// FulfilRequest deletes the request's person as Delete does, with Reason "request:<id>",
// and records people.request.fulfilled with it. Needs people.data.delete; not for yourself.
// Fails as Delete does, errs.NotFound for an unknown request and errs.Conflict (status:
// F43).
func (s *Service) FulfilRequest(ctx context.Context, id RequestID) (_ DeletionReport, err error) {
	const op = "people.Service.FulfilRequest"
	ctx, span := s.start(ctx, "fulfil-request")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return DeletionReport{}, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return DeletionReport{}, err
	}
	if _, err := s.requireLinked(op); err != nil {
		return DeletionReport{}, err
	}
	// The request names the person, which the lock needs, so it is read first inside the
	// transaction; deleteInTx then re-reads it under the lock.
	var rep DeletionReport
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		r, err := s.store.GetRequest(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if r.Person == a.Person {
			return fieldInvalid(op, "person", codeSelf, "you cannot delete yourself; ask another administrator")
		}
		if r.Status != rows.StatusOpen {
			return fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
		st, _ := s.links()
		rep, err = s.deleteInTx(ctx, op, st, deletion{person: r.Person, reason: "request:" + id.String(), request: id})
		return err
	})
	if err != nil {
		s.metrics.deletion(ctx, outcomeFailed)
		return DeletionReport{}, wrap(op, err)
	}
	span.SetAttrs(telemetry.ID("person_id", rep.Person), telemetry.Int("people", rep.People))
	s.metrics.deletion(ctx, outcomeOK)
	return rep, nil
}
