package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/disk"
	"github.com/zedgama3/bms/internal/core/objects/s3"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// filesPart is core part 10: the attachment service and the object store under it (files.md;
// app.md "How later core services plug in", row 10). It is built after jobs and notify.
var filesPart = part{
	name:    "files",
	declare: declareFiles,
	build:   buildFiles,
	link:    linkFiles,
}

// filesArea is the audit scope, settings area and job owner of files.
const filesArea = "files"

// filesSettings is the settings area of the files.* keys.
const filesSettings = filesArea

// verifyTimeout bounds `bms files verify`, which heads every object.
const verifyTimeout = 30 * time.Minute

// declareFiles adds the files migration set and the `files` command. No I/O.
func declareFiles(a *assembly) error {
	if err := a.addCoreSet(files.Set); err != nil {
		return err
	}
	a.commands = append(a.commands, command{
		name:    "files",
		summary: "check the stored files against their records (files verify)",
		usage:   "verify",
		run:     runFiles,
	})
	return nil
}

// filesDir is the absolute files directory of cfg.
func filesDir(cfg config.Config) (string, error) {
	dir, err := filepath.Abs(cfg.Files.Dir)
	if err != nil {
		return "", fieldInvalid("app.files", config.Prefix+"FILES_DIR", "cannot resolve the files directory", err)
	}
	return dir, nil
}

// newObjectStore builds the object store cfg.Files names (no I/O): the disk store under
// <files.dir>/store, or the S3-compatible store.
func newObjectStore(cfg config.Config) (objects.Store, error) {
	switch cfg.Files.Store {
	case config.FilesS3:
		prefix := cfg.Files.S3Prefix
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/" // the store's prefix is a directory-like path
		}
		st, err := s3.New(s3.Config{
			Endpoint:  cfg.Files.S3Endpoint,
			Bucket:    cfg.Files.S3Bucket,
			Region:    cfg.Files.S3Region,
			Prefix:    prefix,
			PathStyle: cfg.Files.S3PathStyle,
			AccessKey: cfg.Files.S3AccessKey,
			SecretKey: cfg.Files.S3SecretKey,
		})
		if err != nil {
			return nil, err //nolint:wrapcheck // s3.New names itself
		}
		return st, nil
	default:
		dir, err := filesDir(cfg)
		if err != nil {
			return nil, err
		}
		st, err := disk.New(filepath.Join(dir, "store"))
		if err != nil {
			return nil, err //nolint:wrapcheck // disk.New names itself
		}
		return st, nil
	}
}

