package auditlog

import (
	"log/slog"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Options builds the service. Every field is required.
type Options struct {
	DB       db.DB
	Clock    clock.Clock
	Logger   *slog.Logger
	Tracer   telemetry.Tracer
	Authz    authz.Authorizer // the Lookup
	Actor    authz.ActorFunc  // identity.Actor
	Settings settings.Reader  // scoped "audit"
	Jobs     jobs.Registrar   // scoped "audit": Start registers audit.purge
	Audit    audit.Recorder   // scoped "audit": records audit.searched, audit.exported and audit.purged
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency: "auditlog.db",
// "auditlog.clock", "auditlog.logger", "auditlog.tracer", "auditlog.authz", "auditlog.actor",
// "auditlog.settings", "auditlog.jobs", "auditlog.audit".
func (o Options) Validate() error {
	v := validate.Errors{Op: "auditlog.Options.Validate"}
	for _, f := range []struct {
		name  string
		unset bool
	}{
		{"auditlog.db", o.DB == nil},
		{"auditlog.clock", o.Clock == nil},
		{"auditlog.logger", o.Logger == nil},
		{"auditlog.tracer", o.Tracer == nil},
		{"auditlog.authz", o.Authz == nil},
		{"auditlog.actor", o.Actor == nil},
		{"auditlog.settings", o.Settings == nil},
		{"auditlog.jobs", o.Jobs == nil},
		{"auditlog.audit", o.Audit == nil},
	} {
		if f.unset {
			v.Addf(f.name, validate.CodeRequired, "must be set")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
