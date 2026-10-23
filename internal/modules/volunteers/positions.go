package volunteers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// withRequirements loads the requirement sets of ps and sets them on the positions.
func (s *Service) withRequirements(ctx context.Context, ps []vol.Position) error {
	if len(ps) == 0 {
		return nil
	}
	ids := make([]vol.PositionID, len(ps))
	for i := range ps {
		ids[i] = ps[i].ID
	}
	for chunk := range slices.Chunk(ids, vol.MaxIDs) {
		reqs, err := s.store.RequirementsOf(ctx, chunk)
		if err != nil {
			return fmt.Errorf("requirements: %w", err)
		}
		for i := range ps {
			if r, ok := reqs[ps[i].ID]; ok {
				ps[i].Requirements = r
			}
		}
	}
	return nil
}

// ListPositions lists a department's positions with their requirements: active ones to every
// signed-in person (they label shifts), archived ones only with positions.manage at the
// department. Fails errs.Unauthenticated, errs.Invalid (zero department), errs.Permission
// (includeArchived without the permission) and store kinds.
func (s *Service) ListPositions(ctx context.Context, dept model.DepartmentID, includeArchived bool) (_ []vol.Position, err error) {
	const op = "volunteers.ListPositions"
	ctx, span := s.begin(ctx, "volunteers.list-positions")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return nil, err
	}
	if dept.IsZero() {
		return nil, invalid(op, "department", validate.CodeRequired, "must be set")
	}
	if includeArchived {
		if err := s.allowIn(ctx, vol.PermissionPositionsManage, dept); err != nil {
			return nil, wrap(op, err)
		}
	}
	out, err := s.store.ListPositions(ctx, dept, includeArchived)
	if err != nil {
		return nil, wrap(op, err)
	}
	if out == nil {
		out = []vol.Position{}
	}
	if err := s.withRequirements(ctx, out); err != nil {
		return nil, wrap(op, err)
	}
	if err := filterAll(plainView(actor), out, noRel); err != nil {
		return nil, wrap(op, err)
	}
	return out, nil
}

