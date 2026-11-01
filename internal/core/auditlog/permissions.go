package auditlog

import (
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/model"
)

// The permissions this package checks. The names are audit's vocabulary (a package that only
// asks whether the viewer holds one, as people does, reads them there).
const (
	PermissionSearch = audit.PermissionSearch // search the log
	PermissionExport = audit.PermissionExport // export the views of one person's data
)

// Permissions declares PermissionSearch and PermissionExport (moved from audit); app adds
// them to the permissions it registers with authz.
var Permissions = []authz.PermissionDecl{
	{Name: PermissionSearch, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive, StarterRoles: []string{authz.RoleAuditReviewer}},
	{Name: PermissionExport, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive, StarterRoles: []string{authz.RoleAuditReviewer}},
}
