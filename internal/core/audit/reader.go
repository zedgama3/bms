package audit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Query selects entries. Every field is optional; empty means "any". Results are ordered
// newest first (At desc, ID desc) and paged by keyset.
type Query struct {
	Actor         model.PersonID
	Subject       model.PersonID
	Record        Ref
	Area          string          // Action.Area() == Area
	Action        Action          // exact action
	Within        model.TimeRange // [Start, End); zero means any time
	SuperuserOnly bool
	After         Cursor // Page.Next of the previous page
	Limit         int    // 0 = DefaultPageSize; at most MaxPageSize
}

// Validate fails errs.Invalid with field errors ("record", "area", "action", "within",
// "after", "limit"). Messages never echo a value.
func (q Query) Validate() error {
	v := validate.Errors{Op: "audit.Query.Validate"}
	if q.Record.Validate() != nil {
		v.Addf("record", codeMalformed, "must be a kind and an id together, the kind as <area>.<ident>")
	}
	if q.Area != "" && (len(q.Area) > 32 || !searchAreaRE.MatchString(q.Area)) {
		v.Addf("area", codeMalformed, "must be a lower-case area name")
	}
	if q.Action != "" {
		if q.Action.Validate() != nil {
			v.Addf("action", codeMalformed, "must be <area>.<ident>(.<ident>)*")
		} else if q.Area != "" && q.Action.Area() != q.Area {
			v.Addf("action", codeMismatch, "must belong to the area filter")
		}
	}
	if q.Within != (model.TimeRange{}) && q.Within.Validate() != nil {
		v.Addf("within", validate.CodeRange, "must have a start before its end, both set")
	}
	if q.After != "" {
		if _, _, err := q.After.parse(); err != nil {
			v.Addf("after", codeMalformed, "is not a cursor from a previous page")
		}
	}
	if q.Limit < 0 || q.Limit > MaxPageSize {
		v.Addf("limit", validate.CodeRange, "must be between 0 and %d", MaxPageSize)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func (q Query) pageSize() int {
	if q.Limit == 0 {
		return DefaultPageSize
	}
	return q.Limit
}

// Cursor is an opaque, URL-safe continuation token (At and ID of the last record).
type Cursor string

func newCursor(at time.Time, id RecordID) Cursor {
	raw := strconv.FormatInt(at.UnixMicro(), 10) + "." + id.String()
	return Cursor(base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

// parse returns the cursor's position. The error says nothing about the input.
func (c Cursor) parse() (time.Time, RecordID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(string(c))
	if err != nil {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	micro, id, ok := strings.Cut(string(raw), ".")
	if !ok {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	n, err := strconv.ParseInt(micro, 10, 64)
	if err != nil {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	rid, err := model.ParseID[RecordID](id)
	if err != nil {
		return time.Time{}, RecordID{}, errors.New("malformed cursor")
	}
	return time.UnixMicro(n).UTC(), rid, nil
}

// Page is one page of results. Next is "" on the last page.
type Page struct {
	Records []Record
	Next    Cursor
}

// authorize decides that the actor in ctx is signed in and holds p.
func (l *Log) authorize(ctx context.Context, op string, p authz.Permission) error {
	actor, ok := l.opts.Actor(ctx)
	if !ok || actor.Kind != ActorPerson {
		return errs.E(op, errs.Unauthenticated, errors.New("sign in to use the audit log"))
	}
	if err := l.perms.Allow(ctx, p); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// Search returns one page matching q. Needs an actor (errs.Unauthenticated, F23) holding
// audit.search (errs.Permission, F24). Records the entry {audit.searched, Succeeded,
// Subject: q.Subject, Record: q.Record} before returning (F27). Works on a read-only copy.
// Fails errs.Invalid (F25) or the store's kind (F26).
func (l *Log) Search(ctx context.Context, q Query) (page Page, err error) {
	const op = "audit.Log.Search"
	ctx, span := l.tracer.Start(ctx, spanSearch)
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	if err := l.authorize(ctx, op, PermissionSearch); err != nil {
		return Page{}, err
	}
	if err := q.Validate(); err != nil {
		return Page{}, errs.E(op, errs.Invalid, err)
	}
	f := postgres.Filter{
		Actor: q.Actor, Subject: q.Subject, RecordKind: q.Record.Kind, RecordID: q.Record.ID,
		Area: q.Area, Action: string(q.Action),
		From: q.Within.Start, To: q.Within.End, SuperuserOnly: q.SuperuserOnly,
	}
	if q.After != "" {
		at, id, perr := q.After.parse()
		if perr != nil { // unreachable after Validate
			return Page{}, fieldErr(op, "after", codeMalformed, "is not a cursor from a previous page")
		}
		f.AfterAt, f.AfterID = at, id
	}
	limit := q.pageSize()

	// The entry is recorded before the query, outside the read transaction (decision 12).
	searched := Entry{Action: actionSearch, Outcome: Succeeded, Subject: q.Subject, Record: q.Record}
	if err := l.record(ctx, ownArea, []Entry{searched}); err != nil {
		return Page{}, fmt.Errorf("%s: record the search: %w", op, err)
	}

	var rows []postgres.Row
	err = l.opts.DB.Transact(ctx, db.Read, func(ctx context.Context) error {
		var serr error
		rows, serr = l.store.Search(ctx, f, limit+1)
		if serr != nil {
			return fmt.Errorf("query: %w", serr)
		}
		return nil
	})
	if err != nil {
		return Page{}, fmt.Errorf("%s: %w", op, err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := Page{Records: make([]Record, len(rows))}
	for i, row := range rows {
		if out.Records[i], err = fromRow(row); err != nil {
			return Page{}, fmt.Errorf("%s: %w", op, err)
		}
	}
	if more {
		last := out.Records[len(out.Records)-1]
		out.Next = newCursor(last.At, last.ID)
	}
	return out, nil
}

// Export writes every entry whose Subject is person, oldest first, as JSON Lines
// (application/x-ndjson, one Record per line) to w, and returns how many. Needs an actor
// holding audit.export (F29). Records {audit.exported, Succeeded, Subject: person} before
// the first line is written (F31; a failed export is still on record). Fails errs.Invalid
// for a zero person (F28), errs.Canceled wrapping the write error when w fails (F30), or
// the store's kind.
func (l *Log) Export(ctx context.Context, person model.PersonID, w io.Writer) (n int, err error) {
	const op = "audit.Log.Export"
	ctx, span := l.tracer.Start(ctx, spanExport)
	defer span.End()
	defer func() {
		span.SetAttrs(telemetry.Int("records", n))
		if err != nil {
			span.RecordError(err)
		}
	}()

	if person.IsZero() {
		return 0, fieldErr(op, "person", validate.CodeRequired, "must be set")
	}
	if w == nil {
		return 0, errs.E(op, errs.Internal, errors.New("nil writer"))
	}
	if err := l.authorize(ctx, op, PermissionExport); err != nil {
		return 0, err
	}
	exported := Entry{Action: actionExport, Outcome: Succeeded, Subject: person}
	if err := l.record(ctx, ownArea, []Entry{exported}); err != nil {
		return 0, fmt.Errorf("%s: record the export: %w", op, err)
	}

	var (
		afterAt time.Time
		afterID [16]byte
	)
	for {
		if err := ctx.Err(); err != nil {
			return n, errs.E(op, errs.KindOf(err), err)
		}
		var rows []postgres.Row
		err := l.opts.DB.Transact(ctx, db.Read, func(ctx context.Context) error {
			var serr error
			rows, serr = l.store.Export(ctx, person, afterAt, afterID, MaxPageSize)
			if serr != nil {
				return fmt.Errorf("query: %w", serr)
			}
			return nil
		})
		if err != nil {
			return n, fmt.Errorf("%s: %w", op, err)
		}
		for _, row := range rows {
			rec, err := fromRow(row)
			if err != nil {
				return n, fmt.Errorf("%s: %w", op, err)
			}
			line, err := l.encode(toWire(rec))
			if err != nil {
				return n, errs.E(op, errs.Internal, err)
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				return n, errs.E(op, errs.Canceled, fmt.Errorf("write export: %w", err))
			}
			n++
		}
		if len(rows) < MaxPageSize {
			return n, nil
		}
		last := rows[len(rows)-1]
		afterAt, afterID = last.At, last.ID
	}
}

// CountAbout returns how many entries name person as actor or subject. people calls it
// inside an already authorized deletion to report Kept{Kind: "audit.entry"} (M4); no
// permission of its own. Fails errs.Invalid for a zero person, or the store's kind.
func (l *Log) CountAbout(ctx context.Context, person model.PersonID) (int, error) {
	const op = "audit.Log.CountAbout"
	if person.IsZero() {
		return 0, fieldErr(op, "person", validate.CodeRequired, "must be set")
	}
	n, err := l.store.CountAbout(ctx, person)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return n, nil
}

// Purge deletes entries with At < before through core.audit_purge, in one transaction that
// sets the guard flag, and records {audit.purged, Succeeded, Actor: ActorSystem} in the
// same transaction. The retention job (jobs.md, M4) is its consumer; built with it. Fails
// errs.Invalid when before is zero or in the future (F40); errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy (F41); the store's kind otherwise.
func (l *Log) Purge(ctx context.Context, before time.Time) (deleted int64, err error) {
	const op = "audit.Log.Purge"
	ctx, span := l.tracer.Start(ctx, spanPurge)
	defer span.End()
	defer func() {
		span.SetAttrs(telemetry.Int64("deleted", deleted))
		if err != nil {
			span.RecordError(err)
		}
	}()

	if before.IsZero() || before.After(l.clock.Now()) {
		return 0, fieldErr(op, "before", validate.CodeRange, "must be set and not in the future")
	}
	err = l.opts.DB.Transact(ctx, db.Write, func(ctx context.Context) error {
		n, perr := l.store.Purge(ctx, before)
		if perr != nil {
			return fmt.Errorf("purge: %w", perr)
		}
		deleted = n
		purged := Entry{Action: actionPurged, Outcome: Succeeded, Actor: Actor{Kind: ActorSystem}}
		if rerr := l.record(ctx, ownArea, []Entry{purged}); rerr != nil {
			return fmt.Errorf("record the purge: %w", rerr)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return deleted, nil
}
