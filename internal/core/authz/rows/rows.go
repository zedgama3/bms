// Package rows holds the row types authz's service and its PostgreSQL store share. It is a
// leaf: it imports model only, because authz imports postgres to build its store and so
// postgres cannot import authz. Permissions travel as plain strings and scopes as the words
// "site" and "department", exactly as the tables hold them.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

type (
	role  struct{}
	grant struct{}
	rule  struct{}
)

// RoleID identifies a role. authz re-exports it under the same name.
type RoleID = model.ID[role]

// GrantID identifies a grant. authz re-exports it under the same name.
type GrantID = model.ID[grant]

// RuleID identifies a rule. authz re-exports it under the same name.
type RuleID = model.ID[rule]

// The two scopes, as core.role.scope holds them.
const (
	ScopeSite       = "site"
	ScopeDepartment = "department"
)

// Role is a row of core.role without its permissions. Key is "" for a regional's own role
// (NULL in the table).
type Role struct {
	ID          RoleID
	Key         string
	Name        string
	Description string
	Scope       string
	Base        bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Grant is a row of core.role_grant. A zero Department is site-wide (NULL), a zero EndsAt
// is no end, a zero GrantedBy is no granter.
type Grant struct {
	ID         GrantID
	Person     model.PersonID
	Role       RoleID
	Department model.DepartmentID
	EndsAt     time.Time
	GrantedBy  model.PersonID
	CreatedAt  time.Time
}

// Held is one permission a person holds through an active grant (or the base role). A zero
// Department means site-wide.
type Held struct {
	Permission string
	Department model.DepartmentID
}

// HeldRule is a separation-of-duties rule whose role a person holds. Exactly one of
// Permission and Reaches is set; Reaches is the sensitivity word.
type HeldRule struct {
	ID         RuleID
	Role       RoleID
	Permission string
	Reaches    string
	Note       string
}

// Rule is a row of core.sod_rule with the number of people it affects now.
type Rule struct {
	HeldRule
	CreatedAt time.Time
	Affected  int
}

// Declared is one entry of the in-memory catalogue as ListRules needs it: the name and the
// sensitivity word of its Reaches.
type Declared struct {
	Name    string
	Reaches string
}

// GrantFilter selects grants for a list. Zero values mean "any". WholeSite true lets rows of
// every department through; otherwise only grants whose department is in Departments.
type GrantFilter struct {
	Person       model.PersonID
	Role         RoleID
	Department   model.DepartmentID
	Permission   string
	IncludeEnded bool
	Now          time.Time
	WholeSite    bool
	Departments  []model.DepartmentID
	AfterTime    time.Time // the cursor; zero on the first page
	AfterID      GrantID
	Limit        int // the page size plus one
}

// Holders names the grants a change removes, for the last-access-admin check. Any of the
// three may be zero. Permission is the permission whose site-wide holders are counted.
type Holders struct {
	Permission string
	Now        time.Time
	Grant      GrantID        // Revoke: the grant being deleted
	Person     model.PersonID // DeletePerson: every grant of this person
	Role       RoleID         // UpdateRole: every grant of the role losing the permission
}

// ExportGrant is one grant as ExportPerson shows it, with its role's key and name.
type ExportGrant struct {
	Grant     GrantID
	Role      RoleID
	RoleKey   string
	RoleName  string
	Dept      model.DepartmentID
	EndsAt    time.Time
	GrantedBy model.PersonID
	CreatedAt time.Time
}