// buildFiles constructs the service (no I/O: it succeeds against db.None() and on a
// read-only copy) over the configured object store and the files.* settings, and keeps it
// for provision to scope per module.
func buildFiles(_ context.Context, a *assembly) error {
	const op = "app.files.build"
	if a.deps.audit == nil || a.settings == nil {
		return errs.E(op, errs.Internal, errors.New("files is built after audit and settings"))
	}
	rec, err := a.deps.audit.Scoped(filesArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	rd, err := a.settings.Scoped(filesSettings)
	if err != nil {
		return fmt.Errorf("%s: settings scope: %w", op, err)
	}
	tr, m, err := a.scopes("files")
	if err != nil {
		return err
	}
	store, err := newObjectStore(a.cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	dir, err := filesDir(a.cfg)
	if err != nil {
		return err
	}
	a.filesReader = rd
	opts := files.Options{
		DB:       a.dbc,
		Clock:    a.clock,
		Logger:   a.component("files"),
		Tracer:   tr,
		Meter:    m,
		Audit:    rec,
		Actor:    identity.Actor,
		Objects:  store,
		TempDir:  filepath.Join(dir, "tmp"),
		Settings: rd,
	}
	if a.cfg.Demo.Enabled {
		// Transitional: the same settings, capped by the demo service (core_demo.go);
		// Options.Ceiling replaces it in step 10. Without demo mode files maps its settings itself.
		opts.Policy = lazyFilesPolicy{a: a}
	}
	svc, err := files.New(opts)
	if err != nil {
		return err //nolint:wrapcheck // files.New names itself
	}
	a.files = svc
	a.deps.files = svc
	return nil
}

// linkFiles registers the two jobs through the jobs runner and the start-up probe as a
// runnable. It runs after the modules were built; the runnable goes in before the jobs
// runner's, which must start last (after every registration is in).
func linkFiles(_ context.Context, a *assembly) error {
	const op = "app.files.link"
	if a.files == nil || a.jobs == nil {
		return errs.E(op, errs.Internal, errors.New("files is linked after it and the jobs runner are built"))
	}
	reg, err := a.jobs.Scoped(filesArea)
	if err != nil {
		return fmt.Errorf("%s: scope jobs: %w", op, err)
	}
	svc := a.files
	reap := func(ctx context.Context, _ jobs.Job) error {
		_, err := svc.Reap(ctx)
		return err //nolint:wrapcheck // files names itself
	}
	sweep := func(ctx context.Context, _ jobs.Job) error {
		_, err := svc.SweepOrphans(ctx)
		return err //nolint:wrapcheck // files names itself
	}
	for _, d := range []jobs.Decl{
		{Name: files.JobReap, Every: files.ReapEvery, Timeout: files.JobTimeout, Handler: reap},
		{Name: files.JobSweepOrphans, Every: files.SweepEvery, Timeout: files.JobTimeout, Handler: sweep},
	} {
		if err := reg.Register(d); err != nil {
			return fmt.Errorf("%s: register %s: %w", op, d.Name, err)
		}
	}
	r := runnable{name: "files", start: startFiles(svc)}
	at := slices.IndexFunc(a.runnables, func(x runnable) bool { return x.name == "jobs" })
	if at < 0 {
		at = len(a.runnables)
	}
	a.runnables = slices.Insert(a.runnables, at, r)
	return nil
}

// startFiles is the runnable that probes the store once. Only a bucket that answers
// anonymous reads fails the start (exit 1): the process must not add files to it.
func startFiles(svc *files.Service) func(context.Context) error {
	return func(ctx context.Context) error {
		err := svc.Start(ctx)
		if errors.Is(err, objects.ErrPublic) {
			return errs.E("app.files.start", errs.Unavailable, fmt.Errorf("the S3 bucket answers anonymous reads; fix its policy: %w", err))
		}
		return err //nolint:wrapcheck // Start names itself
	}
}

// filesSource is the "files" source of people's export and deletion, linked after the
// modules.
func filesSource(svc *files.Service) people.Source {
	return people.Source{Name: filesArea, Exporter: filesData{svc: svc}}
}

type filesData struct{ svc *files.Service }

// ExportPerson is files' export as people's Export.
func (d filesData) ExportPerson(ctx context.Context, person model.PersonID) (people.Export, error) {
	raw, err := d.svc.ExportPerson(ctx, person)
	if err != nil {
		return people.Export{}, err //nolint:wrapcheck // people names the source
	}
	return people.Export{Data: raw}, nil
}

// DeletePerson is files' backstop deletion as people's ModuleDeletion.
func (d filesData) DeletePerson(ctx context.Context, person model.PersonID) (people.ModuleDeletion, error) {
	del, err := d.svc.DeletePerson(ctx, person)
	if err != nil {
		return people.ModuleDeletion{}, err //nolint:wrapcheck // people names the source
	}
	return people.ModuleDeletion{Deleted: del.Deleted, Anonymized: del.Anonymized}, nil
}

// filesDirsWarning is the one line migrate prints when it cannot create a files directory.
const filesDirsWarning = "warning: cannot create the files directory %s (%v), so uploads fail until it exists and is writable."

// ensureFilesDirs creates <files.dir>, <files.dir>/tmp and, for the disk store,
// <files.dir>/store, mode 0700 (bms migrate). It creates nothing when the files directory
// would sit under a data directory that does not exist (a session key injected and no
// secrets file to write, config.md item 19: migrate then neither creates the data directory
// nor complains), and a directory it cannot create is one warning, not a failure: serve and
// `bms check` report the store through its probe.
func ensureFilesDirs(p *process, cfg config.Config) {
	dir, err := filesDir(cfg)
	if err != nil {
		say(p.stderr, filesDirsWarning+"\n", cfg.Files.Dir, err)
		return
	}
	if data, derr := filepath.Abs(cfg.Data.Dir); derr == nil {
		if rel, rerr := filepath.Rel(data, dir); rerr == nil && !strings.HasPrefix(rel, "..") {
			if st, serr := os.Stat(data); serr != nil || !st.IsDir() {
				return
			}
		}
	}
	dirs := []string{dir, filepath.Join(dir, "tmp")}
	if cfg.Files.Store != config.FilesS3 {
		dirs = append(dirs, filepath.Join(dir, "store"))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, dataDirMode); err != nil {
			say(p.stderr, filesDirsWarning+"\n", d, err)
			return
		}
	}
}

// runFiles is `bms files verify`: it heads every stored file's object and prints the counts.
// Exit 0 when nothing is missing or differs, 1 when something is (or the store cannot be
// reached), 2 for a usage or configuration problem, 3 for a schema behind.
func runFiles(ctx context.Context, p *process) int {
	if len(p.args) != 2 || p.args[1] != "verify" {
		sayln(p.stderr, "usage: bms files verify")
		return ExitUsage
	}
	return runFilesCommand(ctx, p, func(ctx context.Context, svc *files.Service, out io.Writer) (bool, error) {
		res, err := svc.Verify(ctx)
		if err != nil {
			return false, err //nolint:wrapcheck // printed by the caller
		}
		say(out, "checked: %d\nmissing: %d\nmismatched: %d\ntombstones: %d\n", res.Checked, res.Missing, res.Mismatched, res.Tombstones)
		return res.Missing == 0 && res.Mismatched == 0, nil
	})
}

// runFilesCommand builds the core as `bms export` does (not started), and runs fn as the
// operator. It loads configuration like serve and requires the session key.
func runFilesCommand(ctx context.Context, p *process, fn func(context.Context, *files.Service, io.Writer) (bool, error)) int {
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	p.warnDebugToStderr(cfg)
	if err := requireSessionKey(cfg); err != nil {
		p.problem(err)
		return ExitUsage
	}
	a := newAssembly(p, cfg)
	ctx = audit.WithOperator(ctx) // the command acts with the operator's hands
	if err := a.startTelemetry(ctx, p.lookup, p.stderr); err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	bail := func(err error, status int) int {
		code := a.fail(ctx, p, err, status)
		if cerr := a.closeQuiet(ctx); cerr != nil {
			p.problem(cerr)
		}
		return code
	}
	enabled, err := enabledModules(p.registry, cfg.Modules.Disabled)
	if err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.resolve(ctx, p.registry, enabled); err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.openDatabase(ctx); err != nil {
		return bail(err, statusOf(err, ExitFailure))
	}
	if err := a.newDispatcher(); err != nil {
		return bail(err, ExitFailure)
	}
	if err := a.buildModules(ctx, a.migrator); err != nil {
		return bail(err, statusOf(err, ExitSchema))
	}
	if a.files == nil {
		return bail(errs.E("app.files", errs.Internal, errors.New("the files part built no service")), ExitFailure)
	}
	cctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	ok, err := fn(cctx, a.files, p.stdout)
	cancel()
	if err != nil {
		return bail(err, ExitFailure)
	}
	if err := a.closeQuiet(ctx); err != nil {
		p.problem(err)
		return ExitFailure
	}
	if !ok {
		return ExitFailure
	}
	return ExitOK
}

// files prints the state of the file store (files.md "Start and Ping"): `files: disk ok`,
// `files: s3 <endpoint> ok`, or the error. Only a public bucket is a problem, as only that
// would stop serve.
func (r *checkReport) files(ctx context.Context, a *assembly) {
	if a.files == nil {
		r.printf("files: skipped (the modules did not resolve)\n")
		return
	}
	label := string(a.cfg.Files.Store)
	if a.cfg.Files.Store == config.FilesS3 {
		label += " " + a.cfg.Files.S3Endpoint
	}
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	err := a.files.Ping(cctx)
	switch {
	case err == nil:
		r.printf("files: %s ok\n", label)
	case errors.Is(err, objects.ErrPublic):
		r.printf("files: %s %s\n", label, indent("the S3 bucket answers anonymous reads; fix its policy"))
		r.record(ExitFailure)
	default:
		r.printf("files: %s %s\n", label, indent(problemText(err)))
	}
}
