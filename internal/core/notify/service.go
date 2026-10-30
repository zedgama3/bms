package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/core/notify/postgres"
	"github.com/zedgama3/bms/internal/core/notify/smtp"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap.
type store interface {
	// Enqueue inserts a queued row; false means the (owner, key, recipient) exists.
	Enqueue(ctx context.Context, r message.Record, now time.Time) (bool, error)
	// Lease takes up to limit due rows (Attempts counts this attempt; LeasedUntil is the
	// fence for the four statements below).
	Lease(ctx context.Context, now time.Time, limit int, leasedUntil time.Time) ([]message.Record, error)
	// MarkSent, MarkFailed, MarkSkipped, Requeue and Release end or move a leased row; false means the
	// row is gone or was re-leased.
	MarkSent(ctx context.Context, id message.ID, lease, at time.Time) (bool, error)
	MarkFailed(ctx context.Context, id message.ID, lease time.Time, token string, at time.Time) (bool, error)
	// MarkSkipped ends a leased row as skipped (no address) and clears its text.
	MarkSkipped(ctx context.Context, id message.ID, lease time.Time, token string, at time.Time) (bool, error)
	Requeue(ctx context.Context, id message.ID, lease time.Time, token string, next time.Time) (bool, error)
	Release(ctx context.Context, id message.ID, lease, now time.Time, token string) (bool, error)
	// Retry requeues a failed row that still has its text; Cancel deletes a queued or
	// failed row. False means nothing matched; StatusOf says why.
	Retry(ctx context.Context, id message.ID, now time.Time) (bool, error)
	Cancel(ctx context.Context, id message.ID) (bool, error)
	StatusOf(ctx context.Context, id message.ID) (message.State, error)
	CountByStatus(ctx context.Context) (map[message.Status]int, error)
	CountOpenByStatus(ctx context.Context) (map[message.Status]int, error)
	ListOutbox(ctx context.Context, limit int) ([]message.Record, error)
	// Purge deletes final rows completed before a time; ExpireStale ends rows past
	// their expiry and clears their text.
	Purge(ctx context.Context, before time.Time, limit int) (int64, error)
	ExpireStale(ctx context.Context, now time.Time, limit int) (changed, expired int64, err error)
	ExportPerson(ctx context.Context, person model.PersonID) ([]message.Exported, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int64, error)
	Override(ctx context.Context, name, lang string) (message.Override, error)
	Overrides(ctx context.Context) ([]message.Override, error)
	PutOverride(ctx context.Context, o message.Override) error
	ResetOverride(ctx context.Context, name, lang string) (bool, error)
}

var _ store = (*postgres.Store)(nil)

// backoff is the retry schedule: 1m, doubling to 1h, MaxAttempts in all.
var backoff = retry.Policy{Initial: time.Minute, Max: time.Hour, Multiplier: 2, MaxAttempts: MaxAttempts}

// The tokens a row's last_error and the delivered metric take.
const (
	tokenConnect   = "connect"
	tokenAuth      = "auth"
	tokenTimeout   = "timeout"
	tokenSMTP4xx   = "smtp_4xx" //nolint:gosec // a failure token, not a credential
	tokenRejected  = "rejected"
	tokenNoAddress = "no_address"
	tokenCanceled  = "canceled"
	tokenExpired   = "expired"
	tokenAddress   = "address" // the resolved address is malformed
	tokenLookup    = "lookup"  // the address lookup failed (retried)
)

// Service is notify. Safe for concurrent use; the one goroutine is Run's, owned by the
// caller.
type Service struct {
	store  store
	db     db.DB
	clock  clock.Clock
	logger *slog.Logger
	tracer telemetry.Tracer
	audit  audit.Recorder
	authz  authz.Authorizer
	actorF authz.ActorFunc

	smtp      SMTPSource
	site      SiteSource
	retention RetentionSource
	addresses AddressSource
	languages LanguageSource
	names     NameSource
	channel   ChannelFactory

	poll, lease, sendTimeout time.Duration
	hostname                 string

	cat *catalogue

	runMu   sync.Mutex
	running atomic.Bool
	stop    chan struct{} // closed by Close
	done    chan struct{} // closed when Run returns; nil until Run starts
	stopped sync.Once

	gauge atomic.Pointer[map[Status]int]

	logMu       sync.Mutex
	noSMTPLogAt time.Time // when the "messages wait" line was last logged
	unhealthy   bool      // the loop is failing (log once per change)
	readOnly    bool      // the last tick found a read-only copy

	metrics metrics
}

