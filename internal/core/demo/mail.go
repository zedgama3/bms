package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo/postgres"
	"github.com/zedgama3/bms/internal/core/demo/rows"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// captureStore is the one write the Capture needs; postgres.Store implements it.
type captureStore interface {
	// Capture inserts the mail and trims to the newest keep rows in one Write transaction.
	Capture(ctx context.Context, m rows.Mail, keep int) error
}

var (
	_ captureStore   = (*postgres.Store)(nil)
	_ notify.Channel = (*Capture)(nil)
)

// CaptureOptions builds a Capture. DB, Clock, Logger and Tracer are required; Meter is
// optional (without one nothing is counted). The Capture does not log: Send returns its
// failures for notify's sender to log once where it handles them, and never carries an
// address, a subject or a body. Logger is required so the constructor's shape matches the
// other core parts' and a future log line needs no signature change.
type CaptureOptions struct {
	DB     db.DB
	Clock  clock.Clock
	Logger *slog.Logger
	Tracer telemetry.Tracer
	Meter  telemetry.Meter
}

// Capture is notify's Channel in demo mode: it stores each envelope in core.demo_mail
// instead of sending it (demo.md "Mail capture"). It holds no state and has no lifecycle;
// safe for concurrent use.
type Capture struct {
	store   captureStore
	clock   clock.Clock
	tracer  telemetry.Tracer
	metrics metrics
}

// NewCapture builds the Capture over the demo store on o.DB. No I/O. Fails errs.Invalid for
// a nil DB, Clock, Logger or Tracer.
func NewCapture(o CaptureOptions) (*Capture, error) {
	const op = "demo.NewCapture"
	if o.DB == nil || o.Clock == nil || o.Logger == nil || o.Tracer == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil dependency"))
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	c := &Capture{store: st, clock: o.Clock, tracer: o.Tracer}
	if err := c.metrics.init(o.Meter); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return c, nil
}

// Ping answers nil, so the outbox screen's "check connection" succeeds. Cannot fail.
func (*Capture) Ping(context.Context) error { return nil }

// Send keeps the envelope instead of delivering it (span demo.mail.capture, counter
// bms.demo.mail_captured), then trims the table to the newest MaxMail rows. An envelope
// without a recipient or a subject is errs.Invalid wrapping notify.ErrRejected, so notify
// ends the row rejected instead of retrying; a store failure is errs.Unavailable (wrapping
// db.ErrReadOnly on a read-only copy) and notify retries; an ended ctx is errs.Canceled or
// errs.Timeout. It never logs an address, a subject or a body.
func (c *Capture) Send(ctx context.Context, e notify.Envelope) (err error) {
	const op = "demo.Capture.Send"
	ctx, span := c.tracer.Start(ctx, "demo.mail.capture")
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()
	if cerr := checkCtx(op, ctx); cerr != nil {
		return cerr
	}
	if e.To == "" || strings.TrimSpace(e.Subject) == "" {
		return errs.E(op, errs.Invalid, fmt.Errorf("%w: the envelope has no recipient or no subject", notify.ErrRejected))
	}
	m := rows.Mail{
		ID: [16]byte(model.NewID[MailID]()), To: string(e.To), Subject: e.Subject, Body: e.Body,
		MessageID: e.MessageID, CapturedAt: c.clock.Now().UTC(),
	}
	if m.MessageID == "" {
		m.MessageID = "none"
	}
	if cerr := c.store.Capture(ctx, m, MaxMail); cerr != nil {
		return c.classify(op, cerr)
	}
	span.SetAttrs(telemetry.ID("mail_id", MailID(m.ID)))
	add(c.metrics.captured, ctx)
	return nil
}

// classify makes a store failure retryable (Unavailable) unless it is a cancellation or
// deadline, or the envelope was refused by the table (a permanent rejection).
func (*Capture) classify(op string, err error) error {
	switch {
	case errs.Is(err, errs.Canceled), errs.Is(err, errs.Timeout), errs.Is(err, errs.Unavailable):
		return fmt.Errorf("%s: %w", op, err)
	case errs.Is(err, errs.Invalid):
		return errs.E(op, errs.Invalid, fmt.Errorf("%w: %w", notify.ErrRejected, err))
	default:
		return errs.E(op, errs.Unavailable, err)
	}
}

// ListMail lists captured mail addressed to the signed-in account's login address, newest
// first; never another address's mail, whoever asks. It needs a session and no permission,
// and is not audited: the rows are the viewer's own mail. The order of checks: actor, then
// the switch, then the limit, then the viewer's address (identity's EmailOf; no account
// gives an empty list).
//
// Fails errs.Unauthenticated without a session, errs.Unsupported when demo is off,
// errs.Invalid (field limit, code range) for a limit outside 0..MaxMailList and store kinds.
func (s *Service) ListMail(ctx context.Context, q MailQuery) ([]Mail, error) {
	const op = "demo.Service.ListMail"
	a, ok := s.o.Actor(ctx)
	if !ok || a.Person.IsZero() {
		return nil, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	if !s.o.Enabled {
		return nil, errs.E(op, errs.Unsupported, errors.New("this is not a demo instance"))
	}
	limit := q.Limit
	switch {
	case limit == 0:
		limit = MaxMailList
	case limit < 0 || limit > MaxMailList:
		return nil, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "limit", Code: validate.CodeRange,
			Params: map[string]string{"min": "1", "max": fmt.Sprint(MaxMailList)},
			Reason: fmt.Sprintf("must be between 1 and %d", MaxMailList),
		})
	}
	addr, err := s.o.Addresses.EmailOf(ctx, a.Person)
	if err != nil {
		if errs.Is(err, errs.NotFound) {
			return []Mail{}, nil
		}
		return nil, fmt.Errorf("%s: viewer address: %w", op, err)
	}
	got, err := s.store.ListMail(ctx, strings.ToLower(string(addr)), limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	out := make([]Mail, len(got))
	for i, m := range got {
		out[i] = Mail{ID: MailID(m.ID), To: model.Email(m.To), Subject: m.Subject, Body: m.Body, CapturedAt: m.CapturedAt}
	}
	return out, nil
}
