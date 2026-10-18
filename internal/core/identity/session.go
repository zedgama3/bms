package identity

import (
	"context"
	"time"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

type session struct{}

// SessionID identifies one issued token; it is in the token and in the audit entries
// identity.login, identity.logout and identity.sessions.ended (Record kind "identity.session").
type SessionID = model.ID[session]

// Session is the principal a request carries once Authenticate accepted its token. It is
// request-scoped: it lives in the request ctx only, is never stored, and is never the actor
// inside a signal delivery or a job (signals decision 7).
type Session struct {
	ID                     SessionID
	Account                model.AccountID
	Person                 model.PersonID
	Superuser              bool // D23: the audit log marks it; authz allows everything
	PasswordChangeRequired bool // a temporary password: only identity's own routes see a person (decision 9)
	IssuedAt, ExpiresAt    time.Time
}

// Validate fails errs.Invalid joining a validate.FieldError per problem (fields "id",
// "account", "person", "expires_at"): a zero ID, Account or Person; ExpiresAt not after
// IssuedAt.
func (s Session) Validate() error {
	v := validate.Errors{Op: "identity.Session.Validate"}
	if s.ID.IsZero() {
		v.Addf("id", codeRequired, "must be set")
	}
	if s.Account.IsZero() {
		v.Addf("account", codeRequired, "must be set")
	}
	if s.Person.IsZero() {
		v.Addf("person", codeRequired, "must be set")
	}
	if !s.ExpiresAt.After(s.IssuedAt) {
		v.Addf("expires_at", validate.CodeRange, "must be after issued_at")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

type sessionKey struct{}

// WithSession puts s in ctx. Consumers: tests of any service that reads PersonFrom, and
// app's end-to-end test. Production code never calls it: Authenticate is the only writer.
// Cannot fail (an invalid s is a test bug; PersonFrom still returns it).
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFrom returns the session in ctx. Consumers: identity's handlers; app's closures
// for audit.ActorFunc (M1) and authz's actor option (M2). Cannot fail; ok is false without
// a session. Returns the session even when PasswordChangeRequired is set.
func SessionFrom(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}

// PersonFrom is what every service and module reads the actor with. ok is false when there
// is no session, when the session is restricted (PasswordChangeRequired) or when
// signals.InDelivery(ctx) is true: a handler runs as the system, never as the person whose
// request published the signal. Cannot fail.
func PersonFrom(ctx context.Context) (model.PersonID, bool) {
	if signals.InDelivery(ctx) {
		return model.PersonID{}, false
	}
	s, ok := SessionFrom(ctx)
	if !ok || s.PasswordChangeRequired {
		return model.PersonID{}, false
	}
	return s.Person, true
}
