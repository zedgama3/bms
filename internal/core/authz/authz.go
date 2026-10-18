package authz

import (
	"context"
	"encoding/base64"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/authz/perm"
	"github.com/zedgama3/bms/internal/core/authz/postgres"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits (bytes, not characters, to match the CHECK constraints).
const (
	MaxRoleNameBytes    = 64  // Role.Name
	MaxDescriptionBytes = 512 // Role.Description
	MaxNoteBytes        = 256 // Rule.Note
	MaxRolePermissions  = 256 // permissions in one role
	MaxPageSize         = 200 // GrantQuery.Limit
	DefaultPageSize     = 50  // GrantQuery.Limit when 0
)

// The starter role keys migration 0001 creates (decision 12), re-exported from package perm,
// where audit can name them without importing authz. RoleParticipant is the base role: every
// signed-in person holds it without a grant. RoleDepartmentAdmin is department-scoped; the
// rest are site roles.
const (
	RoleParticipant       = perm.RoleParticipant
	RoleAccountAdmin      = perm.RoleAccountAdmin
	RoleAccessAdmin       = perm.RoleAccessAdmin
	RoleDepartmentSteward = perm.RoleDepartmentSteward
	RoleEventAdmin        = perm.RoleEventAdmin
	RoleSettingsAdmin     = perm.RoleSettingsAdmin
	RoleAuditReviewer     = perm.RoleAuditReviewer
	RoleDepartmentAdmin   = perm.RoleDepartmentAdmin
)

// SiteAdminRoles are the six site-wide administrative starter roles: the starter roles of
// api's core.modules.read. It is perm.SiteAdminRoles; callers must not modify it.
var SiteAdminRoles = perm.SiteAdminRoles

// The permissions authz declares; app adds Permissions to corePerms.
const (
	PermissionRolesRead              Permission = "access.roles.read"               // list the catalogue, roles and rules
	PermissionRolesManage            Permission = "access.roles.manage"             // create, change, delete roles
	PermissionRulesManage            Permission = "access.rules.manage"             // create and delete separation-of-duties rules
	PermissionGrantsRead             Permission = "access.grants.read"              // see who holds what
	PermissionGrantsManage           Permission = "access.grants.manage"            // grant and revoke roles
	PermissionDepartmentGrantsManage Permission = "access.department-grants.manage" // grant and revoke department roles anywhere
)

// Permissions lists the declarations of the six permissions above, with their starter roles
// (authz.md, "Permissions").
var Permissions = []PermissionDecl{
	{Name: PermissionRolesRead, Scope: ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleAccessAdmin}},
	{Name: PermissionRolesManage, Scope: ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleAccessAdmin}},
	{Name: PermissionRulesManage, Scope: ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleAccessAdmin}},
	{Name: PermissionGrantsRead, Scope: ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleAccessAdmin, RoleDepartmentAdmin}},
	{Name: PermissionGrantsManage, Scope: ScopeDepartment, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleAccessAdmin, RoleDepartmentAdmin}},
	{Name: PermissionDepartmentGrantsManage, Scope: ScopeSite, Reaches: model.SensitivityInternal, StarterRoles: []string{RoleDepartmentSteward}},
}

// Signals authz publishes, and their payload keys. The payload carries IDs only; Department
// is present only for a department grant.
const (
	SignalGrantCreated signals.Name = "access.grant-created"
	SignalGrantRevoked signals.Name = "access.grant-revoked"

	PayloadGrant      = "grant"
	PayloadPerson     = "person"
	PayloadRole       = "role"
	PayloadDepartment = "department"
)

// Set is the migration set app adds to coreSets (db.md Proposed change 10).
var Set = db.Set{Name: "authz", Schema: "core", Files: postgres.Migrations}

