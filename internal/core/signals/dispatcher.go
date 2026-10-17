package signals

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals/internal/rules"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// MaxDepth is how deep handlers may publish from handlers: a signal published by a handler
// running at depth MaxDepth fails (F19). Eight is more than any sane chain and small
// enough that a loop between two modules is caught within one request.
const MaxDepth = rules.MaxDepth

// InDelivery reports whether ctx is the ctx of a running handler (first delivery or
// Redeliver), at any nesting depth. Cannot fail.
func InDelivery(ctx context.Context) bool { return rules.Depth(ctx) > 0 }

// subscription is one registered handler and the scope that owns it.
type subscription struct {
	owner string
	h     Handler
}

// Dispatcher routes committed signals to their subscribers. One per process; built by app
// before the modules, closed after the HTTP server drained and before Set.Stop. Safe for
// concurrent use from New until Close returns.
type Dispatcher struct {
	opts      Options
	published telemetry.Counter
	delivered telemetry.Counter

	mu       sync.RWMutex
	closed   bool
	scopes   map[string]struct{}
	handlers map[Name][]subscription // in subscription order; replaced, never appended in place
	inflight int
	idle     chan struct{} // closed when inflight reaches 0, while a Close waits
}

// New validates opts, creates the two counters and returns a dispatcher with no scopes. It
// fails with errs.Invalid for bad options (F7) and with the meter's kind if a counter
// cannot be created (F8).
func New(opts Options) (*Dispatcher, error) {
	const op = "signals.New"
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	published, err := opts.Meter.Counter("bms.signals.published", telemetry.WithUnit("{signal}"))
	if err != nil {
		return nil, fmt.Errorf("%s: published counter: %w", op, err)
	}
	delivered, err := opts.Meter.Counter("bms.signals.delivered", telemetry.WithUnit("{delivery}"))
	if err != nil {
		return nil, fmt.Errorf("%s: delivered counter: %w", op, err)
	}
	return &Dispatcher{
		opts:      opts,
		published: published,
		delivered: delivered,
		scopes:    map[string]struct{}{},
		handlers:  map[Name][]subscription{},
	}, nil
}

// Scope returns the Bus for one publisher-and-subscriber: a module (owner = Manifest.Name,
// publishes = Manifest.Publishes, subscribes = Manifest.Subscribes) or a core area
// (owner = "people", its own lists). owner follows the module-name grammar
// "[a-z][a-z0-9]{1,31}" (F9); every name in publishes must validate and have Publisher()
// == owner, every name in subscribes must validate, and neither list may repeat a name
// (F10); one Scope per owner (F11, errs.Conflict); errs.Internal after Close (F15). The
// scope is a view: it holds no goroutines and nothing to close.
func (d *Dispatcher) Scope(owner string, publishes, subscribes []Name) (Bus, error) {
	const op = "signals.Dispatcher.Scope"
	sc, err := rules.NewScope(op, owner, names(publishes), names(subscribes))
	if err != nil {
		return nil, err //nolint:wrapcheck // NewScope returns an errs.Invalid carrying op
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errs.E(op, errs.Internal, errors.New("the dispatcher is closed"))
	}
	if _, dup := d.scopes[owner]; dup {
		return nil, errs.E(op, errs.Conflict, fmt.Errorf("%q is already scoped", owner))
	}
	d.scopes[owner] = struct{}{}
	return &bus{d: d, scope: sc}, nil
}

func names(in []Name) []string {
	out := make([]string, len(in))
	for i, n := range in {
		out[i] = string(n)
	}
	return out
}

// Close refuses further Publish, Subscribe and Scope calls and waits for deliveries in
// flight to finish, within ctx. It fails with errs.Timeout or errs.Canceled when ctx ends
// first (F32); the deliveries then finish on their own goroutines and their results are
// still counted. Idempotent: a Close after the deliveries finished returns nil, and one
// made while some are still running waits for them again.
func (d *Dispatcher) Close(ctx context.Context) error {
	const op = "signals.Dispatcher.Close"
	d.mu.Lock()
	d.closed = true
	if d.inflight == 0 {
		d.mu.Unlock()
		return nil
	}
	if d.idle == nil {
		d.idle = make(chan struct{})
	}
	idle := d.idle
	d.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		err := ctx.Err()
		return errs.E(op, errs.KindOf(err), fmt.Errorf("waiting for deliveries in flight: %w", err))
	}
}

