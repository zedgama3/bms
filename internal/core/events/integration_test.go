//go:build integration

package events_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func payloadEvent(t testing.TB, s signals.Signal, key string) (model.EventID, error) {
	t.Helper()
	return signals.Get[model.EventID](s.Payload, key)
}

func isRef(e audit.Entry, kind string, id [16]byte) bool {
	return e.Record.Kind == kind && e.Record.ID == id
}

func names(es []events.Event) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

// ---- migrations and kinds --------------------------------------------------------------

func TestMigrations_ApplyOnEmptyDatabase_AndAgain(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateEvents(t, pool)
	migrateEvents(t, pool)
	r := newRig(t, pool)
	r.link()
	got, err := r.svc.List(asSuper(ctxT(t), newPerson()), events.ListQuery{})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("a fresh database holds events: %v", got)
	}
}

func TestListKinds_SeededThree_AnySignedInPerson(t *testing.T) {
	r := newIRig(t, nil)
	got, err := r.svc.ListKinds(asPerson(ctxT(t), newPerson()))
	wantNoErr(t, err)
	want := map[string]string{"burn": "Burn", "decompression": "Decompression", "other": "Other"}
	if len(got) != len(want) {
		t.Fatalf("ListKinds = %+v, want the three seeded kinds", got)
	}
	for _, k := range got {
		name, ok := want[k.Key]
		if !ok {
			t.Errorf("unexpected kind %q", k.Key)
			continue
		}
		if k.Name != name || !k.Builtin || k.CreatedAt.IsZero() || k.UpdatedAt.IsZero() {
			t.Errorf("kind %q = %+v", k.Key, k)
		}
	}
}

func TestCreateKind_Succeeds_RecordsAudit_NoSignal(t *testing.T) {
	r := newIRig(t, nil)
	e0, s0 := r.mark()
	k, err := r.svc.CreateKind(r.sctx(), events.KindInput{Key: "work-weekend", Name: "Work weekend"})
	wantNoErr(t, err)
	if k.Key != "work-weekend" || k.Name != "Work weekend" || k.Builtin || k.CreatedAt.IsZero() {
		t.Errorf("CreateKind = %+v", k)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.kind.created" || es[0].Record.Kind != "events.kind" {
		t.Fatalf("audit = %v, want one events.kind.created", actionsOf(es))
	}
	if len(r.bus.Published()) != s0 {
		t.Error("a kind change published a signal")
	}
	got, err := r.svc.ListKinds(r.sctx())
	wantNoErr(t, err)
	if len(got) != 4 {
		t.Errorf("ListKinds has %d kinds, want 4", len(got))
	}
	// A new kind is usable by an event.
	in := validCreate("Work weekend 1")
	in.Kind = "work-weekend"
	_, err = r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
}

// F67
func TestCreateKind_Taken_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	_, err := r.svc.CreateKind(r.sctx(), events.KindInput{Key: "work", Name: "Work"})
	wantNoErr(t, err)
	for _, key := range []string{"work", "burn", "decompression", "other"} {
		t.Run(key, func(t *testing.T) {
			e0, s0 := r.mark()
			_, err := r.svc.CreateKind(r.sctx(), events.KindInput{Key: key, Name: "Another"})
			wantCoded(t, err, errs.Conflict, "key", "taken")
			if e, s := r.mark(); e != e0 || s != s0 {
				t.Error("a refused kind recorded an audit entry")
			}
		})
	}
}

// F68, and decision 9: keys are immutable, builtin names are editable, kinds never go.
func TestUpdateKind_RenamesKeepingTheKey(t *testing.T) {
	r := newIRig(t, nil)
	e0, _ := r.mark()
	k, err := r.svc.UpdateKind(r.sctx(), "burn", events.KindInput{Key: "ignored-key", Name: "Regional burn"})
	wantNoErr(t, err)
	if k.Key != "burn" || k.Name != "Regional burn" || !k.Builtin {
		t.Errorf("UpdateKind = %+v, want burn renamed and still builtin", k)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.kind.changed" {
		t.Errorf("audit = %v", actionsOf(es))
	}
	got, err := r.svc.ListKinds(r.sctx())
	wantNoErr(t, err)
	for _, kk := range got {
		if kk.Key == "ignored-key" {
			t.Error("UpdateKind created a kind from the Key in its input")
		}
	}
	if len(got) != 3 {
		t.Errorf("%d kinds after a rename, want 3", len(got))
	}
}

func TestUpdateKind_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, nil)
	_, err := r.svc.UpdateKind(r.sctx(), "no-such-kind", events.KindInput{Name: "X"})
	wantKind(t, err, errs.NotFound)
}

