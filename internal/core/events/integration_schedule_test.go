//go:build integration

package events_test

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func labels(items []events.ScheduleItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func TestAddScheduleItem_WindowAndMoment_RecordsAuditNoSignal(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	st := e.Dates.Start.Add(time.Hour)
	e0, s0 := r.mark()

	win, err := r.svc.AddScheduleItem(r.sctx(), e.ID, events.ScheduleInput{Key: "gate", Label: "Gate hours", Starts: st, Ends: st.Add(8 * time.Hour)})
	wantNoErr(t, err)
	if win.ID.IsZero() || win.Event != e.ID || win.Key != "gate" || win.Label != "Gate hours" ||
		!win.Starts.Equal(st) || !win.Ends.Equal(st.Add(8*time.Hour)) {
		t.Errorf("window = %+v", win)
	}
	mom, err := r.svc.AddScheduleItem(r.sctx(), e.ID, events.ScheduleInput{Key: "temple-burn", Label: "Temple burn", Starts: st.Add(24 * time.Hour)})
	wantNoErr(t, err)
	if !mom.Ends.IsZero() {
		t.Errorf("a moment has Ends = %v", mom.Ends)
	}
	if mom.Position <= win.Position {
		t.Errorf("positions %d then %d, want increasing insertion order", win.Position, mom.Position)
	}

	es := r.rec.Recorded()[e0:]
	if len(es) != 2 || es[0].Action != "events.schedule-item.created" || es[1].Action != "events.schedule-item.created" ||
		!isRef(es[0], "events.schedule-item", [16]byte(win.ID)) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	if len(r.bus.Published()) != s0 {
		t.Error("a schedule change published a signal")
	}

	d, err := r.svc.Get(asPerson(ctxT(t), newPerson()), e.ID)
	wantNoErr(t, err)
	if !slices.Equal(labels(d.Schedule), []string{"Gate hours", "Temple burn"}) {
		t.Errorf("schedule = %v", labels(d.Schedule))
	}
}

// Decision 10: no containment rule.
func TestAddScheduleItem_OutsideTheDates_Allowed(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	r.addItem(e.ID, events.ScheduleInput{Label: "Before", Starts: e.Dates.Start.AddDate(0, 0, -10)})
	r.addItem(e.ID, events.ScheduleInput{Label: "After", Starts: e.Dates.End.AddDate(0, 0, 10), Ends: e.Dates.End.AddDate(0, 0, 11)})
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if len(d.Schedule) != 2 {
		t.Errorf("schedule = %v", labels(d.Schedule))
	}
}

func TestGet_ScheduleSortedByStartsThenPosition(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	base := e.Dates.Start
	r.addItem(e.ID, events.ScheduleInput{Label: "late", Starts: base.Add(5 * time.Hour)})
	r.addItem(e.ID, events.ScheduleInput{Label: "tie-first", Starts: base.Add(2 * time.Hour)})
	r.addItem(e.ID, events.ScheduleInput{Label: "early", Starts: base.Add(time.Hour)})
	r.addItem(e.ID, events.ScheduleInput{Label: "tie-second", Starts: base.Add(2 * time.Hour)})
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if want := []string{"early", "tie-first", "tie-second", "late"}; !slices.Equal(labels(d.Schedule), want) {
		t.Errorf("schedule = %v, want %v", labels(d.Schedule), want)
	}
}

func TestAddScheduleItem_EmptyKeysDoNotCollide(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	r.addItem(e.ID, events.ScheduleInput{Label: "one", Starts: e.Dates.Start})
	r.addItem(e.ID, events.ScheduleInput{Label: "two", Starts: e.Dates.Start})
}

// F70
func TestAddScheduleItem_Closed_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	it := r.addItem(e.ID, events.ScheduleInput{Label: "Gate", Starts: e.Dates.Start})
	_, err := r.svc.Close(r.sctx(), e.ID)
	wantNoErr(t, err)
	e0, s0 := r.mark()

	_, err = r.svc.AddScheduleItem(r.sctx(), e.ID, events.ScheduleInput{Label: "More", Starts: e.Dates.Start})
	wantCoded(t, err, errs.Conflict, "", "closed")
	_, err = r.svc.UpdateScheduleItem(r.sctx(), e.ID, it.ID, events.ScheduleInput{Label: "Changed", Starts: e.Dates.Start})
	wantCoded(t, err, errs.Conflict, "", "closed")
	wantCoded(t, r.svc.RemoveScheduleItem(r.sctx(), e.ID, it.ID), errs.Conflict, "", "closed")
	if en, s := r.mark(); en != e0 || s != s0 {
		t.Error("a refused schedule change recorded an audit entry or published a signal")
	}
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if len(d.Schedule) != 1 || d.Schedule[0].Label != "Gate" {
		t.Errorf("schedule = %+v after refused changes", d.Schedule)
	}
}

