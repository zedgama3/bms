package volunteers

import (
	"context"
	"fmt"
	"slices"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// namesOf returns the playa names of ids (in chunks the directory accepts); a person with no
// playa name or no profile is absent or "".
func (s *Service) namesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	out := make(map[model.PersonID]string, len(ids))
	for chunk := range slices.Chunk(ids, vol.MaxIDs) {
		m, err := s.people.NamesOf(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("names: %w", err)
		}
		for k, v := range m {
			out[k] = v
		}
	}
	return out, nil
}

// ListMembers lists a department's members, a page at a time (order created_at, id), with
// their playa names and the active positions each is eligible for today. It needs
// rosters.read at the department. Fails errs.Unauthenticated, errs.Invalid (zero department,
// limit, after), errs.Permission, People.NamesOf's kinds and store kinds.
func (s *Service) ListMembers(ctx context.Context, q vol.MemberQuery) (_ vol.MemberViewList, err error) {
	const op = "volunteers.ListMembers"
	ctx, span := s.begin(ctx, "volunteers.list-members")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.MemberViewList{}, err
	}
	if err := q.Validate(); err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	if err := s.allowIn(ctx, vol.PermissionRostersRead, q.Department); err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	afterCreated, afterID, err := vol.DecodeCursor[vol.MemberID](q.After)
	if err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	size := q.Size()
	members, err := s.store.ListMembers(ctx, vol.MemberFilter{
		Department: q.Department, AfterCreated: afterCreated, AfterID: afterID, Limit: size + 1,
	})
	if err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	var next vol.Cursor
	if len(members) > size {
		members = members[:size]
		next = vol.NewCursor(members[size-1].CreatedAt, members[size-1].ID)
	}
	people := make([]model.PersonID, len(members))
	for i, m := range members {
		people[i] = m.Person
	}
	names, err := s.namesOf(ctx, people)
	if err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	eligible, err := s.eligiblePositions(ctx, q.Department, people)
	if err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	out := make([]vol.MemberView, len(members))
	for i, m := range members {
		out[i] = vol.MemberView{Member: m, PlayaName: names[m.Person], Eligible: eligible[m.Person]}
	}
	if err := filterAll(plainView(actor), out, noRel); err != nil {
		return vol.MemberViewList{}, wrap(op, err)
	}
	return vol.MemberViewList{Items: orEmpty(out), Next: next}, nil
}

// eligiblePositions returns, for each of people, the active positions of dept whose
// requirements the person holds today (verified and unexpired).
func (s *Service) eligiblePositions(ctx context.Context, dept model.DepartmentID, people []model.PersonID) (map[model.PersonID][]vol.PositionID, error) {
	out := map[model.PersonID][]vol.PositionID{}
	if len(people) == 0 {
		return out, nil
	}
	positions, err := s.store.ListPositions(ctx, dept, false)
	if err != nil {
		return nil, fmt.Errorf("positions: %w", err)
	}
	if len(positions) == 0 {
		return out, nil
	}
	ids := make([]vol.PositionID, len(positions))
	for i, p := range positions {
		ids[i] = p.ID
	}
	reqs, err := s.store.RequirementsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("requirements: %w", err)
	}
	var all []vol.QualificationID
	for _, r := range reqs {
		all = append(all, r...)
	}
	slices.SortFunc(all, cmpBytes[vol.QualificationID])
	all = slices.Compact(all)
	held := map[model.PersonID]map[vol.QualificationID]bool{}
	if len(all) > 0 {
		for chunk := range slices.Chunk(people, vol.MaxIDs) {
			rows, err := s.store.ValidQualifications(ctx, chunk, s.today(), all)
			if err != nil {
				return nil, fmt.Errorf("qualifications: %w", err)
			}
			for _, h := range rows {
				if held[h.Person] == nil {
					held[h.Person] = map[vol.QualificationID]bool{}
				}
				held[h.Person][h.Qualification] = true
			}
		}
	}
	for _, person := range people {
		for _, p := range positions {
			if holdsAll(held[person], reqs[p.ID]) {
				out[person] = append(out[person], p.ID)
			}
		}
	}
	return out, nil
}

// holdsAll reports whether every requirement is in held.
func holdsAll(held map[vol.QualificationID]bool, reqs []vol.QualificationID) bool {
	for _, q := range reqs {
		if !held[q] {
			return false
		}
	}
	return true
}

