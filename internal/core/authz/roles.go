package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Audit actions and record kinds authz writes.
const (
	actionRoleCreated  audit.Action = "access.role.created"
	actionRoleChanged  audit.Action = "access.role.changed"
	actionRoleDeleted  audit.Action = "access.role.deleted"
	actionGrantCreated audit.Action = "access.grant.created"
	actionGrantRevoked audit.Action = "access.grant.revoked"
	actionRuleCreated  audit.Action = "access.rule.created"
	actionRuleDeleted  audit.Action = "access.rule.deleted"
	actionGrantsGone   audit.Action = "access.grants.deleted"

	kindRole  = "access.role"
	kindGrant = "access.grant"
	kindRule  = "access.rule"
)

func scopeOf(word string) Scope {
	if word == rows.ScopeDepartment {
		return ScopeDepartment
	}
	return ScopeSite
}

func scopeWord(sc Scope) string {
	if sc == ScopeDepartment {
		return rows.ScopeDepartment
	}
	return rows.ScopeSite
}

// roleOf builds the role with its sorted permissions and their Declared flags.
func (s *Service) roleOf(r rows.Role, perms []string) Role {
	out := Role{
		ID: r.ID, Key: r.Key, Name: r.Name, Description: r.Description, Scope: scopeOf(r.Scope), Base: r.Base,
		Permissions: make([]RolePermission, 0, len(perms)), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for _, name := range perms {
		_, ok := s.cat.byName[Permission(name)]
		out.Permissions = append(out.Permissions, RolePermission{Name: Permission(name), Declared: ok})
	}
	slices.SortFunc(out.Permissions, func(a, b RolePermission) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return out
}

// StarterRoleIDs returns the ID of each starter role named in keys ("access-admin", ...). It
// is the actor-less, read-only lookup of the demo seed (D53), which must name roles before
// any account holds one: it checks no permission and records no audit entry, because it
// reads only role keys and IDs, which are not personal data, and it changes nothing. A
// request never reaches it. Works on a read-only copy.
//
// Fails errs.Invalid (field keys) for no keys or an empty key, errs.NotFound (field keys,
// code unknown) when a key names no starter role, and store kinds. The result holds exactly
// the requested keys.
func (s *Service) StarterRoleIDs(ctx context.Context, keys []string) (_ map[string]RoleID, err error) {
	const op = "authz.Service.StarterRoleIDs"
	ctx, span := s.start(ctx, "starter-role-ids")
	defer endSpan(span, &err)
	if len(keys) == 0 || slices.Contains(keys, "") {
		return nil, fieldInvalid(op, "keys", codeRequired, "name at least one starter role key, none empty")
	}
	var rs []rows.Role
	err = s.inTx(ctx, db.Read, func(ctx context.Context) error {
		var lerr error
		rs, lerr = s.store.ListRoles(ctx, "")
		return wrap(op, lerr)
	})
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]RoleID, len(rs))
	for _, r := range rs {
		if r.Key != "" {
			byKey[r.Key] = r.ID
		}
	}
	out := make(map[string]RoleID, len(keys))
	for _, k := range keys {
		id, ok := byKey[k]
		if !ok {
			return nil, errs.E(op, errs.NotFound, &validate.FieldError{Field: "keys", Code: "unknown", Reason: "a key names no starter role"})
		}
		out[k] = id
	}
	return out, nil
}

// ListRoles returns the roles sorted by name (key for an unrenamed starter role), each with
// its permissions. Same permission rule as ListPermissions; a caller who sees departments
// only gets the department roles. Works on a read-only copy.
func (s *Service) ListRoles(ctx context.Context) (_ []Role, err error) {
	const op = "authz.Service.ListRoles"
	ctx, span := s.start(ctx, "list-roles")
	defer endSpan(span, &err)
	view, err := s.viewer(ctx, op)
	if err != nil {
		return nil, err
	}
	scope := ""
	if !view.full {
		scope = rows.ScopeDepartment
	}
	var (
		rs    []rows.Role
		perms map[rows.RoleID][]string
	)
	err = s.inTx(ctx, db.Read, func(ctx context.Context) error {
		var err error
		if rs, err = s.store.ListRoles(ctx, scope); err != nil {
			return wrap(op, err)
		}
		perms, err = s.store.ListRolePermissions(ctx)
		return wrap(op, err)
	})
	if err != nil {
		return nil, err
	}
	out := make([]Role, len(rs))
	for i, r := range rs {
		out[i] = s.roleOf(r, perms[r.ID])
	}
	span.SetAttrs(telemetry.Int("roles", len(out)))
	return out, nil
}

