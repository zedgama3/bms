// Package postgres is identity's storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of identity, rooted at the files (db.md: NNNN_name.sql at
// the set root): 0001_init.sql creates core.account. It is fs.Sub(embedded, "migrations")
// resolved in init; that Sub failing on an embedded directory is impossible, so init panics
// (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("identity/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlInsert         = mustSQL("insert_account.sql")
	sqlByID           = mustSQL("get_account_by_id.sql")
	sqlByIDNoHash     = mustSQL("get_account_by_id_nohash.sql")
	sqlByEmail        = mustSQL("get_account_by_email.sql")
	sqlByPerson       = mustSQL("get_account_by_person.sql")
	sqlState          = mustSQL("get_account_state.sql")
	sqlList           = mustSQL("list_accounts.sql")
	sqlListByEmail    = mustSQL("list_accounts_by_email.sql")
	sqlExport         = mustSQL("export_account_by_person.sql")
	sqlDelete         = mustSQL("delete_account_by_person.sql")
	sqlSetPassword    = mustSQL("set_password.sql")
	sqlSetTemporary   = mustSQL("set_temporary_password.sql")
	sqlBump           = mustSQL("bump_session_generation.sql")
	sqlDisable        = mustSQL("disable_account.sql")
	sqlEnable         = mustSQL("enable_account.sql")
	sqlTouchLogin     = mustSQL("touch_login.sql")
	sqlRehash         = mustSQL("rehash_password.sql")
	sqlUpsertSuperuse = mustSQL("upsert_superuser.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("identity/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the documentation
// that sits above each statement is not sent to the server with every call (and a comment
// that names a column cannot be mistaken for a statement that reads it).
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps core.account in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use. Every method runs inside the caller's transaction when ctx carries one.
// Every error is already classified by db.Wrap.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("identity.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

func nilIfZeroTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// Insert adds one account. A duplicate address fails errs.Conflict with *db.Error whose
// Constraint is account_email_canonical_key; a second account for one person fails the
// same way on account_person_id_key. Also errs.Unavailable wrapping db.ErrReadOnly on a
// read-only copy, and every other kind db.Wrap assigns.
func (s *Store) Insert(ctx context.Context, r account.Record) error {
	const op = "identity.postgres.Insert"
	_, err := s.db.Exec(ctx, sqlInsert,
		[16]byte(r.ID), [16]byte(r.Person), string(r.Email), r.PasswordHash.Value(),
		r.Superuser, r.PasswordChangeRequired, r.Generation,
		nilIfZeroTime(r.DisabledAt), nilIfZeroTime(r.LastLoginAt),
		r.PasswordChangedAt.UTC(), r.CreatedAt.UTC(), r.UpdatedAt.UTC())
	return db.Wrap(op, err)
}

// ByID returns the account with its hash, for the paths that verify a password. Fails errs.NotFound for an unknown ID.
func (s *Store) ByID(ctx context.Context, id model.AccountID) (account.Record, error) {
	return s.one(ctx, "identity.postgres.ByID", sqlByID, [16]byte(id))
}

// ByIDNoHash returns the account without its hash, for the admin reads. Fails
// errs.NotFound for an unknown ID.
func (s *Store) ByIDNoHash(ctx context.Context, id model.AccountID) (account.Record, error) {
	const op = "identity.postgres.ByIDNoHash"
	rows, err := s.db.Query(ctx, sqlByIDNoHash, [16]byte(id))
	if err != nil {
		return account.Record{}, db.Wrap(op, err)
	}
	recs, err := collect(op, rows)
	if err != nil {
		return account.Record{}, err
	}
	if len(recs) == 0 {
		return account.Record{}, errs.E(op, errs.NotFound, errors.New("no such account"))
	}
	return recs[0], nil
}

// ByCanonicalEmail returns the account whose address equals e ignoring case, with its
// hash. e is passed as submitted: the comparison is lower($1) in SQL, so Go and SQL cannot
// disagree about the canonical form. Fails errs.NotFound when there is none.
func (s *Store) ByCanonicalEmail(ctx context.Context, e model.Email) (account.Record, error) {
	return s.one(ctx, "identity.postgres.ByCanonicalEmail", sqlByEmail, string(e))
}

// ByPerson returns the person's account with its hash. Fails errs.NotFound when none.
func (s *Store) ByPerson(ctx context.Context, p model.PersonID) (account.Record, error) {
	return s.one(ctx, "identity.postgres.ByPerson", sqlByPerson, [16]byte(p))
}

func (s *Store) one(ctx context.Context, op, query string, arg any) (account.Record, error) {
	var (
		r        account.Record
		id, pid  [16]byte
		email    string
		hash     string
		disabled *time.Time
		login    *time.Time
	)
	err := s.db.QueryRow(ctx, query, arg).Scan(&id, &pid, &email, &hash, &r.Superuser,
		&r.PasswordChangeRequired, &r.Generation, &disabled, &login, &r.PasswordChangedAt,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return account.Record{}, db.Wrap(op, err)
	}
	r.ID, r.Person, r.Email, r.PasswordHash = model.AccountID(id), model.PersonID(pid), model.Email(email), secret.New(hash)
	finish(&r, disabled, login)
	return r, nil
}

// finish maps NULL to a zero time and every time to UTC.
func finish(r *account.Record, disabled, login *time.Time) {
	if disabled != nil {
		r.DisabledAt = disabled.UTC()
	}
	if login != nil {
		r.LastLoginAt = login.UTC()
	}
	r.PasswordChangedAt = r.PasswordChangedAt.UTC()
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
}

// StateByID is the one read Authenticate makes per request: no write, no hash, nothing
// personal. Fails errs.NotFound for an unknown ID.
func (s *Store) StateByID(ctx context.Context, id model.AccountID) (account.State, error) {
	const op = "identity.postgres.StateByID"
	var (
		st  account.State
		pid [16]byte
	)
	if err := s.db.QueryRow(ctx, sqlState, [16]byte(id)).Scan(&pid, &st.Superuser,
		&st.PasswordChangeRequired, &st.Generation, &st.Disabled); err != nil {
		return account.State{}, db.Wrap(op, err)
	}
	st.Person = model.PersonID(pid)
	return st, nil
}

// List returns up to limit accounts, newest first, strictly after the key (a zero key
// starts at the newest). The records carry no hash. Fails with db's kinds.
func (s *Store) List(ctx context.Context, after account.Key, limit int) ([]account.Record, error) {
	const op = "identity.postgres.List"
	var afterID any
	if !after.ID.IsZero() {
		afterID = [16]byte(after.ID)
	}
	rows, err := s.db.Query(ctx, sqlList, nilIfZeroTime(after.CreatedAt), afterID,
		int32(limit)) //nolint:gosec // limit is at most identity.MaxPageSize + 1
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collect(op, rows)
}

// ListByEmail returns the account whose address equals e ignoring case: zero or one
// record, without the hash. Fails with db's kinds.
func (s *Store) ListByEmail(ctx context.Context, e model.Email) ([]account.Record, error) {
	const op = "identity.postgres.ListByEmail"
	rows, err := s.db.Query(ctx, sqlListByEmail, string(e))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collect(op, rows)
}

// ExportByPerson returns the person's account without the hash. Fails errs.NotFound when
// the person has none.
func (s *Store) ExportByPerson(ctx context.Context, p model.PersonID) (account.Record, error) {
	const op = "identity.postgres.ExportByPerson"
	rows, err := s.db.Query(ctx, sqlExport, [16]byte(p))
	if err != nil {
		return account.Record{}, db.Wrap(op, err)
	}
	recs, err := collect(op, rows)
	if err != nil {
		return account.Record{}, err
	}
	if len(recs) == 0 {
		return account.Record{}, errs.E(op, errs.NotFound, errors.New("no account for the person"))
	}
	return recs[0], nil
}

// collect reads rows of the columns without password_hash.
func collect(op string, rows pgx.Rows) ([]account.Record, error) {
	defer rows.Close()
	out := []account.Record{}
	for rows.Next() {
		var (
			r        account.Record
			id, pid  [16]byte
			email    string
			disabled *time.Time
			login    *time.Time
		)
		if err := rows.Scan(&id, &pid, &email, &r.Superuser, &r.PasswordChangeRequired,
			&r.Generation, &disabled, &login, &r.PasswordChangedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, db.Wrap(op, err)
		}
		r.ID, r.Person, r.Email = model.AccountID(id), model.PersonID(pid), model.Email(email)
		finish(&r, disabled, login)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// DeleteByPerson deletes the person's account row and returns its ID (the zero ID and nil
// when there was none). Fails errs.Unavailable wrapping db.ErrReadOnly on a read-only
// copy, and with db's kinds.
func (s *Store) DeleteByPerson(ctx context.Context, p model.PersonID) (model.AccountID, int, error) {
	const op = "identity.postgres.DeleteByPerson"
	rows, err := s.db.Query(ctx, sqlDelete, [16]byte(p))
	if err != nil {
		return model.AccountID{}, 0, db.Wrap(op, err)
	}
	defer rows.Close()
	var (
		id model.AccountID
		n  int
	)
	for rows.Next() {
		var raw [16]byte
		if err := rows.Scan(&raw); err != nil {
			return model.AccountID{}, 0, db.Wrap(op, err)
		}
		id = model.AccountID(raw)
		n++
	}
	if err := rows.Err(); err != nil {
		return model.AccountID{}, 0, db.Wrap(op, err)
	}
	return id, n, nil
}

// SetPassword stores a password the person chose: it clears password_change_required and
// ends every older session, and returns the new session generation. Fails errs.NotFound
// for an unknown ID, errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Store) SetPassword(ctx context.Context, id model.AccountID, hash secret.Secret, at time.Time) (int32, error) {
	return s.generation(ctx, "identity.postgres.SetPassword", sqlSetPassword, [16]byte(id), hash.Value(), at.UTC())
}

// SetTemporaryPassword stores a password an account admin generated: it sets
// password_change_required and ends every session, and returns the new generation. Fails
// as SetPassword does.
func (s *Store) SetTemporaryPassword(ctx context.Context, id model.AccountID, hash secret.Secret, at time.Time) (int32, error) {
	return s.generation(ctx, "identity.postgres.SetTemporaryPassword", sqlSetTemporary, [16]byte(id), hash.Value(), at.UTC())
}

// BumpGeneration ends every session of the account and returns the new generation. Fails
// as SetPassword does.
func (s *Store) BumpGeneration(ctx context.Context, id model.AccountID, at time.Time) (int32, error) {
	return s.generation(ctx, "identity.postgres.BumpGeneration", sqlBump, [16]byte(id), at.UTC())
}

func (s *Store) generation(ctx context.Context, op, query string, args ...any) (int32, error) {
	var gen int32
	if err := s.db.QueryRow(ctx, query, args...).Scan(&gen); err != nil {
		return 0, db.Wrap(op, err)
	}
	return gen, nil
}

// Disable sets disabled_at and ends every session. It reports whether it changed the
// account: false means it was already disabled (or gone). Fails as SetPassword does.
func (s *Store) Disable(ctx context.Context, id model.AccountID, at time.Time) (bool, error) {
	return s.changed(ctx, "identity.postgres.Disable", sqlDisable, [16]byte(id), at.UTC())
}

// Enable clears disabled_at. It reports whether it changed the account: false means it was
// already enabled (or gone). Fails as SetPassword does.
func (s *Store) Enable(ctx context.Context, id model.AccountID, at time.Time) (bool, error) {
	return s.changed(ctx, "identity.postgres.Enable", sqlEnable, [16]byte(id), at.UTC())
}

func (s *Store) changed(ctx context.Context, op, query string, args ...any) (bool, error) {
	tag, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return false, db.Wrap(op, err)
	}
	return tag.RowsAffected() > 0, nil
}

// TouchLogin sets last_login_at and nothing else. A vanished account is not an error.
// Fails errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and with db's kinds.
func (s *Store) TouchLogin(ctx context.Context, id model.AccountID, at time.Time) error {
	const op = "identity.postgres.TouchLogin"
	_, err := s.db.Exec(ctx, sqlTouchLogin, [16]byte(id), at.UTC())
	return db.Wrap(op, err)
}

// Rehash replaces oldHash by newHash for the same password, only while the account still
// holds oldHash. Changing nothing is not an error. Fails as TouchLogin does.
func (s *Store) Rehash(ctx context.Context, id model.AccountID, newHash, oldHash secret.Secret) error {
	const op = "identity.postgres.Rehash"
	_, err := s.db.Exec(ctx, sqlRehash, [16]byte(id), newHash.Value(), oldHash.Value())
	return db.Wrap(op, err)
}

// Superuser creates the superuser account at e or resets the one that exists (new
// password, re-enabled, every session ended) in one statement. id and person are used only
// when it creates. Fails errs.Conflict when a participant account holds the address, and
// as SetPassword does.
func (s *Store) Superuser(ctx context.Context, id model.AccountID, person model.PersonID, e model.Email, hash secret.Secret, at time.Time) (account.Superuser, error) {
	const op = "identity.postgres.Superuser"
	var (
		out     account.Superuser
		aid, pi [16]byte
	)
	err := s.db.QueryRow(ctx, sqlUpsertSuperuse, [16]byte(id), [16]byte(person), string(e), hash.Value(), at.UTC()).
		Scan(&aid, &pi, &out.Created)
	if err != nil {
		if errs.Is(err, errs.NotFound) {
			return account.Superuser{}, errs.E(op, errs.Conflict, errors.New("a participant account holds the address"))
		}
		return account.Superuser{}, db.Wrap(op, err)
	}
	out.ID, out.Person = model.AccountID(aid), model.PersonID(pi)
	return out, nil
}