// CreatePosition adds a position to a department, which must be active. It needs
// positions.manage at the department. Records position.created. Fails errs.Unauthenticated,
// errs.Invalid, errs.Permission, errs.NotFound (unknown department), errs.Conflict (archived;
// name, taken), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) CreatePosition(ctx context.Context, dept model.DepartmentID, in vol.PositionInput) (_ vol.Position, err error) {
	const op = "volunteers.CreatePosition"
	ctx, span := s.begin(ctx, "volunteers.create-position")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Position{}, err
	}
	if dept.IsZero() {
		return vol.Position{}, invalid(op, "department", validate.CodeRequired, "must be set")
	}
	if err := in.Validate(); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	if err := s.allowIn(ctx, vol.PermissionPositionsManage, dept); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	if err := s.requireActive(ctx, dept); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	now := s.now()
	p := vol.Position{
		ID: model.NewID[vol.PositionID](), Department: dept, Name: strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description), Access: in.Access, HideShiftMates: in.HideShiftMates,
		CreatedAt: now, UpdatedAt: now,
	}
	var saved vol.Position
	err = s.write(ctx, func(ctx context.Context) error {
		var ierr error
		if saved, ierr = s.store.InsertPosition(ctx, p); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		return s.record(ctx, act("volunteers.position.created", ref(kindPosition, saved.ID), model.PersonID{}, ""))
	})
	if err != nil {
		return vol.Position{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	return saved, nil
}

// UpdatePosition changes a position's name, description, access and HideShiftMates. It
// needs positions.manage at the position's department. Records position.changed. Fails
// errs.Unauthenticated, errs.Invalid, errs.Permission, errs.NotFound, errs.Conflict
// (archived; name, taken), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) UpdatePosition(ctx context.Context, id vol.PositionID, in vol.PositionInput) (_ vol.Position, err error) {
	const op = "volunteers.UpdatePosition"
	ctx, span := s.begin(ctx, "volunteers.update-position")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Position{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	cur, err := s.store.GetPosition(ctx, id)
	if err != nil {
		return vol.Position{}, wrap(op, err)
	}
	if err := s.allowIn(ctx, vol.PermissionPositionsManage, cur.Department); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	var saved vol.Position
	err = s.write(ctx, func(ctx context.Context) error {
		locked, gerr := s.store.GetPositionForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Archived() {
			return archivedErr("position")
		}
		locked.Name, locked.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
		locked.Access, locked.HideShiftMates, locked.UpdatedAt = in.Access, in.HideShiftMates, s.now()
		var uerr error
		if saved, uerr = s.store.UpdatePosition(ctx, locked); uerr != nil {
			return fmt.Errorf("update: %w", uerr)
		}
		return s.record(ctx, act("volunteers.position.changed", ref(kindPosition, id), model.PersonID{}, ""))
	})
	if err != nil {
		return vol.Position{}, wrap(op, err)
	}
	ps := []vol.Position{saved}
	if err := s.withRequirements(ctx, ps); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	saved = ps[0]
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	return saved, nil
}

// ArchivePosition hides a position from pickers; it refuses while the position has future,
// non-canceled shifts. Permission as UpdatePosition. Records position.archived. Fails
// errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Conflict (archived; in_use),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) ArchivePosition(ctx context.Context, id vol.PositionID) (err error) {
	const op = "volunteers.ArchivePosition"
	ctx, span := s.begin(ctx, "volunteers.archive-position")
	defer endSpan(span, &err)
	if _, err := s.actorOf(ctx, op); err != nil {
		return err
	}
	cur, err := s.store.GetPosition(ctx, id)
	if err != nil {
		return wrap(op, err)
	}
	if err := s.allowIn(ctx, vol.PermissionPositionsManage, cur.Department); err != nil {
		return wrap(op, err)
	}
	err = s.write(ctx, func(ctx context.Context) error {
		locked, gerr := s.store.GetPositionForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Archived() {
			return archivedErr("position")
		}
		now := s.now()
		n, cerr := s.store.CountFutureShifts(ctx, id, now)
		if cerr != nil {
			return fmt.Errorf("count shifts: %w", cerr)
		}
		if n > 0 {
			return conflict(op, "position", vol.ConflictInUse, "the position has shifts that have not ended", nil)
		}
		if _, aerr := s.store.ArchivePosition(ctx, id, now); aerr != nil {
			return fmt.Errorf("archive: %w", aerr)
		}
		return s.record(ctx, act("volunteers.position.archived", ref(kindPosition, id), model.PersonID{}, ""))
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// SetRequirements replaces the position's set of required qualifications; each must exist,
// be active and be org-wide or of the position's department. Existing bookings are not
// re-checked. It needs positions.manage at the department. Records
// position.requirements-changed. Fails errs.Unauthenticated, errs.Invalid (too_many,
// duplicate, unknown, scope), errs.Permission, errs.NotFound, errs.Conflict (archived),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) SetRequirements(ctx context.Context, id vol.PositionID, quals []vol.QualificationID) (_ vol.Position, err error) {
	const op = "volunteers.SetRequirements"
	ctx, span := s.begin(ctx, "volunteers.set-requirements")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Position{}, err
	}
	set := slices.Clone(quals)
	slices.SortFunc(set, cmpBytes[vol.QualificationID])
	switch {
	case len(set) > vol.MaxRequirements:
		return vol.Position{}, wrap(op, fieldProblem("", "qualifications", vol.CodeTooMany, "too many requirements",
			map[string]string{"max": fmt.Sprint(vol.MaxRequirements)}))
	case len(slices.Compact(slices.Clone(set))) != len(set):
		return vol.Position{}, invalid(op, "qualifications", vol.CodeDuplicate, "a qualification is listed twice")
	case slices.ContainsFunc(set, func(q vol.QualificationID) bool { return q.IsZero() }):
		return vol.Position{}, invalid(op, "qualifications", validate.CodeRequired, "an ID is empty")
	}
	cur, err := s.store.GetPosition(ctx, id)
	if err != nil {
		return vol.Position{}, wrap(op, err)
	}
	if err := s.allowIn(ctx, vol.PermissionPositionsManage, cur.Department); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	var saved vol.Position
	err = s.write(ctx, func(ctx context.Context) error {
		locked, gerr := s.store.GetPositionForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Archived() {
			return archivedErr("position")
		}
		if err := s.checkRequirements(ctx, locked.Department, set); err != nil {
			return err
		}
		now := s.now()
		if _, derr := s.store.DeleteRequirements(ctx, id); derr != nil {
			return fmt.Errorf("clear: %w", derr)
		}
		if len(set) > 0 {
			if _, ierr := s.store.InsertRequirements(ctx, id, set, now); ierr != nil {
				return fmt.Errorf("insert: %w", ierr)
			}
		}
		var terr error
		if saved, terr = s.store.TouchPosition(ctx, id, now); terr != nil {
			return fmt.Errorf("touch: %w", terr)
		}
		saved.Requirements = set
		return s.record(ctx, act("volunteers.position.requirements-changed", ref(kindPosition, id), model.PersonID{}, ""))
	})
	if err != nil {
		return vol.Position{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Position{}, wrap(op, err)
	}
	return saved, nil
}

// checkRequirements fails when a qualification of set is unknown (Invalid unknown), of
// another department (Invalid scope) or archived (Conflict archived).
func (s *Service) checkRequirements(ctx context.Context, dept model.DepartmentID, set []vol.QualificationID) error {
	if len(set) == 0 {
		return nil
	}
	found, err := s.store.QualificationsByIDs(ctx, set)
	if err != nil {
		return fmt.Errorf("qualifications: %w", err)
	}
	byID := make(map[vol.QualificationID]vol.Qualification, len(found))
	for _, q := range found {
		byID[q.ID] = q
	}
	for _, id := range set {
		q, ok := byID[id]
		switch {
		case !ok:
			return invalid("volunteers.checkRequirements", "qualifications", vol.CodeUnknown, "no such qualification")
		case !q.Department.IsZero() && q.Department != dept:
			return invalid("volunteers.checkRequirements", "qualifications", vol.CodeScope, "a qualification of another department")
		case q.Archived():
			return archivedErr("qualification")
		}
	}
	return nil
}
