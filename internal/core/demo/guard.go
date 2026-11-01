package demo

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo/postgres"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// protectedStore is the one query the Guard needs; postgres.Store implements it.
type protectedStore interface {
	Protected(ctx context.Context, id model.AccountID) (bool, error)
}

var (
	_ protectedStore = (*postgres.Store)(nil)
	_ identity.Guard = (*Guard)(nil)
)

// GuardOptions builds a Guard. Both fields are required.
type GuardOptions struct {
	DB     db.DB
	Tracer telemetry.Tracer
}

// Guard is identity's Guard over core.demo_account alone: it keeps the shared demo accounts
// usable by everyone. It is built in demo mode only, holds no state and has no lifecycle;
// safe for concurrent use.
type Guard struct {
	store  protectedStore
	tracer telemetry.Tracer
}

// NewGuard builds the Guard over the demo store on o.DB. No I/O. Fails errs.Invalid for a
// nil DB or Tracer.
func NewGuard(o GuardOptions) (*Guard, error) {
	const op = "demo.NewGuard"
	if o.DB == nil || o.Tracer == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil dependency"))
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &Guard{store: st, tracer: o.Tracer}, nil
}

// Protected answers whether the account is a demo account (a row in core.demo_account):
// false, nil for an unknown account. It fails only with the store's kinds (Unavailable,
// Timeout, Canceled), never "not built", so identity fails closed on a broken store.
func (g *Guard) Protected(ctx context.Context, a model.AccountID) (protected bool, err error) {
	ctx, span := g.tracer.Start(ctx, "demo.guard.protected")
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()
	ok, err := g.store.Protected(ctx, a)
	if err != nil {
		return false, fmt.Errorf("demo.Guard.Protected: %w", err)
	}
	return ok, nil
}
