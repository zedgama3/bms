package eventstest_test

import (
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/events/eventstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func newEvent() model.EventID { return model.NewID[model.EventID]() }

func TestFake_Table(t *testing.T) {
	ctx := t.Context()
	start := time.Date(2027, 8, 24, 0, 0, 0, 0, time.UTC)
	open := events.Info{ID: newEvent(), Name: "Burn 2027", Kind: "burn", Dates: model.TimeRange{Start: start, End: start.Add(72 * time.Hour)}, Zone: "America/Los_Angeles"}
	closed := events.Info{ID: newEvent(), Name: "Burn 2026", Kind: "burn", Dates: open.Dates, Zone: "UTC", Closed: true}
	unknown := newEvent()
	f := eventstest.NewFake()
	f.Add(open)
	f.Add(closed)

	t.Run("InfoOf returns known, omits unknown", func(t *testing.T) {
		got, err := f.InfoOf(ctx, []model.EventID{open.ID, closed.ID, unknown})
		if err != nil || len(got) != 2 {
			t.Fatalf("InfoOf = %v, %v", got, err)
		}
		if got[open.ID] != open || got[closed.ID] != closed || !got[closed.ID].Closed || got[open.ID].Closed {
			t.Errorf("InfoOf = %+v", got)
		}
		if _, ok := got[unknown]; ok {
			t.Error("unknown ID present")
		}
	})
	t.Run("nil and empty", func(t *testing.T) {
		for _, ids := range [][]model.EventID{nil, {}} {
			if got, err := f.InfoOf(ctx, ids); err != nil || len(got) != 0 {
				t.Errorf("InfoOf = %v, %v", got, err)
			}
		}
	})
	t.Run("too many", func(t *testing.T) {
		ids := make([]model.EventID, events.MaxInfo+1)
		for i := range ids {
			ids[i] = newEvent()
		}
		_, err := f.InfoOf(ctx, ids)
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v, want Invalid", err)
		}
		if fe := validate.Fields(err); len(fe) == 0 || fe[0].Field != "ids" || fe[0].Code != "too_many" || fe[0].Params["max"] != "200" {
			t.Errorf("fields = %v", fe)
		}
		if _, err := f.InfoOf(ctx, ids[:events.MaxInfo]); err != nil {
			t.Errorf("exactly MaxInfo: %v", err)
		}
	})
	t.Run("Add replaces (close and reopen)", func(t *testing.T) {
		c := open
		c.Closed = true
		f.Add(c)
		got, err := f.InfoOf(ctx, []model.EventID{open.ID})
		if err != nil || !got[open.ID].Closed {
			t.Error("replacement not seen")
		}
	})
}

func TestFake_ConcurrentUse(t *testing.T) {
	f := eventstest.NewFake()
	info := events.Info{ID: newEvent(), Name: "x"}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.Add(info)
			if _, err := f.InfoOf(t.Context(), []model.EventID{info.ID}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
