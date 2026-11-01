package regional

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// logoCache is the logo's file ID ("" when none) as read at one settings revision. Every logo
// change bumps the revision, so the ID is current for as long as the revision is.
type logoCache struct {
	revision settings.Revision
	id       string
}

// Service is the regional's profile. Safe for concurrent use.
type Service struct {
	db       db.DB
	clock    clock.Clock
	logger   *slog.Logger
	tracer   telemetry.Tracer
	audit    audit.Recorder
	authz    authz.Authorizer
	actorF   authz.ActorFunc
	settings settings.Reader
	revision RevisionSource
	files    files.Store
	terms    TermSource

	logo   atomic.Pointer[logoCache] // the logo's file ID at one settings revision
	warned sync.Map                  // full key -> struct{}: refused stored values already warned about
}

// New validates o and builds the service. It does no I/O, so it succeeds against db.None()
// and on a read-only copy. Fails errs.Invalid, one field error per nil dependency (a nil
// Files or Settings among them).
func New(o Options) (*Service, error) {
	if err := o.Validate(); err != nil {
		return nil, errs.E("regional.New", errs.Invalid, err)
	}
	return &Service{
		db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		authz: o.Authz, actorF: o.Actor, settings: o.Settings, revision: o.Revision,
		files: o.Files, terms: o.Terms,
	}, nil
}

// start opens the span of one use case, "regional.<name>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "regional."+name)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// wrap adds op to err and keeps its kind and its chain. nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// require fails errs.Unauthenticated without a signed-in person and errs.Permission unless
// the actor holds the site-wide permission p.
func (s *Service) require(ctx context.Context, op string, p authz.Permission) error {
	a, ok := s.actorF(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	if err := s.authz.Allow(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
