package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

// The forgotten-password statements (core.password_reset, migration 0002).
var (
	sqlLockResetAccount = mustSQL("lock_reset_account.sql")
	sqlDeleteResets     = mustSQL("delete_account_password_resets.sql")
	sqlInsertReset      = mustSQL("insert_password_reset.sql")
	sqlResetByHash      = mustSQL("peek_password_reset_by_hash.sql")
	sqlMarkResetUsed    = mustSQL("mark_password_reset_used.sql")
	sqlNewestReset      = mustSQL("newest_password_reset.sql")
)

// lockAccount takes the account row lock and reads the flags a reset depends on. Fails
// errs.NotFound when the account is gone.
func (s *Store) lockAccount(ctx context.Context, op string, acct model.AccountID) (disabled, superuser bool, err error) {
	var locked [16]byte
	if err := s.db.QueryRow(ctx, sqlLockResetAccount, [16]byte(acct)).Scan(&locked, &disabled, &superuser); err != nil {
		return false, false, db.Wrap(op, err)
	}
	return disabled, superuser, nil
}

// ReplaceResetToken gives the account exactly one token: in one Write transaction (the
// caller's when ctx carries one) it locks the account row, deletes the account's reset rows
// (used, expired and the earlier live one) and inserts a new row holding hash, the SHA-256
// of the token (the token itself never reaches the store), and returns the new row's ID. now
// is the row's created_at. Fails errs.NotFound when the account is gone, errs.Conflict
// when hash collides with another row (the random source is broken), errs.Invalid when the
// hash is not 32 bytes or expires is not after now, errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy.
func (s *Store) ReplaceResetToken(ctx context.Context, acct model.AccountID, hash []byte, expires, now time.Time) (account.ResetID, error) {
	const op = "identity.postgres.ReplaceResetToken"
	id := model.NewID[account.ResetID]()
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		if _, _, err := s.lockAccount(ctx, op, acct); err != nil {
			return err
		}
		if _, err := s.db.Exec(ctx, sqlDeleteResets, [16]byte(acct)); err != nil {
			return db.Wrap(op, err)
		}
		_, err := s.db.Exec(ctx, sqlInsertReset, [16]byte(id), [16]byte(acct), hash, expires.UTC(), now.UTC())
		return db.Wrap(op, err)
	})
	if err != nil {
		return account.ResetID{}, err //nolint:wrapcheck // every statement's error went through db.Wrap
	}
	return id, nil
}

// ResetState locks the account row (the order ReplaceResetToken and CompleteReset use, so
// none of them can deadlock with it) and returns, as read under that lock, the creation time
// of the account's newest reset row (used or not; zero when it has none) and the account's
// disabled and superuser flags. Run it in the Write transaction that goes on to replace the
// token: the lock is what keeps two concurrent requests from both passing the cooldown, and
// what keeps an account disabled meanwhile from getting an e-mail. Fails errs.NotFound when
// the account is gone, errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Store) ResetState(ctx context.Context, acct model.AccountID) (account.ResetState, error) {
	const op = "identity.postgres.ResetState"
	var st account.ResetState
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		disabled, superuser, err := s.lockAccount(ctx, op, acct)
		if err != nil {
			return err
		}
		var at *time.Time
		if err := s.db.QueryRow(ctx, sqlNewestReset, [16]byte(acct)).Scan(&at); err != nil {
			return db.Wrap(op, err)
		}
		st = account.ResetState{Disabled: disabled, Superuser: superuser}
		if at != nil {
			st.NewestAt = at.UTC()
		}
		return nil
	})
	if err != nil {
		return account.ResetState{}, err //nolint:wrapcheck // every statement's error went through db.Wrap
	}
	return st, nil
}

// ResetByHash returns the reset row whose token hash is hash, locking nothing: the
// answer is advisory, and CompleteReset decides a race. Fails errs.NotFound when there is
// none.
func (s *Store) ResetByHash(ctx context.Context, hash []byte) (account.ResetRow, error) {
	const op = "identity.postgres.ResetByHash"
	var (
		r       account.ResetRow
		id, aid [16]byte
		used    *time.Time
	)
	if err := s.db.QueryRow(ctx, sqlResetByHash, hash).
		Scan(&id, &aid, &r.ExpiresAt, &used, &r.CreatedAt, &r.AccountDisabled); err != nil {
		return account.ResetRow{}, db.Wrap(op, err)
	}
	r.ID, r.Account = account.ResetID(id), model.AccountID(aid)
	r.ExpiresAt, r.CreatedAt = r.ExpiresAt.UTC(), r.CreatedAt.UTC()
	if used != nil {
		r.UsedAt = used.UTC()
	}
	return r, nil
}

// CompleteReset spends the token and sets the account's password, in one Write transaction
// (the caller's when ctx carries one). It takes the account row lock first (the order
// ReplaceResetToken uses, so the two cannot deadlock), then marks the token used (the
// statement that decides a race: used_at IS NULL), so a lost race fails before the password
// is touched, then writes the password, which clears password_change_required and bumps the
// session generation. Fails errs.NotFound also when the account, read under the lock, is
// disabled or the superuser's: it changed since the caller's unlocked lookup. Fails
// errs.NotFound when the token was already used or is gone, or the account is gone;
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Store) CompleteReset(ctx context.Context, id account.ResetID, acct model.AccountID, passwordHash secret.Secret, now time.Time) error {
	const op = "identity.postgres.CompleteReset"
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		disabled, superuser, err := s.lockAccount(ctx, op, acct)
		if err != nil {
			return err
		}
		if disabled || superuser {
			// Changed since the caller looked: an admin disabled the account, or it is the
			// superuser's, which a token never resets. The caller answers token: invalid.
			return errs.E(op, errs.NotFound, errors.New("the account can no longer be reset"))
		}
		var marked [16]byte
		if err := s.db.QueryRow(ctx, sqlMarkResetUsed, [16]byte(id), now.UTC()).Scan(&marked); err != nil {
			return db.Wrap(op, err)
		}
		_, err = s.generation(ctx, op, sqlSetPassword, [16]byte(acct), passwordHash.Value(), now.UTC())
		return err
	})
	return err //nolint:wrapcheck // every statement's error went through db.Wrap
}
