package authz

import (
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// The JSON forms of the access API (openapi.json, schemas Access*). IDs are canonical UUID
// strings and times RFC 3339 UTC.

type wirePermission struct {
	Name         Permission `json:"name"`
	Scope        string     `json:"scope"`
	Reaches      string     `json:"reaches"`
	Module       string     `json:"module"`
	StarterRoles []string   `json:"starter_roles"`
}

type wireRolePermission struct {
	Name     Permission `json:"name"`
	Declared bool       `json:"declared"`
}

type wireRole struct {
	ID          RoleID               `json:"id"`
	Key         string               `json:"key,omitempty"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Scope       string               `json:"scope"`
	Base        bool                 `json:"base"`
	Permissions []wireRolePermission `json:"permissions"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type wireRoleInput struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scope       string       `json:"scope"`
	Permissions []Permission `json:"permissions"`
}

type wireGrant struct {
	ID           GrantID            `json:"id"`
	PersonID     model.PersonID     `json:"person_id"`
	RoleID       RoleID             `json:"role_id"`
	RoleKey      *string            `json:"role_key"`
	RoleName     string             `json:"role_name"`
	DepartmentID model.DepartmentID `json:"department_id,omitzero"`
	EndsAt       time.Time          `json:"ends_at,omitzero"`
	GrantedBy    model.PersonID     `json:"granted_by,omitzero"`
	CreatedAt    time.Time          `json:"created_at"`
	Ended        bool               `json:"ended"`
}

type wireGrantInput struct {
	PersonID     model.PersonID     `json:"person_id"`
	RoleID       RoleID             `json:"role_id"`
	DepartmentID model.DepartmentID `json:"department_id"`
	EndsAt       time.Time          `json:"ends_at"`
}

type wireGrantPage struct {
	Grants []wireGrant `json:"grants"`
	Next   Cursor      `json:"next,omitempty"`
}

type wireRule struct {
	ID        RuleID     `json:"id"`
	RoleID    RoleID     `json:"role_id"`
	Perm      Permission `json:"permission,omitempty"`
	Reaches   string     `json:"reaches,omitempty"`
	Note      string     `json:"note"`
	Affected  int        `json:"affected"`
	CreatedAt time.Time  `json:"created_at"`
}

type wireRuleInput struct {
	RoleID     RoleID     `json:"role_id"`
	Permission Permission `json:"permission"`
	Reaches    string     `json:"reaches"`
	Note       string     `json:"note"`
}

func wirePermissionOf(i PermissionInfo) wirePermission {
	roles := i.StarterRoles
	if roles == nil {
		roles = []string{}
	}
	return wirePermission{
		Name: i.Name, Scope: i.Scope.String(), Reaches: i.Reaches.String(), Module: i.Name.Module(), StarterRoles: roles,
	}
}

func wireRoleOf(r Role) wireRole {
	out := wireRole{
		ID: r.ID, Key: r.Key, Name: r.Name, Description: r.Description, Scope: r.Scope.String(), Base: r.Base,
		Permissions: make([]wireRolePermission, len(r.Permissions)), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for i, p := range r.Permissions {
		out.Permissions[i] = wireRolePermission(p)
	}
	return out
}

// scopeFromWord reads the scope word of an input: "" is the zero Scope (UpdateRole accepts
// it), "site" and "department" are themselves, anything else fails errs.Invalid.
func scopeFromWord(op, word string) (Scope, error) {
	switch word {
	case "":
		return 0, nil
	case "site":
		return ScopeSite, nil
	case "department":
		return ScopeDepartment, nil
	}
	return 0, fieldInvalid(op, "scope", codeOneOf, "must be site or department")
}

func (w wireRoleInput) input(op string) (RoleInput, error) {
	sc, err := scopeFromWord(op, w.Scope)
	if err != nil {
		return RoleInput{}, err
	}
	return RoleInput{Name: w.Name, Description: w.Description, Scope: sc, Permissions: w.Permissions}, nil
}

func (s *Service) wireGrantOf(g Grant, now time.Time) wireGrant {
	var key *string
	if g.RoleKey != "" {
		key = &g.RoleKey
	}
	return wireGrant{
		ID: g.ID, PersonID: g.Person, RoleID: g.Role, RoleKey: key, RoleName: g.RoleName, DepartmentID: g.Department, EndsAt: g.EndsAt,
		GrantedBy: g.GrantedBy, CreatedAt: g.CreatedAt, Ended: !g.EndsAt.IsZero() && !g.EndsAt.After(now),
	}
}

func wireRuleOf(r Rule) wireRule {
	out := wireRule{ID: r.ID, RoleID: r.Role, Perm: r.Permission, Note: r.Note, Affected: r.Affected, CreatedAt: r.CreatedAt}
	if r.Reaches != 0 {
		out.Reaches = r.Reaches.String()
	}
	return out
}

func (w wireRuleInput) input(op string) (RuleInput, error) {
	in := RuleInput{Role: w.RoleID, Permission: w.Permission, Note: w.Note}
	if w.Reaches != "" {
		level, err := model.ParseSensitivity(w.Reaches)
		if err != nil {
			v := validate.Errors{Op: op}
			v.Addf("reaches", codeOneOf, "must be one of: public, internal, personal, sensitive")
			return RuleInput{}, v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
		}
		in.Reaches = level
	}
	return in, nil
}
