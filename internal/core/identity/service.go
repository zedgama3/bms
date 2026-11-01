package identity

import (
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/core/identity/postgres"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// hkdfInfo is the context string the signing key is derived under (config decision 7).
const hkdfInfo = "bms identity session v1"

// store is the storage the service declares (D8); postgres.Store implements it. Every
// error is already classified by db.Wrap. A unique violation on account_email_canonical_key
// is Conflict with *db.Error.Constraint set. The per-purpose writes are single statements,
// so a concurrent change cannot be lost to a read-modify-write.
type store interface {
	Insert(ctx context.Context, r account.Record) error
	ByID(ctx context.Context, id model.AccountID) (account.Record, error)       // with the hash: to verify a password
	ByIDNoHash(ctx context.Context, id model.AccountID) (account.Record, error) // without it: the admin reads
	ByCanonicalEmail(ctx context.Context, e model.Email) (account.Record, error)
	ByPerson(ctx context.Context, p model.PersonID) (account.Record, error)
	StateByID(ctx context.Context, id model.AccountID) (account.State, error)
	List(ctx context.Context, after account.Key, limit int) ([]account.Record, error)
	ListByEmail(ctx context.Context, e model.Email) ([]account.Record, error)
	ExportByPerson(ctx context.Context, p model.PersonID) (account.Record, error)
	DeleteByPerson(ctx context.Context, p model.PersonID) (model.AccountID, int, error)
	SetPerson(ctx context.Context, id model.AccountID, person model.PersonID, at time.Time) (model.PersonID, error)
	SetPassword(ctx context.Context, id model.AccountID, hash secret.Secret, at time.Time) (int32, error)
	SetTemporaryPassword(ctx context.Context, id model.AccountID, hash secret.Secret, at time.Time) (int32, error)
	BumpGeneration(ctx context.Context, id model.AccountID, at time.Time) (int32, error)
	Disable(ctx context.Context, id model.AccountID, at time.Time) (bool, error)
	Enable(ctx context.Context, id model.AccountID, at time.Time) (bool, error)
	TouchLogin(ctx context.Context, id model.AccountID, at time.Time) error
	Rehash(ctx context.Context, id model.AccountID, newHash, oldHash secret.Secret) error
	Superuser(ctx context.Context, id model.AccountID, person model.PersonID, e model.Email, hash secret.Secret, at time.Time) (account.Superuser, error)
	// EnsureAccount is Superuser's sibling for a participant account (D53): it creates the
	// account or replaces the password of the participant account that holds the address,
	// and fails Conflict when the superuser's account holds it.
	EnsureAccount(ctx context.Context, id model.AccountID, person model.PersonID, e model.Email, hash secret.Secret, at time.Time) (account.Ensured, error)

	// The forgotten-password flow (D39). hash is the SHA-256 of the token; the token itself
	// never reaches the store.

	// ReplaceResetToken locks the account, deletes every token it has (used, expired and
	// the earlier live one: only the newest is valid) and inserts this one, returning its
	// ID. now is the new row's created_at. NotFound when the account is gone. Runs inside
	// the caller's transaction.
	ReplaceResetToken(ctx context.Context, acct model.AccountID, hash []byte, expires, now time.Time) (account.ResetID, error)
	// ResetByHash finds a token by its hash without locking anything; NotFound when there
	// is none.
	ResetByHash(ctx context.Context, hash []byte) (account.ResetRow, error)
	// ResetState locks the account row (the order ReplaceResetToken uses) and returns the
	// newest created_at among the account's reset rows (used or not; zero when none: the
	// cooldown, D40) with the account's disabled and superuser flags read under the lock.
	// NotFound when the account is gone. Call it inside the Write transaction that goes on
	// to replace the token.
	ResetState(ctx context.Context, acct model.AccountID) (account.ResetState, error)
	// CompleteReset locks the account row (the order ReplaceResetToken uses), refuses
	// (NotFound) an account that is now disabled or the superuser's, marks the
	// token used and then sets the account's password (clearing password_change_required
	// and bumping the session generation); NotFound when the token was already used, so two
	// concurrent redemptions cannot both win, and before the password is touched.
	CompleteReset(ctx context.Context, id account.ResetID, acct model.AccountID, passwordHash secret.Secret, now time.Time) error
}

var _ store = (*postgres.Store)(nil)

// Options builds the service. app copies config.Session.Key and config.HTTP.AllowPlaintext
// in.
type Options struct {
	DB             db.DB
	Clock          clock.Clock
	Logger         *slog.Logger
	Tracer         telemetry.Tracer
	Meter          telemetry.Meter // optional: nil records no metrics
	Key            secret.Secret   // config Session.Key; required
	AllowPlaintext bool            // config HTTP.AllowPlaintext: may write DevCookieName (api decision 24)
	Audit          audit.Recorder  // scoped to "identity"; required
	Signals        signals.Bus     // scoped to "identity", publishes SignalAccountCreated; required
	Settings       settings.Reader // scoped "accounts"; required; the policy is read per use case, never cached
	Permissions    PermissionChecker
	Lister         PermissionLister
	Names          NameSource
	Holders        HolderSource       // the authz.Lookup; required: the superuser alert's holders of accounts.manage
	Notify         notify.Notifier    // scoped to "identity"; required: the password reset and the alert's holders
	Alerts         notify.AlertSender // scoped to "identity"; required: the superuser alert's alert addresses
	Guard          Guard              // optional; nil protects nothing (D53, demo mode only)
	Lookup         *Lookup            // EmailOf; built before notify; required
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: "identity.db",
// "identity.clock", "identity.logger", "identity.tracer", "identity.audit",
// "identity.signals", "identity.settings", "identity.holders", "identity.notify", "identity.alerts", "identity.lookup" (nil) and "session.key" (unset).
func (o Options) Validate() error {
	v := validate.Errors{Op: "identity.Options.Validate"}
	if o.DB == nil {
		v.Addf("identity.db", codeRequired, "must be set")
	}
	if o.Clock == nil {
		v.Addf("identity.clock", codeRequired, "must be set")
	}
	if o.Logger == nil {
		v.Addf("identity.logger", codeRequired, "must be set")
	}
	if o.Tracer == nil {
		v.Addf("identity.tracer", codeRequired, "must be set")
	}
	if o.Audit == nil {
		v.Addf("identity.audit", codeRequired, "must be set")
	}
	if o.Signals == nil {
		v.Addf("identity.signals", codeRequired, "must be set")
	}
	if o.Settings == nil {
		v.Addf("identity.settings", codeRequired, "must be set")
	}
	if o.Holders == nil {
		v.Addf("identity.holders", codeRequired, "must be set")
	}
	if o.Notify == nil {
		v.Addf("identity.notify", codeRequired, "must be set")
	}
	if o.Alerts == nil {
		v.Addf("identity.alerts", codeRequired, "must be set")
	}
	if o.Lookup == nil {
		v.Addf("identity.lookup", codeRequired, "must be set")
	}
	if !o.Key.IsSet() {
		v.Addf("session.key", codeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Service is identity. Safe for concurrent use; nothing to close (no goroutines).
type Service struct {
	store          store
	db             db.DB
	clock          clock.Clock
	logger         *slog.Logger
	tracer         telemetry.Tracer
	key            []byte
	allowPlaintext bool
	audit          audit.Recorder
	signals        signals.Bus
	settings       settings.Reader
	perms          PermissionChecker
	lister         PermissionLister
	names          NameSource
	holders        HolderSource
	notify         notify.Notifier
	alerts         notify.AlertSender
	guard          Guard
	lookup         *Lookup

	common  map[string]struct{}
	dummy   secret.Secret // verified when no account matched, so the answer costs the same
	limiter *limiter      // sign-in, sign-up and ChangePassword
	// resetLimiter holds the buckets of password-reset requests only (D40).
	resetLimiter *limiter
	sem          chan struct{} // HashConcurrency slots; a hash holds one while it runs
	rand         io.Reader     // crypto/rand.Reader; tests replace it to fail
	// dummyHook is called once per dummy verification of a reset request; tests count with it.
	dummyHook func()
	metrics   metrics
}

// New validates o, derives the signing key (HKDF-SHA256, info "bms identity session v1"),
// loads the embedded common-password list and returns the service over postgres.Store. No
// I/O: it works against db.None() (app decision 7) and on a read-only copy. Fails
// errs.Invalid (F1), errs.Internal when the embedded list is unreadable, and the meter's
// kind when an instrument cannot be created.
func New(o Options) (*Service, error) {
	const op = "identity.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return newService(o, st)
}

// newService builds the service over st. Tests hand it a fake store.
func newService(o Options, st store) (*Service, error) {
	const op = "identity.New"
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil store"))
	}
	key, err := hkdf.Key(sha256.New, []byte(o.Key.Value()), nil, hkdfInfo, sha256.Size)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("derive the signing key: %w", err))
	}
	common, err := commonSet()
	if err != nil {
		return nil, err
	}
	m, err := newMetrics(o.Meter)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &Service{
		store: st, db: o.DB, clock: o.Clock, logger: o.Logger, tracer: o.Tracer,
		key: key, allowPlaintext: o.AllowPlaintext, audit: o.Audit, signals: o.Signals,
		settings: o.Settings, perms: o.Permissions, lister: o.Lister, names: o.Names,
		holders: o.Holders, notify: o.Notify, alerts: o.Alerts, guard: o.Guard, lookup: o.Lookup,
		common: common, dummy: dummyHash(), limiter: newLimiter(), resetLimiter: newResetLimiter(), sem: make(chan struct{}, HashConcurrency),
		rand: rand.Reader, metrics: m,
	}, nil
}

// now is the clock's time in UTC.
func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// start opens the span of one use case, "identity.<use-case>".
func (s *Service) start(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, "identity."+name)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// record writes entries through the audit recorder; the error keeps audit's kind.
func (s *Service) record(ctx context.Context, entries ...audit.Entry) error {
	return wrap("identity.Service.record", s.audit.Record(ctx, entries...))
}

// wrap adds op to err and keeps its kind and its chain (errors.Is still finds
// db.ErrReadOnly and httpx.ErrRateLimited). nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// metrics holds the instruments; each method is a no-op without a Meter.
type metrics struct {
	logins      telemetry.Counter
	rejected    telemetry.Counter
	limited     telemetry.Counter
	alertFailed telemetry.Counter
}

func newMetrics(m telemetry.Meter) (metrics, error) {
	if m == nil {
		return metrics{}, nil
	}
	var (
		out metrics
		err error
	)
	if out.logins, err = m.Counter("bms.identity.logins", telemetry.WithUnit("{login}"),
		telemetry.WithDescription("Sign-in attempts by outcome.")); err != nil {
		return metrics{}, fmt.Errorf("identity.New: %w", err)
	}
	if out.rejected, err = m.Counter("bms.identity.sessions_rejected", telemetry.WithUnit("{request}"),
		telemetry.WithDescription("Requests whose session token was refused, by reason.")); err != nil {
		return metrics{}, fmt.Errorf("identity.New: %w", err)
	}
	if out.limited, err = m.Counter("bms.identity.rate_limited", telemetry.WithUnit("{request}"),
		telemetry.WithDescription("Sign-in, sign-up and password-reset attempts refused by the limiter, by bucket.")); err != nil {
		return metrics{}, fmt.Errorf("identity.New: %w", err)
	}
	if out.alertFailed, err = m.Counter("bms.identity.alert_failures", telemetry.WithUnit("{alert}"),
		telemetry.WithDescription("Superuser sign-in alerts that could not be sent.")); err != nil {
		return metrics{}, fmt.Errorf("identity.New: %w", err)
	}
	return out, nil
}

func (m metrics) login(ctx context.Context, outcome string) {
	if m.logins != nil {
		m.logins.Add(ctx, 1, telemetry.Token("outcome", outcome))
	}
}

func (m metrics) reject(ctx context.Context, reason string) {
	if m.rejected != nil {
		m.rejected.Add(ctx, 1, telemetry.Token("reason", reason))
	}
}

func (m metrics) limit(ctx context.Context, bucket string) {
	if m.limited != nil {
		m.limited.Add(ctx, 1, telemetry.Token("bucket", bucket))
	}
}

func (m metrics) alertFailure(ctx context.Context) {
	if m.alertFailed != nil {
		m.alertFailed.Add(ctx, 1)
	}
}
