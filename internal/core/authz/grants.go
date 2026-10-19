package authz

import (
	"context"
	"errors"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func grantOf(g rows.Grant) Grant {
	return Grant{
		ID: g.ID, Person: g.Person, Role: g.Role, Department: g.Department,
		EndsAt: g.EndsAt, GrantedBy: g.GrantedBy, CreatedAt: g.CreatedAt,
	}
}

// ListGrants needs access.grants.read in some extent, or q.Person to be the actor ("my
// roles"). The result is limited to the caller's extent: a department-only reader sees
// only grants for their departments, which is a filter, never an error. Newest first,
// keyset-paged. q.RoleKey names a role by key; an unknown key gives an empty page, as an
// unknown q.Role does. Works on a read-only copy. Fails errs.Invalid (bad cursor, limit or
// role key, F34),
// errs.Permission (F31) and store kinds.
func (s *Service) ListGrants(ctx context.Context, q GrantQuery) (_ GrantPage, err error) {
	const op = "authz.Service.ListGrants"
	ctx, span := s.start(ctx, "list-grants")
	defer endSpan(span, &err)
	a, err := s.actor(ctx, op)
	if err != nil {
		return GrantPage{}, err
	}
	if err := q.Validate(); err != nil {
		return GrantPage{}, err
	}
	now := s.now()
	f := rows.GrantFilter{
		Person: q.Person, Role: q.Role, Department: q.Department, Permission: string(q.Permission),
		IncludeEnded: q.IncludeEnded, Now: now, Limit: q.pageSize() + 1,
	}
	if !q.After.IsZero() {
		if f.AfterTime, f.AfterID, err = q.After.position(); err != nil {
			return GrantPage{}, err
		}
	}
	switch own := !q.Person.IsZero() && q.Person == a.Person; {
	case a.Superuser || own:
		f.WholeSite = true
	default:
		acc, err := s.load(ctx, a.Person, PermissionGrantsRead)
		if err != nil {
			return GrantPage{}, wrap(op, err)
		}
		ext := acc.extent(PermissionGrantsRead)
		if ext.IsZero() {
			return GrantPage{}, denied(op, PermissionGrantsRead, "the permission")
		}
		f.WholeSite, f.Departments = ext.Site, ext.Departments
	}
	if q.RoleKey != "" {
		// Resolved after the permission check, so a caller who may not list is refused before
		// anything is learned. A key names a role as an ID does; an unknown key matches
		// nothing, as an unknown ID does (an empty page, never an error).
		id, found, err := s.roleIDOfKey(ctx, op, q.RoleKey)
		if err != nil {
			return GrantPage{}, err
		}
		if !found {
			return GrantPage{Grants: []Grant{}}, nil
		}
		f.Role = id
	}
	gs, err := s.store.ListGrants(ctx, f)
	if err != nil {
		return GrantPage{}, wrap(op, err)
	}
	page := GrantPage{}
	limit := q.pageSize()
	more := len(gs) > limit
	if more {
		gs = gs[:limit]
	}
	page.Grants = make([]Grant, len(gs))
	if len(gs) > 0 {
		// The role's key and name travel with each grant, so a person who may list their own
		// grants but not the role catalogue can still say which role they hold. One small
		// read of the roles (a regional has tens), in a transaction of its own.
		var roles map[rows.RoleID]rows.Role
		if err := s.inTx(ctx, db.Read, func(ctx context.Context) error {
			rs, err := s.store.ListRoles(ctx, "")
			if err != nil {
				return wrap(op, err)
			}
			roles = make(map[rows.RoleID]rows.Role, len(rs))
			for _, r := range rs {
				roles[r.ID] = r
			}
			return nil
		}); err != nil {
			return GrantPage{}, err
		}
		for i, g := range gs {
			page.Grants[i] = grantOf(g)
			page.Grants[i].RoleKey, page.Grants[i].RoleName = roles[g.Role].Key, roles[g.Role].Name
		}
	}
	if more {
		page.Next = cursorOf(page.Grants[limit-1])
	}
	span.SetAttrs(telemetry.Int("grants", len(page.Grants)))
	return page, nil
}

// roleIDOfKey resolves a starter role's key to its role ID in a read transaction of its
// own. found is false for a key no role has (a starter role a regional deleted too).
func (s *Service) roleIDOfKey(ctx context.Context, op, key string) (id RoleID, found bool, err error) {
	err = s.inTx(ctx, db.Read, func(ctx context.Context) error {
		rs, err := s.store.ListRoles(ctx, "")
		if err != nil {
			return wrap(op, err)
		}
		for _, r := range rs {
			if r.Key == key {
				id, found = r.ID, true
				return nil
			}
		}
		return nil
	})
	return id, found, err
}

// mayGrant says whether the actor may make (or revoke) a grant of role r, with department
// d for a department role (decision 5). Site roles need access.grants.manage site-wide.
// Department roles need it site-wide, or access.department-grants.manage site-wide, or
// access.grants.manage for d; only the last, department-scoped holder is bound by the
// no-escalation rule: every permission of the role must be held at d. The error, when not
// nil, is a Permission denial; err is a failure to decide.
func (s *Service) mayGrant(ctx context.Context, op string, a Actor, acc *access, r rows.Role, d model.DepartmentID) (ok bool, err error) {
	if a.Superuser || acc.site(PermissionGrantsManage) {
		return true, nil
	}
	if r.Scope != rows.ScopeDepartment {
		return false, nil
	}
	if acc.site(PermissionDepartmentGrantsManage) {
		return true, nil
	}
	if !acc.at(PermissionGrantsManage, d) {
		return false, nil
	}
	perms, err := s.store.RolePermissions(ctx, r.ID)
	if err != nil {
		return false, wrap(op, err)
	}
	for _, p := range perms {
		if !acc.at(Permission(p), d) {
			return false, nil
		}
	}
	return true, nil
}

// canManageAny reports whether the actor holds any permission that lets them grant
// something somewhere, so a caller with none learns nothing from a NotFound.
func canManageAny(a Actor, acc *access) bool {
	return a.Superuser || acc.anywhere(PermissionGrantsManage) || acc.site(PermissionDepartmentGrantsManage)
}

// Grant gives in.Person the role (decision 5) in one Write transaction under the
// invariants lock: it checks the caller (Permission, F35, F36), refuses a grant to oneself
// (Invalid, field person, code self, F37; the superuser is exempt) and of the base role
// (Invalid, field role, code base, F38), checks the department against the role's scope
// (Invalid mismatch or required, F40; Unsupported without a DepartmentSource, F41;
// Invalid unknown for an archived or unknown department, F42) and EndsAt (Invalid, field
// ends_at, code range, F43), inserts the row (Conflict duplicate, F44), then checks the
// person's permissions, the new role included, against the separation-of-duties rules
// (Conflict separation_of_duties with the rule in the params, F45). It records
// access.grant.created and publishes access.grant-created. Fails errs.NotFound for an
// unknown role (F39) and errs.Unavailable wrapping db.ErrReadOnly (F48); a failed Record or
// Publish rolls the grant back (F46, F47).
func (s *Service) Grant(ctx context.Context, in GrantInput) (_ Grant, err error) {
	const op = "authz.Service.Grant"
	ctx, span := s.start(ctx, "grant")
	defer endSpan(span, &err)
	a, err := s.actor(ctx, op)
	if err != nil {
		return Grant{}, err
	}
	if err := in.Validate(); err != nil {
		return Grant{}, err
	}
	span.SetAttrs(telemetry.ID("role_id", in.Role))
	var out Grant
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		acc, err := s.accessOf(ctx, a, "")
		if err != nil {
			return err
		}
		if !canManageAny(a, acc) {
			return denied(op, PermissionGrantsManage, "the permission")
		}
		role, err := s.store.GetRole(ctx, in.Role)
		if err != nil {
			return wrap(op, err)
		}
		if role.Base {
			return fieldInvalid(op, "role", codeBase, "the base role is held by everyone and cannot be granted")
		}
		if err := departmentShape(op, role, in.Department); err != nil {
			return err
		}
		ok, err := s.mayGrant(ctx, op, a, acc, role, in.Department)
		if err != nil {
			return err
		}
		if !ok {
			return errs.E(op, errs.Permission, errors.New("this grant is outside what the caller may grant"))
		}
		if !a.Superuser && in.Person == a.Person {
			return fieldInvalid(op, "person", codeSelf, "cannot grant a role to yourself")
		}
		if err := s.activeDepartment(ctx, op, role, in.Department); err != nil {
			return err
		}
		now := s.now()
		if !in.EndsAt.IsZero() && !in.EndsAt.After(now) {
			return fieldInvalid(op, "ends_at", codeRange, "must be in the future")
		}
		g := rows.Grant{
			ID: model.NewID[GrantID](), Person: in.Person, Role: in.Role, Department: in.Department,
			EndsAt: in.EndsAt.UTC(), GrantedBy: a.Person, CreatedAt: now,
		}
		if err := s.store.InsertGrant(ctx, g); err != nil {
			switch constraintOf(err) {
			case "role_grant_person_role_department_key":
				return fieldConflict(op, "role", codeDuplicate, nil, "this person already has this role here")
			case "role_grant_person_id_fkey":
				return fieldInvalid(op, "person", codeUnknown, "is not a known person")
			case "role_grant_granted_by_fkey":
				// The granting actor has no core.person row yet (people's account-created
				// handler has not run or failed); saving their own profile creates it.
				return fieldInvalid(op, "actor", codeUnknown, "the person granting has no profile yet; save your profile, then try again")
			}
			return wrap(op, err)
		}
		if err := s.checkSeparation(ctx, op, in.Person); err != nil {
			return err
		}
		out = grantOf(g)
		out.RoleKey, out.RoleName = role.Key, role.Name
		if err := s.record(ctx, audit.Entry{
			Action: actionGrantCreated, Outcome: audit.Succeeded, Subject: g.Person, Record: audit.RefOf(kindGrant, g.ID),
		}); err != nil {
			return err
		}
		return wrap(op, s.signals.Publish(ctx, SignalGrantCreated, grantPayload(g)))
	})
	if err != nil {
		return Grant{}, err
	}
	span.SetAttrs(telemetry.ID("grant_id", out.ID))
	return out, nil
}

