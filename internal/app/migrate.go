package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/zedgama3/bms/internal/app/secrets"
	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// dataDirMode is the mode of the data directory.
const dataDirMode os.FileMode = 0o700

// secretsNotice is config.md decision 7's notice, printed once when a secrets file is
// created or gains a field. The path is the operator's own.
const secretsNotice = "Created %s. It holds this installation's session key and the key that seals settings' " +
	"secret values. Back it up separately from the database and copy it to every copy of this " +
	"installation; without it, every session is invalid and anything sealed with it is " +
	"unreadable. Keep it readable by the bms user only."

// secretsAddedNotice is the notice when migrate adds a field to a secrets file that already
// exists (config.md decision 7, "Further generated secrets"): the field is named by what it
// does, and the keys already in the file are not touched.
const secretsAddedNotice = "Added %s to %s. Back it up with the rest of the file, separately from the database " +
	"and copied to every copy of this installation; without it, anything it seals is unreadable."

// sealingKeyWarning is the one line migrate prints when it cannot write the sealing key
// (the session key being injected, so nothing else needed the file). The path is the
// operator's own.
const sealingKeyWarning = "warning: cannot write the settings sealing key to %s, so secret settings (such as an " +
	"SMTP password) cannot be stored until BMS_SETTINGS_SEALING_KEY or a writable secrets file provides one."

// secretsFieldLabel says what a generated field is for, in the notices.
var secretsFieldLabel = map[string]string{
	config.SecretsFieldSessionKey:         "the session key",
	config.SecretsFieldSettingsSealingKey: "the key that seals settings' secret values",
}

// fieldInvalid builds an errs.Invalid under the variable env, for problems the operator
// fixes by changing that variable.
func fieldInvalid(op, env, reason string, cause error) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: env, Code: "unwritable", Reason: reason + ": " + cause.Error()})
}

// ensureSecrets, when a generated key (the session key, the settings sealing key) is not
// configured, creates the data directory and the secrets file or adds the missing fields to
// it, then reloads the configuration so the rest of the command sees what any other command
// would (decision 14; F22, F23). A key an operator injects is never written. When only the
// sealing key is missing (the session key is injected) and the file cannot be written, that
// is one warning, not an error: an unset sealing key is never an error (config decision 17;
// item 19), and the key simply stays unset.
func ensureSecrets(ctx context.Context, p *process, cfg config.Config) (config.Config, error) {
	const op = "app.migrate"
	var missing []string
	if !cfg.Session.Key.IsSet() {
		missing = append(missing, config.SecretsFieldSessionKey)
	}
	if !cfg.Settings.SealingKey.IsSet() {
		missing = append(missing, config.SecretsFieldSettingsSealingKey)
	}
	if len(missing) == 0 {
		// Nothing to write, so the data directory may be read-only or absent (item 19).
		return cfg, nil
	}
	// With the session key injected only the sealing key is wanted, and failing to write it
	// is a warning, not an error.
	unwritable := func(err error) (config.Config, error) {
		if cfg.Session.Key.IsSet() && errs.Is(err, errs.Invalid) {
			say(p.stderr, sealingKeyWarning+"\n", cfg.Secrets.File)
			return cfg, nil
		}
		return cfg, err
	}
	if err := os.MkdirAll(cfg.Data.Dir, dataDirMode); err != nil {
		return unwritable(fieldInvalid(op, config.Prefix+"DATA_DIR", "cannot create the data directory", err))
	}
	_, statErr := os.Stat(cfg.Secrets.File)
	existed := statErr == nil
	created, err := secrets.Ensure(ctx, cfg.Secrets.File, missing)
	if err != nil {
		if errs.Is(err, errs.Invalid) {
			err = fieldInvalid(op, config.Prefix+"SECRETS_FILE", "cannot create or update the secrets file", err)
		}
		return unwritable(err)
	}
	if len(created) == 0 {
		return cfg, nil
	}
	sayln(p.stderr)
	if existed {
		for _, f := range created {
			say(p.stderr, secretsAddedNotice+"\n", secretsFieldLabel[f], cfg.Secrets.File)
		}
	} else {
		say(p.stderr, secretsNotice+"\n", cfg.Secrets.File)
	}
	sayln(p.stderr)
	reloaded, err := config.Load(ctx, p.environ)
	if err != nil {
		return cfg, err //nolint:wrapcheck // config.Load names itself
	}
	return reloaded, nil
}

// runMigrate creates the secrets file on a new installation and applies the core's and
// the enabled modules' migrations in order ("migrate, step by step").
func runMigrate(ctx context.Context, p *process) int {
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	p.warnDebugToStderr(cfg)
	ectx, cancel := context.WithTimeout(ctx, checkTimeout)
	cfg, err = ensureSecrets(ectx, p, cfg)
	cancel()
	if err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	a := newAssembly(p, cfg)
	a.operator = true
	if err := a.startTelemetry(ctx, p.lookup, p.stderr); err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	a.logger.DebugContext(ctx, "bms migrating", slog.String("version", a.version), slog.Any("config", cfg))

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

	mctx, cancel := context.WithTimeout(ctx, migrateTimeout)
	applied, err := a.migrator.Migrate(mctx, a.setsFor(a.plan.Order()))
	cancel()
	printApplied(p, applied)
	if err != nil {
		if errors.Is(err, db.ErrReadOnly) {
			err = fmt.Errorf("this copy of the database is read-only: run `bms migrate` on the copy that writes: %w", err)
		}
		return bail(err, migrateStatus(err))
	}
	// The parts' seed steps come before the summary line, which stays last.
	if err := a.seed(ctx); err != nil {
		return bail(err, statusOf(err, ExitFailure))
	}
	if len(applied) == 0 {
		sayln(p.stdout, "nothing to apply")
	} else {
		say(p.stdout, "applied %d migration(s)\n", len(applied))
	}
	if err := a.closeQuiet(ctx); err != nil {
		p.problem(err)
		return ExitFailure
	}
	return ExitOK
}

// seed runs the parts' seed steps (authz's starter permissions) after the migrations. The
// parts are built as serve builds them, over the pool: building reads nothing and starts
// nothing. It fails with the kind of whatever refused, on a read-only copy errs.Unavailable.
func (a *assembly) seed(ctx context.Context) error {
	if !a.hasSeed() {
		return nil
	}
	if err := a.newDispatcher(); err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := a.buildParts(sctx); err != nil {
		return err
	}
	return a.seedParts(sctx)
}

// migrateStatus is the exit status of a failed Migrate: a Conflict of the migrator (another
// run, a changed applied file, a database newer than the binary) is ExitSchema; a failing
// migration file, whatever the kind of the driver's error (a constraint violation is Invalid
// or Conflict), and everything else is ExitFailure (app.md F20, F21). The operator's input was checked before Migrate ran.
func migrateStatus(err error) int {
	var dbErr *db.Error
	if errors.As(err, &dbErr) {
		return ExitFailure // a statement of a migration file failed, whatever its kind
	}
	if errs.Is(err, errs.Conflict) {
		return ExitSchema
	}
	return ExitFailure
}

// printApplied writes one line per applied migration file.
func printApplied(p *process, applied []db.Applied) {
	for _, ap := range applied {
		say(p.stdout, "applied %s %04d_%s (%s)\n", ap.Set, ap.Version, ap.Name, ap.Duration)
	}
}
