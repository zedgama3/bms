package files

import (
	"log/slog"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Options builds a Service.
type Options struct {
	DB     db.DB
	Clock  clock.Clock
	Logger *slog.Logger
	Tracer telemetry.Tracer
	// Meter is optional: without one the service counts nothing.
	Meter   telemetry.Meter
	Audit   audit.Recorder // scoped "files"
	Actor   authz.ActorFunc
	Objects objects.Store
	TempDir string // <files.dir>/tmp; created by bms migrate
	// Settings is a Reader scoped "files" (the Settings declarations): required. The policy
	// is mapped from it on every use case, so a changed setting applies to the next upload;
	// an unset key is its declared default.
	Settings settings.Reader
	// Ceiling bounds the settings-derived policy: each non-zero field is a maximum the policy
	// never exceeds, and a zero QuotaBytes setting (unlimited) becomes Ceiling.QuotaBytes.
	// Zero means no ceiling. It is a constant of the deployment, not a setting, so a
	// settings-admin cannot lift it (demo.FileCeiling in demo mode).
	Ceiling Policy
}

// Validate fails errs.Invalid joining one FieldError per problem: a nil dependency (fields
// files.db, files.clock, files.logger, files.tracer, files.audit, files.actor,
// files.objects, files.settings), an empty TempDir (files.temp_dir) or a negative field of
// Ceiling (files.ceiling).
func (o Options) Validate() error {
	v := validate.Errors{Op: "files.Options.Validate"}
	for _, d := range []struct {
		field string
		isNil bool
	}{
		{"files.db", o.DB == nil},
		{"files.clock", o.Clock == nil},
		{"files.logger", o.Logger == nil},
		{"files.tracer", o.Tracer == nil},
		{"files.audit", o.Audit == nil},
		{"files.actor", o.Actor == nil},
		{"files.objects", o.Objects == nil},
		{"files.settings", o.Settings == nil},
	} {
		if d.isNil {
			v.Addf(d.field, validate.CodeRequired, "must be set")
		}
	}
	if strings.TrimSpace(o.TempDir) == "" || strings.ContainsRune(o.TempDir, 0) {
		v.Addf("files.temp_dir", validate.CodeRequired, "must be set to a directory path")
	}
	if o.Ceiling.MaxBytes < 0 || o.Ceiling.QuotaBytes < 0 || o.Ceiling.MaxPerRecord < 0 {
		v.Addf("files.ceiling", validate.CodeRange, "must not be negative")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