// departmentShape checks the department against the role's scope: a site role has none
// (mismatch), a department role needs one (required).
func departmentShape(op string, r rows.Role, d model.DepartmentID) error {
	switch {
	case r.Scope == rows.ScopeSite && !d.IsZero():
		return fieldInvalid(op, "department", codeMismatch, "a site-wide role is not granted for a department")
	case r.Scope == rows.ScopeDepartment && d.IsZero():
		return fieldInvalid(op, "department", codeRequired, "a department role is granted for one department")
	}
	return nil
}

// activeDepartment asks the DepartmentSource about a department role's department.
func (s *Service) activeDepartment(ctx context.Context, op string, r rows.Role, d model.DepartmentID) error {
	if r.Scope != rows.ScopeDepartment {
		return nil
	}
	if s.depts == nil {
		return errs.E(op, errs.Unsupported, errors.New("department grants need departments, which this installation does not have"))
	}
	active, err := s.depts.Active(ctx, d)
	if err != nil {
		return wrap(op, err)
	}
	if !active {
		return fieldInvalid(op, "department", codeUnknown, "is not an active department")
	}
	return nil
}

func grantPayload(g rows.Grant) signals.Payload {
	entries := []signals.Entry{
		signals.Ref(PayloadGrant, g.ID), signals.Ref(PayloadPerson, g.Person), signals.Ref(PayloadRole, g.Role),
	}
	if !g.Department.IsZero() {
		entries = append(entries, signals.Ref(PayloadDepartment, g.Department))
	}
	return signals.NewPayload(entries...)
}

