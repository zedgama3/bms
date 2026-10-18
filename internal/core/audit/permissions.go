package audit

import (
	"io/fs"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/model"
)

// Permissions this package declares; app registers them in corePerms (authz, M2).
const (
	PermissionSearch authz.Permission = "audit.search" // search the log
	PermissionExport authz.Permission = "audit.export" // export the views of one person's data
)

// Permissions lists the declarations of PermissionSearch and PermissionExport.
var Permissions = []authz.PermissionDecl{
	{Name: PermissionSearch, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
	{Name: PermissionExport, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive},
}

// Migrations is the embedded postgres/migrations: 0001_init.sql creates core.audit_entry,
// its guard and core.audit_purge.
var Migrations fs.FS = postgres.Migrations

// Set is what app hands db.Migrate for the audit tables.
var Set = db.Set{Name: "audit", Schema: "core", Files: Migrations}
