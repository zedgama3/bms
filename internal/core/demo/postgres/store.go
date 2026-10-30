// Package postgres is demo's storage: the only package with SQL and the driver for the
// tables core.demo_state, core.demo_account and core.demo_mail (demo.md "Data"). It also
// holds the one direct read of core.account the seed's emptiness guard makes (decision 20).
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/demo/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of demo, rooted at the files (db.md: NNNN_name.sql at the
// set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on an
// embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("demo/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlState       = mustSQL("get_state.sql")
	sqlInsertState = mustSQL("insert_state.sql")
	sqlNotEmpty    = mustSQL("has_non_superuser_account.sql")
	sqlMarkSeeded  = mustSQL("mark_seeded.sql")
	sqlAddAccount  = mustSQL("insert_account.sql")
	sqlProtected   = mustSQL("is_protected.sql")
	sqlInsertMail  = mustSQL("insert_mail.sql")
	sqlTrimMail    = mustSQL("trim_mail.sql")
	sqlListMail    = mustSQL("list_mail.sql")
	sqlDropStmt    = mustSQL("drop_schemas_statement.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("demo/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the
// documentation above each statement is not sent to the server with every call.
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps the demo tables in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use. Every method runs inside the caller's transaction when ctx carries one.
// Every error is already classified by db.Wrap. Nothing here logs a value.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("demo.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

func conflict(op, code, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: rows.Field, Code: code, Reason: reason})
}

// State returns the marker row. Fails errs.NotFound when there is none (this database is not
// a demo, or was never seeded).
func (s *Store) State(ctx context.Context) (rows.State, error) {
	const op = "demo.postgres.State"
	var (
		st      rows.State
		version int32
		seeded  *time.Time
	)
	if err := s.db.QueryRow(ctx, sqlState).Scan(&version, &st.Instance, &st.StartedAt, &seeded); err != nil {
		return rows.State{}, db.Wrap(op, err)
	}
	st.SeedVersion = int(version)
	st.StartedAt = st.StartedAt.UTC()
	if seeded != nil {
		st.SeededAt = seeded.UTC()
	}
	return st, nil
}

// BeginSeed writes the marker (SeededAt NULL) and checks the database is empty, in one Write
// transaction: it fails errs.Conflict (field demo, code seeded) when a marker exists,
// errs.Conflict (code not_empty) when a non-superuser account exists (the marker is rolled
// back), errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and store kinds.
func (s *Store) BeginSeed(ctx context.Context, st rows.State) error {
	const op = "demo.postgres.BeginSeed"
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		rs, err := s.db.Query(ctx, sqlInsertState, int32(st.SeedVersion), st.Instance, st.StartedAt.UTC()) //nolint:gosec // SeedVersion is a small constant
		if err != nil {
			return db.Wrap(op, err)
		}
		inserted := rs.Next()
		rs.Close()
		if err := rs.Err(); err != nil {
			return db.Wrap(op, err)
		}
		if !inserted {
			return conflict(op, rows.CodeSeeded, "this database was already seeded: run `bms demo reset` to wipe and seed it again")
		}
		var notEmpty bool
		if err := s.db.QueryRow(ctx, sqlNotEmpty).Scan(&notEmpty); err != nil {
			return db.Wrap(op, err)
		}
		if notEmpty {
			return conflict(op, rows.CodeNotEmpty, "the database already holds accounts, so it is not empty: the demo seed needs a fresh database")
		}
		return nil
	})
	if err != nil {
		return wrapTx(op, err)
	}
	return nil
}

// wrapTx keeps err's kind and chain, adding op when it has none of ours.
func wrapTx(op string, err error) error {
	if err == nil {
		return nil
	}
	return db.Wrap(op, err)
}

// MarkSeeded sets seeded_at. Fails errs.Internal when there is no unseeded marker (the seed
// owns it), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Store) MarkSeeded(ctx context.Context, at time.Time) error {
	const op = "demo.postgres.MarkSeeded"
	rs, err := s.db.Query(ctx, sqlMarkSeeded, at.UTC())
	if err != nil {
		return db.Wrap(op, err)
	}
	found := rs.Next()
	rs.Close()
	if err := rs.Err(); err != nil {
		return db.Wrap(op, err)
	}
	if !found {
		return errs.E(op, errs.Internal, errors.New("no unseeded demo marker to complete"))
	}
	return nil
}

