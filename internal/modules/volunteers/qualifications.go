package volunteers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// ListQualifications lists the catalogue: active entries to every signed-in person (they
// label positions), archived ones only inside the caller's qualifications.manage extent.
// With q.Department set it lists that department's entries plus the org-wide ones. Fails
// errs.Unauthenticated, errs.Invalid, errs.Permission (IncludeArchived with no
// qualifications.manage extent in scope) and store kinds.
func (s *Service) ListQualifications(ctx context.Context, q vol.QualificationQuery) (_ []vol.Qualification, err error) {
	const op = "volunteers.ListQualifications"
	ctx, span := s.begin(ctx, "volunteers.list-qualifications")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return nil, err
	}
	f := vol.QualificationFilter{Department: q.Department, IncludeOrgWide: !q.Department.IsZero(), Limit: vol.MaxList}
	if q.IncludeArchived {
		ext, err := s.extent(ctx, vol.PermissionQualificationsManage)
		if err != nil {
			return nil, wrap(op, err)
		}
		switch {
		case q.Department.IsZero() && ext.IsZero(), !q.Department.IsZero() && !ext.Covers(q.Department):
			return nil, errs.E(op, errs.Permission, errors.New("archived qualifications need qualifications.manage"))
		}
		f.ArchivedOrgWide = ext.Site
		f.ArchivedDepartments = ext.Departments
		if ext.Site && !q.Department.IsZero() {
			f.ArchivedDepartments = []model.DepartmentID{q.Department}
		}
	}
	out, err := s.store.ListQualifications(ctx, f)
	if err != nil {
		return nil, wrap(op, err)
	}
	if out == nil {
		out = []vol.Qualification{}
	}
	if err := filterAll(plainView(actor), out, noRel); err != nil {
		return nil, wrap(op, err)
	}
	return out, nil
}

// allowManageQualification checks qualifications.manage for a qualification of department d:
// at the department for a department-specific one, site-wide for an org-wide one.
func (s *Service) allowManageQualification(ctx context.Context, d model.DepartmentID) error {
	if d.IsZero() {
		if err := s.az.Allow(ctx, vol.PermissionQualificationsManage); err != nil {
			return fmt.Errorf("%s site-wide: %w", vol.PermissionQualificationsManage, err)
		}
		return nil
	}
	return s.allowIn(ctx, vol.PermissionQualificationsManage, d)
}

// CreateQualification adds a catalogue entry. It needs qualifications.manage site-wide for
// an org-wide entry, or at the department for a department-specific one, which must be
// active. Records qualification.created. Fails errs.Unauthenticated, errs.Invalid,
// errs.Permission, errs.NotFound (unknown department), errs.Conflict (archived; name, taken),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) CreateQualification(ctx context.Context, in vol.QualificationInput) (_ vol.Qualification, err error) {
	const op = "volunteers.CreateQualification"
	ctx, span := s.begin(ctx, "volunteers.create-qualification")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Qualification{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	if err := s.allowManageQualification(ctx, in.Department); err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	if !in.Department.IsZero() {
		if err := s.requireActive(ctx, in.Department); err != nil {
			return vol.Qualification{}, wrap(op, err)
		}
	}
	now := s.now()
	q := vol.Qualification{
		ID: model.NewID[vol.QualificationID](), Department: in.Department,
		Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description),
		ValidityDays: in.ValidityDays, CreatedAt: now, UpdatedAt: now,
	}
	var saved vol.Qualification
	err = s.write(ctx, func(ctx context.Context) error {
		var ierr error
		if saved, ierr = s.store.InsertQualification(ctx, q); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		return s.record(ctx, act("volunteers.qualification.created", ref(kindQualification, saved.ID), model.PersonID{}, ""))
	})
	if err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	return saved, nil
}

// UpdateQualification changes name, description or validity; the department never changes
// (in.Department must equal the entry's: errs.Invalid code scope). Permission as
// CreateQualification. Records qualification.changed. Fails errs.Unauthenticated,
// errs.Invalid, errs.Permission, errs.NotFound, errs.Conflict (archived; name, taken),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) UpdateQualification(ctx context.Context, id vol.QualificationID, in vol.QualificationInput) (_ vol.Qualification, err error) {
	const op = "volunteers.UpdateQualification"
	ctx, span := s.begin(ctx, "volunteers.update-qualification")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Qualification{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	cur, err := s.store.GetQualification(ctx, id)
	if err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	if err := s.allowManageQualification(ctx, cur.Department); err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	if in.Department != cur.Department {
		return vol.Qualification{}, wrap(op, fieldProblem("", "department", vol.CodeScope, "the department of a qualification never changes", nil))
	}
	var saved vol.Qualification
	err = s.write(ctx, func(ctx context.Context) error {
		locked, gerr := s.store.GetQualificationForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Archived() {
			return archivedErr("qualification")
		}
		locked.Name, locked.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
		locked.ValidityDays, locked.UpdatedAt = in.ValidityDays, s.now()
		var uerr error
		if saved, uerr = s.store.UpdateQualification(ctx, locked); uerr != nil {
			return fmt.Errorf("update: %w", uerr)
		}
		return s.record(ctx, act("volunteers.qualification.changed", ref(kindQualification, id), model.PersonID{}, ""))
	})
	if err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Qualification{}, wrap(op, err)
	}
	return saved, nil
}

// ArchiveQualification hides a qualification from pickers. Records keep counting until they
// expire and positions keep requiring it. Permission as UpdateQualification. Records
// qualification.archived. Fails errs.Unauthenticated, errs.Permission, errs.NotFound,
// errs.Conflict (archived), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) ArchiveQualification(ctx context.Context, id vol.QualificationID) (err error) {
	const op = "volunteers.ArchiveQualification"
	ctx, span := s.begin(ctx, "volunteers.archive-qualification")
	defer endSpan(span, &err)
	if _, err := s.actorOf(ctx, op); err != nil {
		return err
	}
	cur, err := s.store.GetQualification(ctx, id)
	if err != nil {
		return wrap(op, err)
	}
	if err := s.allowManageQualification(ctx, cur.Department); err != nil {
		return wrap(op, err)
	}
	err = s.write(ctx, func(ctx context.Context) error {
		locked, gerr := s.store.GetQualificationForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Archived() {
			return archivedErr("qualification")
		}
		if _, aerr := s.store.ArchiveQualification(ctx, id, s.now()); aerr != nil {
			return fmt.Errorf("archive: %w", aerr)
		}
		return s.record(ctx, act("volunteers.qualification.archived", ref(kindQualification, id), model.PersonID{}, ""))
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// archivedErr is errs.Conflict code "archived" on the named thing.
func archivedErr(what string) error {
	return conflict("volunteers", what, vol.ConflictArchived, "it is archived", nil)
}
