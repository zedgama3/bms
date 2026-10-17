// Package txctx is the context-carried transaction that db and dbtest share. It is
// internal to internal/core/db so that dbtest.Fake can make db.AfterCommit work without
// db exporting its mechanism.
package txctx

import (
	"context"
	"errors"
	"sync"

	"github.com/zedgama3/bms/internal/lib/errs"
)

type key struct{}

// State is the transaction a context carries and the hooks waiting for its commit.
type State struct {
	// Owner is the DB that began the transaction; a DB ignores a transaction it does not own.
	Owner any
	// Impl is the owner's private value (a pgx.Tx for db.Pool; nil for dbtest.Fake).
	Impl any
	// Write reports whether the transaction may write.
	Write bool

	mu    sync.Mutex
	done  bool
	ok    bool
	hooks []func(ctx context.Context)
}

// With returns ctx carrying s.
func With(ctx context.Context, s *State) context.Context {
	return context.WithValue(ctx, key{}, s)
}

// From returns the transaction ctx carries, or nil.
func From(ctx context.Context) *State {
	s, ok := ctx.Value(key{}).(*State)
	if !ok {
		return nil
	}
	return s
}

// Add registers fn: queued while the transaction is open, run at once when it already
// committed, refused with errs.Invalid when it ended without committing. A nil fn is
// errs.Invalid.
func (s *State) Add(ctx context.Context, fn func(ctx context.Context)) error {
	const op = "db.AfterCommit"
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil function"))
	}
	s.mu.Lock()
	switch {
	case !s.done:
		s.hooks = append(s.hooks, fn)
		s.mu.Unlock()
		return nil
	case s.ok:
		s.mu.Unlock()
		fn(ctx)
		return nil
	default:
		s.mu.Unlock()
		return errs.E(op, errs.Invalid, errors.New("the transaction already ended without committing"))
	}
}

// Finish marks the transaction ended and returns, when it committed, the hooks to run in
// registration order.
func (s *State) Finish(committed bool) []func(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done, s.ok = true, committed
	hooks := s.hooks
	s.hooks = nil
	if !committed {
		return nil
	}
	return hooks
}