// AddAccount records one demo account. Fails errs.Conflict for a repeated account, key or
// position, errs.Invalid for a missing account or person or a malformed value, and
// errs.Unavailable wrapping db.ErrReadOnly.
func (s *Store) AddAccount(ctx context.Context, a rows.Account) error {
	const op = "demo.postgres.AddAccount"
	if _, err := s.db.Exec(ctx, sqlAddAccount, [16]byte(a.Account), [16]byte(a.Person), a.Key, a.Email, int32(a.Position)); err != nil { //nolint:gosec // Position is 0..9
		return db.Wrap(op, err)
	}
	return nil
}

// Protected reports whether the account is a demo account: false for an unknown one.
func (s *Store) Protected(ctx context.Context, id model.AccountID) (bool, error) {
	const op = "demo.postgres.Protected"
	var protected bool
	if err := s.db.QueryRow(ctx, sqlProtected, [16]byte(id)).Scan(&protected); err != nil {
		return false, db.Wrap(op, err)
	}
	return protected, nil
}

// Capture keeps one notification and trims the table to the newest keep rows, in one short
// Write transaction. Fails errs.Unavailable wrapping db.ErrReadOnly on a read-only copy,
// errs.Invalid for a value the table refuses, and store kinds. The values are personal and
// never appear in an error.
func (s *Store) Capture(ctx context.Context, m rows.Mail, keep int) error {
	const op = "demo.postgres.Capture"
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		if _, err := s.db.Exec(ctx, sqlInsertMail, m.ID, m.To, m.Subject, m.Body, m.MessageID, m.CapturedAt.UTC()); err != nil {
			return db.Wrap(op, err)
		}
		var trimmed int64
		if err := s.db.QueryRow(ctx, sqlTrimMail, int32(keep)).Scan(&trimmed); err != nil { //nolint:gosec // keep is MaxMail
			return db.Wrap(op, err)
		}
		return nil
	})
	if err != nil {
		return wrapTx(op, err)
	}
	return nil
}

// ListMail returns the rows addressed to address (compared ignoring case), newest first, at
// most limit (1..50). There is no other filter.
func (s *Store) ListMail(ctx context.Context, address string, limit int) ([]rows.Mail, error) {
	const op = "demo.postgres.ListMail"
	rs, err := s.db.Query(ctx, sqlListMail, address, int32(limit)) //nolint:gosec // limit is 1..MaxMailList
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Mail{}
	for rs.Next() {
		var m rows.Mail
		if err := rs.Scan(&m.ID, &m.To, &m.Subject, &m.Body, &m.CapturedAt); err != nil {
			return nil, db.Wrap(op, err)
		}
		m.CapturedAt = m.CapturedAt.UTC()
		out = append(out, m)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// DropSchemas drops every named schema with CASCADE in one statement, built on the server
// with quote_ident from the names that match db's schema grammar. It fails errs.Internal
// when a name is refused by the grammar (nothing is dropped), errs.Invalid for no names,
// and the kind Wrap gives the server's error (a privilege error is errs.Permission,
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy).
func (s *Store) DropSchemas(ctx context.Context, schemas []string) error {
	const op = "demo.postgres.DropSchemas"
	if len(schemas) == 0 {
		return errs.E(op, errs.Invalid, errors.New("no schemas to drop"))
	}
	var (
		stmt    *string
		matched int64
	)
	if err := s.db.QueryRow(ctx, sqlDropStmt, schemas).Scan(&stmt, &matched); err != nil {
		return db.Wrap(op, err)
	}
	if stmt == nil || int(matched) != uniqueCount(schemas) {
		return errs.E(op, errs.Internal, errors.New("a schema name is outside the schema grammar"))
	}
	if _, err := s.db.Exec(ctx, *stmt); err != nil {
		return db.Wrap(op, err)
	}
	return nil
}

func uniqueCount(ss []string) int {
	seen := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		seen[s] = struct{}{}
	}
	return len(seen)
}
