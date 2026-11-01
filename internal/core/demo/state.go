package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo/postgres"
)

// State is the demo marker as bms check prints it.
type State struct {
	// SeededAt is when the seed finished; zero while the seed is running (or failed).
	SeededAt time.Time
}

// StateOf reads the marker through its own store on d: bms check's demo line. Fails
// errs.NotFound when there is no marker (never seeded) and with the store's kinds
// otherwise; errs.Invalid for a nil d.
func StateOf(ctx context.Context, d db.DB) (State, error) {
	st, err := postgres.NewStore(d)
	if err != nil {
		return State{}, fmt.Errorf("demo.StateOf: %w", err)
	}
	row, err := st.State(ctx)
	if err != nil {
		return State{}, fmt.Errorf("demo.StateOf: %w", err)
	}
	return State{SeededAt: row.SeededAt}, nil
}
