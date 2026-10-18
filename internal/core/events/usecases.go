package events

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// instant is t in UTC at the database's precision, so a value compares equal to what the
// store returns.
func instant(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(time.Microsecond)
}

func rangeOf(r model.TimeRange) model.TimeRange {
	return model.TimeRange{Start: instant(r.Start), End: instant(r.End)}
}

// ---- reads ------------------------------------------------------------------------------

// List returns the events newest first (Dates.Start descending). Any signed-in person may
// list them (decision 2). A zero Status is StatusAny. Fails errs.Unauthenticated,
// errs.Invalid (status, one_of) and store kinds.
func (s *Service) List(ctx context.Context, q ListQuery) (_ []Event, err error) {
	const op = "events.Service.List"
	ctx, span := s.start(ctx, "list")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return nil, err
	}
	status := q.Status
	if status == 0 {
		status = StatusAny
	}
	var closed *bool
	switch status {
	case StatusAny:
	case StatusOpen:
		closed = new(bool)
	case StatusClosed:
		t := true
		closed = &t
	default:
		return nil, fieldInvalid(op, "status", codeOneOf, "must be one of: any, open, closed")
	}
	span.SetAttrs(telemetry.Token("status", status.String()))
	rs, err := s.store.ListEvents(ctx, closed)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]Event, len(rs))
	for i, r := range rs {
		out[i] = eventOf(r)
	}
	span.SetAttrs(telemetry.Int("events", len(out)))
	return out, nil
}

