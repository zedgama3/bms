package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Authenticate implements api.Authenticator. It reads CookieName, or DevCookieName only when
// AllowPlaintext is set and the effective scheme is http, verifies the token, reads the account's state (one statement, no
// transaction, no write) and returns the ctx with the Session, the person, and nil. No
// cookie: (ctx, zero, nil). A restricted session (a temporary password): the Session is in
// ctx but the person is zero (decision 9), so every other signed-in route answers 401.
// The Authorization header is ignored. Fails errs.Unauthenticated for a malformed,
// tampered, expired, revoked (generation), disabled or deleted session (F2-F6); the
// store's kind otherwise (F7). Never returns a nil ctx.
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (_ context.Context, _ model.PersonID, err error) {
	const op = "identity.Service.Authenticate"
	tok := s.readToken(ctx, r)
	if tok == "" {
		return ctx, model.PersonID{}, nil
	}
	base := ctx // the returned ctx derives from the caller's, not from the ended span's
	ctx, span := s.start(ctx, "authenticate")
	defer endSpan(span, &err)

	now := s.now()
	c, err := s.verifyToken(tok, now)
	if err != nil {
		s.metrics.reject(ctx, reasonOf(err))
		return base, model.PersonID{}, err
	}
	span.SetAttrs(telemetry.ID("account_id", c.account))
	st, err := s.store.StateByID(ctx, c.account)
	switch {
	case errs.Is(err, errs.NotFound):
		s.metrics.reject(ctx, "unknown")
		return base, model.PersonID{}, errs.E(op, errs.Unauthenticated, errors.New("the session's account no longer exists"))
	case err != nil:
		return base, model.PersonID{}, fmt.Errorf("%s: %w", op, err)
	case st.Disabled:
		s.metrics.reject(ctx, "disabled")
		return base, model.PersonID{}, errs.E(op, errs.Unauthenticated, errors.New("the session's account is disabled"))
	case st.Generation != c.generation:
		s.metrics.reject(ctx, "revoked")
		return base, model.PersonID{}, errs.E(op, errs.Unauthenticated, errors.New("the session was ended"))
	}
	sess := Session{
		ID: c.session, Account: c.account, Person: st.Person, Superuser: st.Superuser,
		PasswordChangeRequired: st.PasswordChangeRequired, IssuedAt: c.issued, ExpiresAt: c.expires,
	}
	out := WithSession(base, sess)
	if st.PasswordChangeRequired {
		return out, model.PersonID{}, nil
	}
	return out, st.Person, nil
}

// SessionInfo is GET /session: SignedIn false (with SignupOpen) when ctx has no session,
// else the session with PlayaName (Names) and Permissions (Lister). A restricted session
// gets neither, since it cannot use them. Works on a read-only copy; nothing is audited
// (the person reads their own state). With a non-nil Guard, Protected is Guard.Protected for
// the session's account; on a Guard error it answers Protected false and logs a warning with
// the account's ID and the error's kind, never failing the call (F110): the refusals of the
// guarded use cases stay the fail-closed backstop. Fails with Names', Lister's or Policy's
// kind (F8).
func (s *Service) SessionInfo(ctx context.Context) (_ SessionInfo, err error) {
	const op = "identity.Service.SessionInfo"
	ctx, span := s.start(ctx, "session-info")
	defer endSpan(span, &err)

	pol, err := s.policy(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	info := SessionInfo{SignupOpen: pol.SignupMode == SignupOpen, Permissions: []string{}}
	sess, ok := SessionFrom(ctx)
	if !ok {
		return info, nil
	}
	info.SignedIn = true
	info.PersonID, info.AccountID = sess.Person, sess.Account
	info.Superuser, info.PasswordChangeRequired = sess.Superuser, sess.PasswordChangeRequired
	info.ExpiresAt = sess.ExpiresAt
	info.Protected = s.protectedForInfo(ctx, sess.Account)
	if sess.PasswordChangeRequired {
		return info, nil
	}
	if s.names != nil {
		name, err := s.names.PlayaNameOf(ctx, sess.Person)
		switch {
		case errs.Is(err, errs.NotFound):
			name = ""
		case err != nil:
			return SessionInfo{}, fmt.Errorf("%s: names: %w", op, err)
		}
		info.PlayaName = name
	}
	if s.lister != nil {
		perms, err := s.lister.PermissionsOf(ctx, sess.Person)
		if err != nil {
			return SessionInfo{}, fmt.Errorf("%s: permissions: %w", op, err)
		}
		if perms != nil {
			info.Permissions = perms
		}
	}
	return info, nil
}

// protectedForInfo is the Guard's answer for SessionInfo.Protected: false without a Guard
// and, so a hiccup in the guard cannot break every session answer, on a Guard error, which
// it logs as a warning (account ID and kind only; no address). The guarded use cases
// still fail closed on their own (F108).
func (s *Service) protectedForInfo(ctx context.Context, a model.AccountID) bool {
	if s.guard == nil {
		return false
	}
	p, err := s.guard.Protected(ctx, a)
	if err != nil {
		s.logger.WarnContext(ctx, "session info: guard failed; answering protected false",
			slog.String("account", a.String()), slog.String("kind", errs.KindOf(err).String()))
		return false
	}
	return p
}
