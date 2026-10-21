package files

import (
	"context"
	"log/slog"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// PolicySource reads the policy at use-case time (app: over a settings.Reader scoped
// "files"), so a change of setting applies to the next upload.
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

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
	// Policy is optional: nil means DefaultPolicy.
	Policy PolicySource
}

// Validate fails errs.Invalid joining one FieldError per problem: a nil dependency (fields
// files.db, files.clock, files.logger, files.tracer, files.audit, files.actor,
// files.objects) or an empty TempDir (files.temp_dir).
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
	} {
		if d.isNil {
			v.Addf(d.field, validate.CodeRequired, "must be set")
		}
	}
	if strings.TrimSpace(o.TempDir) == "" || strings.ContainsRune(o.TempDir, 0) {
		v.Addf("files.temp_dir", validate.CodeRequired, "must be set to a directory path")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
