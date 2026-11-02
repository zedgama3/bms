// Package postgres is datarights' storage: the only package with SQL and the driver. It
// reads and writes core.data_request, whose migrations stay in people's set (forward-only;
// plan/design/boundaries.md, "Data"); datarights owns who inserts, reads and deletes its
// rows.
package postgres

import (
	"context"
	"embed"
	"errors"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/datarights/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed sql/*.sql
var sqlFiles embed.FS

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlLockDeletion     = mustSQL("lock_person_deletion.sql")
	sqlInsertRequest    = mustSQL("insert_request.sql")
	sqlGetRequest       = mustSQL("get_request.sql")
	sqlRequestForUpdate = mustSQL("get_request_for_update.sql")
	sqlMyRequests       = mustSQL("list_requests_by_person.sql")
	sqlListRequests     = mustSQL("list_requests.sql")
	sqlWithdrawRequest  = mustSQL("withdraw_request.sql")
	sqlDeclineRequest   = mustSQL("decline_request.sql")
	sqlDeleteRequests   = mustSQL("delete_requests_by_person.sql")
	sqlExportRequests   = mustSQL("export_requests_by_person.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("datarights/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the documentation
// that sits above each statement is not sent to the server with every call.
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps core.data_request in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use. Every method runs inside the caller's transaction when ctx carries one.
// Every error is already classified by db.Wrap.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("datarights.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// uuidOrNil is the statement argument for an optional ID: NULL for the zero ID.
func uuidOrNil[T ~[16]byte](id T) any {
	if id == (T{}) {
		return nil
	}
	return [16]byte(id)
}

// idOf converts a scanned optional uuid; NULL is the zero ID.
func idOf[T ~[16]byte](raw *[16]byte) T {
	if raw == nil {
		return T{}
	}
	return T(*raw)
}

// utcOf converts a scanned optional time; NULL is the zero time.
func utcOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func notFound(op, what string) error {
	return errs.E(op, errs.NotFound, errors.New("no such "+what))
}

// scanner is the Scan of a pgx.Row and of a pgx.Rows.
type scanner interface{ Scan(dest ...any) error }

// queryAll runs query and scans every row.
func queryAll[T any](ctx context.Context, d db.DB, op, query string, scan func(scanner) (T, error), args ...any) ([]T, error) {
	rs, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []T{}
	for rs.Next() {
		v, err := scan(rs)
		if err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, v)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// queryOne runs query and scans its first row; ok is false when it returned none.
func queryOne[T any](ctx context.Context, d db.DB, op, query string, scan func(scanner) (T, error), args ...any) (v T, ok bool, err error) {
	all, err := queryAll(ctx, d, op, query, scan, args...)
	if err != nil || len(all) == 0 {
		return v, false, err
	}
	return all[0], true, nil
}

// scanRequest reads the columns R: id, person_id, kind, status, note, created_at,
// decided_at, decided_by, decision_note.
func scanRequest(sc scanner) (rows.Request, error) {
	var (
		r       rows.Request
		id, pid [16]byte
		at      *time.Time
		by      *[16]byte
	)
	if err := sc.Scan(&id, &pid, &r.Kind, &r.Status, &r.Note, &r.CreatedAt, &at, &by, &r.DecisionNote); err != nil {
		return rows.Request{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	r.ID, r.Person = rows.RequestID(id), model.PersonID(pid)
	r.CreatedAt, r.DecidedAt, r.DecidedBy = r.CreatedAt.UTC(), utcOf(at), idOf[model.PersonID](by)
	return r, nil
}

// LockDeletion takes the transaction-scoped advisory lock two deletions of one person
// serialise on (the same lock people's UpdateMe and LinkAccount take). Fails
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Store) LockDeletion(ctx context.Context, id model.PersonID) error {
	const op = "datarights.postgres.LockDeletion"
	_, err := s.db.Exec(ctx, sqlLockDeletion, [16]byte(id))
	return db.Wrap(op, err)
}

// InsertRequest adds an open deletion request. A second open request for the person fails
// errs.Conflict with *db.Error.Constraint data_request_open_idx.
func (s *Store) InsertRequest(ctx context.Context, id rows.RequestID, person model.PersonID, note string, at time.Time) (rows.Request, error) {
	const op = "datarights.postgres.InsertRequest"
	r, ok, err := queryOne(ctx, s.db, op, sqlInsertRequest, scanRequest, [16]byte(id), [16]byte(person), note, at.UTC())
	if err != nil {
		return rows.Request{}, err
	}
	if !ok {
		return rows.Request{}, errs.E(op, errs.Internal, errors.New("insert returned no row"))
	}
	return r, nil
}

// GetRequest returns one request. Fails errs.NotFound when there is none.
func (s *Store) GetRequest(ctx context.Context, id rows.RequestID) (rows.Request, error) {
	return s.request(ctx, "datarights.postgres.GetRequest", sqlGetRequest, id)
}

// RequestForUpdate is GetRequest with a row lock, for a Write transaction.
func (s *Store) RequestForUpdate(ctx context.Context, id rows.RequestID) (rows.Request, error) {
	return s.request(ctx, "datarights.postgres.RequestForUpdate", sqlRequestForUpdate, id)
}

func (s *Store) request(ctx context.Context, op, query string, id rows.RequestID) (rows.Request, error) {
	r, ok, err := queryOne(ctx, s.db, op, query, scanRequest, [16]byte(id))
	if err != nil {
		return rows.Request{}, err
	}
	if !ok {
		return rows.Request{}, notFound(op, "request")
	}
	return r, nil
}

// MyRequests returns the person's own requests, newest first, at most limit.
func (s *Store) MyRequests(ctx context.Context, person model.PersonID, limit int) ([]rows.Request, error) {
	return queryAll(ctx, s.db, "datarights.postgres.MyRequests", sqlMyRequests, scanRequest,
		[16]byte(person), nil, nil, int32(limit)) //nolint:gosec // limit is a small constant
}

// ListRequests returns the requests with status (rows.StatusOpen and the others), oldest
// first, at most limit; a zero person is any person.
func (s *Store) ListRequests(ctx context.Context, status string, person model.PersonID, limit int) ([]rows.Request, error) {
	return queryAll(ctx, s.db, "datarights.postgres.ListRequests", sqlListRequests, scanRequest,
		status, uuidOrNil(person), nil, nil, int32(limit)) //nolint:gosec // limit is a small constant
}

// ExportRequests is every request of the person for the export document, oldest first.
func (s *Store) ExportRequests(ctx context.Context, person model.PersonID) ([]rows.Request, error) {
	return queryAll(ctx, s.db, "datarights.postgres.ExportRequests", sqlExportRequests, scanRequest, [16]byte(person))
}

// WithdrawRequest withdraws the person's own open request; decided is false when it is not
// open or not theirs.
func (s *Store) WithdrawRequest(ctx context.Context, id rows.RequestID, person model.PersonID, at time.Time) (rows.Request, bool, error) {
	return queryOne(ctx, s.db, "datarights.postgres.WithdrawRequest", sqlWithdrawRequest, scanRequest,
		[16]byte(id), [16]byte(person), at.UTC())
}

// DeclineRequest declines an open request; decided is false when it is not open.
func (s *Store) DeclineRequest(ctx context.Context, id rows.RequestID, by model.PersonID, note string, at time.Time) (rows.Request, bool, error) {
	return queryOne(ctx, s.db, "datarights.postgres.DeclineRequest", sqlDeclineRequest, scanRequest,
		[16]byte(id), at.UTC(), uuidOrNil(by), note)
}

// DeleteRequestsOf deletes every request of the person and reports how many.
func (s *Store) DeleteRequestsOf(ctx context.Context, person model.PersonID) (int, error) {
	const op = "datarights.postgres.DeleteRequestsOf"
	rs, err := s.db.Query(ctx, sqlDeleteRequests, [16]byte(person))
	if err != nil {
		return 0, db.Wrap(op, err)
	}
	defer rs.Close()
	n := 0
	for rs.Next() {
		n++
	}
	if err := rs.Err(); err != nil {
		return 0, db.Wrap(op, err)
	}
	return n, nil
}
