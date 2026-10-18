// Package eventstest gives modules an events.Directory for their tests: Fake keeps event
// summaries in memory, so a module's use case that renders times in an event's zone or
// refuses a write for a closed event runs without a database.
package eventstest

import (
	"context"
	"fmt"
	"sync"

	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Fake implements events.Directory in memory. Safe for concurrent use. It answers what the
// real service answers for what a test can set up: known events with their Info, unknown
// ones absent, and the same Invalid refusal.
type Fake struct {
	mu   sync.Mutex
	byID map[model.EventID]events.Info
}

var _ events.Directory = (*Fake)(nil)

// NewFake returns an empty Fake: no event is known. Cannot fail.
func NewFake() *Fake {
	return &Fake{byID: map[model.EventID]events.Info{}}
}

// Add makes info.ID a known event with that Info, replacing an earlier one (set Closed to
// close it). Cannot fail.
func (f *Fake) Add(info events.Info) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[info.ID] = info
}

// InfoOf returns the Info of each known event among ids and omits the rest. Fails
// errs.Invalid (field ids, code too_many) when len(ids) > events.MaxInfo, like the real
// service. Serves any caller.
func (f *Fake) InfoOf(_ context.Context, ids []model.EventID) (map[model.EventID]events.Info, error) {
	const op = "eventstest.Fake.InfoOf"
	if len(ids) > events.MaxInfo {
		return nil, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "ids", Code: "too_many", Params: map[string]string{"max": fmt.Sprint(events.MaxInfo)},
			Reason: fmt.Sprintf("at most %d IDs", events.MaxInfo),
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[model.EventID]events.Info, len(ids))
	for _, id := range ids {
		if info, ok := f.byID[id]; ok {
			out[id] = info
		}
	}
	return out, nil
}