// GetRole returns one role. Fails errs.NotFound for an unknown ID and, for a caller who
// sees departments only, for a site role.
func (s *Service) GetRole(ctx context.Context, id RoleID) (_ Role, err error) {
	const op = "authz.Service.GetRole"
	ctx, span := s.start(ctx, "get-role")
	defer endSpan(span, &err)
	view, err := s.viewer(ctx, op)
	if err != nil {
		return Role{}, err
	}
	span.SetAttrs(telemetry.ID("role_id", id))
	return s.readRole(ctx, op, id, !view.full)
}

// readRole loads a role and its permissions; departmentOnly makes a site role NotFound.
func (s *Service) readRole(ctx context.Context, op string, id RoleID, departmentOnly bool) (Role, error) {
	var out Role
	err := s.inTx(ctx, db.Read, func(ctx context.Context) error {
		r, err := s.store.GetRole(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if departmentOnly && r.Scope != rows.ScopeDepartment {
			return errs.E(op, errs.NotFound, errors.New("no such role"))
		}
		perms, err := s.store.RolePermissions(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		out = s.roleOf(r, perms)
		return nil
	})
	if err != nil {
		return Role{}, err
	}
	return out, nil
}

// scopeCheck fails errs.Invalid ("permissions[i]" scope) for each site-only permission in a
// department role. A permission nobody declared is allowed (decision 13).
func (s *Service) scopeCheck(sc Scope, perms []Permission) error {
	if sc != ScopeDepartment {
		return nil
	}
	v := validate.Errors{Op: "authz.RoleInput.Validate"}
	for i, p := range perms {
		if d, ok := s.cat.byName[p]; ok && d.Scope == ScopeSite {
			v.Addf(fmt.Sprintf("permissions[%d]", i), codeScope, "is site-wide only and cannot be part of a department role")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// permissionStrings converts for the store.
func permissionStrings(perms []Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}

// nameTaken re-kinds a role-name collision to Conflict with the field error name/taken.
func nameTaken(op string, err error) error {
	if constraintOf(err) == "role_name_key" {
		return fieldConflict(op, "name", codeTaken, nil, "another role has this name")
	}
	return err
}

// CreateRole needs access.roles.manage. It validates in, then in one Write transaction
// inserts the role and its permissions and records access.role.created. Fails
// errs.Permission, errs.Invalid (F21 to F23), errs.Conflict (name taken, F24), errs.Unavailable
// wrapping db.ErrReadOnly (F25).
func (s *Service) CreateRole(ctx context.Context, in RoleInput) (_ Role, err error) {
	const op = "authz.Service.CreateRole"
	ctx, span := s.start(ctx, "create-role")
	defer endSpan(span, &err)
	if err := s.Allow(ctx, PermissionRolesManage); err != nil {
		return Role{}, wrap(op, err)
	}
	if err := in.Validate(); err != nil {
		return Role{}, err
	}
	if err := s.scopeCheck(in.Scope, in.Permissions); err != nil {
		return Role{}, err
	}
	now := s.now()
	r := rows.Role{
		ID: model.NewID[RoleID](), Name: strings.TrimSpace(in.Name), Description: in.Description,
		Scope: scopeWord(in.Scope), CreatedAt: now, UpdatedAt: now,
	}
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.InsertRole(ctx, r); err != nil {
			return nameTaken(op, err)
		}
		if err := s.store.InsertRolePermissions(ctx, r.ID, permissionStrings(in.Permissions)); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{Action: actionRoleCreated, Outcome: audit.Succeeded, Record: audit.RefOf(kindRole, r.ID)})
	})
	if err != nil {
		return Role{}, err
	}
	span.SetAttrs(telemetry.ID("role_id", r.ID))
	return s.roleOf(r, permissionStrings(in.Permissions)), nil
}

// UpdateRole needs access.roles.manage. It replaces the name, description and permissions
// of the role in one Write transaction under the invariants lock and records
// access.role.changed. in.Scope must be zero or the role's own scope (errs.Invalid, field
// scope, code immutable). A starter role may be saved without a name (it keeps its key). A
// change that removes access.grants.manage from the role of the last site-wide holder
// fails errs.Conflict (last_access_admin, F27). Fails errs.NotFound for an unknown ID (F28)
// and as CreateRole does.
func (s *Service) UpdateRole(ctx context.Context, id RoleID, in RoleInput) (_ Role, err error) {
	const op = "authz.Service.UpdateRole"
	ctx, span := s.start(ctx, "update-role")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("role_id", id))
	if err := s.Allow(ctx, PermissionRolesManage); err != nil {
		return Role{}, wrap(op, err)
	}
	if err := in.validate(false, false); err != nil {
		return Role{}, err
	}
	var out Role
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		cur, err := s.store.GetRole(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if in.Scope != 0 && in.Scope != scopeOf(cur.Scope) {
			return fieldInvalid(op, "scope", codeImmutable, "cannot change after the role is created")
		}
		name := strings.TrimSpace(in.Name)
		if name == "" && cur.Key == "" {
			return fieldInvalid(op, "name", codeRequired, "must be set")
		}
		if err := s.scopeCheck(scopeOf(cur.Scope), in.Permissions); err != nil {
			return err
		}
		old, err := s.store.RolePermissions(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if slices.Contains(old, string(PermissionGrantsManage)) && !slices.Contains(in.Permissions, PermissionGrantsManage) {
			if err := s.lastAdmin(ctx, op, rows.Holders{Permission: string(PermissionGrantsManage), Now: s.now(), Role: id}); err != nil {
				return err
			}
		}
		now := s.now()
		if err := s.store.DeleteRolePermissions(ctx, id); err != nil {
			return wrap(op, err)
		}
		if err := s.store.InsertRolePermissions(ctx, id, permissionStrings(in.Permissions)); err != nil {
			return wrap(op, err)
		}
		if err := s.store.UpdateRole(ctx, id, name, in.Description, now); err != nil {
			return nameTaken(op, err)
		}
		cur.Name, cur.Description, cur.UpdatedAt = name, in.Description, now
		out = s.roleOf(cur, permissionStrings(in.Permissions))
		return s.record(ctx, audit.Entry{Action: actionRoleChanged, Outcome: audit.Succeeded, Record: audit.RefOf(kindRole, id)})
	})
	if err != nil {
		return Role{}, err
	}
	return out, nil
}

// effectiveAdmins counts the active, site-wide grants of roles holding access.grants.manage
// that q removes and that it leaves, ignoring the grants of people a separation-of-duties
// rule denies the permission (they are not effective access admins). Call it under the
// invariants lock.
func (s *Service) effectiveAdmins(ctx context.Context, op string, q rows.Holders) (removed, remaining int, err error) {
	gs, err := s.store.AdminGrants(ctx, q)
	if err != nil {
		return 0, 0, wrap(op, err)
	}
	now := s.now()
	denied := map[model.PersonID]bool{}
	for _, g := range gs {
		d, ok := denied[g.Person]
		if !ok {
			rules, err := s.store.RulesHeldBy(ctx, g.Person, now)
			if err != nil {
				return 0, 0, wrap(op, err)
			}
			forbidden, err := s.forbiddenSet(rules)
			if err != nil {
				return 0, 0, wrap(op, err)
			}
			_, d = forbidden[q.Permission]
			denied[g.Person] = d
		}
		switch {
		case d:
		case g.Removes:
			removed++
		default:
			remaining++
		}
	}
	return removed, remaining, nil
}

// lastAdmin fails errs.Conflict (field role, code last_access_admin) when the change q
// describes would remove every effective access admin: an active site-wide holder of
// access.grants.manage whom no separation-of-duties rule denies it. Call it under the
// invariants lock, before the change.
func (s *Service) lastAdmin(ctx context.Context, op string, q rows.Holders) error {
	removed, remaining, err := s.effectiveAdmins(ctx, op, q)
	if err != nil {
		return err
	}
	if removed > 0 && remaining == 0 {
		return fieldConflict(op, "role", codeLastAdmin, nil, "another person must hold access management first")
	}
	return nil
}

// DeleteRole needs access.roles.manage. It deletes the role in one Write transaction and
// records access.role.deleted. Fails errs.NotFound for an unknown ID, errs.Invalid (field
// role, code base) for the base role (F30), errs.Conflict (in_use) while any grant, active
// or ended, or rule references it (F29).
func (s *Service) DeleteRole(ctx context.Context, id RoleID) (err error) {
	const op = "authz.Service.DeleteRole"
	ctx, span := s.start(ctx, "delete-role")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("role_id", id))
	if err := s.Allow(ctx, PermissionRolesManage); err != nil {
		return wrap(op, err)
	}
	if id.IsZero() {
		return fieldInvalid(op, "role", codeRequired, "must be set")
	}
	return s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		cur, err := s.store.GetRole(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if cur.Base {
			return fieldInvalid(op, "role", codeBase, "the base role cannot be deleted")
		}
		if err := s.store.DeleteRole(ctx, id); err != nil {
			if errs.Is(err, errs.Conflict) {
				return fieldConflict(op, "role", codeInUse, nil, "grants or rules still use this role")
			}
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{Action: actionRoleDeleted, Outcome: audit.Succeeded, Record: audit.RefOf(kindRole, id)})
	})
}
