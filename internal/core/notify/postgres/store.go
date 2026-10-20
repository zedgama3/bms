// Package postgres is notify's storage: the only package with SQL and the driver. The
// statements are in sql/ and the migration in migrations/; each statement's header names its
// arguments and columns, which the methods here follow.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of notify, rooted at the files (db.md: NNNN_name.sql at
// the set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on
// an embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("notify/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlEnqueue        = mustSQL("enqueue_message.sql")
	sqlLease          = mustSQL("lease_messages.sql")
	sqlMarkSent       = mustSQL("mark_sent.sql")
	sqlMarkFailed     = mustSQL("mark_failed.sql")
	sqlRequeue        = mustSQL("requeue_message.sql")
	sqlRelease        = mustSQL("release_message.sql")
	sqlRetry          = mustSQL("retry_message.sql")
	sqlCancel         = mustSQL("cancel_message.sql")
	sqlStatusOf       = mustSQL("get_message_status.sql")
	sqlCountAll       = mustSQL("count_messages_by_status.sql")
	sqlCountOpen      = mustSQL("count_open_messages_by_status.sql")
	sqlListOutbox     = mustSQL("list_outbox.sql")
	sqlPurge          = mustSQL("purge_messages.sql")
	sqlExpireStale    = mustSQL("expire_stale_messages.sql")
	sqlExportPerson   = mustSQL("export_person_messages.sql")
	sqlDeletePerson   = mustSQL("delete_person_messages.sql")
	sqlGetTemplate    = mustSQL("get_template.sql")
	sqlListTemplates  = mustSQL("list_templates.sql")
	sqlUpsertTemplate = mustSQL("upsert_template.sql")
	sqlDeleteTemplate = mustSQL("delete_template.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("notify/postgres: embedded SQL missing: " + name)
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

// exportBatch is how many rows one ExportPerson statement returns.
const exportBatch = 500

// Store keeps core.notify_message and core.notify_template in PostgreSQL. It holds a db.DB
// and no state; safe for concurrent use. Every method runs inside the caller's transaction
// when ctx carries one. Every error is already classified by db.Wrap. No method logs, and
// no error carries a value from a row.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("notify.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// scanner is the Scan of a pgx.Rows.
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

// returned runs a statement with RETURNING id and reports whether it returned a row.
func (s *Store) returned(ctx context.Context, op, query string, args ...any) (bool, error) {
	ids, err := queryAll(ctx, s.db, op, query, func(sc scanner) ([16]byte, error) {
		var id [16]byte
		err := sc.Scan(&id)
		return id, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}, args...)
	return len(ids) > 0, err
}

// count runs a statement whose one row is one bigint (no row reads as 0).
func (s *Store) count(ctx context.Context, op, query string, args ...any) (int64, error) {
	ns, err := queryAll(ctx, s.db, op, query, func(sc scanner) (int64, error) {
		var n int64
		err := sc.Scan(&n)
		return n, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}, args...)
	if err != nil || len(ns) == 0 {
		return 0, err
	}
	return ns[0], nil
}

// ---- argument helpers -------------------------------------------------------------------

func uuidArg(id [16]byte) any {
	if id == ([16]byte{}) {
		return nil
	}
	return id
}

func textArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func utc(t time.Time) time.Time { return t.UTC() }

func orZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func personOf(p *[16]byte) model.PersonID {
	if p == nil {
		return model.PersonID{}
	}
	return model.PersonID(*p)
}

// ---- the outbox -------------------------------------------------------------------------

// Enqueue inserts one queued row for r (ID, Owner, Template, Key, Person xor Address,
// Language, Subject, Body, ExpiresAt) due at now. It reports false when the row was skipped
// silently: a row for (owner, key, person) exists, a live row for (owner, key, address)
// exists, or the ID collides with another (the conflict clause names no target). Must run in
// the caller's Write transaction.
// Fails errs.Invalid for a constraint violation (a caller bug), errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy.
func (s *Store) Enqueue(ctx context.Context, r message.Record, now time.Time) (bool, error) {
	return s.returned(ctx, "notify.postgres.Enqueue", sqlEnqueue,
		[16]byte(r.ID), r.Owner, r.Template, textArg(r.Key), uuidArg(r.Person), textArg(r.Address),
		r.Language, r.Subject, r.Body, utc(now), utc(r.ExpiresAt))
}

// Lease marks up to limit due rows sending until leasedUntil and returns them with Attempts
// already counting this attempt and LeasedUntil as stored: the fence the four statements
// below take unchanged. Rows past their expiry are never leased. Must run in a short Write
// transaction.
func (s *Store) Lease(ctx context.Context, now time.Time, limit int, leasedUntil time.Time) ([]message.Record, error) {
	return queryAll(ctx, s.db, "notify.postgres.Lease", sqlLease, func(sc scanner) (message.Record, error) {
		var (
			r        message.Record
			id       [16]byte
			person   *[16]byte
			addr     *string
			subj, bd *string
		)
		if err := sc.Scan(&id, &r.Owner, &r.Template, &person, &addr, &subj, &bd, &r.Attempts, &r.NextAttemptAt, &r.LeasedUntil, &r.ExpiresAt); err != nil {
			return message.Record{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		r.ID = message.ID(id)
		r.Person = personOf(person)
		r.Address, r.Subject, r.Body = deref(addr), deref(subj), deref(bd)
		r.NextAttemptAt, r.LeasedUntil, r.ExpiresAt = r.NextAttemptAt.UTC(), r.LeasedUntil.UTC(), r.ExpiresAt.UTC()
		r.Status = message.Sending
		return r, nil
	}, utc(now), limit, utc(leasedUntil))
}

// MarkSent ends a leased row as sent, clearing its address and text. lease is the
// LeasedUntil Lease returned, the fence. It reports false when the row is gone or was
// re-leased (nothing changed).
func (s *Store) MarkSent(ctx context.Context, id message.ID, lease, at time.Time) (bool, error) {
	return s.returned(ctx, "notify.postgres.MarkSent", sqlMarkSent, [16]byte(id), utc(lease), utc(at))
}

// MarkFailed ends a leased row as failed with a token. It keeps the address and text, so an
// admin can retry the row; ExpireStale clears them at expires_at.
func (s *Store) MarkFailed(ctx context.Context, id message.ID, lease time.Time, token string, at time.Time) (bool, error) {
	return s.returned(ctx, "notify.postgres.MarkFailed", sqlMarkFailed, [16]byte(id), utc(lease), token, utc(at))
}

// Requeue puts a leased row back to queued, due at next, keeping the attempt counted.
func (s *Store) Requeue(ctx context.Context, id message.ID, lease time.Time, token string, next time.Time) (bool, error) {
	return s.returned(ctx, "notify.postgres.Requeue", sqlRequeue, [16]byte(id), utc(lease), token, utc(next))
}

// Release puts a leased row back to queued, due now, without counting the attempt.
func (s *Store) Release(ctx context.Context, id message.ID, lease, now time.Time, token string) (bool, error) {
	return s.returned(ctx, "notify.postgres.Release", sqlRelease, [16]byte(id), utc(lease), utc(now), token)
}

// Retry requeues a failed row that still has its text, with attempts 0, due now. It reports
// false when nothing matched: ask StatusOf why.
func (s *Store) Retry(ctx context.Context, id message.ID, now time.Time) (bool, error) {
	return s.returned(ctx, "notify.postgres.Retry", sqlRetry, [16]byte(id), utc(now))
}

// Cancel deletes a queued or failed row. It reports false when nothing matched: ask
// StatusOf why.
func (s *Store) Cancel(ctx context.Context, id message.ID) (bool, error) {
	return s.returned(ctx, "notify.postgres.Cancel", sqlCancel, [16]byte(id))
}

// StatusOf returns the status, expiry and whether the text is still held. Fails
// errs.NotFound when there is no such row.
func (s *Store) StatusOf(ctx context.Context, id message.ID) (message.State, error) {
	const op = "notify.postgres.StatusOf"
	var (
		text string
		st   message.State
	)
	if err := s.db.QueryRow(ctx, sqlStatusOf, [16]byte(id)).Scan(&text, &st.ExpiresAt, &st.HasText); err != nil {
		return message.State{}, db.Wrap(op, err)
	}
	var err error
	if st.Status, err = message.ParseStatus(text); err != nil {
		return message.State{}, errs.E(op, errs.Internal, err)
	}
	st.ExpiresAt = st.ExpiresAt.UTC()
	return st, nil
}

func (s *Store) counts(ctx context.Context, op, query string) (map[message.Status]int, error) {
	type pair struct {
		st message.Status
		n  int
	}
	ps, err := queryAll(ctx, s.db, op, query, func(sc scanner) (pair, error) {
		var (
			text string
			n    int64
		)
		if err := sc.Scan(&text, &n); err != nil {
			return pair{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		st, err := message.ParseStatus(text)
		if err != nil {
			return pair{}, errs.E(op, errs.Internal, err)
		}
		return pair{st, int(n)}, nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[message.Status]int, len(ps))
	for _, p := range ps {
		out[p.st] = p.n
	}
	return out, nil
}

// CountByStatus counts every row by status (reads the whole table once: for the admin
// screen). A status with no rows is absent.
func (s *Store) CountByStatus(ctx context.Context) (map[message.Status]int, error) {
	return s.counts(ctx, "notify.postgres.CountByStatus", sqlCountAll)
}

// CountOpenByStatus counts the rows that are not sent (cheap: for the per-tick gauge).
func (s *Store) CountOpenByStatus(ctx context.Context) (map[message.Status]int, error) {
	return s.counts(ctx, "notify.postgres.CountOpenByStatus", sqlCountOpen)
}

// ListOutbox returns the newest limit rows that are not sent, without address, subject or
// body (Language, LeasedUntil, SentAt and FailedAt are not selected).
func (s *Store) ListOutbox(ctx context.Context, limit int) ([]message.Record, error) {
	const op = "notify.postgres.ListOutbox"
	return queryAll(ctx, s.db, op, sqlListOutbox, func(sc scanner) (message.Record, error) {
		var (
			r      message.Record
			id     [16]byte
			person *[16]byte
			text   string
			lastE  *string
		)
		if err := sc.Scan(&id, &r.Owner, &r.Template, &person, &text, &r.Attempts, &lastE, &r.CreatedAt, &r.NextAttemptAt, &r.ExpiresAt); err != nil {
			return message.Record{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		st, err := message.ParseStatus(text)
		if err != nil {
			return message.Record{}, errs.E(op, errs.Internal, err)
		}
		r.ID, r.Person, r.Status, r.LastError = message.ID(id), personOf(person), st, deref(lastE)
		r.CreatedAt, r.NextAttemptAt, r.ExpiresAt = r.CreatedAt.UTC(), r.NextAttemptAt.UTC(), r.ExpiresAt.UTC()
		return r, nil
	}, limit, nil, nil)
}

// Purge deletes up to limit final rows completed before before and returns how many.
func (s *Store) Purge(ctx context.Context, before time.Time, limit int) (int64, error) {
	return s.count(ctx, "notify.postgres.Purge", sqlPurge, utc(before), limit)
}

// ExpireStale clears the text of up to limit rows past their expiry and reports how many
// rows changed (ended) and how many of those became failed "expired" in this call (a failed
// row is only cleared, so it counts in changed only; expired <= changed).
func (s *Store) ExpireStale(ctx context.Context, now time.Time, limit int) (changed, expired int64, err error) {
	const op = "notify.postgres.ExpireStale"
	type pair struct{ changed, expired int64 }
	ps, err := queryAll(ctx, s.db, op, sqlExpireStale, func(sc scanner) (pair, error) {
		var p pair
		err := sc.Scan(&p.changed, &p.expired)
		return p, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}, utc(now), limit)
	if err != nil || len(ps) == 0 {
		return 0, 0, err
	}
	return ps[0].changed, ps[0].expired, nil
}

// ExportPerson returns every row addressed to person, oldest first.
func (s *Store) ExportPerson(ctx context.Context, person model.PersonID) ([]message.Exported, error) {
	const op = "notify.postgres.ExportPerson"
	var (
		out     []message.Exported
		afterAt any
		afterID any
	)
	for {
		batch, err := queryAll(ctx, s.db, op, sqlExportPerson, func(sc scanner) (message.Exported, error) {
			var (
				e      message.Exported
				id     [16]byte
				text   string
				sentAt *time.Time
				subj   *string
			)
			if err := sc.Scan(&id, &e.Template, &text, &e.Language, &e.CreatedAt, &sentAt, &subj); err != nil {
				return message.Exported{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
			}
			st, err := message.ParseStatus(text)
			if err != nil {
				return message.Exported{}, errs.E(op, errs.Internal, err)
			}
			e.ID, e.Status, e.CreatedAt, e.SentAt, e.Subject = message.ID(id), st, e.CreatedAt.UTC(), orZero(sentAt), deref(subj)
			return e, nil
		}, [16]byte(person), afterAt, afterID, exportBatch)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < exportBatch {
			return out, nil
		}
		last := batch[len(batch)-1]
		afterAt, afterID = last.CreatedAt, [16]byte(last.ID)
	}
}

// DeletePerson deletes every row addressed to person, whatever its status, and returns how
// many. It runs on the ctx it is given (people's transaction).
func (s *Store) DeletePerson(ctx context.Context, person model.PersonID) (int64, error) {
	return s.count(ctx, "notify.postgres.DeletePerson", sqlDeletePerson, [16]byte(person))
}

// ---- template overrides -----------------------------------------------------------------

// Override returns one override with its text. Fails errs.NotFound when there is none.
func (s *Store) Override(ctx context.Context, name, lang string) (message.Override, error) {
	const op = "notify.postgres.Override"
	var (
		o  message.Override
		by *[16]byte
	)
	if err := s.db.QueryRow(ctx, sqlGetTemplate, name, lang).Scan(&o.Name, &o.Language, &o.Subject, &o.Body, &by, &o.UpdatedAt); err != nil {
		return message.Override{}, db.Wrap(op, err)
	}
	o.UpdatedBy, o.UpdatedAt = personOf(by), o.UpdatedAt.UTC()
	return o, nil
}

// Overrides lists every override without its text (Subject and Body are empty), sorted by
// name and language.
func (s *Store) Overrides(ctx context.Context) ([]message.Override, error) {
	return queryAll(ctx, s.db, "notify.postgres.Overrides", sqlListTemplates, func(sc scanner) (message.Override, error) {
		var o message.Override
		if err := sc.Scan(&o.Name, &o.Language, &o.UpdatedAt); err != nil {
			return message.Override{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		o.UpdatedAt = o.UpdatedAt.UTC()
		return o, nil
	})
}

// PutOverride saves or replaces an override. Must run in the caller's Write transaction.
func (s *Store) PutOverride(ctx context.Context, o message.Override) error {
	const op = "notify.postgres.PutOverride"
	var name, lang string
	var at time.Time
	if err := s.db.QueryRow(ctx, sqlUpsertTemplate, o.Name, o.Language, o.Subject, o.Body,
		uuidArg(o.UpdatedBy), utc(o.UpdatedAt)).Scan(&name, &lang, &at); err != nil {
		return db.Wrap(op, err)
	}
	return nil
}

// ResetOverride removes an override and reports whether one existed.
func (s *Store) ResetOverride(ctx context.Context, name, lang string) (bool, error) {
	rs, err := queryAll(ctx, s.db, "notify.postgres.ResetOverride", sqlDeleteTemplate, func(sc scanner) (string, error) {
		var n string
		err := sc.Scan(&n)
		return n, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}, name, lang)
	return len(rs) > 0, err
}