// F69, success side
func TestKinds_Permission_HolderSucceeds_OthersRefused(t *testing.T) {
	r := newIRig(t, nil)
	nobody := asPerson(ctxT(t), newPerson())
	_, err := r.svc.CreateKind(nobody, events.KindInput{Key: "work", Name: "Work"})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.UpdateKind(nobody, "burn", events.KindInput{Name: "X"})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.CreateKind(asSuper(ctxT(t), newPerson()), events.KindInput{Key: "work", Name: "Work"})
	wantNoErr(t, err)
}

// ---- Create ----------------------------------------------------------------------------

func TestCreate_Succeeds_StoresRecordsAndPublishes(t *testing.T) {
	r := newIRig(t, nil)
	advance(t, r.clk, time.Hour)
	in := validCreate("Burn 2027")
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	e := c.Event
	if e.ID.IsZero() || e.Name != "Burn 2027" || e.Kind != "burn" || e.Zone != validZone {
		t.Errorf("Create returned %+v", e)
	}
	if !e.Dates.Start.Equal(in.Dates.Start) || !e.Dates.End.Equal(in.Dates.End) {
		t.Errorf("dates = %+v, want %+v (instants round-trip)", e.Dates, in.Dates)
	}
	if e.Closed || !e.ClosedAt.IsZero() {
		t.Errorf("a new event is closed: %+v", e)
	}
	if !e.CreatedAt.Equal(r.clk.Now()) || !e.UpdatedAt.Equal(r.clk.Now()) {
		t.Errorf("times = %v, %v; want %v", e.CreatedAt, e.UpdatedAt, r.clk.Now())
	}
	if len(c.Setup) != 0 {
		t.Errorf("Setup = %v without CopyFrom", c.Setup)
	}

	es := r.rec.Recorded()
	if len(es) != 1 || es[0].Action != "events.event.created" || !isRef(es[0], "events.event", [16]byte(e.ID)) {
		t.Fatalf("audit = %v, want exactly events.event.created", actionsOf(es))
	}
	if !es[0].Subject.IsZero() {
		t.Error("the audit entry has a subject; events hold nothing about a person")
	}
	ss := r.bus.Published()
	if len(ss) != 1 || ss[0].Name != events.SignalCreated {
		t.Fatalf("signals = %v", ss)
	}
	got, err := payloadEvent(t, ss[0], events.PayloadEvent)
	if err != nil || got != e.ID {
		t.Errorf("payload event = %v, %v", got, err)
	}
	if _, err := payloadEvent(t, ss[0], events.PayloadCopiedFrom); err == nil {
		t.Error("copied-from is in the payload although no source was named")
	}

	d, err := r.svc.Get(asPerson(ctxT(t), newPerson()), e.ID)
	wantNoErr(t, err)
	if d.ID != e.ID || d.Name != e.Name || len(d.Schedule) != 0 {
		t.Errorf("Get = %+v", d)
	}
}

func TestCreate_DatesKeepTheirInstantsAcrossZones(t *testing.T) {
	r := newIRig(t, nil)
	berlin := time.FixedZone("CEST", 2*3600)
	start := time.Date(2027, 8, 24, 10, 0, 0, 0, berlin)
	in := validCreate("Instants")
	in.Dates = model.TimeRange{Start: start, End: start.Add(72 * time.Hour)}
	in.Zone = "Europe/Berlin"
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	got, err := r.svc.Get(r.sctx(), c.Event.ID)
	wantNoErr(t, err)
	if !got.Dates.Start.Equal(start) || !got.Dates.End.Equal(start.Add(72*time.Hour)) || got.Zone != "Europe/Berlin" {
		t.Errorf("Get = %+v dates %+v", got, got.Dates)
	}
}

func TestCreate_BoundaryValues_Accepted(t *testing.T) {
	r := newIRig(t, nil)
	start := epoch.AddDate(0, 0, 30)
	for name, mod := range map[string]func(*events.CreateInput){
		"name at 100 bytes": func(in *events.CreateInput) { in.Name = repeat("a", 100) },
		"exactly 366 days": func(in *events.CreateInput) {
			in.Dates = model.TimeRange{Start: start, End: start.Add(events.MaxEventDuration)}
		},
		"one second long":    func(in *events.CreateInput) { in.Dates = model.TimeRange{Start: start, End: start.Add(time.Second)} },
		"UTC":                func(in *events.CreateInput) { in.Zone = "UTC" },
		"other kind":         func(in *events.CreateInput) { in.Kind = events.KindOther },
		"decompression kind": func(in *events.CreateInput) { in.Kind = events.KindDecompression },
		"dates in the past": func(in *events.CreateInput) {
			in.Dates = model.TimeRange{Start: epoch.AddDate(-2, 0, 0), End: epoch.AddDate(-2, 0, 3)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := validCreate(name)
			mod(&in)
			if _, err := r.svc.Create(r.sctx(), in); err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

// F46
func TestCreate_UnknownKind_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, nil)
	in := validCreate("Burn 2027")
	in.Kind = "no-such-kind"
	e0, s0 := r.mark()
	_, err := r.svc.Create(r.sctx(), in)
	wantField(t, err, "kind", "unknown")
	if e, s := r.mark(); e != e0 || s != s0 {
		t.Error("a refused create recorded an audit entry or published a signal")
	}
	got, err := r.svc.List(r.sctx(), events.ListQuery{})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("an event exists after the refused create: %v", got)
	}
}

// F50
func TestCreate_UnknownCopyFrom_ReturnsInvalid(t *testing.T) {
	p := provider("alpha", nil, 1)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	in := validCreate("Burn 2027")
	in.CopyFrom = newEvent()
	_, err := r.svc.Create(r.sctx(), in)
	wantField(t, err, "copy_from", "unknown")
	if p.calls() != 0 {
		t.Error("a provider ran for an unknown source")
	}
	got, err := r.svc.List(r.sctx(), events.ListQuery{})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("an event exists after the refused create: %v", names(got))
	}
}

