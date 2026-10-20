package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// emailField parses the submitted address and, on failure, reports it under field "email"
// with code required (empty) or malformed (any other shape problem), never echoing it.
func emailField(v *validate.Errors, e model.Email) model.Email {
	parsed, err := model.ParseEmail(string(e))
	if err == nil {
		return parsed
	}
	code, reason := codeMalformed, "must be a valid e-mail address"
	if fes := validate.Fields(err); len(fes) > 0 && fes[0].Code == codeRequired {
		code, reason = codeRequired, "must be set"
	}
	v.Addf("email", code, "%s", reason)
	return ""
}

// reservation is one attempt counted in the limiter's buckets before the password is
// checked. It stays counted when the attempt failed and is taken back (forgive) when it did
// not: a success, or an error that says nothing about the password.
type reservation struct {
	l     *limiter
	addr  netip.Addr
	email string
	at    time.Time
	kept  bool
}

// reserve counts an attempt for the client address (limit Policy.ClientFailureLimit) and the
// canonical address (limit Policy.LoginFailureLimit), or fails errs.Unavailable wrapping
// httpx.ErrRateLimited when either bucket is full (F14, F18).
func (s *Service) reserve(ctx context.Context, op string, pol Policy, email model.Email) (*reservation, error) {
	return s.reserveIn(ctx, op, s.limiter, email, pol.ClientFailureLimit, pol.LoginFailureLimit, pol.LoginFailureWindow)
}

// reserveIn counts an attempt in the buckets of l, with the given limits per client address
// and per canonical address inside window, or fails errs.Unavailable wrapping
// httpx.ErrRateLimited when either bucket is full. The metric names the bucket that was.
func (s *Service) reserveIn(ctx context.Context, op string, l *limiter, email model.Email, addrLimit, emailLimit int, window time.Duration) (*reservation, error) {
	addr, _ := httpx.ClientAddrFrom(ctx)
	rv := &reservation{l: l, addr: addr, email: string(email.Canonical())}
	at, bucket, ok := l.reserve(rv.addr, rv.email, s.now, addrLimit, emailLimit, window)
	if !ok {
		s.metrics.limit(ctx, bucket)
		return nil, errs.E(op, errs.Unavailable, httpx.ErrRateLimited)
	}
	rv.at = at
	return rv, nil
}

// keep makes the attempt count as a failure.
func (r *reservation) keep() { r.kept = true }

// release takes the attempt back unless keep was called. Call it with defer.
func (r *reservation) release() {
	if !r.kept {
		r.l.forgive(r.addr, r.email, r.at)
		r.kept = true
	}
}

// issue builds the session and its token. The times are whole seconds, as the token holds
// them. Call it only after whatever the session depends on has committed.
func (s *Service) issue(sid SessionID, acct model.AccountID, person model.PersonID, superuser, restricted bool, gen int32, life time.Duration) Issued {
	issued := s.now().Truncate(time.Second)
	sess := Session{
		ID: sid, Account: acct, Person: person, Superuser: superuser,
		PasswordChangeRequired: restricted, IssuedAt: issued, ExpiresAt: issued.Add(life),
	}
	tok := s.sign(claims{account: acct, session: sid, generation: gen, issued: issued, expires: sess.ExpiresAt})
	return Issued{Session: sess, token: tok}
}

