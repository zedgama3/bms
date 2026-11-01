package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// jobsPart is the job runner (jobs.md). It is built after audit, authz and settings, and
// before auditlog, which registers audit.purge with it. The dispatcher's Deferrer is
// jobs.NewDeferrer, built before the parts and independent of the runner.
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
)

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
		Actor:        identity.Actor,
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
	if err := a.addCoreRoutes(jobsArea, r.Routes(), r.OpenAPI()); err != nil {
		return err
	}
	return nil
}

// linkJobs adds the runner as a runnable. It runs after the modules were built, and the
// runnable is appended now, after every other part's, so it starts last (after Set.Start, so
// every registration is in) and stops first (before signals.Close and the other runnables).
func linkJobs(_ context.Context, a *assembly) error {
	const op = "app.jobs.link"
	if a.jobs == nil {
		return errs.E(op, errs.Internal, errors.New("jobs is linked after it is built"))
	}
	r := &jobsRunner{svc: a.jobs, logger: a.component("jobs")}
	a.runnables = append(a.runnables, runnable{name: "jobs", start: r.start, stop: r.stop})
	return nil
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