// F51
func TestCreate_NameTaken_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	r.mk("Burn 2027")
	r.mkClosed("Burn 2026")
	for _, name := range []string{"Burn 2027", "burn 2027", "BURN 2027", "burn 2026"} {
		t.Run(name, func(t *testing.T) {
			e0, s0 := r.mark()
			_, err := r.svc.Create(r.sctx(), validCreate(name))
			wantCoded(t, err, errs.Conflict, "name", "taken")
			if e, s := r.mark(); e != e0 || s != s0 {
				t.Error("a refused create recorded an audit entry or published a signal")
			}
		})
	}
}

func TestCreate_ConcurrentSameName_OneWins(t *testing.T) {
	r := newIRig(t, nil)
	const n = 8
	errsOut := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "Burn 2027"
			if i%2 == 1 {
				name = "BURN 2027"
			}
			_, errsOut[i] = r.svc.Create(r.sctx(), validCreate(name))
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errsOut {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && hasField(err, "name", "taken"):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d creates succeeded, want 1", wins)
	}
	if got := len(sigsOf(r.bus.Published(), events.SignalCreated)); got != 1 {
		t.Errorf("%d created signals, want 1", got)
	}
}

func TestCreate_Permission_HolderAndSuperuserSucceed(t *testing.T) {
	r := newIRig(t, nil)
	if _, err := r.svc.Create(asPerson(ctxT(t), r.holder(events.PermissionManage)), validCreate("By holder")); err != nil {
		t.Errorf("holder: %v", err)
	}
	if _, err := r.svc.Create(asSuper(ctxT(t), newPerson()), validCreate("By superuser")); err != nil {
		t.Errorf("superuser: %v", err)
	}
	if _, err := r.svc.Create(asPerson(ctxT(t), newPerson()), validCreate("By nobody")); !errs.Is(err, errs.Permission) {
		t.Errorf("non-holder: %v, want Permission", err)
	}
	if _, err := r.svc.Create(asPerson(ctxT(t), newPerson()), validCreate("By nobody")); !errs.Is(err, errs.Permission) {
		t.Errorf("non-holder (again): %v, want Permission", err)
	}
}

// ---- Get, List, InfoOf -----------------------------------------------------------------

// F43
func TestGet_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, nil)
	_, err := r.svc.Get(asPerson(ctxT(t), newPerson()), newEvent())
	wantKind(t, err, errs.NotFound)
}

func TestList_NewestFirst_StatusFilters(t *testing.T) {
	r := newIRig(t, nil)
	base := epoch.AddDate(0, 0, 30)
	mk := func(name string, shiftDays int) events.Event {
		in := validCreate(name)
		in.Dates = model.TimeRange{Start: base.AddDate(0, 0, shiftDays), End: base.AddDate(0, 0, shiftDays+2)}
		c, err := r.svc.Create(r.sctx(), in)
		wantNoErr(t, err)
		return c.Event
	}
	mk("Middle", 10)
	old := mk("Oldest", 0)
	mk("Newest", 20)
	_, err := r.svc.Close(r.sctx(), old.ID)
	wantNoErr(t, err)

	ctx := asPerson(ctxT(t), newPerson())
	for name, tc := range map[string]struct {
		q    events.ListQuery
		want []string
	}{
		"zero is any": {events.ListQuery{}, []string{"Newest", "Middle", "Oldest"}},
		"any":         {events.ListQuery{Status: events.StatusAny}, []string{"Newest", "Middle", "Oldest"}},
		"open":        {events.ListQuery{Status: events.StatusOpen}, []string{"Newest", "Middle"}},
		"closed":      {events.ListQuery{Status: events.StatusClosed}, []string{"Oldest"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := r.svc.List(ctx, tc.q)
			wantNoErr(t, err)
			if !slices.Equal(names(got), tc.want) {
				t.Errorf("List = %v, want %v", names(got), tc.want)
			}
			for _, e := range got {
				if e.Closed != (e.Name == "Oldest") {
					t.Errorf("%s: Closed = %v", e.Name, e.Closed)
				}
			}
		})
	}
}

