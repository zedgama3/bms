package datarights

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/datarights/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Delete removes the person and everything about them, in one transaction: every module's
// PersonData in build order, then files', then notify's, then authz's grants, then
// identity's account, then people's own rows. Any failure rolls everything back. Audit
// rows stay and are counted in the report as Kept "audit.entry". Needs people.data.delete;
// not for yourself. Records datarights.delete and, through people, publishes
// people.person-deleted. Fails errs.Permission (F32), errs.Invalid (F33), errs.NotFound
// (F11), errs.Internal before Link (F31), errs.Conflict (last_access_admin: F34; in_use:
// F37), identity's errs.Permission for a superuser account (F35), errs.Conflict (field
// account, code protected: F89) for a shared demo account that identity's Guard protects, a
// source's kind wrapped with its name (F36), errs.Unavailable wrapping db.ErrReadOnly (F38)
// and the recorder's kind (F39).
func (s *Service) Delete(ctx context.Context, person model.PersonID) (_ DeletionReport, err error) {
	const op = "datarights.Service.Delete"
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
	const op = "datarights.Service.DeleteCommand"
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
// error, so datarights does not import identity's types for it.
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

// deleteInTx runs the deletion inside the Write transaction ctx carries. The order is the
// modules in build order, files, notify, access, identity, people: foreign keys point at
// core.person with RESTRICT, so the person row goes last.
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
	// An account holder with no profile row (D37) still has an account and grants: the
	// source, access and identity steps run, people's own deletes are no-ops and no
	// people.person-deleted is published. NotFound only when no account exists either, and
	// always for a request, which cannot exist without its person.
	hasRow := true
	if err := s.people.Exists(ctx, person); err != nil {
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
	rep := DeletionReport{Person: person, Modules: make(map[string]Deletion, len(st.parts))}
	for _, p := range st.parts {
		md, err := p.del(ctx, person)
		if err != nil {
			return DeletionReport{}, fmt.Errorf("%s: module %s: %w", op, p.name, err)
		}
		rep.Modules[p.name] = md
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
		n, err := s.store.DeleteRequestsOf(ctx, person)
		if err != nil {
			return DeletionReport{}, wrap(op, err)
		}
		rep.People += n
		n, err = s.people.DeletePerson(ctx, person)
		if err != nil {
			if errs.Is(err, errs.Conflict) {
				return DeletionReport{}, fieldConflict(op, "person", codeInUse,
					map[string]string{"constraint": constraintOf(err)}, "a record elsewhere still refers to this person")
			}
			return DeletionReport{}, wrap(op, err)
		}
		rep.People += n
	}
	kept, err := s.auditLog.CountAbout(ctx, person)
	if err != nil {
		return DeletionReport{}, fmt.Errorf("%s: count the audit entries: %w", op, err)
	}
	if kept > 0 {
		rep.Kept = append(rep.Kept, Kept{Kind: auditEntryKind, Count: kept})
	}
	entries := []audit.Entry{{
		Action: actionDelete, Outcome: audit.Succeeded, Actor: d.actor, Subject: person,
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
	return rep, nil
}