// checkSeparation fails errs.Conflict (separation_of_duties, params rule and note) when a
// rule of a role the person holds forbids a permission the person holds (decision 3). It
// runs after the insert, in the same transaction, so the new grant counts.
func (s *Service) checkSeparation(ctx context.Context, op string, person model.PersonID) error {
	now := s.now()
	held, err := s.store.EffectivePermissions(ctx, person, now)
	if err != nil {
		return wrap(op, err)
	}
	rules, err := s.store.RulesHeldBy(ctx, person, now)
	if err != nil {
		return wrap(op, err)
	}
	// Undeclared names have no effect (decision 13): neither held nor forbidden ones count,
	// as in a decision and in ListRules's Affected.
	names := make(map[string]struct{}, len(held))
	for _, h := range held {
		if _, ok := s.cat.byName[Permission(h.Permission)]; ok {
			names[h.Permission] = struct{}{}
		}
	}
	for _, r := range rules {
		forbidden, err := s.forbiddenBy(r)
		if err != nil {
			return wrap(op, err)
		}
		for _, name := range forbidden {
			if _, declared := s.cat.byName[Permission(name)]; !declared {
				continue
			}
			if _, ok := names[name]; ok {
				return fieldConflict(op, "role", codeSoD,
					map[string]string{"rule": r.ID.String(), "note": r.Note}, "a separation-of-duties rule forbids this combination")
			}
		}
	}
	return nil
}

