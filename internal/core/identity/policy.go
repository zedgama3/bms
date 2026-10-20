package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// SignupMode says who may create an account.
type SignupMode string

// The sign-up modes.
const (
	SignupOpen       SignupMode = "open"
	SignupInvitation SignupMode = "invitation"
)

// Policy is what a regional may change. settings (M2) maps the accounts.* keys onto it.
type Policy struct {
	SignupMode               SignupMode    // accounts.signup
	SessionLifetime          time.Duration // accounts.session_lifetime
	SuperuserSessionLifetime time.Duration // accounts.superuser_session_lifetime
	PasswordMinLength        int           // accounts.password_min_length (characters, counted as runes)
	LoginFailureLimit        int           // accounts.login_failure_limit (per submitted address; default 10)
	ClientFailureLimit       int           // accounts.client_failure_limit (per client address; default 100)
	LoginFailureWindow       time.Duration // accounts.login_failure_window
	PasswordResetLifetime    time.Duration // accounts.password_reset_lifetime (how long a reset link works)
}

// DefaultPolicy returns the shipped defaults (Settings table). Cannot fail.
func DefaultPolicy() Policy {
	return Policy{
		SignupMode:               SignupOpen,
		SessionLifetime:          30 * 24 * time.Hour,
		SuperuserSessionLifetime: time.Hour,
		PasswordMinLength:        PasswordMinLengthFloor,
		LoginFailureLimit:        10,
		ClientFailureLimit:       100,
		LoginFailureWindow:       15 * time.Minute,
		PasswordResetLifetime:    time.Hour,
	}
}

// Validate fails errs.Invalid with a validate.FieldError per key out of its range; the
// field is the settings key ("accounts.signup", ...). accounts.client_failure_limit must be
// 10..1000 and at least accounts.login_failure_limit.
func (p Policy) Validate() error {
	v := validate.Errors{Op: "identity.Policy.Validate"}
	if p.SignupMode != SignupOpen && p.SignupMode != SignupInvitation {
		v.Addf("accounts.signup", validate.CodeOneOf, "must be one of: open, invitation")
	}
	v.Add(validate.Range("accounts.session_lifetime", p.SessionLifetime, time.Hour, 8760*time.Hour))
	v.Add(validate.Range("accounts.superuser_session_lifetime", p.SuperuserSessionLifetime, 10*time.Minute, 24*time.Hour))
	v.Add(validate.Range("accounts.password_min_length", p.PasswordMinLength, PasswordMinLengthFloor, 128))
	v.Add(validate.Range("accounts.login_failure_limit", p.LoginFailureLimit, 3, 100))
	// Many people share one client address at a venue, so its limit is the larger one: never
	// below the per-submitted-address limit.
	v.Add(validate.Range("accounts.client_failure_limit", p.ClientFailureLimit, min(max(10, p.LoginFailureLimit), 1000), 1000))
	v.Add(validate.Range("accounts.login_failure_window", p.LoginFailureWindow, time.Minute, 24*time.Hour))
	v.Add(validate.Range("accounts.password_reset_lifetime", p.PasswordResetLifetime, 10*time.Minute, 24*time.Hour))
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// lifetime is the session lifetime for an account of the given kind.
func (p Policy) lifetime(superuser bool) time.Duration {
	if superuser {
		return p.SuperuserSessionLifetime
	}
	return p.SessionLifetime
}

// PolicySource returns the current policy. settings implements it (M2); nil means
// DefaultPolicy. A source that fails or returns an invalid Policy fails the use case with
// its kind (F62): identity never falls back silently to the defaults once a source exists.
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

// PermissionChecker decides one site-wide permission for the actor in ctx (the shape of
// api.PermissionChecker). authz implements it (M2); nil denies with errs.Permission.
// Identity allows the superuser before asking it (decision 7).
type PermissionChecker interface {
	Allow(ctx context.Context, p authz.Permission) error
}

// PermissionLister fills SessionInfo.Permissions (09 §4: once per page load). authz
// implements it (M2); nil means an empty list.
type PermissionLister interface {
	PermissionsOf(ctx context.Context, person model.PersonID) ([]string, error)
}

// NameSource fills SessionInfo.PlayaName. people implements it (M2); nil means "". A
// NotFound from it means "" (a person row not yet created); any other kind fails
// SessionInfo (F8).
type NameSource interface {
	PlayaNameOf(ctx context.Context, person model.PersonID) (string, error)
}

// Alerter tells the maintainers and the site admins that the superuser signed in (D23).
// notify implements it (M4); nil means one warn log line with the person ID. An Alerter
// failure is logged and counted, never fails the sign-in (F27): the audit entry is the
// record of the event.
type Alerter interface {
	SuperuserSignedIn(ctx context.Context, person model.PersonID) error
}

// policy reads the current policy: the source's, validated, or the defaults when there is
// no source.
func (s *Service) policy(ctx context.Context) (Policy, error) {
	const op = "identity.Service.policy"
	if s.policySource == nil {
		return DefaultPolicy(), nil
	}
	p, err := s.policySource.Policy(ctx)
	if err != nil {
		return Policy{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, errs.E(op, errs.Invalid, err)
	}
	return p, nil
}
