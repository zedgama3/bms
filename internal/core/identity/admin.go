package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// authorize is the first step of every admin use case: a full session (not restricted, not
// inside a signal delivery), then the permission. The superuser passes before the
// PermissionChecker is asked (decision 7); a nil checker denies everyone else.
func (s *Service) authorize(ctx context.Context, op string, p authz.Permission) (Session, error) {
	sess, ok := SessionFrom(ctx)
	if _, full := PersonFrom(ctx); !ok || !full {
		return Session{}, errs.E(op, errs.Unauthenticated, errNoSession)
	}
	if sess.Superuser {
		return sess, nil
	}
	if s.perms == nil {
		return Session{}, errs.E(op, errs.Permission, errors.New("the permission "+string(p)+" is not granted"))
	}
	if err := s.perms.Allow(ctx, p); err != nil {
		return Session{}, fmt.Errorf("%s: %w", op, err)
	}
	return sess, nil
}

// readFields are the fields an admin read of an account shows, for the audit entry.
var readFields = []string{"email", "last_login_at", "disabled_at"}

// auditReads records identity.account.read for every account but the actor's own, in one
// batch, after the data is loaded and before it is returned (F35).
func (s *Service) auditReads(ctx context.Context, actor Session, accts []Account) error {
	entries := make([]audit.Entry, 0, len(accts))
	for _, a := range accts {
		if a.ID == actor.Account {
			continue
		}
		entries = append(entries, audit.Entry{
			Action: actionRead, Outcome: audit.Succeeded, Subject: a.Person,
			Record: audit.RefOf(kindAccount, a.ID), Fields: append([]string(nil), readFields...),
			Level: model.SensitivityPersonal,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	return s.record(ctx, entries...)
}

// ListAccounts needs accounts.read (F31, F32). With q.Email set it returns the account at
// exactly that address (canonical match); otherwise the newest accounts, keyset-paged.
// Works on a read-only copy. Records identity.account.read for every account returned
// except the actor's own, in one batch, after loading and before returning (F35). Fails
// Invalid (F33), store kinds (F34), audit kinds.
func (s *Service) ListAccounts(ctx context.Context, q AccountQuery) (_ AccountPage, err error) {
	const op = "identity.Service.ListAccounts"
	ctx, span := s.start(ctx, "list-accounts")
	defer endSpan(span, &err)

	actor, err := s.authorize(ctx, op, PermissionAccountsRead)
	if err != nil {
		return AccountPage{}, err
	}
	if err := q.Validate(); err != nil {
		return AccountPage{}, err
	}
	var (
		recs []account.Record
		next Cursor
	)
	if q.Email != "" {
		var email model.Email
		if email, err = model.ParseEmail(string(q.Email)); err == nil {
			recs, err = s.store.ListByEmail(ctx, email)
		}
	} else {
		var after account.Key
		if q.After != "" {
			if after, err = q.After.key(); err != nil {
				return AccountPage{}, err
			}
		}
		limit := q.pageSize()
		recs, err = s.store.List(ctx, after, limit+1)
		if err == nil && len(recs) > limit {
			recs = recs[:limit]
			next = cursorOf(account.Key{CreatedAt: recs[limit-1].CreatedAt, ID: recs[limit-1].ID})
		}
	}
	if err != nil {
		return AccountPage{}, fmt.Errorf("%s: %w", op, err)
	}
	page := AccountPage{Accounts: make([]Account, len(recs)), Next: next}
	for i, r := range recs {
		page.Accounts[i] = accountOf(r)
	}
	span.SetAttrs(telemetry.Int("accounts", len(page.Accounts)))
	if err := s.auditReads(ctx, actor, page.Accounts); err != nil {
		return AccountPage{}, err
	}
	return page, nil
}

// GetAccount is ListAccounts for one ID: it needs accounts.read, fails NotFound for an
// unknown ID (after the permission check, F36), and records the read unless it is the
// actor's own account.
func (s *Service) GetAccount(ctx context.Context, id model.AccountID) (_ Account, err error) {
	const op = "identity.Service.GetAccount"
	ctx, span := s.start(ctx, "get-account")
	defer endSpan(span, &err)

	actor, err := s.authorize(ctx, op, PermissionAccountsRead)
	if err != nil {
		return Account{}, err
	}
	span.SetAttrs(telemetry.ID("account_id", id))
	rec, err := s.store.ByIDNoHash(ctx, id)
	if err != nil {
		return Account{}, fmt.Errorf("%s: %w", op, err)
	}
	a := accountOf(rec)
	if err := s.auditReads(ctx, actor, []Account{a}); err != nil {
		return Account{}, err
	}
	return a, nil
}

// manage runs an account admin's change in one Write transaction: it loads the target
// (NotFound, F41), refuses the superuser account (Permission, F39) and the actor's own
// (Invalid, field account, code self, F40), refuses a protected account when guarded
// (Permission, field account, code protected; D53), then calls change with the target. Doing the
// load inside the transaction makes a read-only copy fail first (F42).
func (s *Service) manage(ctx context.Context, op string, actor Session, id model.AccountID, guarded bool, change func(ctx context.Context, target account.Record) error) error {
	return wrap(op, s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		target, err := s.store.ByIDNoHash(ctx, id)
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		if target.Superuser {
			return errs.E(op, errs.Permission, errors.New("the superuser account is changed by command only"))
		}
		if target.ID == actor.Account {
			return fieldInvalid(op, "account", codeSelf, "cannot be your own account")
		}
		if guarded {
			if err := s.checkProtected(ctx, op, target.ID); err != nil {
				return err
			}
		}
		return change(ctx, target)
	}))
}

// DisableAccount needs accounts.manage (F37, F38). It refuses the superuser account
// (Permission, F39) and the actor's own account (Invalid, field account, code self, F40).
// In one Write transaction it sets disabled_at, ends every session of the account and
// records identity.account.disabled. Already disabled: nil, nothing recorded (decision 11,
// F49). Fails NotFound (F41), Permission (field account, code protected) for an account
// the Guard protects (F104), Unavailable wrapping db.ErrReadOnly (F42), the Guard's kind when
// it fails (F108).
func (s *Service) DisableAccount(ctx context.Context, id model.AccountID) (err error) {
	const op = "identity.Service.DisableAccount"
	ctx, span := s.start(ctx, "disable-account")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("account_id", id))
	actor, err := s.authorize(ctx, op, PermissionAccountsManage)
	if err != nil {
		return err
	}
	return s.manage(ctx, op, actor, id, true, func(ctx context.Context, t account.Record) error {
		changed, err := s.store.Disable(ctx, t.ID, s.now())
		if err != nil || !changed {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionDisabled, Outcome: audit.Succeeded, Subject: t.Person, Record: audit.RefOf(kindAccount, t.ID),
		})
	})
}

