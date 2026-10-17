// Package signalstest gives tests a scoped Bus with no dispatcher behind it. Fake is what
// moduletest.Conformance and a module's unit tests hand to a Factory as Deps.Signals.
package signalstest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/internal/rules"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Fake implements signals.Bus with the same scoping rules as Dispatcher.Scope. Publish
// validates as the real bus does, registers a db.AfterCommit hook and records the Signal
// when the hook runs, so a test that rolls back a dbtest.Fake transaction sees nothing
// published and a test that commits sees the signal with a non-zero ID and At. Publish
// never delivers. Subscribe stores one handler per name. Safe for concurrent use.
type Fake struct {
	scope *rules.Scope
	clk   clock.Clock

	mu        sync.Mutex
	published []signals.Signal
	handlers  map[signals.Name]signals.Handler
}

var _ signals.Bus = (*Fake)(nil)

// NewFake fails as Scope does (F9, F10) for a bad owner or lists; a module's test passes
// its own manifest's fields so an undeclared name fails the test.
func NewFake(owner string, publishes, subscribes []signals.Name) (*Fake, error) {
	const op = "signalstest.NewFake"
	sc, err := rules.NewScope(op, owner, toStrings(publishes), toStrings(subscribes))
	if err != nil {
		return nil, err //nolint:wrapcheck // NewScope returns an errs.Invalid carrying op
	}
	return &Fake{scope: sc, clk: clock.Real(), handlers: map[signals.Name]signals.Handler{}}, nil
}

func toStrings(in []signals.Name) []string {
	out := make([]string, len(in))
	for i, n := range in {
		out[i] = string(n)
	}
	return out
}

// Publish validates as signals.Bus.Publish does (F16-F19) and records the signal when the
// surrounding transaction commits, or at once outside one. It never delivers.
func (f *Fake) Publish(ctx context.Context, name signals.Name, p signals.Payload) error {
	const op = "signalstest.Fake.Publish"
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	if !f.scope.CanPublish(string(name)) {
		return errs.E(op, errs.Invalid,
			fmt.Errorf("%q is not in the publishes of %q", rules.Cap(string(name)), f.scope.Owner()))
	}
	p = maps.Clone(p)
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%s: %q: %w", op, name, err)
	}
	if depth := rules.Depth(ctx); depth >= signals.MaxDepth {
		return errs.E(op, errs.Invalid,
			fmt.Errorf("signal %q published from a handler nested %d deep (the limit is %d)",
				name, depth, signals.MaxDepth))
	}
	sig := signals.Signal{ID: model.NewID[signals.SignalID](), Name: name, At: f.clk.Now(), Payload: p}
	err := db.AfterCommit(ctx, func(context.Context) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.published = append(f.published, sig)
	})
	if err != nil {
		return errs.E(op, errs.Internal, fmt.Errorf("register the after-commit hook: %w", err))
	}
	return nil
}

// Subscribe stores h for name under the rules of signals.Bus.Subscribe (F12-F14).
func (f *Fake) Subscribe(name signals.Name, h signals.Handler) error {
	const op = "signalstest.Fake.Subscribe"
	if !f.scope.CanSubscribe(string(name)) {
		return errs.E(op, errs.Invalid,
			fmt.Errorf("%q is not in the subscribes of %q", rules.Cap(string(name)), f.scope.Owner()))
	}
	if h == nil {
		return errs.E(op, errs.Invalid, fmt.Errorf("nil handler for %q", name))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.handlers[name]; dup {
		return errs.E(op, errs.Conflict, fmt.Errorf("%q already has a handler for %q", f.scope.Owner(), name))
	}
	f.handlers[name] = h
	return nil
}

// Published returns every signal recorded so far (after commit), in order, as a copy.
// Cannot fail.
func (f *Fake) Published() []signals.Signal {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]signals.Signal, len(f.published))
	for i, s := range f.published {
		s.Payload = maps.Clone(s.Payload)
		out[i] = s
	}
	return out
}

// Deliver calls the handler the module registered for s.Name with a ctx derived from ctx
// (InDelivery true, no deadline added: the test's ctx bounds it) and returns the handler's
// error unchanged, so a test asserts the handler's kinds directly. It fails with
// errs.Invalid if s fails Validate, errs.NotFound when no handler is registered for the
// name (F33), and errs.Internal wrapping the panic value when the handler panics (F33).
func (f *Fake) Deliver(ctx context.Context, s signals.Signal) (err error) {
	const op = "signalstest.Fake.Deliver"
	if err := s.Validate(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	f.mu.Lock()
	h, ok := f.handlers[s.Name]
	f.mu.Unlock()
	if !ok {
		return errs.E(op, errs.NotFound, fmt.Errorf("no handler registered for %q", s.Name))
	}
	defer func() {
		if r := recover(); r != nil {
			err = errs.E(op, errs.Internal, fmt.Errorf("handler panicked: %v", r))
		}
	}()
	s.Payload = maps.Clone(s.Payload)
	return h(rules.WithDepth(ctx, rules.Depth(ctx)+1), s) //nolint:wrapcheck // the handler's error is returned unchanged by contract
}

// Handlers returns the names the module subscribed to, sorted. Cannot fail. For a test
// that checks Start subscribed to everything the manifest declares.
func (f *Fake) Handlers() []signals.Name {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Sorted(maps.Keys(f.handlers))
}