// Get returns an event with its schedule, sorted by start then position. Fails
// errs.Unauthenticated, errs.NotFound and store kinds.
func (s *Service) Get(ctx context.Context, id model.EventID) (_ Detail, err error) {
	const op = "events.Service.Get"
	ctx, span := s.start(ctx, "get")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Detail{}, err
	}
	if id.IsZero() {
		return Detail{}, notFound(op, "event")
	}
	span.SetAttrs(telemetry.ID("event_id", id))
	var out Detail
	err = s.inTx(ctx, db.Read, func(ctx context.Context) error {
		ev, err := s.store.GetEvent(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		items, err := s.store.ListScheduleItems(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		out = Detail{Event: eventOf(ev), Schedule: make([]ScheduleItem, len(items))}
		for i, it := range items {
			out.Schedule[i] = itemOf(it)
		}
		return nil
	})
	if err != nil {
		return Detail{}, wrap(op, err)
	}
	return out, nil
}

// InfoOf maps each known event to its Info; unknown IDs are absent. It needs no actor.
// Fails errs.Invalid (ids, too_many {max}) for more than MaxInfo IDs, and with the store's
// kinds.
func (s *Service) InfoOf(ctx context.Context, ids []model.EventID) (_ map[model.EventID]Info, err error) {
	const op = "events.Service.InfoOf"
	ctx, span := s.start(ctx, "info-of")
	defer endSpan(span, &err)
	if len(ids) > MaxInfo {
		return nil, paramInvalid(op, "ids", codeTooMany, map[string]string{"max": fmt.Sprint(MaxInfo)},
			fmt.Sprintf("at most %d IDs", MaxInfo))
	}
	unique := make([]model.EventID, 0, len(ids))
	seen := make(map[model.EventID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id.IsZero() {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	span.SetAttrs(telemetry.Int("ids", len(unique)))
	if len(unique) == 0 {
		return map[model.EventID]Info{}, nil
	}
	rs, err := s.store.InfoOf(ctx, unique)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make(map[model.EventID]Info, len(rs))
	for _, r := range rs {
		out[r.ID] = infoOf(r)
	}
	return out, nil
}

// ---- Create -----------------------------------------------------------------------------

// normalized is a create input after trimming and validation.
type normalizedCreate struct {
	name, kind, zone string
	dates            model.TimeRange
}

func (in CreateInput) normalize(op string) (normalizedCreate, error) {
	n := normalizedCreate{
		name: strings.TrimSpace(in.Name), kind: strings.TrimSpace(in.Kind), zone: strings.TrimSpace(in.Zone),
		dates: in.Dates,
	}
	v := validate.Errors{Op: op}
	checkEventName(&v, n.name)
	checkKindShape(&v, n.kind)
	checkDates(&v, n.dates)
	checkZone(&v, n.zone)
	if err := v.Err(); err != nil {
		return normalizedCreate{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	n.dates = rangeOf(n.dates) // checked as given, stored at the database's precision
	return n, nil
}

// Create makes an event. Needs events.manage. The first transaction inserts the event,
// records events.event.created and publishes events.event-created (with copied-from when
// set). When CopyFrom is set and providers are linked, each runs in link order under
// SetupTimeout, in the caller's ctx; a provider that fails never undoes the event and shows
// as a failed SetupResult. A second transaction then records events.setup.ran. If that
// fails, Create returns the populated Created together with the error: the event exists.
// Fails errs.Unauthenticated, errs.Permission, errs.Invalid (name, kind, dates.*, zone;
// copy_from unknown; kind unknown), errs.Conflict (name, taken), errs.Unavailable wrapping
// db.ErrReadOnly and store kinds.
func (s *Service) Create(ctx context.Context, in CreateInput) (_ Created, err error) {
	const op = "events.Service.Create"
	ctx, span := s.start(ctx, "create")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Created{}, err
	}
	n, err := in.normalize(op)
	if err != nil {
		return Created{}, err
	}
	if err := s.require(ctx, op); err != nil {
		return Created{}, err
	}
	id := model.NewID[model.EventID]()
	span.SetAttrs(telemetry.ID("event_id", id))
	var row rows.Event
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if !in.CopyFrom.IsZero() {
			if _, err := s.store.GetEvent(ctx, in.CopyFrom); err != nil {
				if errs.Is(err, errs.NotFound) {
					return fieldInvalid(op, "copy_from", codeUnknown, "no such event")
				}
				return wrap(op, err)
			}
		}
		now := s.now()
		saved, err := s.store.InsertEvent(ctx, rows.Event{
			ID: id, Name: n.name, Kind: n.kind, Starts: n.dates.Start, Ends: n.dates.End, Zone: n.zone,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return fromConstraint(op, err, fieldName, codeTaken)
		}
		row = saved
		if err := s.record(ctx, audit.Entry{
			Action: actionCreated, Outcome: audit.Succeeded, Record: audit.RefOf(kindEvent, id),
		}); err != nil {
			return err
		}
		return s.publish(ctx, SignalCreated, id, in.CopyFrom)
	})
	if err != nil {
		return Created{}, wrap(op, err)
	}
	out := Created{Event: eventOf(row), Setup: []SetupResult{}}
	if in.CopyFrom.IsZero() || len(s.providers()) == 0 {
		return out, nil
	}
	out.Setup, err = s.setUp(ctx, id, in.CopyFrom)
	if err != nil {
		return out, wrap(op, err)
	}
	return out, nil
}

// ---- Update, Close, Reopen --------------------------------------------------------------

// Update changes the fields set on the input of an open event. Needs events.manage.
// Records events.event.changed (the names of the fields that changed) and publishes
// events.event-changed in the transaction; changing nothing writes nothing. Fails
// errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Invalid (body, required, for
// no field set; field problems as Create; kind unknown), errs.Conflict (closed; name,
// taken), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) Update(ctx context.Context, id model.EventID, in UpdateInput) (_ Event, err error) {
	const op = "events.Service.Update"
	ctx, span := s.start(ctx, "update")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Event{}, err
	}
	trim := func(p *string) *string {
		if p == nil {
			return nil
		}
		t := strings.TrimSpace(*p)
		return &t
	}
	name, kind, zone := trim(in.Name), trim(in.Kind), trim(in.Zone)
	dates := in.Dates
	v := validate.Errors{Op: op}
	if name == nil && kind == nil && zone == nil && dates == nil {
		v.Addf("body", codeRequired, "set at least one field")
	}
	if name != nil {
		checkEventName(&v, *name)
	}
	if kind != nil {
		checkKindShape(&v, *kind)
	}
	if dates != nil {
		checkDates(&v, *dates)
	}
	if zone != nil {
		checkZone(&v, *zone)
	}
	if err := v.Err(); err != nil {
		return Event{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	if dates != nil { // checked as given, stored at the database's precision
		r := rangeOf(*dates)
		dates = &r
	}
	if err := s.require(ctx, op); err != nil {
		return Event{}, err
	}
	if id.IsZero() {
		return Event{}, notFound(op, "event")
	}
	span.SetAttrs(telemetry.ID("event_id", id))
	var row rows.Event
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, err := s.store.GetEventForUpdate(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if !cur.ClosedAt.IsZero() {
			return closedConflict(op)
		}
		patch := rows.Patch{ID: id}
		var changed []string
		if name != nil && *name != cur.Name {
			patch.Name = name
			changed = append(changed, fieldName)
		}
		if kind != nil && *kind != cur.Kind {
			patch.Kind = kind
			changed = append(changed, fieldKind)
		}
		if dates != nil && (!dates.Start.Equal(cur.Starts) || !dates.End.Equal(cur.Ends)) {
			patch.Starts, patch.Ends = &dates.Start, &dates.End
			changed = append(changed, fieldDates)
		}
		if zone != nil && *zone != cur.Zone {
			patch.Zone = zone
			changed = append(changed, fieldZone)
		}
		if len(changed) == 0 {
			row = cur
			return nil
		}
		if row, err = s.store.UpdateEvent(ctx, patch, s.now()); err != nil {
			return fromConstraint(op, err, fieldName, codeTaken)
		}
		if err := s.record(ctx, audit.Entry{
			Action: actionChanged, Outcome: audit.Succeeded, Record: audit.RefOf(kindEvent, id),
			Fields: changed, Level: model.SensitivityInternal,
		}); err != nil {
			return err
		}
		return s.publish(ctx, SignalChanged, id, model.EventID{})
	})
	if err != nil {
		return Event{}, wrap(op, err)
	}
	return eventOf(row), nil
}

func closedConflict(op string) error {
	return fieldConflict(op, "event", codeClosed, "the event is closed; reopen it first")
}

func openConflict(op string) error {
	return fieldConflict(op, "event", codeOpen, "the event is not closed")
}

// Close closes an open event: it accepts no changes until reopened (modules decide what
// closed means for them). Needs events.manage. Records events.event.closed and publishes
// events.event-closed in the transaction. Fails errs.Unauthenticated, errs.Permission,
// errs.NotFound, errs.Conflict (closed), errs.Unavailable wrapping db.ErrReadOnly and store
// kinds.
func (s *Service) Close(ctx context.Context, id model.EventID) (Event, error) {
	return s.flip(ctx, "events.Service.Close", "close", id, true)
}

// Reopen is Close's inverse: it fails errs.Conflict (open) for an event that is not closed,
// and records and publishes the reopened names.
func (s *Service) Reopen(ctx context.Context, id model.EventID) (Event, error) {
	return s.flip(ctx, "events.Service.Reopen", "reopen", id, false)
}

func (s *Service) flip(ctx context.Context, op, spanName string, id model.EventID, closing bool) (_ Event, err error) {
	ctx, span := s.start(ctx, spanName)
	defer endSpan(span, &err)
	if err := s.manage(ctx, op); err != nil {
		return Event{}, err
	}
	if id.IsZero() {
		return Event{}, notFound(op, "event")
	}
	span.SetAttrs(telemetry.ID("event_id", id))
	var row rows.Event
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, err := s.store.GetEventForUpdate(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		action, signal := actionReopened, SignalReopened
		switch {
		case closing && !cur.ClosedAt.IsZero():
			return closedConflict(op)
		case !closing && cur.ClosedAt.IsZero():
			return openConflict(op)
		case closing:
			action, signal = actionClosed, SignalClosed
			row, err = s.store.CloseEvent(ctx, id, s.now())
		default:
			row, err = s.store.ReopenEvent(ctx, id, s.now())
		}
		if errs.Is(err, errs.Conflict) { // the store's safety net: the state changed under the lock
			if closing {
				return closedConflict(op)
			}
			return openConflict(op)
		}
		if err != nil {
			return wrap(op, err)
		}
		if err := s.record(ctx, audit.Entry{
			Action: action, Outcome: audit.Succeeded, Record: audit.RefOf(kindEvent, id),
		}); err != nil {
			return err
		}
		return s.publish(ctx, signal, id, model.EventID{})
	})
	if err != nil {
		return Event{}, wrap(op, err)
	}
	return eventOf(row), nil
}

// openEvent locks the event and fails errs.NotFound for an unknown one and errs.Conflict
// (closed) for a closed one. Inside a Write transaction.
func (s *Service) openEvent(ctx context.Context, op string, id model.EventID) error {
	if id.IsZero() {
		return notFound(op, "event")
	}
	ev, err := s.store.GetEventForUpdate(ctx, id)
	if err != nil {
		return wrap(op, err)
	}
	if !ev.ClosedAt.IsZero() {
		return closedConflict(op)
	}
	return nil
}