// SignUp creates an account and signs it in. Steps: policy (Unsupported when invitation,
// F9); validate (F10-F13); limiter by address (F14); in one Write transaction insert the
// account (Conflict with field email, code taken, F15), publish identity.account-created and
// record identity.account.created with the new person as the actor; then issue a session
// (generation 0). Fails Unavailable wrapping db.ErrReadOnly on a read-only copy (F16),
// Unavailable wrapping httpx.ErrRateLimited, store, signal and audit kinds (F57, F58).
func (s *Service) SignUp(ctx context.Context, c Credentials) (_ Issued, err error) {
	const op = "identity.Service.SignUp"
	ctx, span := s.start(ctx, "sign-up")
	defer endSpan(span, &err)

	pol, err := s.policy(ctx)
	if err != nil {
		return Issued{}, err
	}
	if pol.SignupMode != SignupOpen {
		return Issued{}, errs.E(op, errs.Unsupported, errors.New("sign-up is by invitation in this deployment"))
	}
	v := validate.Errors{Op: op}
	email := emailField(&v, c.Email)
	addPasswordProblems(&v, "password", c.Password, pol.PasswordMinLength, s.common)
	if err := v.Err(); err != nil {
		return Issued{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	rv, err := s.reserve(ctx, op, pol, email)
	if err != nil {
		return Issued{}, err
	}
	defer rv.release()
	hash, err := s.hash(ctx, c.Password)
	if err != nil {
		return Issued{}, err
	}

	now := s.now()
	rec := account.Record{
		ID: model.NewID[model.AccountID](), Person: model.NewID[model.PersonID](), Email: email,
		PasswordHash: hash, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := rec.Validate(); err != nil {
		return Issued{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	span.SetAttrs(telemetry.ID("account_id", rec.ID))
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.Insert(ctx, rec); err != nil {
			return wrap(op, err)
		}
		if err := s.publishCreated(ctx, rec.ID, rec.Person); err != nil {
			return err
		}
		return s.record(ctx, audit.Entry{
			Action: actionCreated, Outcome: audit.Succeeded,
			Actor:   audit.Actor{Kind: audit.ActorPerson, Person: rec.Person},
			Subject: rec.Person, Record: audit.RefOf(kindAccount, rec.ID),
		})
	})
	if err != nil {
		if isTaken(err) {
			rv.keep()
			return Issued{}, errs.E(op, errs.Conflict, &validate.FieldError{
				Field: "email", Code: codeTaken, Reason: "an account with this address already exists",
			})
		}
		return Issued{}, wrap(op, err)
	}
	return s.issue(model.NewID[SessionID](), rec.ID, rec.Person, false, false, 0, pol.lifetime(false)), nil
}

// isTaken reports a unique violation on the address.
func isTaken(err error) bool {
	var de *db.Error
	return errs.Is(err, errs.Conflict) && errors.As(err, &de) && de.Constraint == emailKey
}

// publishCreated publishes identity.account-created inside the caller's transaction.
func (s *Service) publishCreated(ctx context.Context, acct model.AccountID, person model.PersonID) error {
	return wrap("identity.Service.publishCreated", s.signals.Publish(ctx, SignalAccountCreated, signals.NewPayload(
		signals.Ref(PayloadAccount, acct), signals.Ref(PayloadPerson, person))))
}

// SignIn verifies c and issues a session. Steps: validate (F17); limiter by client address
// and by submitted address (F18); load by address (compared as lower() in SQL); verify the
// password (a dummy hash when none matched, F19); a disabled account answers like a wrong
// password (F20, outcome Denied in the audit entry); on success TouchLogin (db.ErrReadOnly
// ignored, F21), record identity.login (identity.superuser.login for the superuser), alert
// when superuser, issue a token (superuser: Policy.SuperuserSessionLifetime). A failure
// records identity.login-failed, counts in both buckets and answers Unauthenticated. Never
// the address in a log, span, metric or audit entry. Fails Unauthenticated, Invalid,
// Unavailable wrapping httpx.ErrRateLimited, store kinds (F22), audit kinds (F23).
func (s *Service) SignIn(ctx context.Context, c Credentials) (_ Issued, err error) {
	const op = "identity.Service.SignIn"
	ctx, span := s.start(ctx, "sign-in")
	defer endSpan(span, &err)

	pol, err := s.policy(ctx)
	if err != nil {
		return Issued{}, err
	}
	v := validate.Errors{Op: op}
	email := emailField(&v, c.Email)
	if c.Password == "" {
		v.Addf("password", codeRequired, "must be set")
	}
	if err := v.Err(); err != nil {
		return Issued{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	rv, err := s.reserve(ctx, op, pol, email)
	if err != nil {
		s.metrics.login(ctx, "rate_limited")
		span.SetAttrs(telemetry.Token("outcome", "rate_limited"))
		return Issued{}, err
	}
	defer rv.release() // only a verdict of "not right" keeps the attempt counted

	rec, err := s.store.ByCanonicalEmail(ctx, email)
	matched := err == nil
	if err != nil && !errs.Is(err, errs.NotFound) {
		return Issued{}, fmt.Errorf("%s: %w", op, err)
	}
	var correct, weak bool
	if matched {
		span.SetAttrs(telemetry.ID("account_id", rec.ID))
		correct, weak, err = s.verify(ctx, c.Password, rec.PasswordHash)
	} else {
		_, _, err = s.verify(ctx, c.Password, s.dummy)
	}
	if err != nil {
		return Issued{}, err
	}
	switch {
	case !matched || !correct:
		span.SetAttrs(telemetry.Token("outcome", "bad_credentials"))
		return Issued{}, s.loginFailed(ctx, op, rv, rec, matched, audit.Failed, "bad_credentials")
	case !rec.DisabledAt.IsZero():
		span.SetAttrs(telemetry.Token("outcome", "disabled"))
		return Issued{}, s.loginFailed(ctx, op, rv, rec, matched, audit.Denied, "disabled")
	}

	now := s.now()
	if !s.db.ReadOnly() {
		if err := s.store.TouchLogin(ctx, rec.ID, now); err != nil && !errors.Is(err, db.ErrReadOnly) {
			return Issued{}, fmt.Errorf("%s: %w", op, err)
		}
		if weak {
			s.rehash(ctx, rec, c.Password)
		}
	}

	sid := model.NewID[SessionID]()
	action, outcome := actionLogin, "ok"
	switch {
	case rec.Superuser:
		action, outcome = actionSuperuserLogin, "superuser"
	case rec.PasswordChangeRequired:
		outcome = "restricted"
	}
	if err := s.record(ctx, audit.Entry{
		Action: action, Outcome: audit.Succeeded,
		Actor:   audit.Actor{Kind: audit.ActorPerson, Person: rec.Person, Superuser: rec.Superuser},
		Subject: rec.Person, Record: audit.RefOf(kindSession, sid),
	}); err != nil {
		return Issued{}, err
	}
	s.metrics.login(ctx, outcome)
	span.SetAttrs(telemetry.Token("outcome", outcome))
	if rec.Superuser {
		s.alertSuperuser(ctx, rec.Person)
	}
	return s.issue(sid, rec.ID, rec.Person, rec.Superuser, rec.PasswordChangeRequired, rec.Generation, pol.lifetime(rec.Superuser)), nil
}

// loginFailed keeps the attempt counted, records identity.login-failed (the account when one
// matched) and returns what SignIn answers: Unauthenticated, or the audit error when the
// entry could not be written. The answer never says which of the failures it was.
func (s *Service) loginFailed(ctx context.Context, op string, rv *reservation, rec account.Record, matched bool, outcome audit.Outcome, label string) error {
	rv.keep()
	s.metrics.login(ctx, label)
	e := audit.Entry{Action: actionLoginFailed, Outcome: outcome, Actor: audit.Actor{Kind: audit.ActorAnonymous}}
	if matched {
		e.Record = audit.RefOf(kindAccount, rec.ID)
	}
	if err := s.record(ctx, e); err != nil {
		return err
	}
	return errs.E(op, errs.Unauthenticated, errors.New("the address or password is not right"))
}

// rehash upgrades a hash made with weaker parameters. It never fails the sign-in: the
// password was right, and the old hash still verifies.
func (s *Service) rehash(ctx context.Context, rec account.Record, pw string) {
	fresh, err := s.hash(ctx, pw)
	if err == nil {
		err = s.store.Rehash(ctx, rec.ID, fresh, rec.PasswordHash)
	}
	if err != nil && !errors.Is(err, db.ErrReadOnly) {
		s.logger.WarnContext(ctx, "could not upgrade a password hash",
			slog.String("account_id", rec.ID.String()), slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// alertSuperuser tells the Alerter, or logs one warn line when there is none. A failure
// is logged and counted and never fails the sign-in (F27): the audit entry is the record.
func (s *Service) alertSuperuser(ctx context.Context, person model.PersonID) {
	if s.alerter == nil {
		s.logger.WarnContext(ctx, "superuser signed in", slog.String("person_id", person.String()))
		return
	}
	if err := s.alerter.SuperuserSignedIn(ctx, person); err != nil {
		s.metrics.alertFailure(ctx)
		s.logger.WarnContext(ctx, "could not alert about a superuser sign-in",
			slog.String("person_id", person.String()), slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// SignOut records identity.logout for the session in ctx (a restricted one included). No
// session: nil, nothing recorded. The handler clears the cookie before calling it
// (decision 10), so a failed audit write still leaves the browser signed out. Works on a
// read-only copy (the entry spools). Fails with the audit kind (F24).
func (s *Service) SignOut(ctx context.Context) (err error) {
	ctx, span := s.start(ctx, "sign-out")
	defer endSpan(span, &err)

	sess, ok := SessionFrom(ctx)
	if !ok {
		return nil
	}
	span.SetAttrs(telemetry.ID("account_id", sess.Account))
	return s.record(ctx, audit.Entry{
		Action: actionLogout, Outcome: audit.Succeeded,
		Actor:   audit.Actor{Kind: audit.ActorPerson, Person: sess.Person, Superuser: sess.Superuser},
		Subject: sess.Person, Record: audit.RefOf(kindSession, sess.ID),
	})
}
