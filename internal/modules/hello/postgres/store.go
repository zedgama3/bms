// Package postgres is hello's storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the module's migration set, rooted at the files (db.md: NNNN_name.sql at
// the set root): 0001_init.sql creates schema hello, table hello.greeting and its index.
// It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on an embedded
// directory is impossible, so init panics (go-conventions). Handed to the Manifest;
// applied by `bms migrate` (D17).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("hello/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlInsert = mustSQL("insert_greeting.sql")
	sqlCount  = mustSQL("count_greetings_by_person.sql")
	sqlList   = mustSQL("list_greetings_by_person.sql")
	sqlDelete = mustSQL("delete_greetings_by_person.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("hello/postgres: embedded SQL missing: " + name)
	}
	return string(b)
}

// Store keeps hello.greeting in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("hello.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// Insert adds one greeting. Runs inside the caller's transaction when there is one.
// Fails errs.Conflict on a duplicate ID, errs.Invalid on a check or not-null violation,
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, Timeout/Canceled, and
// every other kind db.Wrap assigns.
func (s *Store) Insert(ctx context.Context, g greeting.Greeting) error {
	const op = "hello.postgres.Insert"
	if _, err := s.db.Exec(ctx, sqlInsert, [16]byte(g.ID), [16]byte(g.Person), g.Text, g.CreatedAt); err != nil {
		return db.Wrap(op, err)
	}
	return nil
}

// CountByPerson counts the person's greetings (0 for an unknown person; cannot be
// NotFound). Fails with db's kinds.
func (s *Store) CountByPerson(ctx context.Context, person model.PersonID) (int, error) {
	const op = "hello.postgres.CountByPerson"
	var n int64
	if err := s.db.QueryRow(ctx, sqlCount, [16]byte(person)).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return int(n), nil
}

// ListByPerson returns the person's greetings oldest first (empty for an unknown person).
// Fails with db's kinds.
func (s *Store) ListByPerson(ctx context.Context, person model.PersonID) (out []greeting.Greeting, err error) {
	const op = "hello.postgres.ListByPerson"
	rows, err := s.db.Query(ctx, sqlList, [16]byte(person))
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rows.Close()
	out = []greeting.Greeting{}
	for rows.Next() {
		var (
			id, pid [16]byte
			text    string
			at      time.Time
		)
		if err := rows.Scan(&id, &pid, &text, &at); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, greeting.Greeting{ID: greeting.ID(id), Person: model.PersonID(pid), Text: text, CreatedAt: at.UTC()})
	}
	if err := rows.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// DeleteByPerson removes the person's greetings and returns how many. Fails
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and with db's kinds.
func (s *Store) DeleteByPerson(ctx context.Context, person model.PersonID) (int, error) {
	const op = "hello.postgres.DeleteByPerson"
	tag, err := s.db.Exec(ctx, sqlDelete, [16]byte(person))
	if err != nil {
		return 0, db.Wrap(op, err)
	}
	return int(tag.RowsAffected()), nil
}