// F72
func TestAddScheduleItem_DuplicateKey_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	other := r.mk("Decompression 2027")
	first := r.addItem(e.ID, events.ScheduleInput{Key: "temple-burn", Label: "Temple burn", Starts: e.Dates.Start})
	e0, s0 := r.mark()

	_, err := r.svc.AddScheduleItem(r.sctx(), e.ID, events.ScheduleInput{Key: "temple-burn", Label: "Again", Starts: e.Dates.Start})
	wantCoded(t, err, errs.Conflict, "key", "duplicate")
	if en, s := r.mark(); en != e0 || s != s0 {
		t.Error("a refused add recorded an audit entry or published a signal")
	}

	// The same key in another event is fine.
	r.addItem(other.ID, events.ScheduleInput{Key: "temple-burn", Label: "Temple burn", Starts: other.Dates.Start})

	// Replacing an item may keep its own key, but not take another item's.
	second := r.addItem(e.ID, events.ScheduleInput{Key: "gate", Label: "Gate", Starts: e.Dates.Start})
	_, err = r.svc.UpdateScheduleItem(r.sctx(), e.ID, first.ID, events.ScheduleInput{Key: "temple-burn", Label: "Temple burn (late)", Starts: e.Dates.Start.Add(time.Hour)})
	wantNoErr(t, err)
	_, err = r.svc.UpdateScheduleItem(r.sctx(), e.ID, second.ID, events.ScheduleInput{Key: "temple-burn", Label: "Gate", Starts: e.Dates.Start})
	wantCoded(t, err, errs.Conflict, "key", "duplicate")
}

func TestAddScheduleItem_ConcurrentSameKey_OneWins(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	const n = 8
	errsOut := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errsOut[i] = r.svc.AddScheduleItem(r.sctx(), e.ID, events.ScheduleInput{Key: "temple-burn", Label: fmt.Sprintf("Burn %d", i), Starts: e.Dates.Start})
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errsOut {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && hasField(err, "key", "duplicate"):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d adds succeeded, want 1", wins)
	}
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if len(d.Schedule) != 1 {
		t.Errorf("%d items, want 1", len(d.Schedule))
	}
}

// F73
func TestAddScheduleItem_TooMany_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	for i := range events.MaxScheduleItems {
		r.addItem(e.ID, events.ScheduleInput{Label: fmt.Sprintf("Item %d", i), Starts: e.Dates.Start.Add(time.Duration(i) * time.Minute)})
	}
	e0, _ := r.mark()
	_, err := r.svc.AddScheduleItem(r.sctx(), e.ID, events.ScheduleInput{Label: "One too many", Starts: e.Dates.Start})
	wantField(t, err, "schedule", "too_many")
	if got := paramOf(err, "schedule", "too_many", "max"); got != "100" {
		t.Errorf("max param = %q, want 100", got)
	}
	if en, _ := r.mark(); en != e0 {
		t.Error("a refused add recorded an audit entry")
	}
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if len(d.Schedule) != events.MaxScheduleItems {
		t.Errorf("%d items, want %d", len(d.Schedule), events.MaxScheduleItems)
	}
	// Removing one makes room again.
	wantNoErr(t, r.svc.RemoveScheduleItem(r.sctx(), e.ID, d.Schedule[0].ID))
	r.addItem(e.ID, events.ScheduleInput{Label: "Fits now", Starts: e.Dates.Start})
}

