// Package postgres is audit's storage: the only package with SQL and the driver. It speaks
// in plain rows (strings, byte arrays and times), because the audit package imports this
// one to build its store and so cannot be imported back; audit converts between its types
// and Row.
package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of audit, rooted at the files (db.md: NNNN_name.sql at
// the set root): 0001_init.sql creates core.audit_entry, its guard and core.audit_purge.
// It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on an embedded
// directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("audit/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlInsert = mustSQL("insert_entries.sql")
	sqlImport = mustSQL("import_entries.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("audit/postgres: embedded SQL missing: " + name)
	}
	return string(b)
}

// Row is one audit_entry row to write. The zero value of an optional field means NULL: the
// all-zero UUID, an empty string, and (for Level) no level. Reading belongs to auditlog.
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

// Store appends to core.audit_entry in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("audit.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// jsonRow is the element of the $1 array of insert_entries.sql and import_entries.sql.
type jsonRow struct {
	ID             string   `json:"id"`
	At             string   `json:"at"`
	Instance       string   `json:"instance"`
	TraceID        string   `json:"trace_id,omitempty"`
	Action         string   `json:"action"`
	Outcome        string   `json:"outcome"`
	ActorKind      string   `json:"actor_kind"`
	ActorPerson    string   `json:"actor_person_id,omitempty"`
	ActorSuperuser bool     `json:"actor_superuser"`
	ActorTitles    []string `json:"actor_titles"`
	Subject        string   `json:"subject_person_id,omitempty"`
	RecordKind     string   `json:"record_kind,omitempty"`
	RecordID       string   `json:"record_id,omitempty"`
	Fields         []string `json:"fields"`
	Level          string   `json:"level,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

func uuidText(id [16]byte) string {
	if id == ([16]byte{}) {
		return ""
	}
	const hexd = "0123456789abcdef"
	var b [36]byte
	j := 0
	for i, c := range id {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			b[j] = '-'
			j++
		}
		b[j] = hexd[c>>4]
		b[j+1] = hexd[c&0x0f]
		j += 2
	}
	return string(b[:])
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// encode renders rows as the JSON array the insert statements take as $1.
func encode(op string, rows []Row) ([]byte, error) {
	out := make([]jsonRow, len(rows))
	for i, r := range rows {
		out[i] = jsonRow{
			ID:             uuidText(r.ID),
			At:             r.At.UTC().Format(time.RFC3339Nano),
			Instance:       r.Instance,
			TraceID:        r.TraceID,
			Action:         r.Action,
			Outcome:        r.Outcome,
			ActorKind:      r.ActorKind,
			ActorPerson:    uuidText(r.ActorPerson),
			ActorSuperuser: r.ActorSuperuser,
			ActorTitles:    orEmpty(r.ActorTitles),
			Subject:        uuidText(r.Subject),
			RecordKind:     r.RecordKind,
			RecordID:       uuidText(r.RecordID),
			Fields:         orEmpty(r.Fields),
			Level:          r.Level,
			Reason:         r.Reason,
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("encode batch: %w", err))
	}
	return b, nil
}

// Insert adds every row in one statement, inside the caller's transaction when ctx carries
// one. A duplicate ID fails errs.Conflict. Fails errs.Invalid on a check violation,
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, Timeout/Canceled, and every
// other kind db.Wrap assigns; errs.Internal when a write runs in a Read transaction.
func (s *Store) Insert(ctx context.Context, rows []Row) error {
	const op = "audit.postgres.Insert"
	body, err := encode(op, rows)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, sqlInsert, body); err != nil {
		return db.Wrap(op, err)
	}
	return nil
}

// Import adds the rows with spooled = true, dropping those whose ID is already in the
// table, and returns how many were new. Fails as Insert does except that a duplicate ID is
// not an error.
func (s *Store) Import(ctx context.Context, rows []Row) (int, error) {
	const op = "audit.postgres.Import"
	body, err := encode(op, rows)
	if err != nil {
		return 0, err
	}
	tag, err := s.db.Exec(ctx, sqlImport, body)
	if err != nil {
		return 0, db.Wrap(op, err)
	}
	return int(tag.RowsAffected()), nil
}