// EnableAccount is the inverse of DisableAccount (identity.account.enabled; F37-F42 apply).
// Already enabled: nil, nothing recorded.
func (s *Service) EnableAccount(ctx context.Context, id model.AccountID) (err error) {
	const op = "identity.Service.EnableAccount"
	ctx, span := s.start(ctx, "enable-account")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("account_id", id))
	actor, err := s.authorize(ctx, op, PermissionAccountsManage)
	if err != nil {
		return err
	}
	return s.manage(ctx, op, actor, id, false, func(ctx context.Context, t account.Record) error {
		changed, err := s.store.Enable(ctx, t.ID, s.now())
		if err != nil || !changed {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionEnabled, Outcome: audit.Succeeded, Subject: t.Person, Record: audit.RefOf(kindAccount, t.ID),
		})
	})
}

// SetTemporaryPassword needs accounts.manage; it refuses the superuser account and the
// actor's own account (F37-F41). It generates a GeneratedPasswordLength password, stores
// its hash, sets password_change_required, ends every session of the account, records
// identity.password.temporary-set and returns the password once. Not idempotent: each call
// is a new password and a new entry. Fails Permission (field account, code protected) for an
// account the Guard protects (F103), Unavailable wrapping db.ErrReadOnly (F42),
// Internal when the random source fails (F43).
func (s *Service) SetTemporaryPassword(ctx context.Context, id model.AccountID) (_ secret.Secret, err error) {
	const op = "identity.Service.SetTemporaryPassword"
	ctx, span := s.start(ctx, "set-temporary-password")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("account_id", id))

	// Authorize first so a caller without the permission learns nothing; the target checks
	// run inside the transaction, with the hash already made so it holds no lock while the
	// hash is computed.
	actor, err := s.authorize(ctx, op, PermissionAccountsManage)
	if err != nil {
		return secret.Secret{}, err
	}
	pw, err := s.generatePassword()
	if err != nil {
		return secret.Secret{}, err
	}
	hash, err := s.hash(ctx, pw)
	if err != nil {
		return secret.Secret{}, err
	}
	err = s.manage(ctx, op, actor, id, true, func(ctx context.Context, t account.Record) error {
		if _, err := s.store.SetTemporaryPassword(ctx, t.ID, hash, s.now()); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionTemporarySet, Outcome: audit.Succeeded, Subject: t.Person, Record: audit.RefOf(kindAccount, t.ID),
		})
	})
	if err != nil {
		return secret.Secret{}, err
	}
	return secret.New(pw), nil
}

// EnsureSuperuser is the create-superuser command's use case (ActorOperator, D23). It
// validates email (F44), then in one Write transaction creates the superuser account at
// that address (publishing identity.account-created and recording identity.superuser.created)
// or, when a superuser account exists there, replaces its password, re-enables it, ends its
// sessions and records identity.superuser.reset. A participant account at the address is
// Conflict (F45; decision 7). Idempotent in effect: the account exists afterwards; each
// call returns a new password. Fails Unavailable wrapping db.ErrReadOnly (F46), Internal
// when the random source fails (F43).
func (s *Service) EnsureSuperuser(ctx context.Context, email model.Email) (_ SuperuserResult, err error) {
	const op = "identity.Service.EnsureSuperuser"
	ctx, span := s.start(ctx, "ensure-superuser")
	defer endSpan(span, &err)

	v := validate.Errors{Op: op}
	parsed := emailField(&v, email)
	if err := v.Err(); err != nil {
		return SuperuserResult{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	pw, err := s.generatePassword()
	if err != nil {
		return SuperuserResult{}, err
	}
	hash, err := s.hash(ctx, pw)
	if err != nil {
		return SuperuserResult{}, err
	}
	var got account.Superuser
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var err error
		fresh, freshPerson := model.NewID[model.AccountID](), model.NewID[model.PersonID]()
		if got, err = s.store.Superuser(ctx, fresh, freshPerson, parsed, hash, s.now()); err != nil {
			return wrap(op, err)
		}
		action := actionSuperReset
		if got.Created {
			action = actionSuperCreated
			if err := s.publishCreated(ctx, got.ID, got.Person); err != nil {
				return err
			}
		}
		return s.record(ctx, audit.Entry{
			Action: action, Outcome: audit.Succeeded, Actor: audit.Actor{Kind: audit.ActorOperator},
			Subject: got.Person, Record: audit.RefOf(kindAccount, got.ID),
		})
	})
	if err != nil {
		return SuperuserResult{}, wrap(op, err)
	}
	span.SetAttrs(telemetry.ID("account_id", got.ID))
	return SuperuserResult{Account: got.ID, Person: got.Person, Created: got.Created, Password: secret.New(pw)}, nil
}
