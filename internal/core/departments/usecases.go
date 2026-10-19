package departments

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// List returns the active departments by name, and the archived ones too when
// q.IncludeArchived. Any signed-in person may list the active ones (decision 2). Fails
// errs.Unauthenticated, errs.Permission (IncludeArchived without departments.manage) and
// store kinds.
func (s *Service) List(ctx context.Context, q ListQuery) (_ []Department, err error) {
	const op = "departments.Service.List"
	ctx, span := s.start(ctx, "list")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return nil, err
	}
	if q.IncludeArchived {
		if err := s.require(ctx, op, PermissionManage); err != nil {
			return nil, err
		}
	}
	span.SetAttrs(telemetry.Bool("include_archived", q.IncludeArchived))
	rs, err := s.store.List(ctx, q.IncludeArchived)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]Department, len(rs))
	for i, r := range rs {
		out[i] = departmentOf(r)
	}
	span.SetAttrs(telemetry.Int("departments", len(out)))
	return out, nil
}

// Get returns one department. An archived department is NotFound unless the caller holds
// departments.manage or departments.details.manage at it, so the answer never confirms that
// it exists. Fails errs.Unauthenticated, errs.NotFound and store kinds.
func (s *Service) Get(ctx context.Context, id model.DepartmentID) (_ Department, err error) {
	const op = "departments.Service.Get"
	ctx, span := s.start(ctx, "get")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Department{}, err
	}
	if id.IsZero() {
		return Department{}, notFound(op)
	}
	span.SetAttrs(telemetry.ID("department_id", id))
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return Department{}, wrap(op, err)
	}
	if !r.ArchivedAt.IsZero() {
		seen, err := s.mayReadArchived(ctx, op, id)
		if err != nil {
			return Department{}, err
		}
		if !seen {
			return Department{}, notFound(op)
		}
	}
	return departmentOf(r), nil
}

// mayReadArchived reports whether the actor holds departments.manage, or
// departments.details.manage at id. Only errs.Permission is "no"; any other failure is
// returned.
func (s *Service) mayReadArchived(ctx context.Context, op string, id model.DepartmentID) (bool, error) {
	err := s.authz.Allow(ctx, PermissionManage)
	if err == nil {
		return true, nil
	}
	if !errs.Is(err, errs.Permission) {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	err = s.authz.AllowIn(ctx, PermissionDetailsManage, id)
	switch {
	case err == nil:
		return true, nil
	case errs.Is(err, errs.Permission):
		return false, nil
	default:
		return false, fmt.Errorf("%s: %w", op, err)
	}
}

// Create makes a department. Needs departments.manage. Records
// departments.department.created and publishes departments.department-created in the
// transaction. Fails errs.Unauthenticated, errs.Invalid, errs.Permission, errs.Conflict
// (name, taken), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) Create(ctx context.Context, in CreateInput) (_ Department, err error) {
	const op = "departments.Service.Create"
	ctx, span := s.start(ctx, "create")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Department{}, err
	}
	n, err := in.normalize(op)
	if err != nil {
		return Department{}, err
	}
	if err := s.require(ctx, op, PermissionManage); err != nil {
		return Department{}, err
	}
	id := model.NewID[model.DepartmentID]()
	span.SetAttrs(telemetry.ID("department_id", id))
	var row rows.Department
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		now := s.now()
		saved, err := s.store.Insert(ctx, rows.Department{
			ID: id, Name: *n.name, Description: *n.description, ContactEmail: *n.contactEmail,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return takenOr(op, err)
		}
		row = saved
		if err := s.record(ctx, audit.Entry{
			Action: actionCreated, Outcome: audit.Succeeded, Record: audit.RefOf(kindDepartment, id),
		}); err != nil {
			return err
		}
		return s.publish(ctx, SignalCreated, id)
	})
	if err != nil {
		return Department{}, wrap(op, err)
	}
	return departmentOf(row), nil
}

// conDepartmentName is the unique index on lower(name) (migration 0001); its violation is
// the only Conflict that means "name taken".
const conDepartmentName = "department_name_key"