// New validates o and builds the service over postgres.Store, registers notify's own
// templates (Templates) and the metrics. No I/O: it works against db.None() and on a
// read-only copy. Fails errs.Invalid.
func New(o Options) (*Service, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("notify.New: %w", err)
	}
	return newService(o, st)
}

// newService builds the service over st. Tests hand it a fake store.
func newService(o Options, st store) (*Service, error) {
	const op = "notify.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	s := &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer, audit: o.Audit,
		authz: o.Authz, actorF: o.Actor, smtp: o.SMTP, site: o.Site, retention: o.Retention,
		addresses: o.Addresses, languages: o.Languages, names: o.Names, channel: o.Channel,
		poll: o.PollInterval, lease: o.Lease, sendTimeout: o.SendTimeout, hostname: o.Hostname,
		cat: newCatalogue(), stop: make(chan struct{}),
	}
	if s.poll == 0 {
		s.poll = DefaultPollInterval
	}
	if s.lease == 0 {
		s.lease = DefaultLease
	}
	if s.sendTimeout == 0 {
		s.sendTimeout = DefaultSendTimeout
	}
	// A lease must outlast at least one send, or the sender could never start one.
	s.lease = max(s.lease, 2*s.sendTimeout)
	if s.channel == nil {
		s.channel = smtp.Factory(smtp.Options{SendTimeout: s.sendTimeout, Hostname: s.hostname, Clock: s.clock})
	}
	m, err := newMetrics(o.Meter, s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	s.metrics = m
	if err := s.Register(ownerNotify, Templates); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return s, nil
}

// ownerNotify is the owner of notify's own template and audit area.
const ownerNotify = "notify"

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "notify.<name>".
func (s *Service) start(ctx context.Context, name string, opts ...telemetry.StartOption) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "notify."+name, opts...)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// ---- metrics ----------------------------------------------------------------------------

// The outcomes bms.notify.delivered takes.
const (
	outcomeOK       = "ok"
	outcomeRetry    = "retry"
	outcomeFailed   = "failed"
	outcomeRejected = "rejected"
	outcomeSkipped  = "skipped"
	outcomeNoSMTP   = "no_smtp"
	outcomeReadOnly = "read_only"
	outcomeExpired  = "expired"
)

// metrics holds the instruments; each method is a no-op without a Meter.
type metrics struct {
	enqueued  telemetry.Counter
	delivered telemetry.Counter
	purged    telemetry.Counter
}

func newMetrics(m telemetry.Meter, s *Service) (metrics, error) {
	if m == nil {
		return metrics{}, nil
	}
	var (
		out metrics
		err error
	)
	if out.enqueued, err = m.Counter("bms.notify.enqueued", telemetry.WithUnit("{message}"),
		telemetry.WithDescription("Messages queued, by owner.")); err != nil {
		return metrics{}, fmt.Errorf("notify.New: %w", err)
	}
	if out.delivered, err = m.Counter("bms.notify.delivered", telemetry.WithUnit("{message}"),
		telemetry.WithDescription("Delivery outcomes of the sender.")); err != nil {
		return metrics{}, fmt.Errorf("notify.New: %w", err)
	}
	if out.purged, err = m.Counter("bms.notify.purged", telemetry.WithUnit("{message}"),
		telemetry.WithDescription("Status rows deleted by the retention purge.")); err != nil {
		return metrics{}, fmt.Errorf("notify.New: %w", err)
	}
	err = m.Gauge("bms.notify.outbox", func(context.Context) []telemetry.Sample {
		counts := s.gauge.Load()
		if counts == nil {
			return nil
		}
		out := make([]telemetry.Sample, 0, 3)
		for _, st := range []Status{Queued, Sending, Failed} {
			out = append(out, telemetry.Sample{Value: float64((*counts)[st]), Attrs: []telemetry.Attr{telemetry.Token("status", st.String())}})
		}
		return out
	}, telemetry.WithUnit("{message}"), telemetry.WithDescription("Open outbox rows by status."))
	if err != nil {
		return metrics{}, fmt.Errorf("notify.New: %w", err)
	}
	return out, nil
}

func (m metrics) enqueue(ctx context.Context, owner string, n int) {
	if m.enqueued != nil && n > 0 {
		m.enqueued.Add(ctx, int64(n), telemetry.Token("owner", owner))
	}
}

func (m metrics) deliver(ctx context.Context, outcome string, n int) {
	if m.delivered != nil && n > 0 {
		m.delivered.Add(ctx, int64(n), telemetry.Token("outcome", outcome))
	}
}

func (m metrics) purge(ctx context.Context, n int64) {
	if m.purged != nil && n > 0 {
		m.purged.Add(ctx, n)
	}
}