// enter counts a delivery in flight unless the dispatcher is closed, and reports whether
// it did. The check and the increment are one step, so Close cannot miss a delivery that
// got past the check. leave uncounts it and wakes a waiting Close.
func (d *Dispatcher) enter() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	d.inflight++
	return true
}

func (d *Dispatcher) leave() {
	d.mu.Lock()
	d.inflight--
	if d.inflight == 0 && d.idle != nil {
		close(d.idle)
		d.idle = nil
	}
	d.mu.Unlock()
}

// snapshot returns the handlers for name in subscription order. The slice is never
// mutated after it is published, so the caller may iterate it without the lock.
func (d *Dispatcher) snapshot(name Name) []subscription {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.handlers[name]
}

// bus is one scope: the Bus a module or a core publisher holds.
type bus struct {
	d     *Dispatcher
	scope *rules.Scope
}

var _ Bus = (*bus)(nil)

// Subscribe implements Bus.
func (b *bus) Subscribe(name Name, h Handler) error {
	const op = "signals.Bus.Subscribe"
	d := b.d
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errs.E(op, errs.Internal, errors.New("the dispatcher is closed"))
	}
	if !b.scope.CanSubscribe(string(name)) {
		return errs.E(op, errs.Invalid,
			fmt.Errorf("%q is not in the subscribes of %q", rules.Cap(string(name)), b.scope.Owner()))
	}
	if h == nil {
		return errs.E(op, errs.Invalid, fmt.Errorf("nil handler for %q", name))
	}
	cur := d.handlers[name]
	for _, s := range cur {
		if s.owner == b.scope.Owner() {
			return errs.E(op, errs.Conflict,
				fmt.Errorf("%q already has a handler for %q", b.scope.Owner(), name))
		}
	}
	next := make([]subscription, len(cur), len(cur)+1)
	copy(next, cur)
	d.handlers[name] = append(next, subscription{owner: b.scope.Owner(), h: h})
	return nil
}

// Publish implements Bus.
func (b *bus) Publish(ctx context.Context, name Name, p Payload) error {
	const op = "signals.Bus.Publish"
	d := b.d
	owner := b.scope.Owner()
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	d.mu.RLock()
	closed := d.closed
	subscribers := len(d.handlers[name])
	d.mu.RUnlock()
	if closed {
		return errs.E(op, errs.Unavailable, errors.New("the dispatcher is closed"))
	}
	if !b.scope.CanPublish(string(name)) {
		return errs.E(op, errs.Invalid,
			fmt.Errorf("%q is not in the publishes of %q", rules.Cap(string(name)), owner))
	}
	p = maps.Clone(p)
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%s: %q: %w", op, name, err)
	}
	if depth := rules.Depth(ctx); depth >= MaxDepth {
		return errs.E(op, errs.Invalid,
			fmt.Errorf("signal %q published from a handler nested %d deep (the limit is %d)", name, depth, MaxDepth))
	}
	sig := Signal{ID: model.NewID[SignalID](), Name: name, At: d.opts.Clock.Now(), Payload: p}

	_, span := d.opts.Tracer.Start(ctx, "signal:"+string(name),
		telemetry.WithKind(telemetry.SpanProducer),
		telemetry.WithAttrs(
			telemetry.Token("signal", string(name)), telemetry.Module(owner),
			telemetry.ID("signal_id", sig.ID), telemetry.Int("subscribers", subscribers)))
	defer span.End()

	// The hook gets Transact's ctx, not the span's, so deliveries are children of the
	// request's span. Published is counted when the hook runs, so a rollback counts nothing.
	err := db.AfterCommit(ctx, func(hctx context.Context) {
		d.published.Add(hctx, 1, telemetry.Token("signal", string(name)), telemetry.Module(owner))
		d.deliver(hctx, sig)
	})
	if err != nil {
		span.RecordError(err)
		return errs.E(op, errs.Internal, fmt.Errorf("register the after-commit hook: %w", err))
	}
	return nil
}
