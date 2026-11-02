package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// auditPart is core part 1 (app.md "The build order"): the audit log. It
// is built before identity because identity's build takes the audit recorder; audit needs
// nothing from identity at build time, only a closure over identity's ctx accessor that is
// called per request.
var auditPart = part{
	name:    "audit",
	declare: declareAudit,
	build:   buildAudit,
}

// declareAudit adds the audit migration set. No I/O.
func declareAudit(a *assembly) error {
	if err := a.addCoreSet(audit.Set); err != nil {
		return err
	}
	return nil
}

// buildAudit constructs the log (no I/O: it succeeds against db.None() and on a read-only
// copy), keeps it unscoped in a.deps for provision to scope per module, and registers the
// importer as a runnable. The Close closer is a.closers().
func buildAudit(_ context.Context, a *assembly) error {
	if a.authzLookup == nil {
		return errs.E("app.audit.build", errs.Internal, errors.New("audit is built after the authz lookup"))
	}
	tr, m, err := a.scopes("audit")
	if err != nil {
		return err
	}
	log, err := audit.New(audit.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component("audit"),
		Tracer:   tr,
		Meter:    m,
		Instance: a.cfg.Instance.Name,
		DataDir:  a.cfg.Data.Dir,
		Actor:    identity.AuditActor,
		Titles:   a.authzLookup,
	})
	if err != nil {
		return err //nolint:wrapcheck // audit.New names itself
	}
	a.deps.audit = log
	rn, err := newRunner("audit importer", log.Run, nil, a.component("audit"))
	if err != nil {
		return err
	}
	a.runnables = append(a.runnables, auditRunnable(rn, filepath.Join(a.cfg.Data.Dir, filepath.FromSlash(audit.SpoolDirName))))
	return nil
}

// auditRunnable is the importer's runner with one step before Run: it creates the spool
// directory, and an unusable data directory fails the start with errs.Invalid under
// BMS_DATA_DIR (audit F33).
func auditRunnable(rn *runner, dir string) runnable {
	r := rn.runnable("audit")
	launch := r.start
	r.start = func(ctx context.Context) error {
		const op = "app.audit.importer.start"
		if err := os.MkdirAll(dir, dataDirMode); err != nil {
			return fieldInvalid(op, config.Prefix+"DATA_DIR", "cannot create the audit spool directory", err)
		}
		return launch(ctx)
	}
	return r
}
