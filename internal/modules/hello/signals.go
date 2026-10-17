package hello

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var errCounterStopped = errors.New("the counter has stopped")

// greetedPayload is the payload of SignalGreeted: the person who greeted, an ID only (no
// word, no time; signals decision 4).
func greetedPayload(person model.PersonID) signals.Payload {
	return signals.NewPayload(signals.Ref(PayloadPerson, person))
}

// handleGreeted is the subscriber for SignalGreeted. It reads PayloadPerson with
// signals.Get (errs.Invalid if absent), hands {Signal.ID} to the counter and returns once
// the counter has taken it. It touches no database, so it runs on a read-only copy. A
// Signal.ID already seen is counted once (nil). Fails errs.Canceled/Timeout if ctx ends
// before the counter takes it, and errs.Canceled when the counter has stopped.
func (s *service) handleGreeted(ctx context.Context, sig signals.Signal) error {
	const op = "hello.handleGreeted"
	if _, err := signals.Get[model.PersonID](sig.Payload, PayloadPerson); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := s.counter.add(ctx, sig.ID); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// tick is one delivery handed to the counter goroutine; ack is closed once it is counted.
type tick struct {
	id  signals.SignalID
	ack chan struct{}
}

// counter counts hello.greeted deliveries on one goroutine. The goroutine owns the ring of
// recent Signal.IDs; Seen reads the count under a mutex the goroutine updates. It exists
// so the reference shows the lifecycle of background work: started in Start, stopped and
// waited for in Stop.
type counter struct {
	in       chan tick
	stopCh   chan struct{}
	done     chan struct{} // closed when the goroutine has exited
	stopOnce sync.Once
	running  sync.Once

	mu   sync.Mutex
	seen int

	// owned by the goroutine
	ring [seenRing]signals.SignalID
	set  map[signals.SignalID]struct{}
	pos  int
}

func newCounter() *counter {
	return &counter{
		in:     make(chan tick, 64),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
		set:    make(map[signals.SignalID]struct{}, seenRing),
	}
}

// start launches the goroutine once. It exits when stopCh is closed.
func (c *counter) start() {
	c.running.Do(func() { go c.run() })
}

func (c *counter) run() {
	defer close(c.done)
	for {
		select {
		case <-c.stopCh:
			return
		case t := <-c.in:
			c.record(t.id)
			close(t.ack)
		}
	}
}

// record counts id unless it is among the last seenRing IDs.
func (c *counter) record(id signals.SignalID) {
	if _, dup := c.set[id]; dup {
		return
	}
	if old := c.ring[c.pos]; !old.IsZero() {
		delete(c.set, old)
	}
	c.ring[c.pos] = id
	c.set[id] = struct{}{}
	c.pos = (c.pos + 1) % seenRing
	c.mu.Lock()
	c.seen++
	c.mu.Unlock()
}

// add hands id to the goroutine and waits until it is counted. It never blocks past ctx.
func (c *counter) add(ctx context.Context, id signals.SignalID) error {
	const op = "hello.counter.add"
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	t := tick{id: id, ack: make(chan struct{})}
	select {
	case c.in <- t:
	case <-c.stopCh:
		return errs.E(op, errs.Canceled, errCounterStopped)
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
	}
	select {
	case <-t.ack:
		return nil
	case <-c.done:
		return errs.E(op, errs.Canceled, errCounterStopped)
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
	}
}

// Seen is how many distinct deliveries the counter has counted since start.
func (c *counter) Seen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

// stop asks the goroutine to exit and waits for it or for ctx. A ctx that is already done
// fails at once (the request to exit is still made). Nil when the goroutine has already
// exited, whatever ctx is: the goroutine exits only on stop, so that means "already
// stopped".
func (c *counter) stop(ctx context.Context) error {
	select {
	case <-c.done:
		return nil
	default:
	}
	c.stopOnce.Do(func() { close(c.stopCh) })
	if err := ctx.Err(); err != nil {
		return err //nolint:wrapcheck // the caller adds the op and kind
	}
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller adds the op and kind
	}
}
