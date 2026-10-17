package signals

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals/internal/rules"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Delivery names one attempt to deliver one signal to one subscriber. It is a plain value
// a Deferrer can store and hand back to Redeliver.
type Delivery struct {
	Signal     Signal
	Subscriber string // the scope owner: a module name or core area
	Attempt    int    // 1 for the delivery that failed; the retrier increments
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: Signal
// invalid, Subscriber not a module name, Attempt < 1 (F28). Fields are the signal's own,
// "delivery.subscriber" and "delivery.attempt".
func (d Delivery) Validate() error {
	acc := validate.Errors{Op: "signals.Delivery.Validate"}
	acc.Add(d.Signal.Validate())
	if !rules.Owner(d.Subscriber) {
		acc.Addf("delivery.subscriber", codeMalformed, "must be a module name")
	}
	if d.Attempt < 1 {
		acc.Addf("delivery.attempt", validate.CodeRange, "must be at least 1, got %d", d.Attempt)
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Deferrer takes a delivery whose handler failed with errs.Unavailable or errs.Timeout
// (and not db.ErrReadOnly) so it can be retried later. jobs implements it: it stores the
// Delivery on the writing copy and calls Redeliver with backoff. Defer is called from the
// delivering goroutine with a ctx that carries the handler's trace and in-delivery marker
// and a fresh deadline, but no request cancellation; it must be quick and must not deliver
// anything itself. A Defer error is logged beside the handler's error and the delivery is
// final (F23).
type Deferrer interface {
	Defer(ctx context.Context, d Delivery) error
}

// The delivery outcomes, as they appear on spans and on bms.signals.delivered.
const (
	outcomeOK       = "ok"
	outcomeFailed   = "failed"
	outcomeDeferred = "deferred"
	outcomeReadOnly = "read_only"
	outcomePanicked = "panicked"
)

// deliver runs every handler of sig in subscription order. ctx is the hook's ctx: it
// carries the request's span and values, and its cancellation is deliberately dropped,
// because a reaction to a committed change must not depend on the client still being there.
func (d *Dispatcher) deliver(ctx context.Context, sig Signal) {
	if !d.enter() {
		// Committed after Close: the dispatcher no longer delivers. Nothing is durable, so
		// the reaction is lost; say so, with the identifiers and never the payload.
		d.opts.Logger.LogAttrs(ctx, slog.LevelWarn, "signal dropped: the dispatcher is closed",
			slog.String("signal", string(sig.Name)), slog.String("signal_id", sig.ID.String()))
		return
	}
	defer d.leave()
	subs := d.snapshot(sig.Name)
	if len(subs) == 0 {
		return
	}
	depth := rules.Depth(ctx) + 1
	base := rules.WithDepth(context.WithoutCancel(ctx), depth)
	for _, s := range subs {
		// A handler's error never reaches the publisher: run has logged and counted it.
		d.run(base, attempt{sig: sig, sub: s, n: 1, depth: depth})
	}
}

type attempt struct {
	sig       Signal
	sub       subscription
	n         int
	depth     int
	redeliver bool // the caller is the retrier: return the error, never defer
}

// runResult is what run reports: the outcome token and the handler's error (a panic as
// errs.Internal), which only Redeliver passes on.
type runResult struct {
	outcome string
	err     error
}

// run delivers one signal to one handler under HandlerTimeout, and records the outcome:
// span, counter and log are written before it returns.
func (d *Dispatcher) run(base context.Context, a attempt) runResult {
	const op = "signals.Dispatcher.deliver"
	name := string(a.sig.Name)
	hctx, cancel := context.WithTimeout(base, d.opts.HandlerTimeout)
	defer cancel()
	hctx, span := d.opts.Tracer.Start(hctx, "deliver:"+name,
		telemetry.WithKind(telemetry.SpanConsumer),
		telemetry.WithAttrs(
			telemetry.Token("signal", name), telemetry.Module(a.sub.owner),
			telemetry.ID("signal_id", a.sig.ID), telemetry.Int("attempt", a.n),
			telemetry.Int("depth", a.depth)))
	defer span.End()

	start := d.opts.Clock.Now()
	res := invoke(hctx, a.sub.h, a.sig)
	herr := res.err
	elapsed := d.opts.Clock.Since(start)

	handled := herr
	outcome := outcomeOK
	switch {
	case res.panicked:
		outcome = outcomePanicked
		handled = errs.E(op, errs.Internal, fmt.Errorf("handler panicked: %v", res.panicVal))
		d.log(hctx, slog.LevelError, "signal handler panicked", a, elapsed, handled,
			slog.String("stack", string(res.stack)))
	case herr == nil:
		d.log(hctx, slog.LevelDebug, "signal delivered", a, elapsed, nil)
	case errors.Is(herr, db.ErrReadOnly):
		outcome = outcomeReadOnly
		d.log(hctx, slog.LevelWarn, "signal handler cannot write on a read-only copy", a, elapsed, herr)
	case isRetryable(herr):
		outcome = d.retryable(hctx, a, elapsed, herr)
	default:
		outcome = outcomeFailed
		d.log(hctx, slog.LevelError, "signal delivery failed", a, elapsed, herr)
	}
	if elapsed >= d.opts.HandlerTimeout {
		d.log(hctx, slog.LevelWarn, "signal handler overran", a, elapsed, nil)
	}

	if outcome != outcomeOK {
		span.RecordError(handled)
	}
	span.SetAttrs(telemetry.Token("outcome", outcome), telemetry.Duration("duration_ms", elapsed))
	d.delivered.Add(hctx, 1, telemetry.Token("signal", name), telemetry.Module(a.sub.owner),
		telemetry.Token("outcome", outcome))
	return runResult{outcome: outcome, err: handled}
}

// retryable handles a handler failure of kind Unavailable or Timeout and returns the
// outcome: deferred when a Deferrer took it, failed otherwise.
func (d *Dispatcher) retryable(hctx context.Context, a attempt, elapsed time.Duration, herr error) string {
	if a.redeliver { // the caller is the retrier
		d.log(hctx, slog.LevelError, "signal redelivery failed", a, elapsed, herr)
		return outcomeFailed
	}
	if d.opts.Deferrer == nil {
		d.log(hctx, slog.LevelError, "signal delivery failed (not retried: no deferrer)", a, elapsed, herr)
		return outcomeFailed
	}
	// The handler's own deadline may be what expired, so the Deferrer gets a fresh one.
	dctx, cancel := context.WithTimeout(context.WithoutCancel(hctx), d.opts.HandlerTimeout)
	defer cancel()
	derr := d.opts.Deferrer.Defer(dctx, Delivery{Signal: a.sig.clone(), Subscriber: a.sub.owner, Attempt: a.n})
	if derr != nil {
		d.log(hctx, slog.LevelError, "signal delivery failed (could not be deferred)", a, elapsed, herr,
			slog.Any("defer_error", derr))
		return outcomeFailed
	}
	d.log(hctx, slog.LevelInfo, "signal delivery deferred", a, elapsed, herr)
	return outcomeDeferred
}

func isRetryable(err error) bool {
	return errs.Is(err, errs.Unavailable) || errs.Is(err, errs.Timeout)
}

// log writes one line carrying the delivery's identifiers and never the payload.
func (d *Dispatcher) log(ctx context.Context, lvl slog.Level, msg string, a attempt, elapsed time.Duration,
	err error, extra ...slog.Attr) {
	attrs := []slog.Attr{
		slog.String("signal", string(a.sig.Name)),
		slog.String("subscriber", a.sub.owner),
		slog.String("signal_id", a.sig.ID.String()),
		slog.Int("attempt", a.n),
		slog.Int64("duration_ms", elapsed.Milliseconds()),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error.kind", errs.KindOf(err).String()), slog.Any("error", err))
	}
	d.opts.Logger.LogAttrs(ctx, lvl, msg, append(attrs, extra...)...)
}

// result is what a handler call produced.
type result struct {
	err      error  // the handler's own error
	panicked bool   // the handler panicked; err is nil
	panicVal any    // the recovered value
	stack    []byte // the stack at the panic
}

// invoke calls h and recovers a panic. Each handler gets its own copy of the payload, so
// one cannot change what the next sees.
func invoke(ctx context.Context, h Handler, s Signal) (res result) {
	defer func() {
		if r := recover(); r != nil {
			res = result{panicked: true, panicVal: r, stack: debug.Stack()}
		}
	}()
	return result{err: h(ctx, s.clone())}
}

func (s Signal) clone() Signal {
	s.Payload = maps.Clone(s.Payload)
	return s
}

// Redeliver delivers dl.Signal to the one handler dl.Subscriber registered for
// dl.Signal.Name, with the same ctx rules as a first delivery (a deadline of
// HandlerTimeout, InDelivery true) except that cancellation of ctx is honoured, since the
// caller is a job runner that must stop on shutdown. Unlike a first delivery, the
// handler's error is returned unchanged so the caller can decide by kind; the delivery is
// still counted and spanned, with attempt = dl.Attempt, and never handed to the Deferrer
// (the caller is the retrier). It fails with errs.Invalid for an invalid dl (F28),
// errs.NotFound when no such subscriber has a handler for that name, for example because
// the module was disabled since (F29), errs.Unavailable after Close (F31) and
// errs.Canceled or errs.Timeout when ctx ends (F31).
func (d *Dispatcher) Redeliver(ctx context.Context, dl Delivery) error {
	const op = "signals.Dispatcher.Redeliver"
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	if d.isClosed() {
		return errs.E(op, errs.Unavailable, errors.New("the dispatcher is closed"))
	}
	if err := dl.Validate(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	sub, err := d.admit(op, dl.Signal.Name, dl.Subscriber)
	if err != nil {
		return err
	}
	defer d.leave()
	dl.Signal = dl.Signal.clone()
	return d.run(rules.WithDepth(ctx, 1), attempt{
		sig: dl.Signal, sub: sub, n: dl.Attempt, depth: 1, redeliver: true,
	}).err
}

func (d *Dispatcher) isClosed() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.closed
}

// admit finds the handler subscriber registered for name and counts a delivery in flight,
// both under one lock so Close cannot slip between the closed check and the count. The
// caller must leave.
func (d *Dispatcher) admit(op string, name Name, subscriber string) (subscription, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return subscription{}, errs.E(op, errs.Unavailable, errors.New("the dispatcher is closed"))
	}
	for _, s := range d.handlers[name] {
		if s.owner == subscriber {
			d.inflight++
			return s, nil
		}
	}
	return subscription{}, errs.E(op, errs.NotFound, fmt.Errorf("%q has no handler for %q", subscriber, name))
}
