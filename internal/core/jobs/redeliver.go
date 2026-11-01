package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
)

// wireDelivery is the stored form of a signals.Delivery: identifiers, names and times, and a
// payload of record references (signals decision 4).
type wireDelivery struct {
	Signal     wireSignal `json:"signal"`
	Subscriber string     `json:"subscriber"`
	Attempt    int        `json:"attempt"`
}

type wireSignal struct {
	ID      signals.SignalID `json:"id"`
	Name    signals.Name     `json:"name"`
	At      time.Time        `json:"at"`
	Payload signals.Payload  `json:"payload"`
}

type wireArgs struct {
	Delivery wireDelivery `json:"delivery"`
}

func encodeDelivery(d signals.Delivery) ([]byte, error) {
	p := d.Signal.Payload
	if p == nil {
		p = signals.Payload{}
	}
	b, err := json.Marshal(wireArgs{Delivery: wireDelivery{
		Signal:     wireSignal{ID: d.Signal.ID, Name: d.Signal.Name, At: d.Signal.At.UTC(), Payload: p},
		Subscriber: d.Subscriber, Attempt: d.Attempt,
	}})
	if err != nil {
		return nil, errs.E("jobs.encodeDelivery", errs.Internal, err)
	}
	return b, nil
}

func decodeDelivery(raw []byte) (signals.Delivery, error) {
	var w wireArgs
	if err := json.Unmarshal(raw, &w); err != nil {
		return signals.Delivery{}, errs.E("jobs.decodeDelivery", errs.Invalid, errors.New("the stored delivery is unreadable"))
	}
	d := signals.Delivery{
		Signal: signals.Signal{
			ID: w.Delivery.Signal.ID, Name: w.Delivery.Signal.Name, At: w.Delivery.Signal.At,
			Payload: w.Delivery.Signal.Payload,
		},
		Subscriber: w.Delivery.Subscriber, Attempt: w.Delivery.Attempt,
	}
	if len(d.Signal.Payload) == 0 {
		d.Signal.Payload = nil
	}
	return d, nil
}

// Defer implements signals.Deferrer: it stores d as a signals.redeliver job with key
// "<signal id>:<subscriber>", RunAt = now + Retry.Delay(d.Attempt + 1, u), and returns
// without delivering anything. It is quick (one insert) and never blocks on a worker.
// Fails errs.Invalid for an invalid d; errs.Unavailable wrapping db.ErrReadOnly on a
// read-only copy (signals logs it beside the handler's error, the delivery is final); store
// kinds; errs.Unavailable after Close. A repeated Defer of the same (signal, subscriber) is
// a no-op.
func (r *Runner) Defer(ctx context.Context, d signals.Delivery) (err error) {
	const op = "jobs.Runner.Defer"
	ctx, span := r.span(ctx, "defer", telemetry.WithKind(telemetry.SpanProducer))
	defer endSpan(span, &err)
	if err := d.Validate(); err != nil {
		return wrap(op, err)
	}
	if r.isClosed() {
		return errs.E(op, errs.Unavailable, errors.New("the job runner is closed"))
	}
	if r.db.ReadOnly() {
		return readOnlyErr(op)
	}
	now := r.now()
	trace, _ := telemetry.TraceIDFrom(ctx)
	n, err := redeliverRow(d, now, r.retry, r.jitter(), trace)
	if err != nil {
		return wrap(op, err)
	}
	span.SetAttrs(telemetry.Token("job_name", string(NameRedeliver)), telemetry.ID("signal_id", d.Signal.ID))
	_, err = r.store.Defer(ctx, n, now)
	return wrap(op, err)
}

// redeliverRow is the signals.redeliver row both Runner.Defer and Deferrer.Defer insert: key
// "<signal id>:<subscriber>", the encoded delivery as args, RunAt = now + p.Delay(d.Attempt +
// 1, u), and the caller's trace. One function, so the two paths cannot write different rows.
func redeliverRow(d signals.Delivery, now time.Time, p retry.Policy, u float64, trace string) (rows.New, error) {
	args, err := encodeDelivery(d)
	if err != nil {
		return rows.New{}, err
	}
	return rows.New{
		ID: newJobID(), Name: string(NameRedeliver), Key: d.Signal.ID.String() + ":" + d.Subscriber, Args: args,
		RunAt: now.Add(p.Delay(d.Attempt+1, u)), TraceID: trace,
	}, nil
}

// errDropped is what the redelivery handler returns when the subscriber is gone: the row ends
// done and the outcome is "dropped".
var errDropped = errors.New("jobs: the delivery has no subscriber any more")

// errGaveUp is what the redelivery handler returns when the delivery has failed
// Retry.MaxAttempts times, counting the first: the row fails "gave_up".
var errGaveUp = errors.New("jobs: gave up")

// attemptError carries the number of deliveries made, the first included, so the runner's
// backoff and give-up count them and not only the leases of this row.
type attemptError struct {
	err error
	n   int
}

func (e *attemptError) Error() string { return e.err.Error() }
func (e *attemptError) Unwrap() error { return e.err }

// attempts reports the deliveries made so far (see attemptError).
func (e *attemptError) attempts() int { return e.n }

// redeliverHandler is the handler of signals.redeliver: it hands the stored delivery back to
// its subscriber with Attempt counting the first delivery. NotFound (the module was disabled
// since) drops it with a log line; an Unavailable or Timeout is retried until Retry.MaxAttempts
// deliveries have been made, then gives up.
func (r *Runner) redeliverHandler(ctx context.Context, j Job, raw []byte) error {
	d, err := decodeDelivery(raw)
	if err != nil {
		return err
	}
	d.Attempt += j.Attempt // the first delivery is the one that failed before Defer
	err = r.redeli.Redeliver(ctx, d)
	switch {
	case err == nil:
		return nil
	case errs.Is(err, errs.NotFound):
		r.logger.InfoContext(ctx, "jobs: a deferred delivery was dropped: its subscriber has no handler",
			slog.String("signal", string(d.Signal.Name)), slog.String("signal_id", d.Signal.ID.String()),
			slog.String("subscriber", d.Subscriber), slog.Int("attempt", d.Attempt))
		return errDropped
	case errs.Is(err, errs.Unavailable) || errs.Is(err, errs.Timeout):
		if d.Attempt >= r.retry.MaxAttempts {
			return &attemptError{err: errors.Join(errGaveUp, err), n: d.Attempt}
		}
		return &attemptError{err: err, n: d.Attempt}
	default:
		return fmt.Errorf("jobs.redeliver: %w", err)
	}
}
