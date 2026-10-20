// Package postgres is settings' storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/settings/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of settings, rooted at the files (db.md: NNNN_name.sql at
// the set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on
// an embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("settings/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlGetRevision    = mustSQL("get_revision.sql")
	sqlLockRevision   = mustSQL("lock_revision.sql")
	sqlBumpRevision   = mustSQL("bump_revision.sql")
	sqlLoadSettings   = mustSQL("load_settings.sql")
	sqlLoadTerms      = mustSQL("load_terminology.sql")
	sqlUpsertSetting  = mustSQL("upsert_setting.sql")
	sqlDeleteSetting  = mustSQL("delete_setting.sql")
	sqlUpsertTerms    = mustSQL("upsert_term_forms.sql")
	sqlDeleteTermForm = mustSQL("delete_term_forms.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("settings/postgres: embedded SQL missing: " + name)
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

// Store keeps core.setting, core.setting_revision and core.terminology in PostgreSQL. It
// holds a db.DB and no state; safe for concurrent use. Every method runs inside the caller's
// transaction when ctx carries one. Every error is already classified by db.Wrap; a missing
// revision row is errs.NotFound, which the service re-kinds.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("settings.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// scalar runs a statement that answers one int64 (the revision).
func (s *Store) scalar(ctx context.Context, op, query string) (int64, error) {
	var n int64
	if err := s.db.QueryRow(ctx, query).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return n, nil
}

// Revision returns the current revision without a lock. Fails errs.NotFound when the row
// is missing.
func (s *Store) Revision(ctx context.Context) (int64, error) {
	return s.scalar(ctx, "settings.postgres.Revision", sqlGetRevision)
}

// LockRevision takes the revision row's lock for the rest of the transaction and returns
// the revision before the write. Fails errs.NotFound when the row is missing.
func (s *Store) LockRevision(ctx context.Context) (int64, error) {
	return s.scalar(ctx, "settings.postgres.LockRevision", sqlLockRevision)
}

// BumpRevision adds one to the revision and returns the new one. Fails errs.NotFound when
// the row is missing.
func (s *Store) BumpRevision(ctx context.Context) (int64, error) {
	return s.scalar(ctx, "settings.postgres.BumpRevision", sqlBumpRevision)
}

// LoadSettings returns every stored value by key, including keys no process declares.
func (s *Store) LoadSettings(ctx context.Context) ([]rows.Setting, error) {
	const op = "settings.postgres.LoadSettings"
	rs, err := s.db.Query(ctx, sqlLoadSettings)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Setting{}
	for rs.Next() {
		var r rows.Setting
		if err := rs.Scan(&r.Key, &r.Value, &r.Sealed); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// LoadTerminology returns every stored override, including ones this build does not know.
func (s *Store) LoadTerminology(ctx context.Context) ([]rows.Term, error) {
	const op = "settings.postgres.LoadTerminology"
	rs, err := s.db.Query(ctx, sqlLoadTerms)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Term{}
	for rs.Next() {
		var r rows.Term
		if err := rs.Scan(&r.Lang, &r.Term, &r.Form, &r.Value); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// keys runs a statement whose rows are one text column and returns them sorted.
func (s *Store) keys(ctx context.Context, op, query string, args ...any) ([]string, error) {
	rs, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []string{}
	for rs.Next() {
		var k string
		if err := rs.Scan(&k); err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, k)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	slices.Sort(out)
	return out, nil
}

// UpsertSetting stores one key away from its default: value for a plain setting, sealed for
// a Secret (exactly one is non-nil). It reports whether a row was inserted or changed; a
// sealed value always is. Fails errs.Invalid for a CHECK violation (the constraint name is
// on the *db.Error in the chain) and errs.Unavailable wrapping db.ErrReadOnly on a
// read-only copy.
func (s *Store) UpsertSetting(ctx context.Context, key string, value *string, sealed []byte, now time.Time) (bool, error) {
	ks, err := s.keys(ctx, "settings.postgres.UpsertSetting", sqlUpsertSetting, key, value, sealed, now.UTC())
	return len(ks) > 0, err
}

// DeleteSetting resets one key to its default. It reports whether a row existed.
func (s *Store) DeleteSetting(ctx context.Context, key string) (bool, error) {
	ks, err := s.keys(ctx, "settings.postgres.DeleteSetting", sqlDeleteSetting, key)
	return len(ks) > 0, err
}

// UpsertTermForms stores the given forms of one term in one language (forms[i] has value
// values[i]; each form at most once) and returns, sorted, the forms inserted or changed.
func (s *Store) UpsertTermForms(ctx context.Context, lang, term string, forms, values []string, now time.Time) ([]string, error) {
	return s.keys(ctx, "settings.postgres.UpsertTermForms", sqlUpsertTerms, lang, term, forms, values, now.UTC())
}

// DeleteTermForms resets the given forms of one term in one language and returns, sorted,
// the forms that had an override.
func (s *Store) DeleteTermForms(ctx context.Context, lang, term string, forms []string) ([]string, error) {
	return s.keys(ctx, "settings.postgres.DeleteTermForms", sqlDeleteTermForm, lang, term, forms)
}
