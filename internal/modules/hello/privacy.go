package hello

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

// exportDoc is the shape of ExportPerson's data. The person is not repeated: the export is
// already about them, and the greeting type keeps the ID off the wire.
type exportDoc struct {
	Greetings []greeting.Greeting `json:"greetings"`
}

func checkPerson(op string, person model.PersonID) error {
	if person.IsZero() {
		var acc validate.Errors
		acc.Op = op
		acc.Addf("person", validate.CodeRequired, "must be set")
		return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return nil
}

// ExportPerson returns the person's greetings as {"greetings":[{id, greeting,
// created_at}]}, oldest first. It runs as the system for people, which checks and audits
// the export. Fails with errs.Invalid for a zero ID and with the store's db kinds.
func (m *Module) ExportPerson(ctx context.Context, person model.PersonID) (_ module.Export, err error) {
	const op = "hello.ExportPerson"
	if err := checkPerson(op, person); err != nil {
		return module.Export{}, err
	}
	ctx, span := m.svc.tracer.Start(ctx, "hello.export-person")
	defer endSpan(span, &err)
	rows, err := m.svc.store.ListByPerson(ctx, person)
	if err != nil {
		return module.Export{}, fmt.Errorf("%s: %w", op, err)
	}
	if rows == nil {
		rows = []greeting.Greeting{}
	}
	data, err := json.Marshal(exportDoc{Greetings: rows})
	if err != nil {
		return module.Export{}, errs.E(op, errs.Internal, fmt.Errorf("encode export: %w", err))
	}
	return module.Export{Data: data}, nil
}

// DeletePerson removes the person's greetings. Nothing is kept or anonymised. It is
// idempotent (0 on the second call). Fails with errs.Invalid for a zero ID, errs.Unavailable
// wrapping db.ErrReadOnly on a read-only copy, and the store's db kinds.
func (m *Module) DeletePerson(ctx context.Context, person model.PersonID) (_ module.Deletion, err error) {
	const op = "hello.DeletePerson"
	if err := checkPerson(op, person); err != nil {
		return module.Deletion{}, err
	}
	ctx, span := m.svc.tracer.Start(ctx, "hello.delete-person")
	defer endSpan(span, &err)
	var n int
	err = m.svc.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var derr error
		n, derr = m.svc.store.DeleteByPerson(ctx, person)
		if derr != nil {
			return fmt.Errorf("delete: %w", derr)
		}
		return nil
	})
	if err != nil {
		return module.Deletion{}, fmt.Errorf("%s: %w", op, err)
	}
	return module.Deletion{Deleted: n}, nil
}