// takenOr turns a violation of department_name_key into Conflict (name, taken). Any other
// error, a Conflict from another constraint too, keeps its own kind and stays generic.
func takenOr(op string, err error) error {
	var pe *db.Error
	if errs.Is(err, errs.Conflict) && errors.As(err, &pe) && pe.Constraint == conDepartmentName {
		return fieldConflict(op, fieldName, codeTaken, "another department has this name")
	}
	return wrap(op, err)
}

// Update changes the fields set on the input. Name needs departments.manage; the other
// fields need departments.details.manage at the department; both are checked when both
// kinds are set. Records departments.department.renamed and/or .changed (the field names,
// not their values). Changing nothing writes nothing. Fails errs.Unauthenticated,
// errs.Invalid (body, required, for no field set), errs.Permission, errs.NotFound,
// errs.Conflict (archived; name, taken), errs.Unavailable wrapping db.ErrReadOnly and
// store kinds.
func (s *Service) Update(ctx context.Context, id model.DepartmentID, in UpdateInput) (_ Department, err error) {
	const op = "departments.Service.Update"
	ctx, span := s.start(ctx, "update")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Department{}, err
	}
	if id.IsZero() {
		return Department{}, notFound(op)
	}
	span.SetAttrs(telemetry.ID("department_id", id))
	n, err := in.normalize(op, true)
	if err != nil {
		return Department{}, err
	}
	if n.name != nil {
		if err := s.require(ctx, op, PermissionManage); err != nil {
			return Department{}, err
		}
	}
	if n.description != nil || n.contactEmail != nil {
		if err := s.requireIn(ctx, op, PermissionDetailsManage, id); err != nil {
			return Department{}, err
		}
	}
	var row rows.Department
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, err := s.store.GetForUpdate(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if !cur.ArchivedAt.IsZero() {
			return fieldConflict(op, "department", codeArchived, "the department is archived; restore it first")
		}
		patch := rows.Patch{ID: id}
		var details []string
		renamed := n.name != nil && *n.name != cur.Name
		if renamed {
			patch.Name = n.name
		}
		if n.description != nil && *n.description != cur.Description {
			patch.Description = n.description
			details = append(details, fieldDescription)
		}
		if n.contactEmail != nil && *n.contactEmail != cur.ContactEmail {
			patch.ContactEmail = n.contactEmail
			details = append(details, fieldContactEmail)
		}
		if !renamed && len(details) == 0 {
			row = cur
			return nil
		}
		if row, err = s.store.Update(ctx, patch, s.now()); err != nil {
			return takenOr(op, err)
		}
		var entries []audit.Entry
		if renamed {
			entries = append(entries, audit.Entry{
				Action: actionRenamed, Outcome: audit.Succeeded, Record: audit.RefOf(kindDepartment, id),
				Fields: []string{fieldName}, Level: model.SensitivityInternal,
			})
		}
		if len(details) > 0 {
			entries = append(entries, audit.Entry{
				Action: actionChanged, Outcome: audit.Succeeded, Record: audit.RefOf(kindDepartment, id),
				Fields: details, Level: model.SensitivityInternal,
			})
		}
		return s.record(ctx, entries...)
	})
	if err != nil {
		return Department{}, wrap(op, err)
	}
	return departmentOf(row), nil
}

// Archive archives an active department; grants at it stay (decision 4). Needs
// departments.manage. Records departments.department.archived and publishes
// departments.department-archived in the transaction. Fails errs.Unauthenticated,
// errs.Permission, errs.NotFound, errs.Conflict (archived), errs.Unavailable wrapping
// db.ErrReadOnly and store kinds.
func (s *Service) Archive(ctx context.Context, id model.DepartmentID) (_ Department, err error) {
	return s.flip(ctx, "departments.Service.Archive", "archive", id, true)
}

