package notifytest

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Channel is an in-memory notify.Channel: Send records the envelope unless a failure is
// set, and Ping answers the same failure. Safe for concurrent use.
type Channel struct {
	mu        sync.Mutex
	envelopes []notify.Envelope
	err       error
	sends     int
}

var _ notify.Channel = (*Channel)(nil)

// NewChannel returns an empty Channel. Cannot fail.
func NewChannel() *Channel { return &Channel{} }

// Send records e, or fails with the error set by Fail or Reject (the failed envelope is not
// recorded). It fails errs.Canceled or errs.Timeout when ctx is done.
func (c *Channel) Send(ctx context.Context, e notify.Envelope) error {
	if err := ctx.Err(); err != nil {
		return errs.E("notifytest.Channel.Send", errs.KindOf(err), err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sends++
	if c.err != nil {
		return c.err
	}
	if e.ReplyTo != nil {
		rt := *e.ReplyTo
		e.ReplyTo = &rt
	}
	c.envelopes = append(c.envelopes, e)
	return nil
}

// Ping fails with the error set by Fail (not by Reject) and succeeds otherwise.
func (c *Channel) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errs.E("notifytest.Channel.Ping", errs.KindOf(err), err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil && !errsIsRejected(c.err) {
		return c.err
	}
	return nil
}

// Envelopes returns a copy of the envelopes delivered so far, in order.
func (c *Channel) Envelopes() []notify.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.envelopes)
}

// Sends returns how many times Send was called, failed or not.
func (c *Channel) Sends() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sends
}

// Fail makes every later Send (and Ping) fail with err until it is cleared with Fail(nil).
func (c *Channel) Fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

// Reject makes every later Send fail errs.Invalid wrapping notify.ErrRejected, as a server's
// permanent refusal does.
func (c *Channel) Reject() {
	c.Fail(errs.E("notifytest.Channel.Send", errs.Invalid, notify.ErrRejected))
}

func errsIsRejected(err error) bool { return errors.Is(err, notify.ErrRejected) }
