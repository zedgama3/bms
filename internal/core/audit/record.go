package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// record is the one write path: Scoped recorders and the log's own entries (searched,
// exported, purged) both come through it. area is the only area the entries may use.
func (l *Log) record(ctx context.Context, area string, entries []Entry) (err error) {
	const op = "audit.Log.Record"
	traceID, _ := telemetry.TraceIDFrom(ctx) // the caller's trace, not the span below, so no span means no trace ID
	ctx, span := l.tracer.Start(ctx, spanRecord)
	defer span.End()
	span.SetAttrs(telemetry.Int("entries", len(entries)), telemetry.Token("area", area))
	defer func() {
		if err != nil {
			span.RecordError(err)
			l.metrics.fail(ctx, err)
		}
	}()

	if l.isClosed() {
		return errs.E(op, errs.Unavailable, errors.New("the audit log is closed"))
	}
	if err := checkBatch(area, entries); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}

	recs, err := l.stamp(ctx, traceID, entries)
	if err != nil {
		return err
	}
	if err := l.write(ctx, span, recs); err != nil {
		return err
	}
	return nil
}

// checkBatch validates the batch and each entry's area, reporting every problem at once
// under "entries" and "entries[i].<field>".
func checkBatch(area string, entries []Entry) error {
	const op = "audit.Log.Record"
	if len(entries) == 0 || len(entries) > MaxBatch {
		return fieldErr(op, "entries", validate.CodeRange, "must hold 1 to %d entries", MaxBatch)
	}
	v := validate.Errors{Op: op}
	for i := range entries {
		prefix := "entries[" + itoa(i) + "]."
		addAll(&v, prefix, entries[i].Validate())
		// A bad grammar is already reported; a well-formed action in another area is not.
		if entries[i].Action.Validate() == nil && entries[i].Action.Area() != area {
			v.Addf(prefix+"action", codeMismatch, "must belong to the recorder's area")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// stamp turns entries into records: ID, time, instance, trace ID, resolved actor and titles.
func (l *Log) stamp(ctx context.Context, traceID string, entries []Entry) ([]Record, error) {
	now := l.clock.Now().UTC()
	titles := map[model.PersonID][]string{}

	recs := make([]Record, len(entries))
	for i, e := range entries {
		actor, err := l.resolveActor(ctx, e.Actor)
		if err != nil {
			return nil, err
		}
		if actor.Kind == ActorPerson {
			t, ok := titles[actor.Person]
			if !ok {
				if t, err = l.titlesOf(ctx, actor.Person); err != nil {
					return nil, err
				}
				titles[actor.Person] = t
			}
			actor.Titles = t
		} else {
			actor.Titles = nil
		}
		e.Actor = actor
		e.Fields = append([]string(nil), e.Fields...)
		recs[i] = Record{
			ID:       model.NewID[RecordID](),
			At:       now,
			Instance: l.opts.Instance,
			TraceID:  traceID,
			Entry:    e,
		}
	}
	return recs, nil
}

// resolveActor applies decision 6: a set Kind is taken as given; otherwise a signal
// delivery is the system, a session is its person, and no session is anonymous.
func (l *Log) resolveActor(ctx context.Context, given Actor) (Actor, error) {
	const op = "audit.Log.Record"
	if given.Kind != 0 {
		return given, nil
	}
	if signals.InDelivery(ctx) {
		return Actor{Kind: ActorSystem}, nil
	}
	a, ok := l.opts.Actor(ctx)
	if !ok {
		return Actor{Kind: ActorAnonymous}, nil
	}
	a.Titles = nil
	if a.Kind != ActorPerson || a.Validate() != nil {
		return Actor{}, errs.E(op, errs.Internal, errors.New("the actor function returned an actor that is not a valid person"))
	}
	return a, nil
}

// titlesOf asks the TitleSource (nil: none) and trims the answer to the limits, so a long
// role name never blocks an audit write.
func (l *Log) titlesOf(ctx context.Context, p model.PersonID) ([]string, error) {
	if l.opts.Titles == nil {
		return nil, nil
	}
	raw, err := l.opts.Titles.TitlesOf(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("audit.Log.Record: titles of the actor: %w", err)
	}
	out := make([]string, 0, min(len(raw), MaxTitles))
	for _, t := range raw {
		t = cleanTitle(t)
		if t == "" {
			continue
		}
		out = append(out, capBytes(t, MaxTitleBytes))
		if len(out) == MaxTitles {
			break
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// cleanTitle drops control characters (Unicode Cc, NUL included) and format characters
// (Cf), then trims space. jsonb refuses \u0000, so one bad title would otherwise fail every
// write by that person and stall the importer.
func cleanTitle(t string) string {
	t = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, t)
	return strings.TrimSpace(t)
}

// capBytes cuts s to at most n bytes at a character boundary.
func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// write sends the batch to the table, or to the spool when this copy cannot write.
func (l *Log) write(ctx context.Context, span telemetry.Span, recs []Record) error {
	const op = "audit.Log.Record"
	if l.opts.DB.ReadOnly() {
		return l.toSpool(ctx, span, recs)
	}
	rows := make([]postgres.Row, len(recs))
	for i, r := range recs {
		rows[i] = toRow(r)
	}
	if err := l.store.Insert(ctx, rows); err != nil {
		if readOnlyErr(err) {
			return l.toSpool(ctx, span, recs)
		}
		return fmt.Errorf("%s: insert: %w", op, err)
	}
	span.SetAttrs(telemetry.Token("via", viaDB))
	l.metrics.record(ctx, len(recs), viaDB)
	return nil
}

// toSpool appends the batch to the local spool (decision 4).
func (l *Log) toSpool(ctx context.Context, span telemetry.Span, recs []Record) error {
	const op = "audit.Log.Record"
	var buf bytes.Buffer
	for _, r := range recs {
		w := toWire(r)
		w.Spooled = false // the importer sets the column
		line, err := l.encode(w)
		if err != nil {
			return errs.E(op, errs.Internal, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := l.spool.append(buf.Bytes()); err != nil {
		return fmt.Errorf("%s: spool: %w", op, err)
	}
	span.SetAttrs(telemetry.Token("via", viaSpool))
	l.metrics.record(ctx, len(recs), viaSpool)
	return nil
}
