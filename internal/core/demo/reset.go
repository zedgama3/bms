package demo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// ErrMigrate marks the error of a reset whose migrate step failed, after the schemas were
// dropped. app gives only that failure the schema exit status (3) when the migrator's cause
// is a Conflict. Check with errors.Is.
var ErrMigrate = errors.New("the schema could not be rebuilt")

// recoverHint is what the operator reads when the schema is gone and not rebuilt.
const recoverHint = "the demo schemas were dropped and not rebuilt: run `bms migrate`, then `bms demo seed`"

// Reset returns a demo database to the seeded state (demo.md "The reset"): it clears the
// objects and the audit spool, drops every schema in Options.Schemas with CASCADE, runs
// Schema.Migrate as `bms migrate` does and then Seed. It prints one line per step and
// records demo.reset.ran as the operator. The superuser goes with the rest.
//
// Fails errs.Unsupported when demo is not enabled, errs.Unavailable wrapping db.ErrReadOnly
// on a read-only copy, errs.Conflict (field demo, code not_demo) when the database holds no
// marker (checked before anything is touched), errs.Unavailable when the objects or the
// spool cannot be cleared (the database is intact: rerun), the store's kind when the drop is
// refused, the migrator's kind when the migration fails (the error says to run `bms
// migrate` and then `bms demo seed`) and Seed's kinds.
func (s *Service) Reset(ctx context.Context, out io.Writer) (rep Report, err error) {
	const op = "demo.Service.Reset"
	if !s.o.Enabled {
		return Report{}, errs.E(op, errs.Unsupported, errors.New("demo mode is off; set BMS_DEMO_ENABLED=true"))
	}
	if s.o.DB.ReadOnly() {
		return Report{}, errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	ctx, span := s.o.Tracer.Start(ctx, "demo.reset")
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
		add(s.metrics.resets, ctx, outcomeAttr(err))
	}()
	if _, err := s.store.State(ctx); err != nil {
		if errs.Is(err, errs.NotFound) {
			return Report{}, errs.E(op, errs.Conflict, &validate.FieldError{
				Field: ConflictField, Code: CodeNotDemo, Reason: "this database was not seeded by `bms demo seed`, so it is not a demo and reset refuses it",
			})
		}
		return Report{}, fmt.Errorf("%s: marker: %w", op, err)
	}

	var steps []Step
	do := func(name string, fn func(context.Context) (int, error)) error {
		if err := checkCtx(op, ctx); err != nil {
			return err
		}
		n, err := fn(ctx)
		if err != nil {
			return fmt.Errorf("demo reset step %s: %w", name, err)
		}
		steps = append(steps, Step{Name: name, Items: n})
		s.o.Logger.InfoContext(ctx, "demo reset step", slog.String("step", name), slog.Int("items", n))
		say(out, "%-18s %d\n", name, n)
		return nil
	}
	for _, st := range []struct {
		name string
		fn   func(context.Context) (int, error)
	}{
		{"objects", s.o.Objects.Purge},
		{"spool", s.clearSpool},
		{"drop", s.drop},
		{"migrate", s.migrate},
	} {
		if err := do(st.name, st.fn); err != nil {
			return Report{Steps: steps}, err
		}
	}
	seeded, err := s.Seed(ctx, out)
	steps = append(steps, seeded.Steps...)
	rep = Report{Steps: steps}
	if err != nil {
		return rep, fmt.Errorf("%s: seed: %w", op, err)
	}
	if err := s.recordRan(ctx, actionResetRan, rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// drop drops every schema in one statement under dropTimeout.
func (s *Service) drop(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dropTimeout)
	defer cancel()
	if err := s.store.DropSchemas(ctx, s.o.Schemas); err != nil {
		return 0, err //nolint:wrapcheck // the step wraps it
	}
	return len(s.o.Schemas), nil
}

// migrate rebuilds the schema as `bms migrate` does. A failure says how to recover.
func (s *Service) migrate(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dropTimeout)
	defer cancel()
	if err := s.o.Schema.Migrate(ctx); err != nil {
		return 0, fmt.Errorf("%s: %w: %w", recoverHint, ErrMigrate, err)
	}
	return 0, nil
}

// clearSpool deletes every file under <data.dir>/audit/spool: a failed import of strangers'
// entries must not resurrect them. A missing directory is an empty spool.
func (s *Service) clearSpool(ctx context.Context) (int, error) {
	const op = "demo.Service.clearSpool"
	dir := filepath.Join(s.o.DataDir, filepath.FromSlash(audit.SpoolDirName))
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return 0, nil
	case err != nil:
		return 0, errs.E(op, errs.Unavailable, fmt.Errorf("read the audit spool directory: %w", err))
	}
	n := 0
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return n, checkCtx(op, ctx)
		}
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return n, errs.E(op, errs.Unavailable, fmt.Errorf("remove a spool file: %w", err))
		}
		n++
	}
	return n, nil
}
