package auditlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/auditlog/postgres"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Search returns one page matching q. Needs an actor (errs.Unauthenticated, F23) holding
// audit.search (errs.Permission, F24). Records the entry {audit.searched, Succeeded,
// Subject: q.Subject, Record: q.Record} before returning (F27). Works on a read-only copy.
// Fails errs.Invalid (F25) or the store's kind (F26).
func (s *Service) Search(ctx context.Context, q audit.Query) (page audit.Page, err error) {
	const op = "auditlog.Service.Search"
	ctx, span := s.tracer.Start(ctx, spanSearch)
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	if err := s.authorize(ctx, op, PermissionSearch); err != nil {
		return audit.Page{}, err
	}
	if err := q.Validate(); err != nil {
		return audit.Page{}, errs.E(op, errs.Invalid, err)
	}
	f := postgres.Filter{
		Actor: q.Actor, Subject: q.Subject, RecordKind: q.Record.Kind, RecordID: q.Record.ID,
		Area: q.Area, Action: string(q.Action),
		From: q.Within.Start, To: q.Within.End, SuperuserOnly: q.SuperuserOnly,
	}
	if q.After != "" {
		at, id, perr := q.After.Position()
		if perr != nil { // unreachable after Validate
			return audit.Page{}, fieldErr(op, "after", validate.CodeMalformed, "is not a cursor from a previous page")
		}
		f.AfterAt, f.AfterID = at, id
	}
	limit := q.PageSize()

	// The entry is recorded before the query, outside the read transaction (audit.md decision 12).
	searched := audit.Entry{Action: actionSearch, Outcome: audit.Succeeded, Subject: q.Subject, Record: q.Record}
	if err := s.rec.Record(ctx, searched); err != nil {
		return audit.Page{}, fmt.Errorf("%s: record the search: %w", op, err)
	}

	var rows []postgres.Row
	err = s.db.Transact(ctx, db.Read, func(ctx context.Context) error {
		var serr error
		rows, serr = s.store.Search(ctx, f, limit+1)
		if serr != nil {
			return fmt.Errorf("query: %w", serr)
		}
		return nil
	})
	if err != nil {
		return audit.Page{}, fmt.Errorf("%s: %w", op, err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := audit.Page{Records: make([]audit.Record, len(rows))}
	for i, row := range rows {
		if out.Records[i], err = fromRow(row); err != nil {
			return audit.Page{}, fmt.Errorf("%s: %w", op, err)
		}
	}
	if more {
		last := out.Records[len(out.Records)-1]
		out.Next = audit.NewCursor(last.At, last.ID)
	}
	return out, nil
}

// Export writes every entry whose Subject is person, oldest first, as JSON Lines
// (application/x-ndjson, one Record per line) to w, and returns how many. Needs an actor
// holding audit.export (F29). Records {audit.exported, Succeeded, Subject: person} before
// the first line is written (F31; a failed export is still on record). Fails errs.Invalid
// for a zero person (F28), errs.Canceled wrapping the write error when w fails (F30), or
// the store's kind.
func (s *Service) Export(ctx context.Context, person model.PersonID, w io.Writer) (n int, err error) {
	const op = "auditlog.Service.Export"
	ctx, span := s.tracer.Start(ctx, spanExport)
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
	if err := s.authorize(ctx, op, PermissionExport); err != nil {
		return 0, err
	}
	exported := audit.Entry{Action: actionExport, Outcome: audit.Succeeded, Subject: person}
	if err := s.rec.Record(ctx, exported); err != nil {
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
		err := s.db.Transact(ctx, db.Read, func(ctx context.Context) error {
			var serr error
			rows, serr = s.store.Export(ctx, person, afterAt, afterID, audit.MaxPageSize)
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
			line, err := s.encode(rec)
			if err != nil {
				return n, errs.E(op, errs.Internal, err)
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				return n, errs.E(op, errs.Canceled, fmt.Errorf("write export: %w", err))
			}
			n++
		}
		if len(rows) < audit.MaxPageSize {
			return n, nil
		}
		last := rows[len(rows)-1]
		afterAt, afterID = last.At, last.ID
	}
}

// encodeRecord is the default export line encoder: the record's wire form, no trailing
// newline.
func encodeRecord(r audit.Record) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode record: %w", err)
	}
	return b, nil
}

// CountAbout returns how many entries name person as actor or subject. datarights calls it
// inside an already authorized deletion to report Kept{Kind: "audit.entry"}; no permission
// of its own. Fails errs.Invalid for a zero person, or the store's kind.
func (s *Service) CountAbout(ctx context.Context, person model.PersonID) (int, error) {
	const op = "auditlog.Service.CountAbout"
	if person.IsZero() {
		return 0, fieldErr(op, "person", validate.CodeRequired, "must be set")
	}
	n, err := s.store.CountAbout(ctx, person)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return n, nil
}

// purge deletes entries with At < before through core.audit_purge, in one transaction that
// sets the guard flag, and records {audit.purged, Succeeded, Actor: ActorSystem} in the
// same transaction. The retention job (Start) is its consumer. Fails errs.Invalid when
// before is zero or in the future (F40); errs.Unavailable wrapping db.ErrReadOnly on a
// read-only copy (F41); the store's kind otherwise.
func (s *Service) purge(ctx context.Context, before time.Time) (deleted int64, err error) {
	const op = "auditlog.Service.purge"
	ctx, span := s.tracer.Start(ctx, spanPurge)
	defer span.End()
	defer func() {
		span.SetAttrs(telemetry.Int64("deleted", deleted))
		if err != nil {
			span.RecordError(err)
		}
	}()

	if before.IsZero() || before.After(s.clock.Now()) {
		return 0, fieldErr(op, "before", validate.CodeRange, "must be set and not in the future")
	}
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		n, perr := s.store.Purge(ctx, before)
		if perr != nil {
			return fmt.Errorf("purge: %w", perr)
		}
		deleted = n
		purged := audit.Entry{Action: actionPurged, Outcome: audit.Succeeded, Actor: audit.Actor{Kind: audit.ActorSystem}}
		if rerr := s.rec.Record(ctx, purged); rerr != nil {
			return fmt.Errorf("record the purge: %w", rerr)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return deleted, nil
}

// fromRow converts a stored row back to a record. A value the schema's CHECK constraints
// should have refused fails errs.Internal (a broken invariant, not bad input).
func fromRow(row postgres.Row) (audit.Record, error) {
	const op = "auditlog.fromRow"
	rec := audit.Record{
		ID:       row.ID,
		At:       row.At.UTC(),
		Instance: row.Instance,
		TraceID:  row.TraceID,
		Spooled:  row.Spooled,
		Entry: audit.Entry{
			Action:  audit.Action(row.Action),
			Subject: row.Subject,
			Record:  audit.Ref{Kind: row.RecordKind, ID: row.RecordID},
			Fields:  row.Fields,
			Reason:  row.Reason,
			Actor: audit.Actor{
				Person:    row.ActorPerson,
				Superuser: row.ActorSuperuser,
				Titles:    row.ActorTitles,
			},
		},
	}
	var err error
	if rec.Outcome, err = audit.ParseOutcome(row.Outcome); err != nil {
		return audit.Record{}, errs.E(op, errs.Internal, errors.New("stored outcome is unknown"))
	}
	kind, ok := audit.ParseActorKind(row.ActorKind)
	if !ok {
		return audit.Record{}, errs.E(op, errs.Internal, errors.New("stored actor kind is unknown"))
	}
	rec.Actor.Kind = kind
	if row.Level != "" {
		if rec.Level, err = model.ParseSensitivity(row.Level); err != nil {
			return audit.Record{}, errs.E(op, errs.Internal, errors.New("stored level is unknown"))
		}
	}
	return rec, nil
}