// Restore is Archive's inverse: it fails errs.Conflict (not_archived) for an active
// department, and records and publishes the restored names.
func (s *Service) Restore(ctx context.Context, id model.DepartmentID) (_ Department, err error) {
	return s.flip(ctx, "departments.Service.Restore", "restore", id, false)
}

// flip archives (archive true) or restores the department in one transaction.
func (s *Service) flip(ctx context.Context, op, spanName string, id model.DepartmentID, archive bool) (_ Department, err error) {
	ctx, span := s.start(ctx, spanName)
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Department{}, err
	}
	if err := s.require(ctx, op, PermissionManage); err != nil {
		return Department{}, err
	}
	if id.IsZero() {
		return Department{}, notFound(op)
	}
	span.SetAttrs(telemetry.ID("department_id", id))
	var row rows.Department
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, err := s.store.GetForUpdate(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		action, signal := actionRestored, SignalRestored
		switch {
		case archive && !cur.ArchivedAt.IsZero():
			return fieldConflict(op, "department", codeArchived, "the department is already archived")
		case !archive && cur.ArchivedAt.IsZero():
			return fieldConflict(op, "department", codeNotArch, "the department is not archived")
		case archive:
			action, signal = actionArchived, SignalArchived
			row, err = s.store.Archive(ctx, id, s.now())
		default:
			row, err = s.store.Restore(ctx, id, s.now())
		}
		if errs.Is(err, errs.Conflict) { // the store's safety net: the state changed under the lock
			code := codeNotArch
			if archive {
				code = codeArchived
			}
			return fieldConflict(op, "department", code, "the department is not in the state the change needs")
		}
		if err != nil {
			return wrap(op, err)
		}
		if err := s.record(ctx, audit.Entry{
			Action: action, Outcome: audit.Succeeded, Record: audit.RefOf(kindDepartment, id),
		}); err != nil {
			return err
		}
		return s.publish(ctx, signal, id)
	})
	if err != nil {
		return Department{}, wrap(op, err)
	}
	return departmentOf(row), nil
}

// NamesOf maps each known department, archived included, to its name; unknown IDs are
// absent. It needs no actor: it returns internal data to code that already passed its own
// permission check or runs as the system. Fails errs.Invalid (ids, too_many {max}) for more
// than MaxNames IDs, and with the store's kinds.
func (s *Service) NamesOf(ctx context.Context, ids []model.DepartmentID) (_ map[model.DepartmentID]string, err error) {
	const op = "departments.Service.NamesOf"
	ctx, span := s.start(ctx, "names-of")
	defer endSpan(span, &err)
	if len(ids) > MaxNames {
		return nil, paramInvalid(op, "ids", codeTooMany, map[string]string{"max": fmt.Sprint(MaxNames)},
			fmt.Sprintf("at most %d IDs", MaxNames))
	}
	unique := make([]model.DepartmentID, 0, len(ids))
	seen := make(map[model.DepartmentID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id.IsZero() {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	span.SetAttrs(telemetry.Int("ids", len(unique)))
	if len(unique) == 0 {
		return map[model.DepartmentID]string{}, nil
	}
	names, err := s.store.NamesOf(ctx, unique)
	if err != nil {
		return nil, wrap(op, err)
	}
	return names, nil
}

// Active reports whether the department exists and is not archived. It answers (false,
// nil) for an unknown and for an archived department alike, so it never leaks which. It
// needs no actor and joins the transaction ctx carries (authz's Grant calls it inside its
// own). Fails errs.Invalid (id, required) for the zero ID, and with the store's kinds.
func (s *Service) Active(ctx context.Context, id model.DepartmentID) (_ bool, err error) {
	const op = "departments.Service.Active"
	ctx, span := s.start(ctx, "active")
	defer endSpan(span, &err)
	if id.IsZero() {
		return false, fieldInvalid(op, "id", codeRequired, "must be set")
	}
	span.SetAttrs(telemetry.ID("department_id", id))
	ok, err := s.store.Active(ctx, id)
	if err != nil {
		return false, wrap(op, err)
	}
	return ok, nil
}
