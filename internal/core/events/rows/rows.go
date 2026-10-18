// Package rows holds the row types events' service and its PostgreSQL store share. It is a
// leaf: it imports model only, because events imports postgres to build its store and so
// postgres cannot import events.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

type scheduleItem struct{}

// ScheduleItemID identifies a schedule item. events re-exports it under the same name.
type ScheduleItemID = model.ID[scheduleItem]

// Kind is a row of core.event_kind.
type Kind struct {
	Key       string
	Name      string
	Builtin   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Event is a row of core.event. A zero ClosedAt is NULL: the event is open.
type Event struct {
	ID        model.EventID
	Name      string
	Kind      string
	Starts    time.Time
	Ends      time.Time
	Zone      string
	ClosedAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Patch changes the columns whose field is non-nil. Starts and Ends are both nil or both
// set: they are one TimeRange.
type Patch struct {
	ID     model.EventID
	Name   *string
	Kind   *string
	Zone   *string
	Starts *time.Time
	Ends   *time.Time
}

// Info is the part of an event modules ask for (core.event with closed_at as a flag).
type Info struct {
	ID     model.EventID
	Name   string
	Kind   string
	Starts time.Time
	Ends   time.Time
	Zone   string
	Closed bool
}

// ScheduleItem is a row of core.event_schedule_item. A zero Ends is NULL: a moment.
type ScheduleItem struct {
	ID       ScheduleItemID
	Event    model.EventID
	Key      string
	Label    string
	Starts   time.Time
	Ends     time.Time
	Position int
}
