package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// auditPart is core part 1 (app.md "How later core services plug in"): the audit log. It
// is built before identity because identity's build takes the audit recorder; audit needs
// nothing from identity at build time, only a closure over identity's ctx accessor that is
// called per request.
var auditPart = part{
	name:    "audit",
	declare: declareAudit,
	build:   buildAudit,
}

// declareAudit adds the audit migration set and permissions. No I/O.
func declareAudit(a *assembly) error {
	if err := a.addCoreSet(audit.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, audit.Permissions...)
	return nil
}

// buildAudit constructs the log (no I/O: it succeeds against db.None() and on a read-only
// copy), keeps it unscoped in a.deps for provision to scope per module, adds the "audit"
// route set, and registers the importer as a runnable. The Close closer is a.closers().
func buildAudit(_ context.Context, a *assembly) error {
	tr, m, err := a.scopes("audit")
	if err != nil {
		return err
	}
	log, err := audit.New(audit.Options{
		DB:          a.dbc,
		Clock:       a.clock,
		Logger:      a.component("audit"),
		Tracer:      tr,
		Meter:       m,
		Instance:    a.cfg.Instance.Name,
		DataDir:     a.cfg.Data.Dir,
		Actor:       a.auditActor,
		Titles:      lazyTitles{a: a},
		Permissions: lazyPermissions{a: a},
	})
	if err != nil {
		return err //nolint:wrapcheck // audit.New names itself
	}
	a.deps.audit = log
	a.routeSets = append(a.routeSets, module.ModuleRoutes{
		Module:  "audit",
		Routes:  auditRoutes(log.Routes()),
		OpenAPI: log.OpenAPI(),
	})
	im := &importer{
		log:    log,
		logger: a.component("audit"),
		dir:    filepath.Join(a.cfg.Data.Dir, filepath.FromSlash(audit.SpoolDirName)),
	}
	a.runnables = append(a.runnables, runnable{name: "audit", start: im.start, stop: im.stop})
	return nil
}

// auditActor is audit's ActorFunc: the session's person, or while a command that acts as the
// operator runs (a.operator: create-superuser, export, delete, migrate) and there is no
// session, the operator. Audit asks it only outside a signal delivery and only for an entry
// that names no actor. It does no I/O.
func (a *assembly) auditActor(ctx context.Context) (audit.Actor, bool) {
	if act, ok := actorFromSession(ctx); ok {
		return act, true
	}
	if a.operator {
		return audit.Actor{Kind: audit.ActorOperator}, true
	}
	return audit.Actor{}, false
}

// actorFromSession is audit's ActorFunc over identity's ctx accessor. A restricted session
// is still the person for audit. It does no I/O and is called per request only.
func actorFromSession(ctx context.Context) (audit.Actor, bool) {
	s, ok := identity.SessionFrom(ctx)
	if !ok {
		return audit.Actor{}, false
	}
	return audit.Actor{Kind: audit.ActorPerson, Person: s.Person, Superuser: s.Superuser}, true
}

// auditRoutes converts audit's routes (audit cannot import core/module) to module routes:
// all signed-in (audit decision 14).
func auditRoutes(in []audit.Route) []module.Route {
	out := make([]module.Route, len(in))
	for i, r := range in {
		out[i] = module.Route{Method: r.Method, Pattern: r.Pattern, Access: module.AccessSignedIn, Handler: r.Handler}
	}
	return out
}

// importer runs audit.Log.Run in a goroutine of its own: start launches it, stop cancels it
// and waits for it, within the ctx it is given.
type importer struct {
	log    *audit.Log
	logger *slog.Logger
	dir    string

	cancel context.CancelFunc
	done   chan struct{}
}

// start creates the spool directory and launches Run. The goroutine exits when stop (or
// the log's Close) ends it; its failure, if any, is logged there, once.
func (im *importer) start(ctx context.Context) error {
	const op = "app.audit.importer.start"
	if err := os.MkdirAll(im.dir, dataDirMode); err != nil {
		return fieldInvalid(op, config.Prefix+"DATA_DIR", "cannot create the audit spool directory", err)
	}
	// Run outlives start's own deadline: it stops on stop or on Close.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	im.cancel = cancel
	im.done = make(chan struct{})
	go func() {
		defer close(im.done)
		if err := im.log.Run(runCtx); err != nil {
			im.logger.ErrorContext(runCtx, "the audit importer stopped",
				slog.String("error.kind", errs.KindOf(err).String()), slog.Any("error", err))
		}
	}()
	return nil
}

// stop ends Run and waits for it. It fails errs.Timeout or errs.Canceled when ctx ends
// first; the goroutine then exits on its own when Run notices (the log's Close, which
// follows, also ends it).
func (im *importer) stop(ctx context.Context) error {
	const op = "app.audit.importer.stop"
	if im.cancel == nil {
		return nil
	}
	im.cancel()
	select {
	case <-im.done:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), fmt.Errorf("the audit importer did not stop: %w", ctx.Err()))
	}
}