func TestUpdateScheduleItem_ReplacesAndRecordsChanged(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	it := r.addItem(e.ID, events.ScheduleInput{Key: "gate", Label: "Gate", Starts: e.Dates.Start, Ends: e.Dates.Start.Add(time.Hour)})
	e0, s0 := r.mark()
	got, err := r.svc.UpdateScheduleItem(r.sctx(), e.ID, it.ID, events.ScheduleInput{Label: "Gate (moved)", Starts: e.Dates.Start.Add(2 * time.Hour)})
	wantNoErr(t, err)
	if got.ID != it.ID || got.Event != e.ID || got.Label != "Gate (moved)" || got.Key != "" || !got.Ends.IsZero() ||
		!got.Starts.Equal(e.Dates.Start.Add(2*time.Hour)) {
		t.Errorf("Update = %+v, want a full replacement (key and end cleared)", got)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.schedule-item.changed" || !isRef(es[0], "events.schedule-item", [16]byte(it.ID)) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	if len(r.bus.Published()) != s0 {
		t.Error("a schedule change published a signal")
	}
}

func TestRemoveScheduleItem_DeletesAndRecordsDeleted(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	it := r.addItem(e.ID, events.ScheduleInput{Label: "Gate", Starts: e.Dates.Start})
	keep := r.addItem(e.ID, events.ScheduleInput{Label: "Keep", Starts: e.Dates.Start})
	e0, s0 := r.mark()
	wantNoErr(t, r.svc.RemoveScheduleItem(r.sctx(), e.ID, it.ID))
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.schedule-item.deleted" || !isRef(es[0], "events.schedule-item", [16]byte(it.ID)) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	if len(r.bus.Published()) != s0 {
		t.Error("a schedule change published a signal")
	}
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if len(d.Schedule) != 1 || d.Schedule[0].ID != keep.ID {
		t.Errorf("schedule = %+v", d.Schedule)
	}
	// Removing it again: the item is gone.
	wantKind(t, r.svc.RemoveScheduleItem(r.sctx(), e.ID, it.ID), errs.NotFound)
}

// F74
func TestScheduleItem_WrongEvent_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, nil)
	a := r.mk("Burn 2027")
	b := r.mk("Decompression 2027")
	it := r.addItem(a.ID, events.ScheduleInput{Label: "Gate", Starts: a.Dates.Start})
	in := events.ScheduleInput{Label: "Hijack", Starts: a.Dates.Start}

	_, err := r.svc.UpdateScheduleItem(r.sctx(), b.ID, it.ID, in)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.RemoveScheduleItem(r.sctx(), b.ID, it.ID), errs.NotFound)

	unknownItem := model.NewID[events.ScheduleItemID]()
	_, err = r.svc.UpdateScheduleItem(r.sctx(), a.ID, unknownItem, in)
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.RemoveScheduleItem(r.sctx(), a.ID, unknownItem), errs.NotFound)

	// An unknown event.
	_, err = r.svc.AddScheduleItem(r.sctx(), newEvent(), in)
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.UpdateScheduleItem(r.sctx(), newEvent(), it.ID, in)
	wantKind(t, err, errs.NotFound)

	// The item stayed where it was.
	d, err := r.svc.Get(r.sctx(), a.ID)
	wantNoErr(t, err)
	if len(d.Schedule) != 1 || d.Schedule[0].Label != "Gate" {
		t.Errorf("schedule = %+v", d.Schedule)
	}
}

// F75 success side
func TestSchedule_Permission_HolderSucceeds_OthersRefused(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	it := r.addItem(e.ID, events.ScheduleInput{Label: "Gate", Starts: e.Dates.Start})
	nobody := asPerson(ctxT(t), newPerson())
	in := events.ScheduleInput{Label: "x", Starts: e.Dates.Start}

	_, err := r.svc.AddScheduleItem(nobody, e.ID, in)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.UpdateScheduleItem(nobody, e.ID, it.ID, in)
	wantKind(t, err, errs.Permission)
	wantKind(t, r.svc.RemoveScheduleItem(nobody, e.ID, it.ID), errs.Permission)

	super := asSuper(ctxT(t), newPerson())
	_, err = r.svc.AddScheduleItem(super, e.ID, in)
	wantNoErr(t, err)
}

// Schedule rows have a foreign key to the event: closed events keep their schedule readable.
func TestGet_ClosedEventKeepsItsSchedule(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	r.addItem(e.ID, events.ScheduleInput{Key: "temple-burn", Label: "Temple burn", Starts: e.Dates.Start})
	_, err := r.svc.Close(r.sctx(), e.ID)
	wantNoErr(t, err)
	d, err := r.svc.Get(asPerson(ctxT(t), newPerson()), e.ID)
	wantNoErr(t, err)
	if !d.Closed || len(d.Schedule) != 1 || d.Schedule[0].Key != "temple-burn" {
		t.Errorf("Get = %+v", d)
	}
}
