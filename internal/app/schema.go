package app

import (
	"context"
	"errors"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// schemaStatus is the schema-status reader `serve` hands the API shell as api.Options.Schema
// (boundaries.md amendment D1): the migrator and the migration sets it reports on, the core's
// then each enabled module's with migrations, in build order. The sets are computed once, when
// the reader is built: the plan and the core's declarations are fixed from Resolve on, so a
// call after that would compute the same list. It holds no reference to the assembly.
type schemaStatus struct {
	migrator *db.Migrator
	sets     []db.Set
}

// newSchemaStatus returns the reader over migrator and sets. Fails errs.Internal for a nil
// migrator (a wiring bug: serve opens it before building the reader).
func newSchemaStatus(migrator *db.Migrator, sets []db.Set) (*schemaStatus, error) {
	if migrator == nil {
		return nil, errs.E("app.newSchemaStatus", errs.Internal, errors.New("no migrator"))
	}
	return &schemaStatus{migrator: migrator, sets: sets}, nil
}

// Status reports each set's built and applied versions without changing the database. It
// fails with the kinds of db.Migrator.Status.
func (s *schemaStatus) Status(ctx context.Context) ([]db.Status, error) {
	return s.migrator.Status(ctx, s.sets) //nolint:wrapcheck // Migrator.Status names itself
}
