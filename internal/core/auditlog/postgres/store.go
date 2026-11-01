// Package postgres is auditlog's storage: it reads core.audit_entry (search, export, count)
// and deletes expired rows through core.audit_purge. The table, its guard and its migration
// belong to audit's set; this package has none and owns no write but the purge. It speaks in
// plain rows (strings, byte arrays and times), so the auditlog package converts between its
// types and Row.
package postgres

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed sql/*.sql
var sqlFiles embed.FS

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlSearch          = mustSQL("search_entries.sql")
	sqlSearchByActor   = mustSQL("search_entries_by_actor.sql")
	sqlSearchBySubject = mustSQL("search_entries_by_subject.sql")
	sqlSearchByRecord  = mustSQL("search_entries_by_record.sql")
	sqlExport          = mustSQL("export_entries_by_subject.sql")
	sqlCount           = mustSQL("count_entries_about_person.sql")
	sqlPurge           = mustSQL("purge_entries.sql")
)

// sqlCustomPlan makes the rest of the transaction plan its statements with their parameter
// values (see Search): the transaction-local form of SET plan_cache_mode, which a read-only
// transaction allows. It is a SELECT so it runs as a query, not as a write statement.
const sqlCustomPlan = "SELECT set_config('plan_cache_mode', 'force_custom_plan', true)"

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("auditlog/postgres: embedded SQL missing: " + name)
	}
	return string(b)
}

// Row is one audit_entry row as read. It must track core.audit_entry's columns, as audit's
// write-side Row (audit/postgres) does; a column added to the table is added to both, to the
// SELECT lists of sql/*.sql and to collect.
// The zero value of an optional field means NULL: the
// all-zero UUID, an empty string, and (for Level) no level. Fields and ActorTitles are
// never nil on a row read back.
type Row struct {
	ID             [16]byte
	At             time.Time
	Instance       string
	TraceID        string
	Spooled        bool
	Action         string
	Outcome        string // "succeeded", "denied", "failed"
	ActorKind      string // "person", "anonymous", "operator", "system"
	ActorPerson    [16]byte
	ActorSuperuser bool
	ActorTitles    []string
	Subject        [16]byte
	RecordKind     string
	RecordID       [16]byte
	Fields         []string
	Level          string // "public".."sensitive"; "" when Fields is empty
	Reason         string
}

// Filter selects rows for Search. The zero value of each field means "any".
type Filter struct {
	Actor         [16]byte
	Subject       [16]byte
	RecordKind    string // with RecordID: both zero or both set
	RecordID      [16]byte
	Area          string
	Action        string
	From, To      time.Time // [From, To); either zero is open
	SuperuserOnly bool
	AfterAt       time.Time // the cursor: rows strictly before (At, AfterID)
	AfterID       [16]byte
}

// Store reads core.audit_entry in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("auditlog.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

func nilIfZero(id [16]byte) any {
	if id == ([16]byte{}) {
		return nil
	}
	return id
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfTimeZero(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// Search returns up to limit rows matching f, newest first (at desc, id desc), using the
// statement whose index fits the first of record, subject and actor that f sets. Every
// filter is applied whichever statement runs. Fails with db's kinds.
func (s *Store) Search(ctx context.Context, f Filter, limit int) ([]Row, error) {
	const op = "auditlog.postgres.Search"
	query := sqlSearch // the four files differ only in the equality that opens the index
	switch {
	case f.RecordKind != "":
		query = sqlSearchByRecord
	case f.Subject != ([16]byte{}):
		query = sqlSearchBySubject
	case f.Actor != ([16]byte{}):
		query = sqlSearchByActor
	}
	var out []Row
	// The statement runs with a custom plan: its optional filters are "$n IS NULL" tests the
	// planner can only fold when it sees the values, and a generic plan reads far more rows
	// (search_entries.sql). SET LOCAL lasts for the rest of this transaction, which is the
	// caller's when Transact joins one (the service's Read transaction) and a Read one of its
	// own when there is none; it never outlives the transaction. Today the only caller runs
	// just this query in it, so the setting reaches nothing else.
	err := s.db.Transact(ctx, db.Read, func(ctx context.Context) error {
		if err := s.customPlan(ctx, op); err != nil {
			return err
		}
		rows, err := s.db.Query(ctx, query,
			nilIfZero(f.Actor), nilIfZero(f.Subject),
			nilIfEmpty(f.RecordKind), nilIfZero(f.RecordID),
			nilIfEmpty(f.Area), nilIfEmpty(f.Action),
			nilIfTimeZero(f.From), nilIfTimeZero(f.To),
			f.SuperuserOnly,
			nilIfTimeZero(f.AfterAt), nilIfZero(f.AfterID),
			int32(limit)) //nolint:gosec // limit is at most audit.MaxPageSize + 1
		if err != nil {
			return db.Wrap(op, err)
		}
		out, err = collect(op, rows)
		return err
	})
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// customPlan runs sqlCustomPlan on the transaction ctx carries.
func (s *Store) customPlan(ctx context.Context, op string) error {
	rows, err := s.db.Query(ctx, sqlCustomPlan)
	if err != nil {
		return db.Wrap(op, err)
	}
	rows.Close()
	return db.Wrap(op, rows.Err())
}

// Export returns up to limit rows whose subject is person, oldest first, strictly after
// (afterAt, afterID); a zero afterAt starts from the beginning. Fails with db's kinds.
func (s *Store) Export(ctx context.Context, person [16]byte, afterAt time.Time, afterID [16]byte, limit int) ([]Row, error) {
	const op = "auditlog.postgres.Export"
	rows, err := s.db.Query(ctx, sqlExport, person, nilIfTimeZero(afterAt), nilIfZero(afterID),
		int32(limit)) //nolint:gosec // limit is at most audit.MaxPageSize
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	return collect(op, rows)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func collect(op string, rows pgx.Rows) ([]Row, error) {
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		var (
			r                       Row
			trace, kind, level, rsn *string
			actor, subj, rec        *[16]byte
			titles, fields          []string
		)
		if err := rows.Scan(&r.ID, &r.At, &r.Instance, &trace, &r.Spooled, &r.Action, &r.Outcome,
			&r.ActorKind, &actor, &r.ActorSuperuser, &titles, &subj, &kind, &rec, &fields,
			&level, &rsn); err != nil {
			return nil, db.Wrap(op, err)
		}
		r.At = r.At.UTC()
		r.TraceID = deref(trace)
		r.RecordKind = deref(kind)
		r.Level = deref(level)
		r.Reason = deref(rsn)
		if actor != nil {
			r.ActorPerson = *actor
		}
		if subj != nil {
			r.Subject = *subj
		}
		if rec != nil {
			r.RecordID = *rec
		}
		r.ActorTitles = orEmpty(titles)
		r.Fields = orEmpty(fields)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// CountAbout counts the entries that name person as actor or subject (a row naming the
// person as both counts once). Fails with db's kinds.
func (s *Store) CountAbout(ctx context.Context, person [16]byte) (int, error) {
	const op = "auditlog.postgres.CountAbout"
	var n int64
	if err := s.db.QueryRow(ctx, sqlCount, person).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return int(n), nil
}

// Purge removes the entries with at < before through core.audit_purge and returns how many.
// Run it in the Write transaction that records the purge. Fails errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy, and with db's kinds.
func (s *Store) Purge(ctx context.Context, before time.Time) (int64, error) {
	const op = "auditlog.postgres.Purge"
	var n int64
	if err := s.db.QueryRow(ctx, sqlPurge, before.UTC()).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return n, nil
}
