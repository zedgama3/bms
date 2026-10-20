// Package postgres is authz's storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of authz, rooted at the files (db.md: NNNN_name.sql at
// the set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on
// an embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("authz/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlClearGrantedBy    = mustSQL("clear_granted_by.sql")
	sqlAdminGrants       = mustSQL("list_access_admin_grants.sql")
	sqlDeleteGrant       = mustSQL("delete_grant.sql")
	sqlDeleteByPerson    = mustSQL("delete_grants_by_person.sql")
	sqlDeleteRole        = mustSQL("delete_role.sql")
	sqlDeleteRolePerms   = mustSQL("delete_role_permissions.sql")
	sqlDeleteRule        = mustSQL("delete_rule.sql")
	sqlEffectiveOne      = mustSQL("effective_permission.sql")
	sqlEffectiveAll      = mustSQL("effective_permissions.sql")
	sqlExportGrants      = mustSQL("export_grants_by_person.sql")
	sqlGetGrant          = mustSQL("get_grant.sql")
	sqlGetRole           = mustSQL("get_role.sql")
	sqlHeldRules         = mustSQL("held_rules.sql")
	sqlInsertGrant       = mustSQL("insert_grant.sql")
	sqlInsertRole        = mustSQL("insert_role.sql")
	sqlInsertRolePerms   = mustSQL("insert_role_permissions.sql")
	sqlInsertRule        = mustSQL("insert_rule.sql")
	sqlListGrants        = mustSQL("list_grants.sql")
	sqlListHolders       = mustSQL("list_permission_holders.sql")
	sqlListRolePerms     = mustSQL("list_role_permissions.sql")
	sqlListRolePermsOne  = mustSQL("list_role_permissions_by_role.sql")
	sqlListRoles         = mustSQL("list_roles.sql")
	sqlListRules         = mustSQL("list_rules.sql")
	sqlLockInvariants    = mustSQL("lock_invariants.sql")
	sqlSeedStarterPerms  = mustSQL("seed_starter_permissions.sql")
	sqlTitlesOf          = mustSQL("titles_of.sql")
	sqlUpdateRole        = mustSQL("update_role.sql")
	roleInUseConstraints = map[string]bool{"role_grant_role_id_fkey": true, "sod_rule_role_id_fkey": true}
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("authz/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the documentation
// that sits above each statement is not sent to the server with every call.
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps core.role, core.role_permission, core.role_grant, core.sod_rule and
// core.role_seed in PostgreSQL. It holds a db.DB and no state; safe for concurrent use.
// Every method runs inside the caller's transaction when ctx carries one. Every error is
// already classified by db.Wrap.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("authz.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// ---- argument and column helpers --------------------------------------------------------

// uuidOrNil is the statement argument for an optional ID: NULL for the zero ID.
func uuidOrNil[T ~[16]byte](id T) any {
	if id == (T{}) {
		return nil
	}
	return [16]byte(id)
}

// timeOrNil is the argument for an optional time: NULL for the zero time, UTC otherwise.
func timeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// textOrNil is the argument for an optional text: NULL for "".
func textOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// idOf converts a scanned optional uuid; NULL is the zero ID.
func idOf[T ~[16]byte](raw *[16]byte) T {
	if raw == nil {
		return T{}
	}
	return T(*raw)
}

// utcOf converts a scanned optional time; NULL is the zero time.
func utcOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func uuids[T ~[16]byte](ids []T) [][16]byte {
	out := make([][16]byte, len(ids))
	for i, id := range ids {
		out[i] = [16]byte(id)
	}
	return out
}

// ---- locking ----------------------------------------------------------------------------

// LockInvariants takes the transaction-scoped advisory lock every write that touches the
// invariants takes first (decision 16). Fails errs.Unavailable wrapping db.ErrReadOnly on a
// read-only copy, and with db's kinds.
func (s *Store) LockInvariants(ctx context.Context) error {
	const op = "authz.postgres.LockInvariants"
	_, err := s.db.Exec(ctx, sqlLockInvariants)
	return db.Wrap(op, err)
}

// ---- decisions --------------------------------------------------------------------------

// EffectivePermissions returns every (permission, department) the person holds through an
// active grant or the base role, rules not applied. A zero Department is site-wide.
func (s *Store) EffectivePermissions(ctx context.Context, person model.PersonID, now time.Time) ([]rows.Held, error) {
	return s.held(ctx, "authz.postgres.EffectivePermissions", sqlEffectiveAll, [16]byte(person), now.UTC())
}

// EffectiveFor is EffectivePermissions restricted to one permission name.
func (s *Store) EffectiveFor(ctx context.Context, person model.PersonID, now time.Time, permission string) ([]rows.Held, error) {
	return s.held(ctx, "authz.postgres.EffectiveFor", sqlEffectiveOne, [16]byte(person), now.UTC(), permission)
}

func (s *Store) held(ctx context.Context, op, query string, args ...any) ([]rows.Held, error) {
	rs, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Held{}
	for rs.Next() {
		var (
			h    rows.Held
			dept *[16]byte
		)
		if err := rs.Scan(&h.Permission, &dept); err != nil {
			return nil, db.Wrap(op, err)
		}
		h.Department = idOf[model.DepartmentID](dept)
		out = append(out, h)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// RulesHeldBy returns the rules whose role the person holds now, in any extent.
func (s *Store) RulesHeldBy(ctx context.Context, person model.PersonID, now time.Time) ([]rows.HeldRule, error) {
	const op = "authz.postgres.RulesHeldBy"
	rs, err := s.db.Query(ctx, sqlHeldRules, [16]byte(person), now.UTC())
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.HeldRule{}
	for rs.Next() {
		var (
			r        rows.HeldRule
			id, role [16]byte
			perm     *string
			reaches  *string
		)
		if err := rs.Scan(&id, &role, &perm, &reaches, &r.Note); err != nil {
			return nil, db.Wrap(op, err)
		}
		r.ID, r.Role = rows.RuleID(id), rows.RoleID(role)
		if perm != nil {
			r.Permission = *perm
		}
		if reaches != nil {
			r.Reaches = *reaches
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// TitlesOf returns the key (or renamed name) of each non-base role the person holds now,
// sorted and distinct, at most limit.
func (s *Store) TitlesOf(ctx context.Context, person model.PersonID, now time.Time, limit int) ([]string, error) {
	const op = "authz.postgres.TitlesOf"
	rs, err := s.db.Query(ctx, sqlTitlesOf, [16]byte(person), now.UTC(), int32(limit)) //nolint:gosec // limit is audit.MaxTitles
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	var out []string
	for rs.Next() {
		var title string
		if err := rs.Scan(&title); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, title)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// ---- roles ------------------------------------------------------------------------------

// ListRoles returns the roles sorted by name (key for an unrenamed starter role), without
// permissions. A scope of "" returns every role; "department" only department roles.
func (s *Store) ListRoles(ctx context.Context, scope string) ([]rows.Role, error) {
	const op = "authz.postgres.ListRoles"
	rs, err := s.db.Query(ctx, sqlListRoles, textOrNil(scope))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectRoles(op, rs)
}

// GetRole returns one role without its permissions. Fails errs.NotFound for an unknown ID.
func (s *Store) GetRole(ctx context.Context, id rows.RoleID) (rows.Role, error) {
	const op = "authz.postgres.GetRole"
	rs, err := s.db.Query(ctx, sqlGetRole, [16]byte(id))
	if err != nil {
		return rows.Role{}, db.Wrap(op, err)
	}
	rr, err := collectRoles(op, rs)
	if err != nil {
		return rows.Role{}, err
	}
	if len(rr) == 0 {
		return rows.Role{}, errs.E(op, errs.NotFound, errors.New("no such role"))
	}
	return rr[0], nil
}

func collectRoles(op string, rs pgx.Rows) ([]rows.Role, error) {
	defer rs.Close()
	out := []rows.Role{}
	for rs.Next() {
		var (
			r   rows.Role
			id  [16]byte
			key *string
		)
		if err := rs.Scan(&id, &key, &r.Name, &r.Description, &r.Scope, &r.Base, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, db.Wrap(op, err)
		}
		r.ID = rows.RoleID(id)
		if key != nil {
			r.Key = *key
		}
		r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// ListRolePermissions returns every role's permissions, each list sorted in byte order.
func (s *Store) ListRolePermissions(ctx context.Context) (map[rows.RoleID][]string, error) {
	const op = "authz.postgres.ListRolePermissions"
	rs, err := s.db.Query(ctx, sqlListRolePerms)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := map[rows.RoleID][]string{}
	for rs.Next() {
		var (
			id   [16]byte
			perm string
		)
		if err := rs.Scan(&id, &perm); err != nil {
			return nil, db.Wrap(op, err)
		}
		out[rows.RoleID(id)] = append(out[rows.RoleID(id)], perm)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// RolePermissions returns one role's permissions in byte order; empty when it holds none.
func (s *Store) RolePermissions(ctx context.Context, id rows.RoleID) ([]string, error) {
	const op = "authz.postgres.RolePermissions"
	rs, err := s.db.Query(ctx, sqlListRolePermsOne, [16]byte(id))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []string{}
	for rs.Next() {
		var perm string
		if err := rs.Scan(&perm); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, perm)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// InsertRole adds a regional's own role (no key, not base). A name already used ignoring
// case fails errs.Conflict with *db.Error.Constraint role_name_key.
func (s *Store) InsertRole(ctx context.Context, r rows.Role) error {
	const op = "authz.postgres.InsertRole"
	_, err := s.db.Exec(ctx, sqlInsertRole, [16]byte(r.ID), r.Name, r.Description, r.Scope,
		r.CreatedAt.UTC(), r.UpdatedAt.UTC())
	return db.Wrap(op, err)
}

// UpdateRole sets name and description. Fails errs.NotFound for an unknown ID and
// errs.Conflict with Constraint role_name_key for a name already used.
func (s *Store) UpdateRole(ctx context.Context, id rows.RoleID, name, description string, at time.Time) error {
	const op = "authz.postgres.UpdateRole"
	tag, err := s.db.Exec(ctx, sqlUpdateRole, [16]byte(id), name, description, at.UTC())
	if err != nil {
		return db.Wrap(op, err)
	}
	if tag.RowsAffected() == 0 {
		return errs.E(op, errs.NotFound, errors.New("no such role"))
	}
	return nil
}

// DeleteRolePermissions removes every permission of the role (UpdateRole replaces them).
func (s *Store) DeleteRolePermissions(ctx context.Context, id rows.RoleID) error {
	const op = "authz.postgres.DeleteRolePermissions"
	_, err := s.db.Exec(ctx, sqlDeleteRolePerms, [16]byte(id))
	return db.Wrap(op, err)
}

// InsertRolePermissions adds the permissions to the role. perms must be unique.
func (s *Store) InsertRolePermissions(ctx context.Context, id rows.RoleID, perms []string) error {
	const op = "authz.postgres.InsertRolePermissions"
	if len(perms) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, sqlInsertRolePerms, [16]byte(id), perms)
	return db.Wrap(op, err)
}

// DeleteRole deletes a non-base role. Fails errs.NotFound when there is no such role (or it
// is the base role), and errs.Conflict when a grant or a rule still references it.
func (s *Store) DeleteRole(ctx context.Context, id rows.RoleID) error {
	const op = "authz.postgres.DeleteRole"
	rs, err := s.db.Query(ctx, sqlDeleteRole, [16]byte(id))
	if err != nil {
		return inUse(op, err)
	}
	defer rs.Close()
	n := 0
	for rs.Next() {
		n++
	}
	if err := rs.Err(); err != nil {
		return inUse(op, err)
	}
	if n == 0 {
		return errs.E(op, errs.NotFound, errors.New("no such role"))
	}
	return nil
}

// inUse classifies err, re-kinding the foreign-key violation of a referenced role to
// errs.Conflict (the *db.Error stays in the chain, so Constraint is readable).
func inUse(op string, err error) error {
	wrapped := db.Wrap(op, err)
	var pe *db.Error
	if errors.As(wrapped, &pe) && roleInUseConstraints[pe.Constraint] {
		return errs.E(op, errs.Conflict, wrapped)
	}
	return wrapped
}

// AdminGrants lists the active, site-wide grants of roles holding q.Permission, one row
// per grant with the person and whether the change q describes removes it.
func (s *Store) AdminGrants(ctx context.Context, q rows.Holders) ([]rows.AdminGrant, error) {
	const op = "authz.postgres.AdminGrants"
	rs, err := s.db.Query(ctx, sqlAdminGrants, q.Permission, q.Now.UTC(),
		uuidOrNil(q.Grant), uuidOrNil(q.Person), uuidOrNil(q.Role))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.AdminGrant{}
	for rs.Next() {
		var (
			g      rows.AdminGrant
			person [16]byte
		)
		if err := rs.Scan(&person, &g.Removes); err != nil {
			return nil, db.Wrap(op, err)
		}
		g.Person = model.PersonID(person)
		out = append(out, g)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// PermissionHolders returns the persons who hold permission site-wide now through an
// explicit grant (never the base role), distinct and in person ID order. The
// separation-of-duties rules are not applied.
func (s *Store) PermissionHolders(ctx context.Context, permission string, now time.Time) ([]model.PersonID, error) {
	const op = "authz.postgres.PermissionHolders"
	rs, err := s.db.Query(ctx, sqlListHolders, permission, now.UTC())
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []model.PersonID{}
	for rs.Next() {
		var person [16]byte
		if err := rs.Scan(&person); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, model.PersonID(person))
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// ---- grants -----------------------------------------------------------------------------

// GetGrant returns one grant. Fails errs.NotFound for an unknown ID.
func (s *Store) GetGrant(ctx context.Context, id rows.GrantID) (rows.Grant, error) {
	const op = "authz.postgres.GetGrant"
	rs, err := s.db.Query(ctx, sqlGetGrant, [16]byte(id))
	if err != nil {
		return rows.Grant{}, db.Wrap(op, err)
	}
	gg, err := collectGrants(op, rs)
	if err != nil {
		return rows.Grant{}, err
	}
	if len(gg) == 0 {
		return rows.Grant{}, errs.E(op, errs.NotFound, errors.New("no such grant"))
	}
	return gg[0], nil
}

// InsertGrant adds a grant. A second grant of the same role to the same person for the same
// department fails errs.Conflict with Constraint role_grant_person_role_department_key; a
// person that does not exist fails on the foreign key (errs.Invalid, Constraint
// role_grant_person_id_fkey, once migration 0002 has run).
func (s *Store) InsertGrant(ctx context.Context, g rows.Grant) error {
	const op = "authz.postgres.InsertGrant"
	_, err := s.db.Exec(ctx, sqlInsertGrant, [16]byte(g.ID), [16]byte(g.Person), [16]byte(g.Role),
		uuidOrNil(g.Department), timeOrNil(g.EndsAt), uuidOrNil(g.GrantedBy), g.CreatedAt.UTC())
	return db.Wrap(op, err)
}

// DeleteGrant deletes one grant and returns it. Fails errs.NotFound for an unknown ID.
func (s *Store) DeleteGrant(ctx context.Context, id rows.GrantID) (rows.Grant, error) {
	const op = "authz.postgres.DeleteGrant"
	rs, err := s.db.Query(ctx, sqlDeleteGrant, [16]byte(id))
	if err != nil {
		return rows.Grant{}, db.Wrap(op, err)
	}
	gg, err := collectGrants(op, rs)
	if err != nil {
		return rows.Grant{}, err
	}
	if len(gg) == 0 {
		return rows.Grant{}, errs.E(op, errs.NotFound, errors.New("no such grant"))
	}
	return gg[0], nil
}

// ListGrants returns the grants matching f, newest first, strictly after the cursor, up to
// f.Limit rows.
func (s *Store) ListGrants(ctx context.Context, f rows.GrantFilter) ([]rows.Grant, error) {
	const op = "authz.postgres.ListGrants"
	departments := uuids(f.Departments)
	rs, err := s.db.Query(ctx, sqlListGrants,
		uuidOrNil(f.Person), uuidOrNil(f.Role), uuidOrNil(f.Department), textOrNil(f.Permission),
		f.IncludeEnded, f.Now.UTC(), f.WholeSite, departments,
		timeOrNil(f.AfterTime), uuidOrNil(f.AfterID), int32(f.Limit)) //nolint:gosec // Limit is at most authz.MaxPageSize + 1
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectGrants(op, rs)
}

func collectGrants(op string, rs pgx.Rows) ([]rows.Grant, error) {
	defer rs.Close()
	out := []rows.Grant{}
	for rs.Next() {
		var (
			g                rows.Grant
			id, person, role [16]byte
			dept, granter    *[16]byte
			endsAt           *time.Time
		)
		if err := rs.Scan(&id, &person, &role, &dept, &endsAt, &granter, &g.CreatedAt); err != nil {
			return nil, db.Wrap(op, err)
		}
		g.ID, g.Person, g.Role = rows.GrantID(id), model.PersonID(person), rows.RoleID(role)
		g.Department = idOf[model.DepartmentID](dept)
		g.GrantedBy = idOf[model.PersonID](granter)
		g.EndsAt, g.CreatedAt = utcOf(endsAt), g.CreatedAt.UTC()
		out = append(out, g)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// DeleteGrantsByPerson deletes every grant of the person, ended ones too, and returns them.
// Idempotent.
func (s *Store) DeleteGrantsByPerson(ctx context.Context, person model.PersonID) ([]rows.Grant, error) {
	const op = "authz.postgres.DeleteGrantsByPerson"
	rs, err := s.db.Query(ctx, sqlDeleteByPerson, [16]byte(person))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collectGrants(op, rs)
}

// ClearGrantedBy clears the granter of every grant the person made. Idempotent.
func (s *Store) ClearGrantedBy(ctx context.Context, person model.PersonID) error {
	const op = "authz.postgres.ClearGrantedBy"
	_, err := s.db.Exec(ctx, sqlClearGrantedBy, [16]byte(person))
	return db.Wrap(op, err)
}

// ExportByPerson returns every grant of the person, ended ones included, oldest first.
func (s *Store) ExportByPerson(ctx context.Context, person model.PersonID) ([]rows.ExportGrant, error) {
	const op = "authz.postgres.ExportByPerson"
	rs, err := s.db.Query(ctx, sqlExportGrants, [16]byte(person))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.ExportGrant{}
	for rs.Next() {
		var (
			e             rows.ExportGrant
			id, role      [16]byte
			key           *string
			dept, granter *[16]byte
			endsAt        *time.Time
		)
		if err := rs.Scan(&id, &role, &key, &e.RoleName, &dept, &endsAt, &granter, &e.CreatedAt); err != nil {
			return nil, db.Wrap(op, err)
		}
		e.Grant, e.Role = rows.GrantID(id), rows.RoleID(role)
		if key != nil {
			e.RoleKey = *key
		}
		e.Dept = idOf[model.DepartmentID](dept)
		e.GrantedBy = idOf[model.PersonID](granter)
		e.EndsAt, e.CreatedAt = utcOf(endsAt), e.CreatedAt.UTC()
		out = append(out, e)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// ---- rules ------------------------------------------------------------------------------

// ListRules returns the rules sorted by role name with the number of people each affects
// now. declared is the catalogue (names and their Reaches words); a zero id lists every
// rule, otherwise just that one.
func (s *Store) ListRules(ctx context.Context, now time.Time, declared []rows.Declared, id rows.RuleID) ([]rows.Rule, error) {
	const op = "authz.postgres.ListRules"
	names := make([]string, len(declared))
	reaches := make([]string, len(declared))
	for i, d := range declared {
		names[i], reaches[i] = d.Name, d.Reaches
	}
	rs, err := s.db.Query(ctx, sqlListRules, now.UTC(), names, reaches, uuidOrNil(id))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Rule{}
	for rs.Next() {
		var (
			r        rows.Rule
			id, role [16]byte
			perm     *string
			lvl      *string
			affected int32
		)
		if err := rs.Scan(&id, &role, &perm, &lvl, &r.Note, &r.CreatedAt, &affected); err != nil {
			return nil, db.Wrap(op, err)
		}
		r.ID, r.Role = rows.RuleID(id), rows.RoleID(role)
		if perm != nil {
			r.Permission = *perm
		}
		if lvl != nil {
			r.Reaches = *lvl
		}
		r.CreatedAt, r.Affected = r.CreatedAt.UTC(), int(affected)
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// InsertRule adds a rule. The same rule twice fails errs.Conflict with Constraint
// sod_rule_role_permission_key or sod_rule_role_reaches_key.
func (s *Store) InsertRule(ctx context.Context, r rows.Rule) error {
	const op = "authz.postgres.InsertRule"
	_, err := s.db.Exec(ctx, sqlInsertRule, [16]byte(r.ID), [16]byte(r.Role),
		textOrNil(r.Permission), textOrNil(r.Reaches), r.Note, r.CreatedAt.UTC())
	return db.Wrap(op, err)
}

// DeleteRule deletes one rule. Fails errs.NotFound for an unknown ID.
func (s *Store) DeleteRule(ctx context.Context, id rows.RuleID) error {
	const op = "authz.postgres.DeleteRule"
	rs, err := s.db.Query(ctx, sqlDeleteRule, [16]byte(id))
	if err != nil {
		return db.Wrap(op, err)
	}
	defer rs.Close()
	n := 0
	for rs.Next() {
		n++
	}
	if err := rs.Err(); err != nil {
		return db.Wrap(op, err)
	}
	if n == 0 {
		return errs.E(op, errs.NotFound, errors.New("no such rule"))
	}
	return nil
}

// ---- seed -------------------------------------------------------------------------------

// SeedStarterPermissions applies parallel (role key, permission) pairs once each and
// returns the role_permission rows inserted, sorted by role then permission.
func (s *Store) SeedStarterPermissions(ctx context.Context, keys, perms []string, at time.Time) ([]rows.SeededPermission, error) {
	const op = "authz.postgres.SeedStarterPermissions"
	if len(keys) != len(perms) {
		return nil, errs.E(op, errs.Internal, errors.New("role keys and permissions differ in length"))
	}
	rs, err := s.db.Query(ctx, sqlSeedStarterPerms, keys, perms, at.UTC())
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.SeededPermission{}
	for rs.Next() {
		var (
			sp   rows.SeededPermission
			role [16]byte
		)
		if err := rs.Scan(&role, &sp.Permission); err != nil {
			return nil, db.Wrap(op, err)
		}
		sp.Role = rows.RoleID(role)
		out = append(out, sp)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}
