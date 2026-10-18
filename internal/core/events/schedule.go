package events

import (
	"context"
	"fmt"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/model"
)

// normalize trims the text fields and puts the instants in UTC at the database's precision.
func (in ScheduleInput) normalize() ScheduleInput {
	return ScheduleInput{
		Key: strings.TrimSpace(in.Key), Label: strings.TrimSpace(in.Label),
		Starts: instant(in.Starts), Ends: instant(in.Ends),
	}
}

// AddScheduleItem adds an item (a window or, with no Ends, a moment) to an open event, at
// most MaxScheduleItems of them. Needs events.manage. Records events.schedule-item.created
// in the transaction; no signal. Fails errs.Unauthenticated, errs.Invalid (key, label,
// starts_at, ends_at; schedule, too_many), errs.Permission, errs.NotFound (event),
// errs.Conflict (closed; key, duplicate), errs.Unavailable wrapping db.ErrReadOnly and
// store kinds.
func (s *Service) AddScheduleItem(ctx context.Context, event model.EventID, in ScheduleInput) (_ ScheduleItem, err error) {
	const op = "events.Service.AddScheduleItem"
	ctx, span := s.start(ctx, "add-schedule-item")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return ScheduleItem{}, err
	}
	in = in.normalize()
	if err := in.Validate(); err != nil {
		return ScheduleItem{}, err //nolint:wrapcheck // Validate's error already carries its Op
	}
	if err := s.require(ctx, op); err != nil {
		return ScheduleItem{}, err
	}
	id := model.NewID[ScheduleItemID]()
	span.SetAttrs(telemetry.ID("event_id", event), telemetry.ID("item_id", id))
	var row rows.ScheduleItem
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.openEvent(ctx, op, event); err != nil {
			return err
		}
		n, err := s.store.CountScheduleItems(ctx, event)
		if err != nil {
			return wrap(op, err)
		}
		if n >= MaxScheduleItems {
			return paramInvalid(op, "schedule", codeTooMany, map[string]string{"max": fmt.Sprint(MaxScheduleItems)},
				fmt.Sprintf("at most %d items", MaxScheduleItems))
		}
		saved, err := s.store.InsertScheduleItem(ctx, rows.ScheduleItem{
			ID: id, Event: event, Key: in.Key, Label: in.Label, Starts: in.Starts, Ends: in.Ends,
		}, s.now())
		if err != nil {
			return fromConstraint(op, err, fieldKey, codeDuplicate)
		}
		row = saved
		return s.record(ctx, audit.Entry{
			Action: actionItemCreated, Outcome: audit.Succeeded, Record: audit.RefOf(kindScheduleItem, id),
		})
	})
	if err != nil {
		return ScheduleItem{}, wrap(op, err)
	}
	return itemOf(row), nil
}

// UpdateScheduleItem replaces the key, label and times of one item of an open event; its
// position stays. Needs events.manage. Records events.schedule-item.changed with the names
// of the fields that changed (nothing is written when none did). Fails as AddScheduleItem
// does, and errs.NotFound for an item that is unknown or belongs to another event.
func (s *Service) UpdateScheduleItem(ctx context.Context, event model.EventID, item ScheduleItemID, in ScheduleInput) (_ ScheduleItem, err error) {
	const op = "events.Service.UpdateScheduleItem"
	ctx, span := s.start(ctx, "update-schedule-item")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return ScheduleItem{}, err
	}
	in = in.normalize()
	if err := in.Validate(); err != nil {
		return ScheduleItem{}, err //nolint:wrapcheck // Validate's error already carries its Op
	}
	if err := s.require(ctx, op); err != nil {
		return ScheduleItem{}, err
	}
	span.SetAttrs(telemetry.ID("event_id", event), telemetry.ID("item_id", item))
	var row rows.ScheduleItem
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.openEvent(ctx, op, event); err != nil {
			return err
		}
		if item.IsZero() {
			return notFound(op, "schedule item")
		}
		cur, err := s.store.GetScheduleItem(ctx, event, item)
		if err != nil {
			return wrap(op, err)
		}
		var changed []string
		if in.Key != cur.Key {
			changed = append(changed, fieldKey)
		}
		if in.Label != cur.Label {
			changed = append(changed, fieldLabel)
		}
		if !in.Starts.Equal(cur.Starts) {
			changed = append(changed, fieldStarts)
		}
		if !in.Ends.Equal(cur.Ends) {
			changed = append(changed, fieldEnds)
		}
		if len(changed) == 0 {
			row = cur
			return nil
		}
		cur.Key, cur.Label, cur.Starts, cur.Ends = in.Key, in.Label, in.Starts, in.Ends
		if row, err = s.store.UpdateScheduleItem(ctx, cur, s.now()); err != nil {
			return fromConstraint(op, err, fieldKey, codeDuplicate)
		}
		return s.record(ctx, audit.Entry{
			Action: actionItemChanged, Outcome: audit.Succeeded, Record: audit.RefOf(kindScheduleItem, item),
			Fields: changed, Level: model.SensitivityInternal,
		})
	})
	if err != nil {
		return ScheduleItem{}, wrap(op, err)
	}
	return itemOf(row), nil
}

// RemoveScheduleItem deletes one item of an open event. Needs events.manage. Records
// events.schedule-item.deleted in the transaction. Fails errs.Unauthenticated,
// errs.Permission, errs.NotFound (event, or an item that is unknown or belongs to another
// event), errs.Conflict (closed), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) RemoveScheduleItem(ctx context.Context, event model.EventID, item ScheduleItemID) (err error) {
	const op = "events.Service.RemoveScheduleItem"
	ctx, span := s.start(ctx, "remove-schedule-item")
	defer endSpan(span, &err)
	if err := s.manage(ctx, op); err != nil {
		return err
	}
	span.SetAttrs(telemetry.ID("event_id", event), telemetry.ID("item_id", item))
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.openEvent(ctx, op, event); err != nil {
			return err
		}
		if item.IsZero() {
			return notFound(op, "schedule item")
		}
		if err := s.store.DeleteScheduleItem(ctx, event, item); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionItemDeleted, Outcome: audit.Succeeded, Record: audit.RefOf(kindScheduleItem, item),
		})
	})
	return wrap(op, err)
}