// Revoke deletes a grant in one Write transaction under the invariants lock and records
// access.grant.revoked, publishing access.grant-revoked. The caller must be able to make
// that same grant (decision 5); a grant outside their reach is errs.NotFound, never
// Permission, so a department admin learns nothing of grants elsewhere (F49), unless they
// hold no granting permission at all (errs.Permission). Fails errs.Invalid (field person,
// code self) for one's own grant (F50), errs.Conflict (last_access_admin) when it is the
// last effective access admin: an active site-wide holder of access.grants.manage no
// separation-of-duties rule denies it (F51) and errs.Unavailable wrapping
// db.ErrReadOnly.
func (s *Service) Revoke(ctx context.Context, id GrantID) (err error) {
	const op = "authz.Service.Revoke"
	ctx, span := s.start(ctx, "revoke")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("grant_id", id))
	a, err := s.actor(ctx, op)
	if err != nil {
		return err
	}
	if id.IsZero() {
		return fieldInvalid(op, "grant", codeRequired, "must be set")
	}
	return s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		acc, err := s.accessOf(ctx, a, "")
		if err != nil {
			return err
		}
		if !canManageAny(a, acc) {
			return denied(op, PermissionGrantsManage, "the permission")
		}
		g, err := s.store.GetGrant(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		role, err := s.store.GetRole(ctx, g.Role)
		if err != nil {
			return wrap(op, err)
		}
		ok, err := s.mayGrant(ctx, op, a, acc, role, g.Department)
		if err != nil {
			return err
		}
		if !ok {
			return errs.E(op, errs.NotFound, errors.New("no such grant"))
		}
		if !a.Superuser && g.Person == a.Person {
			return fieldInvalid(op, "person", codeSelf, "cannot revoke your own role")
		}
		if g.Department.IsZero() {
			if err := s.lastAdmin(ctx, op, rows.Holders{Permission: string(PermissionGrantsManage), Now: s.now(), Grant: id}); err != nil {
				return err
			}
		}
		if _, err := s.store.DeleteGrant(ctx, id); err != nil {
			return wrap(op, err)
		}
		if err := s.record(ctx, audit.Entry{
			Action: actionGrantRevoked, Outcome: audit.Succeeded, Subject: g.Person, Record: audit.RefOf(kindGrant, g.ID),
		}); err != nil {
			return err
		}
		return wrap(op, s.signals.Publish(ctx, SignalGrantRevoked, grantPayload(g)))
	})
}
