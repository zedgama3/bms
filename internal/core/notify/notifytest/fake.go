// Package notifytest gives tests of notify's callers a Notifier and a Channel without a
// database or a mail server. Fake validates like the real notifier; Channel is an in-memory
// notify.Channel for tests of the sender.
package notifytest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Fake is a notify.Notifier scoped to one owner. It validates messages as the real notifier
// does (Message.Validate, the owner's scope), records them after the ctx's transaction
// commits, and fails every Send with the error given to Fail. Safe for concurrent use.
type Fake struct {
	owner string

	mu           sync.Mutex
	sent         []notify.Message
	failErr      error
	unconfigured bool
}

var _ notify.Notifier = (*Fake)(nil)

// NewFake returns a Notifier scoped to owner. Cannot fail.
func NewFake(owner string) *Fake { return &Fake{owner: owner} }

// Send validates m, checks the order of the real notifier (Configured, then scope), and
// records m once the transaction ctx carries commits (at once when it carries none; a
// rolled-back transaction records nothing). Fails errs.Invalid as Message.Validate and
// (field "template", code "scope") for another owner's template, errs.Unsupported wrapping
// notify.ErrNotConfigured after SetConfigured(false), and the error given to Fail.
func (f *Fake) Send(ctx context.Context, m notify.Message) error {
	const op = "notifytest.Fake.Send"
	if err := m.Validate(); err != nil {
		return err //nolint:wrapcheck // Validate returns errs.Invalid with its own op
	}
	f.mu.Lock()
	unconfigured, failErr := f.unconfigured, f.failErr
	f.mu.Unlock()
	if unconfigured {
		return errs.E(op, errs.Unsupported, notify.ErrNotConfigured)
	}
	if m.Template.Owner() != f.owner {
		v := validate.Errors{Op: op}
		v.Addf("template", "scope", "the template belongs to another owner")
		return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	if failErr != nil {
		return failErr
	}
	rec := copyMessage(m)
	if err := db.AfterCommit(ctx, func(context.Context) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sent = append(f.sent, rec)
	}); err != nil {
		return errs.E(op, errs.KindOf(err), errors.New("could not register the record hook"))
	}
	return nil
}

// Configured reports true unless SetConfigured(false). Cannot fail.
func (f *Fake) Configured(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.unconfigured, nil
}

// SetConfigured sets what Configured reports and whether Send works (default true). While
// false, Send fails errs.Unsupported wrapping notify.ErrNotConfigured and records nothing.
func (f *Fake) SetConfigured(configured bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unconfigured = !configured
}

// Sent returns a copy of the messages recorded so far, in order.
func (f *Fake) Sent() []notify.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notify.Message, len(f.sent))
	for i, m := range f.sent {
		out[i] = copyMessage(m)
	}
	return out
}

// Fail makes every later Send return err (nil clears it).
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failErr = err
}

func copyMessage(m notify.Message) notify.Message {
	m.To = slices.Clone(m.To)
	if m.Vars != nil {
		m.Vars = maps.Clone(m.Vars)
	}
	return m
}
