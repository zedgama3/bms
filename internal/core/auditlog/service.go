package auditlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/auditlog/postgres"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// PurgeJob is the retention job's name; PurgeEvery its period.
const (
	PurgeJob   jobs.Name = "audit.purge"
	PurgeEvery           = time.Hour
)

// The actions auditlog records (the audit area's own) and its span names.
const (
	actionSearch = audit.Action("audit.searched")
	actionExport = audit.Action("audit.exported")
	actionPurged = audit.Action("audit.purged")
	spanSearch   = "audit.search"
	spanExport   = "audit.export"
	spanPurge    = "audit.purge"
)

// Service reads and expires the audit log. Safe for concurrent use.
type Service struct {
	clock    clock.Clock
	logger   *slog.Logger
	tracer   telemetry.Tracer
	db       db.DB
	store    *postgres.Store
	authz    authz.Authorizer
	actor    authz.ActorFunc
	settings settings.Reader
	jobs     jobs.Registrar
	rec      audit.Recorder

	encode func(audit.Record) ([]byte, error) // the export line encoder; a seam for fault injection in tests
}

// New validates o and returns the service. It does no I/O, so it succeeds against db.None()
// and on a read-only copy. Fails errs.Invalid, one field error per nil dependency.
func New(o Options) (*Service, error) {
	const op = "auditlog.New"
	if err := o.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	store, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &Service{
		clock: o.Clock, logger: o.Logger, tracer: o.Tracer, db: o.DB, store: store,
		authz: o.Authz, actor: o.Actor, settings: o.Settings, jobs: o.Jobs, rec: o.Audit,
		encode: encodeRecord,
	}, nil
}

// Start registers audit.purge with Jobs, to run every hour. It never writes, so it succeeds
// on a read-only copy. Fails with the Registrar's kinds (errs.Conflict when the name is
// registered twice).
func (s *Service) Start(_ context.Context) error {
	const op = "auditlog.Start"
	if err := s.jobs.Register(jobs.Decl{Name: PurgeJob, Every: PurgeEvery, Handler: s.purgeJob}); err != nil {
		return fmt.Errorf("%s: register %s: %w", op, PurgeJob, err)
	}
	return nil
}

// purgeJob is the handler of audit.purge: it deletes audit entries older than
// audit.retention, and does nothing while the retention is 0 (keep forever; D30). The
// setting is read at run time, so a change takes effect at the next run. On a read-only copy
// it fails errs.Unavailable wrapping db.ErrReadOnly, and jobs retries later.
func (s *Service) purgeJob(ctx context.Context, _ jobs.Job) error {
	const op = "auditlog.purgeJob"
	keep, err := s.settings.Duration(ctx, "retention")
	if err != nil {
		return fmt.Errorf("%s: audit.retention: %w", op, err)
	}
	if keep <= 0 {
		return nil
	}
	if _, err := s.purge(ctx, s.clock.Now().Add(-keep)); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// authorize decides that the actor in ctx is signed in and holds p.
func (s *Service) authorize(ctx context.Context, op string, p authz.Permission) error {
	if _, ok := s.actor(ctx); !ok {
		return errs.E(op, errs.Unauthenticated, errors.New("sign in to use the audit log"))
	}
	if err := s.authz.Allow(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// fieldErr builds an errs.Invalid error carrying one validate.FieldError.
func fieldErr(op, field, code, format string, args ...any) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, format, args...)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
