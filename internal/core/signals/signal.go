package signals

import (
	"context"
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// SignalID identifies one publication, for logs, spans and later redelivery.
type SignalID = model.ID[signal]

type signal struct{}

// Signal is one thing that happened. The dispatcher fills ID and At at Publish (At is the
// publish time inside the transaction, from the clock, not the commit time). It is a
// plain, serializable value: a Deferrer may store it.
type Signal struct {
	ID      SignalID
	Name    Name
	At      time.Time
	Payload Payload
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: zero ID,
// invalid Name, zero At, invalid Payload (F6). Fields are "signal.id", "signal.name",
// "signal.at" and the payload's own "payload" and "payload.<key>".
func (s Signal) Validate() error {
	acc := validate.Errors{Op: "signals.Signal.Validate"}
	if s.ID.IsZero() {
		acc.Addf("signal.id", validate.CodeRequired, "must not be the zero ID")
	}
	if s.Name.Validate() != nil {
		acc.Addf("signal.name", codeMalformed, "must be <publisher>.<ident>")
	}
	if s.At.IsZero() {
		acc.Addf("signal.at", validate.CodeRequired, "must be set")
	}
	acc.Add(s.Payload.Validate())
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Handler reacts to one signal. It runs on the publisher's goroutine after the publisher's
// transaction committed, with a ctx that carries the trace but no transaction, no
// cancellation from the request that caused it, a deadline of Options.HandlerTimeout, and
// InDelivery(ctx) == true. It must be quick (a small write, a notification, enqueueing a
// job); long work goes to jobs. It is called at most once per Signal per subscriber from
// Publish; a Redeliver is a further call with the same Signal, so a handler that writes
// should be idempotent on Signal.ID. Its error never reaches the publisher: the dispatcher
// logs it once, counts it and marks the delivery span (F21-F26). It runs as the system,
// not as any person.
type Handler func(ctx context.Context, s Signal) error

// Bus is what a publisher or a subscriber holds: Deps.Signals for a module, a scoped bus
// from Scope for a core publisher. Safe for concurrent use.
type Bus interface {
	// Publish records that something happened. name must be in the scope's publishes (F16)
	// and p must validate (F17). Inside a transaction (the ctx Transact handed out, or a
	// child of it) the signal is delivered after commit and Publish returns nil at once,
	// meaning "will be delivered if this transaction commits"; if the transaction rolls
	// back or its commit fails, nothing is delivered. Outside a transaction the signal is
	// delivered before Publish returns. Either way Publish returns nil whatever the
	// subscribers did. It fails with errs.Canceled or errs.Timeout if ctx is already done
	// (F18), errs.Invalid when called from a handler nested MaxDepth deep (F19), and
	// errs.Unavailable after Close (F20). Because Publish is called before the publisher
	// commits, any of these errors makes the publisher's use case fail and roll back, which
	// is the intended effect: a change whose signal cannot be published is not made.
	Publish(ctx context.Context, name Name, p Payload) error

	// Subscribe registers h for name, once per name per scope. It is called in
	// Module.Start, does no I/O and succeeds on a read-only copy. name must be in the
	// scope's subscribes (F12); h must not be nil (F13); a second Subscribe for the same
	// name fails with errs.Conflict (F14); after Close it fails with errs.Internal (F15).
	// Handlers run in subscription order, which app makes the module build order.
	Subscribe(name Name, h Handler) error
}
