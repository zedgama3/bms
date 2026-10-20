package jobs

import (
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/postgres"
	"github.com/zedgama3/bms/internal/model"
)

// The permissions jobs declares; app adds Permissions to its core permissions.
const (
	PermissionRead   authz.Permission = "jobs.read"   // GET /api/jobs/: what is registered, last runs, counts, recent rows
	PermissionManage authz.Permission = "jobs.manage" // RunNow, Retry, Cancel
)

// Permissions lists the declarations of the two permissions above (jobs.md, "Permissions").
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionRead, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: authz.SiteAdminRoles,
	},
	{
		Name: PermissionManage, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleSettingsAdmin},
	},
}

// Set is the migration set app adds to its core sets.
var Set = db.Set{Name: "jobs", Schema: "core", Files: postgres.Migrations}

// The audit actions jobs records, and the record kind they point at.
const (
	actionRun      audit.Action = "jobs.job.run"
	actionRetried  audit.Action = "jobs.job.retried"
	actionCanceled audit.Action = "jobs.job.canceled"

	kindJob = "jobs.job"
)