// Overlapping events are allowed and several can be open at once.
func TestCreate_OverlappingEvents_Allowed(t *testing.T) {
	r := newIRig(t, nil)
	r.mk("First")
	r.mk("Second")
	got, err := r.svc.List(r.sctx(), events.ListQuery{Status: events.StatusOpen})
	wantNoErr(t, err)
	if len(got) != 2 {
		t.Errorf("%d open events, want 2", len(got))
	}
}

// F77
func TestInfoOf_UnknownAbsent_ClosedFlag(t *testing.T) {
	r := newIRig(t, nil)
	a := r.mk("Burn 2027")
	b := r.mkClosed("Burn 2026")
	unknown := newEvent()
	got, err := r.svc.InfoOf(ctxT(t), []model.EventID{a.ID, b.ID, unknown, a.ID})
	wantNoErr(t, err)
	if len(got) != 2 {
		t.Fatalf("InfoOf = %v", got)
	}
	ia, ib := got[a.ID], got[b.ID]
	if ia.ID != a.ID || ia.Name != "Burn 2027" || ia.Kind != "burn" || ia.Zone != validZone || ia.Closed ||
		!ia.Dates.Start.Equal(a.Dates.Start) || !ia.Dates.End.Equal(a.Dates.End) {
		t.Errorf("Info(a) = %+v", ia)
	}
	if !ib.Closed {
		t.Errorf("Info(b) = %+v, want Closed", ib)
	}
	if _, present := got[unknown]; present {
		t.Error("an unknown ID is present")
	}
	// Reopening shows up at once.
	_, err = r.svc.Reopen(r.sctx(), b.ID)
	wantNoErr(t, err)
	got, err = r.svc.InfoOf(ctxT(t), []model.EventID{b.ID})
	wantNoErr(t, err)
	if got[b.ID].Closed {
		t.Error("InfoOf still says closed after Reopen")
	}
}

func TestInfoOf_TwoHundredIDs_OneCall(t *testing.T) {
	r := newIRig(t, nil)
	ids := make([]model.EventID, 0, events.MaxInfo)
	for i := range events.MaxInfo {
		if i < 3 {
			ids = append(ids, r.mk(fmt.Sprintf("Event %d", i)).ID)
		} else {
			ids = append(ids, newEvent())
		}
	}
	got, err := r.svc.InfoOf(ctxT(t), ids)
	wantNoErr(t, err)
	if len(got) != 3 {
		t.Errorf("InfoOf returned %d, want 3", len(got))
	}
	_, err = r.svc.InfoOf(ctxT(t), append(ids, newEvent()))
	wantField(t, err, "ids", "too_many")
}

// ---- Update ----------------------------------------------------------------------------

// F58 (unknown)
func TestUpdate_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, nil)
	_, err := r.svc.Update(r.sctx(), newEvent(), events.UpdateInput{Name: str("x")})
	wantKind(t, err, errs.NotFound)
}

func TestUpdate_ChangesFieldsAndPublishesChanged(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	advance(t, r.clk, time.Hour)
	e0, s0 := r.mark()
	nd := model.TimeRange{Start: e.Dates.Start.AddDate(0, 0, 1), End: e.Dates.End.AddDate(0, 0, 2)}
	got, err := r.svc.Update(r.sctx(), e.ID, events.UpdateInput{Name: str("Burn 2027 (moved)"), Kind: str(events.KindOther), Zone: str("Europe/Berlin"), Dates: &nd})
	wantNoErr(t, err)
	if got.Name != "Burn 2027 (moved)" || got.Kind != "other" || got.Zone != "Europe/Berlin" ||
		!got.Dates.Start.Equal(nd.Start) || !got.Dates.End.Equal(nd.End) {
		t.Errorf("Update = %+v", got)
	}
	if !got.UpdatedAt.Equal(r.clk.Now()) || !got.CreatedAt.Equal(e.CreatedAt) {
		t.Errorf("times: created %v updated %v", got.CreatedAt, got.UpdatedAt)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.event.changed" || !isRef(es[0], "events.event", [16]byte(e.ID)) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	for _, f := range []string{"name", "kind", "zone", "dates"} {
		if !slices.Contains(es[0].Fields, f) {
			t.Errorf("audit fields %v lack %q", es[0].Fields, f)
		}
	}
	ss := r.bus.Published()[s0:]
	if len(ss) != 1 || ss[0].Name != events.SignalChanged {
		t.Fatalf("signals = %v", ss)
	}
	if id, err := payloadEvent(t, ss[0], events.PayloadEvent); err != nil || id != e.ID {
		t.Errorf("payload event = %v, %v", id, err)
	}

	// Partial update leaves the rest alone.
	got2, err := r.svc.Update(r.sctx(), e.ID, events.UpdateInput{Zone: str("UTC")})
	wantNoErr(t, err)
	if got2.Name != got.Name || got2.Kind != got.Kind || got2.Zone != "UTC" || !got2.Dates.Start.Equal(nd.Start) {
		t.Errorf("partial Update = %+v", got2)
	}
}

