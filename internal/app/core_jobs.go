package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// jobsPart is core part 8: the job runner (jobs.md; app.md "How later core services plug
// in", row 8). It is built after audit, settings and authz, whose seams it takes. signals,
// which is constructed before the parts, reaches it through lazyDeferrer.
var jobsPart = part{
	name:    "jobs",
	declare: declareJobs,
	build:   buildJobs,
	link:    linkJobs,
}

// jobsArea is the route set, audit scope and permission prefix of jobs: /api/jobs.
const jobsArea = "jobs"

// The runner's tuning (jobs.md Q-JOBS7): constants, not configuration keys, in M4.
const (
	jobsPollInterval = jobs.DefaultPollInterval
	jobsLease        = jobs.DefaultLease
	jobsRunTimeout   = jobs.DefaultRunTimeout
	jobsWorkers      = jobs.DefaultWorkers

	// jobsAuditPurgeEvery is the period of the audit retention job.
	jobsAuditPurgeEvery = time.Hour
)

// nameAuditPurge is the job that deletes audit entries older than audit.retention.
const nameAuditPurge jobs.Name = "audit.purge"

// declareJobs adds the jobs migration set and permissions. No I/O.
func declareJobs(a *assembly) error {
	if err := a.addCoreSet(jobs.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, jobs.Permissions...)
	return nil
}

// buildJobs constructs the runner (no I/O: it succeeds against db.None() and on a read-only
// copy), keeps it for provision to scope per module, and mounts the routes. The runnable is
// added in linkJobs, so that it starts last and is the first to stop.
func buildJobs(_ context.Context, a *assembly) error {
	const op = "app.jobs.build"
	if a.deps.audit == nil || a.deps.authz == nil || a.disp == nil {
		return errs.E(op, errs.Internal, errors.New("jobs is built after audit, settings and authz, and the dispatcher"))
	}
	rec, err := a.deps.audit.Scoped(jobsArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	tr, m, err := a.scopes("jobs")
	if err != nil {
		return err
	}
	r, err := jobs.New(jobs.Options{
		DB:           a.dbc,
		Clock:        a.clock,
		Logger:       a.component("jobs"),
		Tracer:       tr,
		Meter:        m,
		Audit:        rec,
		Authz:        a.deps.authz,
		Actor:        authzActor,
		Redeliverer:  a.disp,
		Instance:     a.cfg.Instance.Name,
		PollInterval: jobsPollInterval,
		Lease:        jobsLease,
		RunTimeout:   jobsRunTimeout,
		Workers:      jobsWorkers,
	})
	if err != nil {
		return err //nolint:wrapcheck // jobs.New names itself
	}
	a.jobs = r
	a.deps.jobs = r
	a.routeSets = append(a.routeSets, module.ModuleRoutes{
		Module:  jobsArea,
		Routes:  jobsRoutes(r.Routes()),
		OpenAPI: r.OpenAPI(),
	})
	return nil
}

// jobsRoutes converts jobs' routes (jobs cannot import core/module) to module routes: all
// signed-in, the permission being checked inside each use case.
func jobsRoutes(in []jobs.Route) []module.Route {
	out := make([]module.Route, len(in))
	for i, r := range in {
		out[i] = module.Route{Method: r.Method, Pattern: r.Pattern, Access: module.AccessSignedIn, Handler: r.Handler}
	}
	return out
}

// linkJobs registers audit.purge (the first scheduled job) and adds the runner as a
// runnable. It runs after the modules were built, and the runnable is appended now, after
// every other part's, so it starts last (after Set.Start, so every registration is in) and
// stops first (before signals.Close and the other runnables).
func linkJobs(_ context.Context, a *assembly) error {
	const op = "app.jobs.link"
	if a.jobs == nil || a.settings == nil || a.deps.audit == nil {
		return errs.E(op, errs.Internal, errors.New("jobs is linked after it is built, with audit and settings"))
	}
	reg, err := a.jobs.Scoped("audit")
	if err != nil {
		return fmt.Errorf("%s: scope audit: %w", op, err)
	}
	retention, err := a.settings.Scoped(auditSettings)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	if err := reg.Register(jobs.Decl{
		Name: nameAuditPurge, Every: jobsAuditPurgeEvery,
		Handler: auditPurge{log: a.deps.audit, retention: retention, now: a.clock.Now}.run,
	}); err != nil {
		return fmt.Errorf("%s: register %s: %w", op, nameAuditPurge, err)
	}
	r := &jobsRunner{svc: a.jobs, logger: a.component("jobs")}
	a.runnables = append(a.runnables, runnable{name: "jobs", start: r.start, stop: r.stop})
	return nil
}

// auditPurge is the handler of audit.purge: it deletes audit entries older than
// audit.retention, and does nothing while the retention is 0 (keep forever; D30). The setting
// is read at run time, so a change takes effect at the next run.
type auditPurge struct {
	log       *audit.Log
	retention settings.Reader
	now       func() time.Time
}

func (p auditPurge) run(ctx context.Context, _ jobs.Job) error {
	const op = "app.audit.purge"
	keep, err := p.retention.Duration(ctx, "retention")
	if err != nil {
		return fmt.Errorf("%s: audit.retention: %w", op, err)
	}
	if keep <= 0 {
		return nil
	}
	if _, err := p.log.Purge(ctx, p.now().Add(-keep)); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// lazyDeferrer is signals' Deferrer over a.jobs, which the jobs part builds after signals.New
// has run: until then Defer fails errs.Unavailable (the dispatcher logs it beside the
// handler's error and the delivery is final), so no setter is needed. It does no I/O of its
// own.
type lazyDeferrer struct{ a *assembly }

var _ signals.Deferrer = lazyDeferrer{}

// Defer stores the delivery through the runner, when there is one.
func (l lazyDeferrer) Defer(ctx context.Context, d signals.Delivery) error {
	if l.a.jobs == nil {
		return errs.E("app.lazyDeferrer.Defer", errs.Unavailable, errors.New("the job runner is not built yet"))
	}
	return l.a.jobs.Defer(ctx, d) //nolint:wrapcheck // the runner's own error
}

// loopService is jobs.Runner's Run and Close, the two calls the runnable makes.
type loopService interface {
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}

var _ loopService = (*jobs.Runner)(nil)

// jobsRunner runs jobs.Runner.Run in a goroutine of its own: start launches it, stop closes
// the runner (which cancels the runs in flight and gives their rows back) and waits.
type jobsRunner struct {
	svc    loopService
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// start launches Run. The goroutine exits when stop ends it; a failure (Run fails only when
// started twice) is logged there, once.
func (r *jobsRunner) start(ctx context.Context) error {
	// Run outlives start's own deadline: it stops on stop.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		if err := r.svc.Run(runCtx); err != nil {
			r.logger.ErrorContext(runCtx, "the job runner stopped",
				slog.String("error.kind", errs.KindOf(err).String()))
		}
	}()
	return nil
}

// stop ends Run and waits for it. It fails with ctx's kind when ctx ends first; the leases of
// rows still running then lapse and another process takes them.
func (r *jobsRunner) stop(ctx context.Context) error {
	const op = "app.jobs.runner.stop"
	if r.cancel == nil {
		return nil
	}
	if err := r.svc.Close(ctx); err != nil {
		r.cancel()
		return fmt.Errorf("%s: %w", op, err)
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), fmt.Errorf("the job runner did not stop: %w", ctx.Err()))
	}
}
