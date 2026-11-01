package audit

import (
	"io/fs"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/core/authz/perm"
	"github.com/zedgama3/bms/internal/core/db"
)

// The names of the permissions that guard reading the log. auditlog declares them (its
// Permissions) and checks them; the names live here, with the vocabulary, so a package that
// asks whether the viewer holds one (people, for the person page) need not import auditlog.
const (
	PermissionSearch perm.Permission = "audit.search" // search the log
	PermissionExport perm.Permission = "audit.export" // export the views of one person's data
)

// Migrations is the embedded postgres/migrations: 0001_init.sql creates core.audit_entry,
// its guard and core.audit_purge.
var Migrations fs.FS = postgres.Migrations

// Set is what app hands db.Migrate for the audit tables.
var Set = db.Set{Name: "audit", Schema: "core", Files: Migrations}