func TestUpdate_BadValuesAgainstStore_ReturnsInvalid(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	_, err := r.svc.Update(r.sctx(), e.ID, events.UpdateInput{Kind: str("no-such-kind")})
	wantField(t, err, "kind", "unknown")
	got, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if got.Kind != "burn" {
		t.Errorf("Kind = %q after a refused update", got.Kind)
	}
}

func TestUpdate_NameTaken_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	r.mk("Burn 2027")
	b := r.mk("Decompression 2027")
	r.mkClosed("Burn 2026")
	for _, name := range []string{"burn 2027", "BURN 2026"} {
		e0, s0 := r.mark()
		_, err := r.svc.Update(r.sctx(), b.ID, events.UpdateInput{Name: str(name)})
		wantCoded(t, err, errs.Conflict, "name", "taken")
		if e, s := r.mark(); e != e0 || s != s0 {
			t.Error("a refused rename recorded an audit entry or published a signal")
		}
	}
}

// F57
func TestUpdate_Closed_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mkClosed("Burn 2027")
	e0, s0 := r.mark()
	_, err := r.svc.Update(r.sctx(), e.ID, events.UpdateInput{Name: str("Changed")})
	wantCoded(t, err, errs.Conflict, "", "closed")
	if en, s := r.mark(); en != e0 || s != s0 {
		t.Error("a refused update recorded an audit entry or published a signal")
	}
	// Reopen lets edits through again.
	_, err = r.svc.Reopen(r.sctx(), e.ID)
	wantNoErr(t, err)
	_, err = r.svc.Update(r.sctx(), e.ID, events.UpdateInput{Name: str("Changed")})
	wantNoErr(t, err)
}

func TestUpdate_Permission_HolderSucceeds(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	_, err := r.svc.Update(asPerson(ctxT(t), newPerson()), e.ID, events.UpdateInput{Name: str("x")})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Update(asSuper(ctxT(t), newPerson()), e.ID, events.UpdateInput{Name: str("By superuser")})
	wantNoErr(t, err)
}

// ---- Close and Reopen ------------------------------------------------------------------

func TestClose_FlipsRecordsAndPublishes(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027") // not over yet: closing early is allowed
	advance(t, r.clk, time.Hour)
	e0, s0 := r.mark()
	got, err := r.svc.Close(r.sctx(), e.ID)
	wantNoErr(t, err)
	if !got.Closed || !got.ClosedAt.Equal(r.clk.Now()) {
		t.Errorf("Close = %+v, want closed at %v", got, r.clk.Now())
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.event.closed" || !isRef(es[0], "events.event", [16]byte(e.ID)) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	ss := r.bus.Published()[s0:]
	if len(ss) != 1 || ss[0].Name != events.SignalClosed {
		t.Fatalf("signals = %v", ss)
	}
	if id, err := payloadEvent(t, ss[0], events.PayloadEvent); err != nil || id != e.ID {
		t.Errorf("payload = %v, %v", id, err)
	}
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if !d.Closed {
		t.Error("Get says open after Close")
	}
}

func TestReopen_FlipsRecordsAndPublishes(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mkClosed("Burn 2027")
	e0, s0 := r.mark()
	got, err := r.svc.Reopen(r.sctx(), e.ID)
	wantNoErr(t, err)
	if got.Closed || !got.ClosedAt.IsZero() {
		t.Errorf("Reopen = %+v", got)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "events.event.reopened" || !isRef(es[0], "events.event", [16]byte(e.ID)) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	ss := r.bus.Published()[s0:]
	if len(ss) != 1 || ss[0].Name != events.SignalReopened {
		t.Fatalf("signals = %v", ss)
	}
}

// F59
func TestClose_Twice_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mkClosed("Burn 2027")
	e0, s0 := r.mark()
	_, err := r.svc.Close(r.sctx(), e.ID)
	wantCoded(t, err, errs.Conflict, "", "closed")
	if en, s := r.mark(); en != e0 || s != s0 {
		t.Error("a refused close recorded an audit entry or published a signal")
	}
}

// F60
func TestReopen_Open_ReturnsConflict(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	e0, s0 := r.mark()
	_, err := r.svc.Reopen(r.sctx(), e.ID)
	wantCoded(t, err, errs.Conflict, "", "open")
	if en, s := r.mark(); en != e0 || s != s0 {
		t.Error("a refused reopen recorded an audit entry or published a signal")
	}
}

// F61
func TestClose_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, nil)
	_, err := r.svc.Close(r.sctx(), newEvent())
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.Reopen(r.sctx(), newEvent())
	wantKind(t, err, errs.NotFound)
}

