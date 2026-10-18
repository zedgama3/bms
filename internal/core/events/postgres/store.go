// Package postgres is events' storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of events, rooted at the files (db.md: NNNN_name.sql at
// the set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on
// an embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("events/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlListEvents   = mustSQL("list_events.sql")
	sqlGetEvent     = mustSQL("get_event.sql")
	sqlGetForUpdate = mustSQL("get_event_for_update.sql")
	sqlInsertEvent  = mustSQL("insert_event.sql")
	sqlUpdateEvent  = mustSQL("update_event.sql")
	sqlCloseEvent   = mustSQL("close_event.sql")
	sqlReopenEvent  = mustSQL("reopen_event.sql")
	sqlInfoOf       = mustSQL("info_of.sql")
	sqlListKinds    = mustSQL("list_kinds.sql")
	sqlGetKind      = mustSQL("get_kind.sql")
	sqlInsertKind   = mustSQL("insert_kind.sql")
	sqlUpdateKind   = mustSQL("update_kind.sql")
	sqlListItems    = mustSQL("list_schedule_items.sql")
	sqlCountItems   = mustSQL("count_schedule_items.sql")
	sqlGetItem      = mustSQL("get_schedule_item.sql")
	sqlInsertItem   = mustSQL("insert_schedule_item.sql")
	sqlUpdateItem   = mustSQL("update_schedule_item.sql")
	sqlDeleteItem   = mustSQL("delete_schedule_item.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("events/postgres: embedded SQL missing: " + name)
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

// Store keeps core.event_kind, core.event and core.event_schedule_item in PostgreSQL. It
// holds a db.DB and no state; safe for concurrent use. Every method runs inside the
// caller's transaction when ctx carries one. Every error is already classified by db.Wrap.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("events.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
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

// queryOne runs query and returns its first row; none is errs.NotFound.
func queryOne[T any](ctx context.Context, d db.DB, op, what, query string, scan func(scanner) (T, error), args ...any) (T, error) {
	all, err := queryAll(ctx, d, op, query, scan, args...)
	var zero T
	if err != nil {
		return zero, err
	}
	if len(all) == 0 {
		return zero, errs.E(op, errs.NotFound, errors.New("no such "+what))
	}
	return all[0], nil
}

// notInState turns the NotFound of a state change (zero rows: the row was not in the state
// the statement needs) into Conflict; the caller has locked the row and checked, so this is
// a safety net.
func notInState(op string, err error) error {
	if errs.Is(err, errs.NotFound) {
		return errs.E(op, errs.Conflict, errors.New("the event is not in the state the change needs"))
	}
	return err
}

func ptrArg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func timeArg(p *time.Time) any {
	if p == nil {
		return nil
	}
	return p.UTC()
}

// ---- scanners ---------------------------------------------------------------------------

// scanEvent reads id, name, kind, starts_at, ends_at, zone, closed_at, created_at,
// updated_at.
func scanEvent(sc scanner) (rows.Event, error) {
	var (
		e      rows.Event
		id     [16]byte
		closed *time.Time
	)
	if err := sc.Scan(&id, &e.Name, &e.Kind, &e.Starts, &e.Ends, &e.Zone, &closed, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return rows.Event{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	e.ID = model.EventID(id)
	e.Starts, e.Ends, e.CreatedAt, e.UpdatedAt = e.Starts.UTC(), e.Ends.UTC(), e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	if closed != nil {
		e.ClosedAt = closed.UTC()
	}
	return e, nil
}

// scanInfo reads id, name, kind, starts_at, ends_at, zone, closed.
func scanInfo(sc scanner) (rows.Info, error) {
	var (
		i  rows.Info
		id [16]byte
	)
	if err := sc.Scan(&id, &i.Name, &i.Kind, &i.Starts, &i.Ends, &i.Zone, &i.Closed); err != nil {
		return rows.Info{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	i.ID = model.EventID(id)
	i.Starts, i.Ends = i.Starts.UTC(), i.Ends.UTC()
	return i, nil
}

// scanKind reads key, name, builtin, created_at, updated_at.
func scanKind(sc scanner) (rows.Kind, error) {
	var k rows.Kind
	if err := sc.Scan(&k.Key, &k.Name, &k.Builtin, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return rows.Kind{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	k.CreatedAt, k.UpdatedAt = k.CreatedAt.UTC(), k.UpdatedAt.UTC()
	return k, nil
}

// scanItem reads id, event_id, key, label, starts_at, ends_at, position.
func scanItem(sc scanner) (rows.ScheduleItem, error) {
	var (
		it       rows.ScheduleItem
		id, eid  [16]byte
		ends     *time.Time
		position int32
	)
	if err := sc.Scan(&id, &eid, &it.Key, &it.Label, &it.Starts, &ends, &position); err != nil {
		return rows.ScheduleItem{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	it.ID, it.Event, it.Position = rows.ScheduleItemID(id), model.EventID(eid), int(position)
	it.Starts = it.Starts.UTC()
	if ends != nil {
		it.Ends = ends.UTC()
	}
	return it, nil
}

// ---- events -----------------------------------------------------------------------------

// ListEvents returns the events newest first: every event for a nil closed, the closed ones
// for true, the open ones for false.
func (s *Store) ListEvents(ctx context.Context, closed *bool) ([]rows.Event, error) {
	var arg any
	if closed != nil {
		arg = *closed
	}
	return queryAll(ctx, s.db, "events.postgres.ListEvents", sqlListEvents, scanEvent, arg)
}

// GetEvent returns one event. Fails errs.NotFound when there is none.
func (s *Store) GetEvent(ctx context.Context, id model.EventID) (rows.Event, error) {
	return queryOne(ctx, s.db, "events.postgres.GetEvent", "event", sqlGetEvent, scanEvent, [16]byte(id))
}

// GetEventForUpdate is GetEvent with a row lock, for a Write transaction.
func (s *Store) GetEventForUpdate(ctx context.Context, id model.EventID) (rows.Event, error) {
	return queryOne(ctx, s.db, "events.postgres.GetEventForUpdate", "event", sqlGetForUpdate, scanEvent, [16]byte(id))
}

// InsertEvent inserts e with its CreatedAt as UpdatedAt too. Fails errs.Conflict for a name
// in use (constraint event_name_key), errs.Invalid for an unknown kind (event_kind_fkey) or
// dates out of order (event_dates_chk), and errs.Unavailable wrapping db.ErrReadOnly on a
// read-only copy.
func (s *Store) InsertEvent(ctx context.Context, e rows.Event) (rows.Event, error) {
	return queryOne(ctx, s.db, "events.postgres.InsertEvent", "event", sqlInsertEvent, scanEvent,
		[16]byte(e.ID), e.Name, e.Kind, e.Starts.UTC(), e.Ends.UTC(), e.Zone, e.CreatedAt.UTC())
}

// UpdateEvent applies the patch. Fails errs.NotFound when there is no such event, and as
// InsertEvent does.
func (s *Store) UpdateEvent(ctx context.Context, p rows.Patch, now time.Time) (rows.Event, error) {
	return queryOne(ctx, s.db, "events.postgres.UpdateEvent", "event", sqlUpdateEvent, scanEvent,
		[16]byte(p.ID), ptrArg(p.Name), ptrArg(p.Kind), ptrArg(p.Zone), timeArg(p.Starts), timeArg(p.Ends), now.UTC())
}

// CloseEvent closes an open event. Fails errs.Conflict when it is not open or unknown.
func (s *Store) CloseEvent(ctx context.Context, id model.EventID, now time.Time) (rows.Event, error) {
	const op = "events.postgres.CloseEvent"
	e, err := queryOne(ctx, s.db, op, "event", sqlCloseEvent, scanEvent, [16]byte(id), now.UTC())
	return e, notInState(op, err)
}

// ReopenEvent reopens a closed event. Fails errs.Conflict when it is not closed or unknown.
func (s *Store) ReopenEvent(ctx context.Context, id model.EventID, now time.Time) (rows.Event, error) {
	const op = "events.postgres.ReopenEvent"
	e, err := queryOne(ctx, s.db, op, "event", sqlReopenEvent, scanEvent, [16]byte(id), now.UTC())
	return e, notInState(op, err)
}

// InfoOf returns the events among ids; unknown IDs are omitted.
func (s *Store) InfoOf(ctx context.Context, ids []model.EventID) ([]rows.Info, error) {
	raw := make([][16]byte, len(ids))
	for i, id := range ids {
		raw[i] = [16]byte(id)
	}
	return queryAll(ctx, s.db, "events.postgres.InfoOf", sqlInfoOf, scanInfo, raw)
}

// ---- kinds ------------------------------------------------------------------------------

// ListKinds returns every kind, the builtin ones first.
func (s *Store) ListKinds(ctx context.Context) ([]rows.Kind, error) {
	return queryAll(ctx, s.db, "events.postgres.ListKinds", sqlListKinds, scanKind)
}

// GetKind returns one kind. Fails errs.NotFound when there is none.
func (s *Store) GetKind(ctx context.Context, key string) (rows.Kind, error) {
	return queryOne(ctx, s.db, "events.postgres.GetKind", "kind", sqlGetKind, scanKind, key)
}

// InsertKind adds a kind that is not builtin. Fails errs.Conflict for a key in use
// (constraint event_kind_pkey).
func (s *Store) InsertKind(ctx context.Context, key, name string, now time.Time) (rows.Kind, error) {
	return queryOne(ctx, s.db, "events.postgres.InsertKind", "kind", sqlInsertKind, scanKind, key, name, now.UTC())
}

// UpdateKind renames a kind. Fails errs.NotFound when there is none.
func (s *Store) UpdateKind(ctx context.Context, key, name string, now time.Time) (rows.Kind, error) {
	return queryOne(ctx, s.db, "events.postgres.UpdateKind", "kind", sqlUpdateKind, scanKind, key, name, now.UTC())
}

// ---- schedule ---------------------------------------------------------------------------

// ListScheduleItems returns one event's schedule sorted by start, position, ID.
func (s *Store) ListScheduleItems(ctx context.Context, event model.EventID) ([]rows.ScheduleItem, error) {
	return queryAll(ctx, s.db, "events.postgres.ListScheduleItems", sqlListItems, scanItem, [16]byte(event))
}

// CountScheduleItems counts one event's schedule items.
func (s *Store) CountScheduleItems(ctx context.Context, event model.EventID) (int, error) {
	const op = "events.postgres.CountScheduleItems"
	var n int64
	if err := s.db.QueryRow(ctx, sqlCountItems, [16]byte(event)).Scan(&n); err != nil {
		return 0, db.Wrap(op, err)
	}
	return int(n), nil
}

// GetScheduleItem returns one item of one event. Fails errs.NotFound when there is none,
// or it belongs to another event.
func (s *Store) GetScheduleItem(ctx context.Context, event model.EventID, id rows.ScheduleItemID) (rows.ScheduleItem, error) {
	return queryOne(ctx, s.db, "events.postgres.GetScheduleItem", "schedule item", sqlGetItem, scanItem,
		[16]byte(event), [16]byte(id))
}

func endsArg(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// InsertScheduleItem adds the item at the next position of its event; the caller holds the
// event's lock. Fails errs.Conflict for a key in use in the event (event_schedule_key).
func (s *Store) InsertScheduleItem(ctx context.Context, it rows.ScheduleItem, now time.Time) (rows.ScheduleItem, error) {
	return queryOne(ctx, s.db, "events.postgres.InsertScheduleItem", "schedule item", sqlInsertItem, scanItem,
		[16]byte(it.ID), [16]byte(it.Event), it.Key, it.Label, it.Starts.UTC(), endsArg(it.Ends), now.UTC())
}

// UpdateScheduleItem replaces the key, label and times of one item of one event. Fails
// errs.NotFound when there is none, or it belongs to another event.
func (s *Store) UpdateScheduleItem(ctx context.Context, it rows.ScheduleItem, now time.Time) (rows.ScheduleItem, error) {
	return queryOne(ctx, s.db, "events.postgres.UpdateScheduleItem", "schedule item", sqlUpdateItem, scanItem,
		[16]byte(it.Event), [16]byte(it.ID), it.Key, it.Label, it.Starts.UTC(), endsArg(it.Ends), now.UTC())
}

// DeleteScheduleItem deletes one item of one event. Fails errs.NotFound when there is none,
// or it belongs to another event.
func (s *Store) DeleteScheduleItem(ctx context.Context, event model.EventID, id rows.ScheduleItemID) error {
	const op = "events.postgres.DeleteScheduleItem"
	tag, err := s.db.Exec(ctx, sqlDeleteItem, [16]byte(event), [16]byte(id))
	if err != nil {
		return db.Wrap(op, err)
	}
	if tag.RowsAffected() == 0 {
		return errs.E(op, errs.NotFound, errors.New("no such schedule item"))
	}
	return nil
}