// AddMember makes a person a member of an active department. It needs members.manage at the
// department. Records member.created. Fails errs.Unauthenticated, errs.Invalid (zero person,
// unknown person), errs.Permission, errs.NotFound (unknown department), errs.Conflict
// (archived; duplicate), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) AddMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (_ vol.Member, err error) {
	const op = "volunteers.AddMember"
	ctx, span := s.begin(ctx, "volunteers.add-member")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Member{}, err
	}
	if dept.IsZero() {
		return vol.Member{}, invalid(op, "department", validate.CodeRequired, "must be set")
	}
	if person.IsZero() {
		return vol.Member{}, invalid(op, "person", validate.CodeRequired, "must be set")
	}
	if err := s.allowIn(ctx, vol.PermissionMembersManage, dept); err != nil {
		return vol.Member{}, wrap(op, err)
	}
	if err := s.requireActive(ctx, dept); err != nil {
		return vol.Member{}, wrap(op, err)
	}
	now := s.now()
	m := vol.Member{ID: model.NewID[vol.MemberID](), Department: dept, Person: person, AddedBy: actor, CreatedAt: now, UpdatedAt: now}
	var saved vol.Member
	err = s.write(ctx, func(ctx context.Context) error {
		var ierr error
		if saved, ierr = s.store.InsertMember(ctx, m); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		return s.record(ctx, act("volunteers.member.created", ref(kindMember, saved.ID), person, ""))
	})
	if err != nil {
		return vol.Member{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Member{}, wrap(op, err)
	}
	return saved, nil
}

// RemoveMember ends a person's membership; their bookings stay (a lead cancels them if
// wanted). It needs members.manage at the department. Records member.deleted. Fails
// errs.Unauthenticated, errs.Invalid, errs.Permission, errs.NotFound (not a member),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) RemoveMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (err error) {
	const op = "volunteers.RemoveMember"
	ctx, span := s.begin(ctx, "volunteers.remove-member")
	defer endSpan(span, &err)
	if _, err := s.actorOf(ctx, op); err != nil {
		return err
	}
	if dept.IsZero() {
		return invalid(op, "department", validate.CodeRequired, "must be set")
	}
	if person.IsZero() {
		return invalid(op, "person", validate.CodeRequired, "must be set")
	}
	if err := s.allowIn(ctx, vol.PermissionMembersManage, dept); err != nil {
		return wrap(op, err)
	}
	err = s.write(ctx, func(ctx context.Context) error {
		gone, derr := s.store.DeleteMember(ctx, dept, person)
		if derr != nil {
			return fmt.Errorf("delete: %w", derr)
		}
		return s.record(ctx, act("volunteers.member.deleted", ref(kindMember, gone.ID), person, ""))
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// MyDepartments lists the actor's memberships. Own records: no permission. Fails
// errs.Unauthenticated and store kinds.
func (s *Service) MyDepartments(ctx context.Context) (_ []vol.Member, err error) {
	const op = "volunteers.MyDepartments"
	ctx, span := s.begin(ctx, "volunteers.my-departments")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return nil, err
	}
	out, err := s.store.MembershipsOf(ctx, actor)
	if err != nil {
		return nil, wrap(op, err)
	}
	if out == nil {
		out = []vol.Member{}
	}
	if err := filterAll(plainView(actor), out, func(m *vol.Member) privacy.Relation {
		return privacy.Relation{Subject: m.Person, Department: m.Department}
	}); err != nil {
		return nil, wrap(op, err)
	}
	return out, nil
}

// Leave ends the actor's own membership of a department and cancels their future bookings
// on members-only positions of it (reason self). Publishes volunteers.signup-canceled for
// each and records member.deleted and signup.canceled. Fails errs.Unauthenticated,
// errs.Invalid, errs.NotFound (not a member), errs.Unavailable wrapping db.ErrReadOnly and
// store kinds.
func (s *Service) Leave(ctx context.Context, dept model.DepartmentID) (err error) {
	const op = "volunteers.Leave"
	ctx, span := s.begin(ctx, "volunteers.leave")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return err
	}
	if dept.IsZero() {
		return invalid(op, "department", validate.CodeRequired, "must be set")
	}
	err = s.write(ctx, func(ctx context.Context) error {
		gone, derr := s.store.DeleteMember(ctx, dept, actor)
		if derr != nil {
			return fmt.Errorf("delete: %w", derr)
		}
		canceled, cerr := s.store.CancelLeaveSignups(ctx, actor, dept, s.now())
		if cerr != nil {
			return fmt.Errorf("cancel bookings: %w", cerr)
		}
		entries := []audit.Entry{act("volunteers.member.deleted", ref(kindMember, gone.ID), actor, "")}
		for _, c := range canceled {
			entries = append(entries, act("volunteers.signup.canceled", ref(kindSignup, c.Signup), actor, string(vol.CancelSelf)))
			if err := s.publish(ctx, SignalSignupCanceled, canceledPayload(c)...); err != nil {
				return err
			}
		}
		return s.record(ctx, entries...)
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// canceledPayload is the payload of volunteers.signup-canceled (record references only).
func canceledPayload(c vol.CanceledSignup) []signals.Entry {
	return []signals.Entry{
		signals.Ref(PayloadSignup, c.Signup), signals.Ref(PayloadShift, c.Shift),
		signals.Ref(PayloadPerson, c.Person), signals.Ref(PayloadDepartment, c.Department),
	}
}
