package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// ChangePassword needs a Session (a restricted one is allowed; Unauthenticated otherwise,
// F25), verifies Current (Invalid, field current_password, code incorrect, F26), checks New
// (F11-F13), then in one Write transaction stores the hash, clears password_change_required,
// bumps the session generation and records identity.password.changed, and issues a new
// session with the new generation (every other session ends). A wrong Current counts in the
// sign-in limiter's buckets (the client address and the account's address): Unavailable
// wrapping httpx.ErrRateLimited once they are full. Fails Permission (field account, code
// protected) when the Guard protects the account (F103; checked before the password is
// verified, so it costs no limiter slot), the Guard's kind when it fails (F108), and
// Unavailable wrapping db.ErrReadOnly (F28).
func (s *Service) ChangePassword(ctx context.Context, c PasswordChange) (_ Issued, err error) {
	const op = "identity.Service.ChangePassword"
	ctx, span := s.start(ctx, "change-password")
	defer endSpan(span, &err)

	sess, ok := SessionFrom(ctx)
	if !ok {
		return Issued{}, errs.E(op, errs.Unauthenticated, errNoSession)
	}
	span.SetAttrs(telemetry.ID("account_id", sess.Account))
	pol, err := s.policy(ctx)
	if err != nil {
		return Issued{}, err
	}
	rec, err := s.store.ByID(ctx, sess.Account)
	switch {
	case errs.Is(err, errs.NotFound):
		return Issued{}, errs.E(op, errs.Unauthenticated, errNoSession)
	case err != nil:
		return Issued{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := s.checkProtected(ctx, op, rec.ID); err != nil {
		return Issued{}, err
	}
	if c.Current == "" {
		return Issued{}, fieldInvalid(op, "current_password", codeRequired, "must be set")
	}
	// A stolen session could guess the current password: the same two buckets as sign-in
	// count each wrong guess, keyed by the account's own address.
	rv, err := s.reserve(ctx, op, pol, rec.Email)
	if err != nil {
		return Issued{}, err
	}
	defer rv.release()
	correct, _, err := s.verify(ctx, c.Current, rec.PasswordHash)
	if err != nil {
		return Issued{}, err
	}
	if !correct {
		rv.keep()
		return Issued{}, fieldInvalid(op, "current_password", codeIncorrect, "is not the current password")
	}
	v := validate.Errors{Op: op}
	addPasswordProblems(&v, "new_password", c.New, pol.PasswordMinLength, s.common)
	if err := v.Err(); err != nil {
		return Issued{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	hash, err := s.hash(ctx, c.New)
	if err != nil {
		return Issued{}, err
	}

	var gen int32
	sid := model.NewID[SessionID]()
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var err error
		if gen, err = s.store.SetPassword(ctx, rec.ID, hash, s.now()); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionPasswordSet, Outcome: audit.Succeeded,
			Subject: rec.Person, Record: audit.RefOf(kindAccount, rec.ID),
		})
	})
	if err != nil {
		return Issued{}, notFoundAsUnauthenticated(op, err)
	}
	return s.issue(sid, rec.ID, rec.Person, rec.Superuser, false, gen, pol.lifetime(rec.Superuser)), nil
}

// EndOtherSessions needs a full Session (PersonFrom; Unauthenticated otherwise, F29), bumps
// the session generation, records identity.sessions.ended and issues a new session; every
// other session of the account ends. Fails Unavailable wrapping db.ErrReadOnly (F30).
func (s *Service) EndOtherSessions(ctx context.Context) (_ Issued, err error) {
	const op = "identity.Service.EndOtherSessions"
	ctx, span := s.start(ctx, "end-other-sessions")
	defer endSpan(span, &err)

	person, ok := PersonFrom(ctx)
	sess, _ := SessionFrom(ctx)
	if !ok {
		return Issued{}, errs.E(op, errs.Unauthenticated, errNoSession)
	}
	span.SetAttrs(telemetry.ID("account_id", sess.Account))
	pol, err := s.policy(ctx)
	if err != nil {
		return Issued{}, err
	}
	var gen int32
	sid := model.NewID[SessionID]()
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var err error
		if gen, err = s.store.BumpGeneration(ctx, sess.Account, s.now()); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionSessionsEnded, Outcome: audit.Succeeded,
			Subject: person, Record: audit.RefOf(kindSession, sid),
		})
	})
	if err != nil {
		return Issued{}, notFoundAsUnauthenticated(op, err)
	}
	return s.issue(sid, sess.Account, person, sess.Superuser, false, gen, pol.lifetime(sess.Superuser)), nil
}

var errNoSession = errors.New("a signed-in session is required")
var errNilWriter = errors.New("no writer to print the result to")

// fieldInvalid builds an errs.Invalid error carrying one validate.FieldError.
func fieldInvalid(op, field, code, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Reason: reason})
}

// notFoundAsUnauthenticated turns "the account vanished" into the answer for a session
// whose account is gone.
func notFoundAsUnauthenticated(op string, err error) error {
	if errs.Is(err, errs.NotFound) {
		return errs.E(op, errs.Unauthenticated, errNoSession)
	}
	return err
}