func TestCloseReopen_Permission_HolderSucceeds(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	_, err := r.svc.Close(asPerson(ctxT(t), newPerson()), e.ID)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Close(asSuper(ctxT(t), newPerson()), e.ID)
	wantNoErr(t, err)
	_, err = r.svc.Reopen(asPerson(ctxT(t), newPerson()), e.ID)
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Reopen(asSuper(ctxT(t), newPerson()), e.ID)
	wantNoErr(t, err)
}

func TestCloseReopen_Concurrent_OneWins(t *testing.T) {
	r := newIRig(t, nil)
	e := r.mk("Burn 2027")
	const n = 8
	errsOut := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errsOut[i] = r.svc.Close(r.sctx(), e.ID)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errsOut {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && hasField(err, "", "closed"):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d closes succeeded, want 1", wins)
	}
	if got := len(sigsOf(r.bus.Published(), events.SignalClosed)); got != 1 {
		t.Errorf("%d closed signals, want 1", got)
	}
}

// ---- audit and publish failures roll the write back ------------------------------------

func TestWrite_AuditFails_RollsBack(t *testing.T) {
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit down"))
	type op struct {
		name   string
		action audit.Action
		setup  func(r *irig) model.EventID
		call   func(r *irig, id model.EventID) error
		check  func(t *testing.T, r *irig, id model.EventID)
	}
	ops := []op{
		{"Create", "events.event.created",
			func(*irig) model.EventID { return model.EventID{} },
			func(r *irig, _ model.EventID) error {
				_, err := r.svc.Create(r.sctx(), validCreate("Burn"))
				return keep(err)
			},
			func(t *testing.T, r *irig, _ model.EventID) {
				got, err := r.svc.List(r.sctx(), events.ListQuery{})
				wantNoErr(t, err)
				if len(got) != 0 {
					t.Errorf("an event exists after the failed create: %v", names(got))
				}
			}},
		{"Update", "events.event.changed",
			func(r *irig) model.EventID { return r.mk("Burn").ID },
			func(r *irig, id model.EventID) error {
				_, err := r.svc.Update(r.sctx(), id, events.UpdateInput{Name: str("Renamed")})
				return keep(err)
			},
			func(t *testing.T, r *irig, id model.EventID) {
				d, err := r.svc.Get(r.sctx(), id)
				wantNoErr(t, err)
				if d.Name != "Burn" {
					t.Errorf("name = %q after the failed update", d.Name)
				}
			}},
		{"Close", "events.event.closed",
			func(r *irig) model.EventID { return r.mk("Burn").ID },
			func(r *irig, id model.EventID) error { _, err := r.svc.Close(r.sctx(), id); return keep(err) },
			func(t *testing.T, r *irig, id model.EventID) {
				d, err := r.svc.Get(r.sctx(), id)
				wantNoErr(t, err)
				if d.Closed {
					t.Error("closed despite the failed audit write")
				}
			}},
		{"Reopen", "events.event.reopened",
			func(r *irig) model.EventID { return r.mkClosed("Burn").ID },
			func(r *irig, id model.EventID) error { _, err := r.svc.Reopen(r.sctx(), id); return keep(err) },
			func(t *testing.T, r *irig, id model.EventID) {
				d, err := r.svc.Get(r.sctx(), id)
				wantNoErr(t, err)
				if !d.Closed {
					t.Error("reopened despite the failed audit write")
				}
			}},
		{"CreateKind", "events.kind.created",
			func(*irig) model.EventID { return model.EventID{} },
			func(r *irig, _ model.EventID) error {
				_, err := r.svc.CreateKind(r.sctx(), events.KindInput{Key: "work", Name: "Work"})
				return keep(err)
			},
			func(t *testing.T, r *irig, _ model.EventID) {
				ks, err := r.svc.ListKinds(r.sctx())
				wantNoErr(t, err)
				if len(ks) != 3 {
					t.Errorf("%d kinds after the failed create", len(ks))
				}
			}},
		{"AddScheduleItem", "events.schedule-item.created",
			func(r *irig) model.EventID { return r.mk("Burn").ID },
			func(r *irig, id model.EventID) error {
				_, err := r.svc.AddScheduleItem(r.sctx(), id, events.ScheduleInput{Label: "Gate", Starts: epoch.AddDate(0, 0, 31)})
				return keep(err)
			},
			func(t *testing.T, r *irig, id model.EventID) {
				d, err := r.svc.Get(r.sctx(), id)
				wantNoErr(t, err)
				if len(d.Schedule) != 0 {
					t.Errorf("schedule = %v after the failed add", d.Schedule)
				}
			}},
	}
	for _, o := range ops {
		t.Run(o.name, func(t *testing.T) {
			r := newIRig(t, nil)
			id := o.setup(r)
			_, s0 := r.mark()
			r.gate.failAction(o.action, boom)
			err := o.call(r, id)
			if err == nil || !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the audit error", err)
			}
			wantKind(t, err, errs.Unavailable)
			if got := len(r.bus.Published()); got != s0 {
				t.Errorf("%d signals published for a rolled-back write", got-s0)
			}
			o.check(t, r, id)
		})
	}
}

