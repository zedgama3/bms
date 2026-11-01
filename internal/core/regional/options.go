package regional

import (
	"context"
	"log/slog"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// RevisionSource is settings' revision: the current number every settings write bumps, and
// the write seam that takes its row lock. *settings.Service is one.
type RevisionSource interface {
	// Revision answers the current snapshot's revision. It fails the snapshot's kinds
	// (Unavailable, Timeout, Canceled) before the first load.
	Revision(ctx context.Context) (settings.Revision, error)
	// Revise runs fn in one Write transaction under the revision lock and bumps the revision
	// when fn reports a change (settings.Service.Revise).
	Revise(ctx context.Context, fn func(ctx context.Context) (changed bool, err error)) (settings.Revision, error)
}

// TermSource answers every declared term in one language. *settings.Service is one
// (settings.TermsFor); it fails errs.Invalid for a language the build does not ship.
type TermSource interface {
	TermsFor(ctx context.Context, lang string) (map[string]settings.Forms, error)
}

// Options builds the service. Every field is required.
type Options struct {
	DB       db.DB // used for ReadOnly() alone: settings.Revise owns the logo's transaction
	Clock    clock.Clock
	Logger   *slog.Logger
	Tracer   telemetry.Tracer
	Audit    audit.Recorder // scoped "regional"
	Authz    authz.Authorizer
	Actor    authz.ActorFunc
	Settings settings.Reader // scoped "regional"
	Revision RevisionSource  // settings: the revision for the ETag and the cache key, and Revise for the logo
	Files    files.Store     // scoped "settings" (the logo)
	Terms    TermSource      // settings: the terms the public document carries per language
}

// Validate fails errs.Invalid joining a validate.FieldError per nil dependency: fields
// "regional.db", "regional.clock", "regional.logger", "regional.tracer", "regional.audit",
// "regional.authz", "regional.actor", "regional.settings", "regional.revision",
// "regional.files" and "regional.terms".
func (o Options) Validate() error {
	v := validate.Errors{Op: "regional.Options.Validate"}
	for _, f := range []struct {
		name  string
		unset bool
	}{
		{"regional.db", o.DB == nil},
		{"regional.clock", o.Clock == nil},
		{"regional.logger", o.Logger == nil},
		{"regional.tracer", o.Tracer == nil},
		{"regional.audit", o.Audit == nil},
		{"regional.authz", o.Authz == nil},
		{"regional.actor", o.Actor == nil},
		{"regional.settings", o.Settings == nil},
		{"regional.revision", o.Revision == nil},
		{"regional.files", o.Files == nil},
		{"regional.terms", o.Terms == nil},
	} {
		if f.unset {
			v.Addf(f.name, validate.CodeRequired, "must be set")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
