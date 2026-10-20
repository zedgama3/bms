package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// The forgotten-password flow (identity.md, "Amendment 2026-10-19 (D39)").
const (
	// templatePasswordReset is the message RequestPasswordReset sends.
	templatePasswordReset notify.Template = "identity.password-reset"
	// resetLinkPath is the screen the e-mailed link opens; the token follows as a query.
	resetLinkPath = "/accounts/password-reset?token="
	// resetKeyPrefix starts the notify idempotency key of one reset e-mail.
	resetKeyPrefix = "password-reset:"
	// resetTokenLen is the length of a token in base64url without padding.
	resetTokenLen = 43 // base64.RawURLEncoding.EncodedLen(PasswordResetTokenBytes)
)

var (
	errNoNotify    = errors.New("password reset by e-mail is not set up in this deployment")
	errSendRefused = errors.New("the reset e-mail could not be queued")
)

// ResetRequest is the body of POST /password-reset.
type ResetRequest struct {
	Email model.Email `json:"email"`
}

// LogValue hides the address, so a ResetRequest passed to a logger prints nothing personal.
func (ResetRequest) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// ResetCompletion is the body of POST /password-reset/complete.
type ResetCompletion struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// LogValue hides both fields.
func (ResetCompletion) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// tokenHash is what the store keeps and finds a token by: the SHA-256 of the token text.
func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// newResetToken draws PasswordResetTokenBytes random bytes and returns them in base64url
// without padding. Fails errs.Internal when the random source fails.
func (s *Service) newResetToken() (string, error) {
	const op = "identity.Service.newResetToken"
	b := make([]byte, PasswordResetTokenBytes)
	if _, err := io.ReadFull(s.rand, b); err != nil {
		return "", errs.E(op, errs.Internal, fmt.Errorf("read random bytes: %w", err))
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RequestPasswordReset asks for a reset link for the account with this address. No session
// and no permission. Steps: validate the address (Invalid, field email, F69); count the
// request in both limiter buckets whatever its outcome (Unavailable wrapping
// httpx.ErrRateLimited, F70); Notify nil, or Notify.Configured false: Unsupported before
// the account lookup, for every address alike (F71), and a Configured failure fails with
// its kind, also before the lookup (F91); verify the dummy hash, exactly once for every
// request that gets this far, so no path costs less than another; a read-only copy fails
// Unavailable wrapping db.ErrReadOnly for every address alike (F78); look the account up.
// An unknown, a disabled and a superuser address record identity.password.reset-requested
// with Outcome Denied, send nothing and return nil (F73). Otherwise, in one Write
// transaction: replace the account's tokens by one (the SHA-256 of a fresh random token is
// stored, never the token), enqueue identity.password-reset with the link and the lifetime,
// and record the request; a Send (other than the cases below) or Record failure rolls
// everything back and fails with its kind (F76, F77). If Send answers Unsupported (the
// settings changed since the probe, F72), Invalid or NotFound (the e-mail cannot be queued
// for this account, for example it has no person row), nothing is committed and the answer
// is still nil, with one warn line that carries the account ID and no address. The token,
// the address and the link never reach a log, a span, a metric or an error.
func (s *Service) RequestPasswordReset(ctx context.Context, email model.Email) (err error) {
	const op = "identity.Service.RequestPasswordReset"
	ctx, span := s.start(ctx, "request-password-reset")
	defer endSpan(span, &err)

	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return err
	}
	v := validate.Errors{Op: op}
	addr := emailField(&v, email)
	if err := v.Err(); err != nil {
		return err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	// The same two buckets as sign-in, and every request counts once: nothing is taken
	// back, so the count cannot tell a known address from an unknown one.
	rv, err := s.reserve(ctx, op, pol, addr)
	if err != nil {
		return err
	}
	rv.keep()

	// Asked before the lookup, so the answer cannot depend on the address (F71, F91).
	if s.notify == nil {
		return errs.E(op, errs.Unsupported, errNoNotify)
	}
	configured, err := s.notify.Configured(ctx)
	if err != nil {
		return fmt.Errorf("%s: is e-mail configured: %w", op, err)
	}
	if !configured {
		return errs.E(op, errs.Unsupported, notify.ErrNotConfigured)
	}

	// One dummy verification for every request, whatever the address and whatever follows:
	// the known path would otherwise answer tens of milliseconds faster than the silent
	// ones, and under load only the silent ones could time out waiting for a hash slot.
	if err := s.spendDummy(ctx); err != nil {
		return err
	}
	if s.db.ReadOnly() {
		return errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	rec, err := s.store.ByCanonicalEmail(ctx, addr)
	matched := err == nil
	if err != nil && !errs.Is(err, errs.NotFound) {
		return fmt.Errorf("%s: %w", op, err)
	}
	if !matched || !rec.DisabledAt.IsZero() || rec.Superuser {
		return s.resetDenied(ctx, op, rec, matched)
	}
	return s.resetSend(ctx, op, pol, rec)
}

// spendDummy verifies the dummy hash once: the Argon2 cost every reset request pays.
func (s *Service) spendDummy(ctx context.Context) error {
	if s.dummyHook != nil {
		s.dummyHook()
	}
	if _, _, err := s.verify(ctx, "dummy password for timing only", s.dummy); err != nil {
		return err
	}
	return nil
}

// resetDenied records the request of an address that gets no e-mail and returns nil.
func (s *Service) resetDenied(ctx context.Context, op string, rec account.Record, matched bool) error {
	e := audit.Entry{Action: actionResetRequested, Outcome: audit.Denied, Actor: audit.Actor{Kind: audit.ActorAnonymous}}
	if matched {
		e.Subject = rec.Person
		e.Record = audit.RefOf(kindAccount, rec.ID)
	}
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error { return s.record(ctx, e) })
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// resetSend stores a fresh token for rec, enqueues its e-mail and records the request, all
// in one Write transaction.
func (s *Service) resetSend(ctx context.Context, op string, pol Policy, rec account.Record) error {
	token, err := s.newResetToken()
	if err != nil {
		return err
	}
	lifetime := pol.PasswordResetLifetime
	expires := s.now().Add(lifetime)
	var refused error
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		id, err := s.store.ReplaceResetToken(ctx, rec.ID, tokenHash(token), expires, s.now())
		if err != nil {
			return wrap(op, err)
		}
		err = s.notify.Send(ctx, notify.Message{
			Template: templatePasswordReset,
			To:       []model.PersonID{rec.Person},
			Key:      resetKeyPrefix + id.String(),
			Link:     resetLinkPath + token,
			Vars:     map[string]string{"lifetime": strconv.Itoa(int(lifetime/time.Minute)) + " minutes"},
			TTL:      lifetime,
		})
		switch {
		case errs.Is(err, errs.Unsupported), errs.Is(err, errs.Invalid), errs.Is(err, errs.NotFound):
			// Switched off since the probe, or the e-mail cannot be queued for this account
			// (no person row behind it, say). Roll back, so no token exists without its
			// e-mail, and answer like every other address.
			refused = err
			return errSendRefused
		case err != nil:
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionResetRequested, Outcome: audit.Succeeded,
			Actor:   audit.Actor{Kind: audit.ActorAnonymous},
			Subject: rec.Person, Record: audit.RefOf(kindAccount, rec.ID),
		})
	})
	switch {
	case errors.Is(err, errSendRefused):
		// Uniformity over honesty for this one request: saying so would tell the caller
		// that the address has an account. The line names the account by ID, never by
		// address, so an administrator can find the cause.
		s.logger.WarnContext(ctx, "a password reset e-mail could not be queued; nothing was sent",
			slog.String("account_id", rec.ID.String()), slog.String("error.kind", errs.KindOf(refused).String()))
		return nil
	case err != nil:
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// resetInvalid is the answer to a token that is not a live one.
func resetInvalid(op, code, reason string) error {
	return fieldInvalid(op, "token", code, reason)
}

// CompletePasswordReset redeems a token and sets a new password. No session. A read-only
// copy fails Unavailable wrapping db.ErrReadOnly (F85). The token is hashed and its row
// found without a lock: an unknown or used token, or one whose account is disabled or the
// superuser's, is Invalid on field token with code invalid (F79, F81, F84); an expired one
// is Invalid with code expired (F80). The new password is checked as ChangePassword checks
// it, field new_password (F82), and a refused password leaves the token valid; it is hashed
// before any transaction opens. Then, in one Write transaction, the account row is locked
// (the order ReplaceResetToken uses, so the two cannot deadlock) and read again: an account
// disabled since the lookup, or the superuser's, answers token: invalid and nothing changes;
// the token is marked used
// (the one statement that decides a race: two redemptions at once cannot both win), the
// password is set, password_change_required cleared and the session generation bumped (every
// session of the account ends), and identity.password.reset is recorded (F86). No session
// is issued. The token and the password never reach a log, a span or an error.
func (s *Service) CompletePasswordReset(ctx context.Context, token, newPassword string) (err error) {
	const op = "identity.Service.CompletePasswordReset"
	ctx, span := s.start(ctx, "complete-password-reset")
	defer endSpan(span, &err)

	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	if s.db.ReadOnly() {
		return errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return err
	}
	if len(token) != resetTokenLen {
		return resetInvalid(op, codeInvalid, "is not a valid reset token")
	}
	row, err := s.store.ResetByHash(ctx, tokenHash(token))
	switch {
	case errs.Is(err, errs.NotFound):
		return resetInvalid(op, codeInvalid, "is not a valid reset token")
	case err != nil:
		return fmt.Errorf("%s: %w", op, err)
	}
	now := s.now()
	switch {
	case !row.UsedAt.IsZero() || row.AccountDisabled:
		return resetInvalid(op, codeInvalid, "is not a valid reset token")
	case !row.ExpiresAt.After(now):
		return resetInvalid(op, codeExpired, "has expired")
	}
	span.SetAttrs(telemetry.ID("account_id", row.Account))
	st, err := s.store.StateByID(ctx, row.Account)
	switch {
	case errs.Is(err, errs.NotFound):
		return resetInvalid(op, codeInvalid, "is not a valid reset token")
	case err != nil:
		return fmt.Errorf("%s: %w", op, err)
	case st.Disabled || st.Superuser: // the superuser is reset by command only (D23)
		return resetInvalid(op, codeInvalid, "is not a valid reset token")
	}
	v := validate.Errors{Op: op}
	addPasswordProblems(&v, "new_password", newPassword, pol.PasswordMinLength, s.common)
	if err := v.Err(); err != nil {
		return err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	hash, err := s.hash(ctx, newPassword)
	if err != nil {
		return err
	}
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.CompleteReset(ctx, row.ID, row.Account, hash, now); err != nil {
			if errs.Is(err, errs.NotFound) { // used by another request first, or the account is gone
				return resetInvalid(op, codeInvalid, "is not a valid reset token")
			}
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionResetDone, Outcome: audit.Succeeded,
			Actor:   audit.Actor{Kind: audit.ActorPerson, Person: st.Person},
			Subject: st.Person, Record: audit.RefOf(kindAccount, row.Account),
		})
	})
	return wrap(op, err)
}