// Field codes authz adds to the shell's vocabulary.
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeOneOf     = validate.CodeOneOf
	codeRange     = validate.CodeRange
	codeTooLong   = "too_long"  // params: max
	codeDuplicate = "duplicate" // a repeated item, or a grant that exists
	codeScope     = "scope"     // a site-only permission in a department role
	codeMismatch  = "mismatch"  // a department given for a site role
	codeBase      = "base"      // the base role cannot be granted or deleted
	codeImmutable = "immutable" // scope, key or base cannot change
	codeSelf      = "self"      // a grant or revoke aimed at oneself
	codeUnknown   = "unknown"   // an inactive department, or a person that does not exist
	codeTaken     = "taken"     // a role name in use
	codeInUse     = "in_use"    // a role that grants or rules reference
	codeLastAdmin = "last_access_admin"
	codeSoD       = "separation_of_duties" // params: rule
)

// Actor is the person a decision is about. app builds it from identity.SessionFrom.
type Actor struct {
	Person    model.PersonID
	Superuser bool // D23: allowed before any query; never a role
}

// ActorFunc reads the actor from ctx without I/O. ok is false when there is no session or
// the session is restricted (identity.PersonFrom's rule). The service additionally treats
// signals.InDelivery(ctx) as "no actor" (signals decision 7).
type ActorFunc func(ctx context.Context) (Actor, bool)

// Extent is where a person holds a permission. Site true means everywhere (Departments is
// then empty). Departments is sorted and unique.
type Extent struct {
	Site        bool
	Departments []model.DepartmentID
}

// Covers reports whether d is inside the extent. The zero department is never covered by a
// department extent. Cannot fail.
func (e Extent) Covers(d model.DepartmentID) bool {
	if e.Site {
		return true
	}
	if d.IsZero() {
		return false
	}
	return slices.Contains(e.Departments, d)
}

// IsZero reports that the permission is held nowhere. Cannot fail.
func (e Extent) IsZero() bool { return !e.Site && len(e.Departments) == 0 }

// Authorizer is what every module receives as Deps.Authz and what the api, audit and
// identity seams are satisfied by. Implementations are safe for concurrent use.
type Authorizer interface {
	// Allow fails errs.Permission unless the actor holds p site-wide. Consumers: api
	// (shell routes), audit (Search, Export), identity (admin methods), every module's
	// site-scoped use cases. Fails errs.Unauthenticated without an actor and errs.Invalid
	// for a permission nobody declared.
	Allow(ctx context.Context, p Permission) error
	// AllowIn fails errs.Permission unless the actor holds p site-wide or for d.
	// Consumers: a module's department-scoped write or single-record read
	// (volunteers.shifts.manage for one department's shift). Fails errs.Invalid for the
	// zero department.
	AllowIn(ctx context.Context, p Permission, d model.DepartmentID) error
	// ExtentOf returns where the actor holds p; a zero Extent and nil error when nowhere.
	// Consumers: list, search, count and export use cases, which filter by it.
	ExtentOf(ctx context.Context, p Permission) (Extent, error)
}

// DepartmentSource says whether a department exists and is not archived.
// departments-events implements it (M3); nil refuses department grants (Unsupported).
type DepartmentSource interface {
	Active(ctx context.Context, d model.DepartmentID) (bool, error)
}

// RoleID identifies a role.
type RoleID = rows.RoleID

// GrantID identifies a grant.
type GrantID = rows.GrantID

// RuleID identifies a separation-of-duties rule.
type RuleID = rows.RuleID

