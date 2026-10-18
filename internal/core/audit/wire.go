package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// wireRecord is the JSON form of a Record: one spool line, one line of an export, one
// element of a search response. Key order is the field order below (the spool line format
// of the spec); optional keys are omitted when empty. It holds names and IDs only.
type wireRecord struct {
	V        int       `json:"v"`
	ID       string    `json:"id"`
	At       string    `json:"at"`
	Instance string    `json:"instance"`
	TraceID  string    `json:"trace_id,omitempty"`
	Spooled  bool      `json:"spooled,omitempty"` // never in a spool line; set on stored records that came through one
	Action   string    `json:"action"`
	Outcome  string    `json:"outcome"`
	Actor    wireActor `json:"actor"`
	Subject  string    `json:"subject,omitempty"`
	Record   *wireRef  `json:"record,omitempty"`
	Fields   []string  `json:"fields,omitempty"`
	Level    string    `json:"level,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

type wireActor struct {
	Kind      string   `json:"kind"`
	Person    string   `json:"person,omitempty"`
	Superuser bool     `json:"superuser,omitempty"`
	Titles    []string `json:"titles,omitempty"`
}

type wireRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// toWire renders r for JSON. Cannot fail.
func toWire(r Record) wireRecord {
	w := wireRecord{
		V:        SpoolVersion,
		ID:       r.ID.String(),
		At:       r.At.UTC().Format(time.RFC3339Nano),
		Instance: r.Instance,
		TraceID:  r.TraceID,
		Spooled:  r.Spooled,
		Action:   string(r.Action),
		Outcome:  r.Outcome.String(),
		Actor: wireActor{
			Kind:      r.Actor.Kind.String(),
			Superuser: r.Actor.Superuser,
			Titles:    r.Actor.Titles,
		},
		Fields: r.Fields,
		Reason: r.Reason,
	}
	if !r.Actor.Person.IsZero() {
		w.Actor.Person = r.Actor.Person.String()
	}
	if !r.Subject.IsZero() {
		w.Subject = r.Subject.String()
	}
	if !r.Record.IsZero() {
		w.Record = &wireRef{Kind: r.Record.Kind, ID: model.ID[record](r.Record.ID).String()}
	}
	if r.Level != 0 {
		w.Level = r.Level.String()
	}
	return w
}

// encodeWire is the default spool/export line encoder: one JSON object, no trailing newline.
func encodeWire(w wireRecord) ([]byte, error) {
	b, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("encode record: %w", err)
	}
	return b, nil
}

// errBadLine is the cause of every spool line that cannot be used. It never carries the
// line: a spool line is audit data.
var errBadLine = errors.New("spool line is not a valid audit record")

// decodeWire parses one spool line strictly: unknown keys, a wrong version, trailing data
// or an invalid record make the line unusable. The returned error names the first problem
// without echoing any value.
func decodeWire(line []byte) (Record, error) {
	const op = "audit.decodeWire"
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var w wireRecord
	if err := dec.Decode(&w); err != nil {
		return Record{}, errs.E(op, errs.Invalid, fmt.Errorf("%w: not the expected JSON object", errBadLine))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Record{}, errs.E(op, errs.Invalid, fmt.Errorf("%w: data after the object", errBadLine))
	}
	if w.V != SpoolVersion {
		return Record{}, errs.E(op, errs.Invalid, fmt.Errorf("%w: unsupported version %d", errBadLine, w.V))
	}
	rec, err := fromWire(w)
	if err != nil {
		return Record{}, errs.E(op, errs.Invalid, fmt.Errorf("%w: %w", errBadLine, err))
	}
	if err := rec.validate(); err != nil {
		return Record{}, errs.E(op, errs.Invalid, fmt.Errorf("%w: %w", errBadLine, err))
	}
	return rec, nil
}

func fromWire(w wireRecord) (Record, error) {
	var (
		rec Record
		err error
	)
	if rec.ID, err = model.ParseID[RecordID](w.ID); err != nil {
		return Record{}, badField("id")
	}
	at, err := time.Parse(time.RFC3339Nano, w.At)
	if err != nil {
		return Record{}, badField("at")
	}
	rec.At = at.UTC()
	rec.Instance, rec.TraceID, rec.Spooled = w.Instance, w.TraceID, w.Spooled
	rec.Action = Action(w.Action)
	if rec.Outcome, err = ParseOutcome(w.Outcome); err != nil {
		return Record{}, badField("outcome")
	}
	kind, ok := parseActorKind(w.Actor.Kind)
	if !ok {
		return Record{}, badField("actor.kind")
	}
	rec.Actor = Actor{Kind: kind, Superuser: w.Actor.Superuser, Titles: w.Actor.Titles}
	if w.Actor.Person != "" {
		if rec.Actor.Person, err = model.ParseID[model.PersonID](w.Actor.Person); err != nil {
			return Record{}, badField("actor.person")
		}
	}
	if w.Subject != "" {
		if rec.Subject, err = model.ParseID[model.PersonID](w.Subject); err != nil {
			return Record{}, badField("subject")
		}
	}
	if w.Record != nil {
		id, err := model.ParseID[model.ID[record]](w.Record.ID)
		if err != nil {
			return Record{}, badField("record.id")
		}
		rec.Record = Ref{Kind: w.Record.Kind, ID: id}
	}
	rec.Fields = w.Fields
	if w.Level != "" {
		if rec.Level, err = model.ParseSensitivity(w.Level); err != nil {
			return Record{}, badField("level")
		}
	}
	rec.Reason = w.Reason
	return rec, nil
}

// badField reports a spool field that does not parse, by name; the value is never echoed.
func badField(field string) error {
	return fieldErr("audit.fromWire", field, codeMalformed, "is not in the expected form")
}

// toRow converts a record to a storage row. Cannot fail.
func toRow(r Record) postgres.Row {
	row := postgres.Row{
		ID:             r.ID,
		At:             r.At,
		Instance:       r.Instance,
		TraceID:        r.TraceID,
		Spooled:        r.Spooled,
		Action:         string(r.Action),
		Outcome:        r.Outcome.String(),
		ActorKind:      r.Actor.Kind.String(),
		ActorPerson:    r.Actor.Person,
		ActorSuperuser: r.Actor.Superuser,
		ActorTitles:    r.Actor.Titles,
		Subject:        r.Subject,
		RecordKind:     r.Record.Kind,
		RecordID:       r.Record.ID,
		Fields:         r.Fields,
		Reason:         r.Reason,
	}
	if r.Level != 0 {
		row.Level = r.Level.String()
	}
	return row
}

// fromRow converts a stored row back to a record. A value the schema's CHECK constraints
// should have refused fails errs.Internal (a broken invariant, not bad input).
func fromRow(row postgres.Row) (Record, error) {
	const op = "audit.fromRow"
	rec := Record{
		ID:       row.ID,
		At:       row.At.UTC(),
		Instance: row.Instance,
		TraceID:  row.TraceID,
		Spooled:  row.Spooled,
		Entry: Entry{
			Action:  Action(row.Action),
			Subject: row.Subject,
			Record:  Ref{Kind: row.RecordKind, ID: row.RecordID},
			Fields:  row.Fields,
			Reason:  row.Reason,
			Actor: Actor{
				Person:    row.ActorPerson,
				Superuser: row.ActorSuperuser,
				Titles:    row.ActorTitles,
			},
		},
	}
	var err error
	if rec.Outcome, err = ParseOutcome(row.Outcome); err != nil {
		return Record{}, errs.E(op, errs.Internal, errors.New("stored outcome is unknown"))
	}
	kind, ok := parseActorKind(row.ActorKind)
	if !ok {
		return Record{}, errs.E(op, errs.Internal, errors.New("stored actor kind is unknown"))
	}
	rec.Actor.Kind = kind
	if row.Level != "" {
		if rec.Level, err = model.ParseSensitivity(row.Level); err != nil {
			return Record{}, errs.E(op, errs.Internal, errors.New("stored level is unknown"))
		}
	}
	return rec, nil
}