func TestWrite_PublishFails_RollsBack(t *testing.T) {
	boom := errs.E("test.bus", errs.Unavailable, errors.New("bus closed"))
	r := newIRig(t, nil)

	r.fbus.fail(boom)
	_, err := r.svc.Create(r.sctx(), validCreate("Burn"))
	if !errors.Is(err, boom) {
		t.Fatalf("Create err = %v, want the publish error", err)
	}
	r.fbus.fail(nil)
	got, err := r.svc.List(r.sctx(), events.ListQuery{})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("an event exists after the failed create: %v", names(got))
	}

	e := r.mk("Burn")
	r.fbus.fail(boom)
	if _, err := r.svc.Update(r.sctx(), e.ID, events.UpdateInput{Name: str("Renamed")}); !errors.Is(err, boom) {
		t.Fatalf("Update err = %v", err)
	}
	if _, err := r.svc.Close(r.sctx(), e.ID); !errors.Is(err, boom) {
		t.Fatalf("Close err = %v", err)
	}
	r.fbus.fail(nil)
	d, err := r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if d.Name != "Burn" || d.Closed {
		t.Errorf("event = %+v after failed publishes", d.Event)
	}
	_, err = r.svc.Close(r.sctx(), e.ID)
	wantNoErr(t, err)
	r.fbus.fail(boom)
	if _, err := r.svc.Reopen(r.sctx(), e.ID); !errors.Is(err, boom) {
		t.Fatalf("Reopen err = %v", err)
	}
	r.fbus.fail(nil)
	d, err = r.svc.Get(r.sctx(), e.ID)
	wantNoErr(t, err)
	if !d.Closed {
		t.Error("reopened despite the failed publish")
	}
}

// ---- read-only copy over a real database -----------------------------------------------

type roDB struct{ db.DB }

func (roDB) ReadOnly() bool { return true }