// Role is a named set of permissions.
type Role struct {
	ID          RoleID
	Key         string // starter roles only ("account-admin"); "" otherwise; immutable
	Name        string // "" while an unrenamed starter role (the shell shows core.access.role.<key>.title)
	Description string
	Scope       Scope // immutable after creation
	Base        bool  // the one role every signed-in person holds; immutable
	Permissions []RolePermission
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RolePermission is one permission of a role.
type RolePermission struct {
	Name     Permission
	Declared bool // false: not available in this installation (decision 13)
}

// RoleInput is what CreateRole and UpdateRole take.
type RoleInput struct {
	Name        string       // 1..MaxRoleNameBytes after trim, no control characters
	Description string       // ..MaxDescriptionBytes
	Scope       Scope        // create only; UpdateRole accepts the role's own scope or zero
	Permissions []Permission // unique; ..MaxRolePermissions; ScopeDepartment only in a department role
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "name"
// (required, too_long, malformed), "description" (too_long, malformed), "scope" (one_of),
// "permissions" (too_long) and "permissions[i]" (malformed, duplicate). Whether a
// permission fits the role's scope is checked against the catalogue by the service
// ("permissions[i]" scope). Messages never echo a value.
func (in RoleInput) Validate() error { return in.validate(true, true) }

func (in RoleInput) validate(needName, needScope bool) error {
	v := validate.Errors{Op: "authz.RoleInput.Validate"}
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "" && needName:
		v.Addf("name", codeRequired, "must be set")
	case len(name) > MaxRoleNameBytes:
		v.Add(tooLong("name", MaxRoleNameBytes))
	case !cleanText(name):
		v.Addf("name", codeMalformed, "must be valid text without control characters")
	}
	switch {
	case len(in.Description) > MaxDescriptionBytes:
		v.Add(tooLong("description", MaxDescriptionBytes))
	case !cleanText(in.Description):
		v.Addf("description", codeMalformed, "must be valid text without control characters")
	}
	if needScope && in.Scope != ScopeSite && in.Scope != ScopeDepartment {
		v.Addf("scope", codeOneOf, "must be site or department")
	}
	if len(in.Permissions) > MaxRolePermissions {
		v.Add(tooLong("permissions", MaxRolePermissions))
	}
	seen := make(map[Permission]struct{}, len(in.Permissions))
	for i, p := range in.Permissions {
		field := "permissions[" + strconv.Itoa(i) + "]"
		if p.Validate() != nil {
			v.Addf(field, codeMalformed, "must be <module>.<ident>(.<ident>)*")
			continue
		}
		if _, dup := seen[p]; dup {
			v.Addf(field, codeDuplicate, "each permission may be listed once")
		}
		seen[p] = struct{}{}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// tooLong is the field error for a length limit, with the limit as the parameter "max".
func tooLong(field string, limit int) error {
	return errs.E("", errs.Invalid, &validate.FieldError{
		Field: field, Code: codeTooLong, Params: map[string]string{"max": strconv.Itoa(limit)},
		Reason: "must be at most " + strconv.Itoa(limit) + " bytes",
	})
}

// cleanText reports valid UTF-8 without control characters.
func cleanText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

// Grant gives a person a role, site-wide or for one department.
type Grant struct {
	ID         GrantID
	Person     model.PersonID
	Role       RoleID
	RoleKey    string             // the role's starter key ("" for a role a regional made); filled by ListGrants and Grant
	RoleName   string             // the role's name ("" while an unrenamed starter role); filled by ListGrants and Grant
	Department model.DepartmentID // zero: site-wide
	EndsAt     time.Time          // zero: no end
	GrantedBy  model.PersonID     // zero: operator, seed or a deleted person
	CreatedAt  time.Time
}

// GrantInput is what Service.Grant takes.
type GrantInput struct {
	Person     model.PersonID
	Role       RoleID
	Department model.DepartmentID
	EndsAt     time.Time
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "person" and
// "role" (required). The rest is checked against the role and the clock by Grant.
func (in GrantInput) Validate() error {
	v := validate.Errors{Op: "authz.GrantInput.Validate"}
	if in.Person.IsZero() {
		v.Addf("person", codeRequired, "must be set")
	}
	if in.Role.IsZero() {
		v.Addf("role", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// GrantQuery selects grants for ListGrants; zero fields match anything.
type GrantQuery struct {
	Person       model.PersonID
	Role         RoleID
	Department   model.DepartmentID
	Permission   Permission // grants of roles holding it ("everyone who holds a permission")
	IncludeEnded bool
	After        Cursor
	Limit        int // 0 = DefaultPageSize; 1..MaxPageSize
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "permission"
// (malformed), "after" (malformed: not a cursor this service made), "limit" (range).
func (q GrantQuery) Validate() error {
	v := validate.Errors{Op: "authz.GrantQuery.Validate"}
	if q.Permission != "" && q.Permission.Validate() != nil {
		v.Addf("permission", codeMalformed, "must be <module>.<ident>(.<ident>)*")
	}
	if !q.After.IsZero() {
		if _, _, err := q.After.position(); err != nil {
			v.Addf("after", codeMalformed, "is not a cursor from a previous page")
		}
	}
	if q.Limit < 0 || q.Limit > MaxPageSize {
		v.Add(validate.Range("limit", q.Limit, 0, MaxPageSize))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// pageSize is Limit with the default applied.
func (q GrantQuery) pageSize() int {
	if q.Limit == 0 {
		return DefaultPageSize
	}
	return q.Limit
}

// GrantPage is one page of grants, newest first.
type GrantPage struct {
	Grants []Grant
	Next   Cursor // zero when no more
}

// Cursor is an opaque, URL-safe continuation token: base64url of a grant's created_at (in
// microseconds, PostgreSQL's precision) and ID.
type Cursor string

// String returns the token. Cannot fail.
func (c Cursor) String() string { return string(c) }

// IsZero reports the empty cursor. Cannot fail.
func (c Cursor) IsZero() bool { return c == "" }

// cursorOf encodes the position of g in the list.
func cursorOf(g Grant) Cursor {
	raw := strconv.FormatInt(g.CreatedAt.UnixMicro(), 10) + ":" + g.ID.String()
	return Cursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

// ParseCursor checks that s is a cursor this package made. It fails errs.Invalid (field
// "after", code malformed) otherwise.
func ParseCursor(s string) (Cursor, error) {
	c := Cursor(s)
	if _, _, err := c.position(); err != nil {
		return "", err
	}
	return c, nil
}

func (c Cursor) position() (time.Time, GrantID, error) {
	const op = "authz.ParseCursor"
	bad := func() error {
		v := validate.Errors{Op: op}
		v.Addf("after", codeMalformed, "is not a cursor from a previous page")
		return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(string(c))
	if err != nil {
		return time.Time{}, GrantID{}, bad()
	}
	ts, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return time.Time{}, GrantID{}, bad()
	}
	micro, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return time.Time{}, GrantID{}, bad()
	}
	gid, err := model.ParseID[GrantID](id)
	if err != nil || gid.IsZero() {
		return time.Time{}, GrantID{}, bad()
	}
	return time.UnixMicro(micro).UTC(), gid, nil
}

// Rule says holding Role forbids Permission, or every declared permission reaching at least
// Reaches (decision 4).
type Rule struct {
	ID         RuleID
	Role       RoleID
	Permission Permission        // exactly one of Permission/Reaches
	Reaches    model.Sensitivity // zero when Permission is set
	Note       string            // the regional's reason, ..MaxNoteBytes
	Affected   int               // people holding the role and a forbidden permission now (filled by ListRules)
	CreatedAt  time.Time
}

// RuleInput is what CreateRule takes.
type RuleInput struct {
	Role       RoleID
	Permission Permission
	Reaches    model.Sensitivity
	Note       string
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "role" (required),
// "permission" and "reaches" (one_of when both or neither are set; malformed or one_of
// when set badly) and "note" (too_long, malformed).
func (in RuleInput) Validate() error {
	v := validate.Errors{Op: "authz.RuleInput.Validate"}
	if in.Role.IsZero() {
		v.Addf("role", codeRequired, "must be set")
	}
	hasPerm, hasLevel := in.Permission != "", in.Reaches != 0
	switch {
	case hasPerm == hasLevel:
		v.Addf("permission", codeOneOf, "set exactly one of permission and reaches")
		v.Addf("reaches", codeOneOf, "set exactly one of permission and reaches")
	case hasPerm && in.Permission.Validate() != nil:
		v.Addf("permission", codeMalformed, "must be <module>.<ident>(.<ident>)*")
	case hasLevel && in.Reaches.Validate() != nil:
		v.Addf("reaches", codeOneOf, "must be one of: public, internal, personal, sensitive")
	}
	switch {
	case len(in.Note) > MaxNoteBytes:
		v.Add(tooLong("note", MaxNoteBytes))
	case !cleanText(in.Note):
		v.Addf("note", codeMalformed, "must be valid text without control characters")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// PermissionInfo is one row of the catalogue: the declaration, with Module() from the name.
type PermissionInfo struct{ PermissionDecl }

// Module returns the first segment of the permission's name. Cannot fail.
func (i PermissionInfo) Module() string { return i.Name.Module() }

// SeedResult is what Seed reports.
type SeedResult struct{ Added int }