func (r roDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	if mode == db.Write {
		return errs.E("test.roDB.Transact", errs.Unavailable, db.ErrReadOnly)
	}
	return r.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

func TestReadOnlyCopy_ReadsWork_WritesRefused(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateEvents(t, pool)
	w := newRig(t, pool)
	w.link()
	steward := newPerson()
	w.az.Grant(steward, events.PermissionManage)
	c, err := w.svc.Create(asPerson(ctxT(t), steward), validCreate("Burn"))
	wantNoErr(t, err)

	ro := newRig(t, roDB{pool})
	ro.link()
	ro.az.Grant(steward, events.PermissionManage)
	ctx := asPerson(ctxT(t), steward)
	if d, err := ro.svc.Get(ctx, c.Event.ID); err != nil || d.Name != "Burn" {
		t.Errorf("Get on a read-only copy = %+v, %v", d, err)
	}
	if ks, err := ro.svc.ListKinds(ctx); err != nil || len(ks) != 3 {
		t.Errorf("ListKinds on a read-only copy = %v, %v", ks, err)
	}
	_, err = ro.svc.Close(ctx, c.Event.ID)
	wantReadOnly(t, err)
}

// ---- handlers over a real store ---------------------------------------------------------

func TestHandlers_Create_ReturnsCreatedWithEmptySetup(t *testing.T) {
	r := newIRig(t, nil)
	body := `{"name":"Burn 2027","kind":"burn","dates":{"start":"2027-08-24T00:00:00Z","end":"2027-09-01T00:00:00Z"},"zone":"America/Los_Angeles"}`
	w, f := r.do(r.sctx(), "POST", "/", body)
	if f.count() != 0 || (w.Code != http.StatusOK && w.Code != http.StatusCreated) {
		t.Fatalf("status %d, failure %v", w.Code, f.last())
	}
	if !strings.Contains(w.Body.String(), `"setup":[]`) {
		t.Errorf("the response has no empty setup array: %s", w.Body.String())
	}
	w, f = r.do(r.sctx(), "POST", "/", body)
	if f.count() != 1 {
		t.Fatalf("second create: %d failures, status %d", f.count(), w.Code)
	}
	wantCoded(t, f.last(), errs.Conflict, "name", "taken")
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestHandlers_GetUnknown_Returns404(t *testing.T) {
	r := newIRig(t, nil)
	w, f := r.do(r.sctx(), "GET", "/"+newEvent().String(), "")
	if f.count() != 1 {
		t.Fatalf("%d failures, status %d", f.count(), w.Code)
	}
	wantKind(t, f.last(), errs.NotFound)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandlers_ListAndKinds_ReturnJSON(t *testing.T) {
	r := newIRig(t, nil)
	r.mk("Burn 2027")
	for _, path := range []string{"/", "/?status=open", "/?status=closed", "/kinds"} {
		w, f := r.do(asPerson(ctxT(t), newPerson()), "GET", path, "")
		if f.count() != 0 || w.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, failure %v", path, w.Code, f.last())
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("GET %s: Content-Type %q", path, w.Header().Get("Content-Type"))
		}
	}
}

// ---- concurrency of reads --------------------------------------------------------------

func TestService_ConcurrentUse_Safe(t *testing.T) {
	r := newIRig(t, []events.SetupProvider{provider("alpha", nil, 1).sp()})
	e := r.mk("Burn 2027")
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := asPerson(ctxT(t), newPerson())
			if _, err := r.svc.List(ctx, events.ListQuery{}); err != nil {
				t.Errorf("List: %v", err)
			}
			if _, err := r.svc.Get(ctx, e.ID); err != nil {
				t.Errorf("Get: %v", err)
			}
			if _, err := r.svc.InfoOf(ctx, []model.EventID{e.ID}); err != nil {
				t.Errorf("InfoOf: %v", err)
			}
			in := validCreate(fmt.Sprintf("Event %d", i))
			in.CopyFrom = e.ID
			if _, err := r.svc.Create(r.sctx(), in); err != nil {
				t.Errorf("Create: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ---- nothing personal in telemetry -----------------------------------------------------

func TestNoPersonalDataInLogs(t *testing.T) {
	const marker = "Marker Zephyr Quince Gathering"
	const labelMarker = "marker schedule label 4471"
	p := provider("alpha", nil, 2)
	r := newIRig(t, []events.SetupProvider{p.sp()})
	src := r.mk("Source event")
	in := validCreate(marker)
	in.CopyFrom = src.ID
	c, err := r.svc.Create(r.sctx(), in)
	wantNoErr(t, err)
	_, err = r.svc.Update(r.sctx(), c.Event.ID, events.UpdateInput{Name: str(marker + " 2")})
	wantNoErr(t, err)
	_, err = r.svc.AddScheduleItem(r.sctx(), c.Event.ID, events.ScheduleInput{Label: labelMarker, Starts: epoch.AddDate(0, 0, 31)})
	wantNoErr(t, err)
	_, err = r.svc.Create(r.sctx(), validCreate(marker+" 2")) // a conflict, logged too
	wantKind(t, err, errs.Conflict)
	_, err = r.svc.Close(r.sctx(), c.Event.ID)
	wantNoErr(t, err)
	for _, needle := range []string{marker, labelMarker} {
		if r.cap.Contains(needle) {
			t.Errorf("telemetry contains %q: names and labels are never logged", needle)
		}
	}
	for _, e := range r.rec.Recorded() {
		for _, f := range e.Fields {
			if f == marker || f == labelMarker {
				t.Errorf("audit field list holds a value: %q", f)
			}
		}
		if strings.Contains(e.Reason, marker) {
			t.Error("audit reason holds a name")
		}
	}
}

// failCommitDB runs fn inside the real transaction, then reports that the commit failed:
// the transaction rolls back after the insert, the audit entry and the publish succeeded.
type failCommitDB struct{ db.DB }

func (f failCommitDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	err := f.DB.Transact(ctx, mode, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		if mode == db.Write {
			return errs.E("test.failCommitDB", errs.Unavailable, errors.New("commit failed"))
		}
		return nil
	})
	return err //nolint:wrapcheck // test double
}

func TestCreate_PublishesAfterCommitOnly(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateEvents(t, pool)
	bad := newRig(t, failCommitDB{pool})
	bad.link()
	steward := newPerson()
	bad.az.Grant(steward, events.PermissionManage)
	_, err := bad.svc.Create(asPerson(ctxT(t), steward), validCreate("Burn 2027"))
	wantKind(t, err, errs.Unavailable)
	if got := bad.bus.Published(); len(got) != 0 {
		t.Errorf("signals delivered for a transaction that did not commit: %v", got)
	}
	good := newRig(t, pool)
	good.link()
	good.az.Grant(steward, events.PermissionManage)
	list, err := good.svc.List(asPerson(ctxT(t), steward), events.ListQuery{})
	wantNoErr(t, err)
	if len(list) != 0 {
		t.Errorf("an event exists after the failed commit: %v", names(list))
	}
	// And a healthy commit publishes exactly once.
	_, err = good.svc.Create(asPerson(ctxT(t), steward), validCreate("Burn 2027"))
	wantNoErr(t, err)
	if got := len(good.bus.Published()); got != 1 {
		t.Errorf("%d signals after a good commit, want 1", got)
	}
}
