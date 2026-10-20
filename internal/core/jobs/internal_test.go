package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/model"
)

var internalEpoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type noRedeliverer struct{}

func (noRedeliverer) Redeliver(context.Context, signals.Delivery) error { return nil }

// slotStore records the scheduler's statements; every other method is the embedded nil
// interface, so a test that reaches one fails loudly.
type slotStore struct {
	store
	slots []slotCall
}

type slotCall struct {
	name, key string
	runAt     time.Time
}

func (s *slotStore) InsertSlot(_ context.Context, _ rows.ID, name, key string, runAt, _ time.Time) (bool, error) {
	s.slots = append(s.slots, slotCall{name, key, runAt})
	return true, nil
}

func (s *slotStore) ReclaimExpired(context.Context, time.Time, int, int) ([]rows.Reclaimed, error) {
	return nil, nil
}

func internalRunner(t *testing.T, st store, mod ...func(*Options)) (*Runner, *clock.Fake) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(internalEpoch)
	az := authztest.NewFake()
	o := Options{
		DB: dbtest.NewFake(), Clock: clk, Logger: c.Logger(), Tracer: c.Tracer("jobs"), Meter: c.Meter("jobs"),
		Audit: audittest.NewFake("jobs"), Authz: az,
		Actor:       func(context.Context) (authz.Actor, bool) { return authz.Actor{}, false },
		Redeliverer: noRedeliverer{}, Instance: "test-a",
	}
	for _, m := range mod {
		m(&o)
	}
	r, err := newRunner(o, st)
	if err != nil {
		t.Fatalf("newRunner: %v", err)
	}
	return r, clk
}

func TestDecide_OutcomeByKind_Table(t *testing.T) {
	r, _ := internalRunner(t, &slotStore{})
	live := context.Background()
	canceled, cancel := context.WithCancel(live)
	cancel()
	lease := func(attempts int) rows.Lease { return rows.Lease{Name: "volunteers.remind", Attempts: attempts} }
	tests := []struct {
		name         string
		ctx          context.Context
		lease        rows.Lease
		err          error
		panicked     bool
		wantKind     verdictKind
		wantToken    string
		wantOutcome  string
		wantPausedRO bool
	}{
		{"nil is done", live, lease(1), nil, false, vDone, "", outcomeOK, false},
		{"panic fails", live, lease(1), nil, true, vFail, tokenPanic, outcomePanicked, false},
		{"invalid fails at once", live, lease(1), errs.E("t", errs.Invalid, errors.New("x")), false, vFail, "invalid", outcomeFailed, false},
		{"internal fails at once", live, lease(1), errors.New("bug"), false, vFail, "internal", outcomeFailed, false},
		{"not found fails", live, lease(3), errs.E("t", errs.NotFound, errors.New("x")), false, vFail, "not_found", outcomeFailed, false},
		{"canceled with a live ctx fails", live, lease(1), errs.E("t", errs.Canceled, context.Canceled), false, vFail, "canceled", outcomeFailed, false},
		{"canceled with a done ctx releases", canceled, lease(1), errs.E("t", errs.Canceled, context.Canceled), false, vRelease, tokenCanceled, outcomeCanceled, false},
		{"unavailable retries", live, lease(1), errs.E("t", errs.Unavailable, errors.New("down")), false, vRequeue, "unavailable", outcomeRetried, false},
		{"timeout retries", live, lease(9), errs.E("t", errs.Timeout, errors.New("slow")), false, vRequeue, "timeout", outcomeRetried, false},
		{"unavailable at the last attempt gives up", live, lease(10), errs.E("t", errs.Unavailable, errors.New("down")), false, vFail, tokenGaveUp, outcomeFailed, false},
		{"read-only releases and pauses", live, lease(1), errs.E("t", errs.Unavailable, db.ErrReadOnly), false, vRelease, "", outcomeReadOnly, true},
		{"a dropped delivery is done", live, lease(1), errDropped, false, vDone, "", outcomeDropped, false},
		{"gave up fails gave_up", live, lease(2), errors.Join(errGaveUp, errs.E("t", errs.Unavailable, errors.New("x"))), false, vFail, tokenGaveUp, outcomeFailed, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r.paused.Store(false)
			v := r.decide(tc.ctx, tc.lease, tc.err, tc.panicked)
			if v.kind != tc.wantKind || v.token != tc.wantToken || v.outcome != tc.wantOutcome {
				t.Errorf("decide = {kind %d, token %q, outcome %q}, want {kind %d, token %q, outcome %q}",
					v.kind, v.token, v.outcome, tc.wantKind, tc.wantToken, tc.wantOutcome)
			}
			if got := r.paused.Load(); got != tc.wantPausedRO {
				t.Errorf("paused = %v, want %v", got, tc.wantPausedRO)
			}
		})
	}
}

func TestDecide_RequeueBackoff_UsesDeliveriesMadeForARedelivery(t *testing.T) {
	r, clk := internalRunner(t, &slotStore{}, func(o *Options) {
		o.Retry = retry.Policy{Initial: time.Minute, Max: time.Hour, Multiplier: 2, MaxAttempts: 10}
	})
	r.jitter = func() float64 { return 1 } // no jitter: the base delay
	down := errs.E("t", errs.Unavailable, errors.New("down"))
	// The row has been leased once, but this is the fourth delivery of the signal.
	v := r.decide(context.Background(), rows.Lease{Attempts: 1}, &attemptError{err: down, n: 4}, false)
	if v.kind != vRequeue {
		t.Fatalf("kind = %d, want requeue", v.kind)
	}
	if want := clk.Now().UTC().Add(8 * time.Minute); !v.runAt.Equal(want) {
		t.Errorf("runAt = %v, want %v (Delay of the fifth attempt)", v.runAt, want)
	}
}

func TestSchedule_InsertsTheCurrentSlotOfEachScheduledName(t *testing.T) {
	st := &slotStore{}
	r, clk := internalRunner(t, st)
	reg, err := r.Scoped("volunteers")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Decl{Name: "volunteers.sweep", Every: 15 * time.Minute, Handler: func(context.Context, Job) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Decl{Name: "volunteers.ondemand", Handler: func(context.Context, Job) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := clk.Advance(37 * time.Minute); err != nil { // 12:37
		t.Fatal(err)
	}
	n, err := r.schedule(context.Background(), r.now())
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	// jobs.purge (hourly) and volunteers.sweep (quarter-hourly); signals.redeliver and the
	// on-demand name have no slot. Only the current slot, never an earlier one.
	want := []slotCall{
		{"jobs.purge", "2026-10-19T12:00:00Z", time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)},
		{"volunteers.sweep", "2026-10-19T12:30:00Z", time.Date(2026, 10, 19, 12, 30, 0, 0, time.UTC)},
	}
	if n != len(want) || len(st.slots) != len(want) {
		t.Fatalf("scheduled %d, recorded %v, want %v", n, st.slots, want)
	}
	for i, w := range want {
		if st.slots[i] != w {
			t.Errorf("slot %d = %+v, want %+v", i, st.slots[i], w)
		}
	}
}

func TestEncodeArgs_RoundTrip(t *testing.T) {
	shift, person := model.NewID[model.ID[struct{}]](), model.NewID[model.PersonID]()
	in := signals.NewPayload(signals.Ref("shift", shift), signals.Ref("person", person))
	raw, err := encodeArgs(in)
	if err != nil {
		t.Fatalf("encodeArgs: %v", err)
	}
	var obj map[string]string
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("the stored form is not an object of strings: %s", raw)
	}
	if obj["shift"] != shift.String() || obj["person"] != person.String() || len(obj) != 2 {
		t.Errorf("stored form = %v, want the two identifiers by role", obj)
	}
	out, err := decodeArgs(raw)
	if err != nil {
		t.Fatalf("decodeArgs: %v", err)
	}
	if len(out) != 2 || out["shift"] != in["shift"] || out["person"] != in["person"] {
		t.Errorf("round trip = %v, want %v", out, in)
	}
}

func TestEncodeArgs_Empty_IsAnEmptyObject(t *testing.T) {
	for _, in := range []Args{nil, {}} {
		raw, err := encodeArgs(in)
		if err != nil || string(raw) != "{}" {
			t.Errorf("encodeArgs(%v) = %q, %v, want {}", in, raw, err)
		}
		out, err := decodeArgs(raw)
		if err != nil || len(out) != 0 {
			t.Errorf("decodeArgs({}) = %v, %v, want none", out, err)
		}
	}
}

func TestDecodeArgs_NotReferences_ReturnsInvalid(t *testing.T) {
	for _, raw := range []string{`{"shift": "not a uuid"}`, `[1]`, `{"delivery": {"attempt": 1}}`} {
		if _, err := decodeArgs([]byte(raw)); !errs.Is(err, errs.Invalid) {
			t.Errorf("decodeArgs(%s) = %v, want Invalid", raw, err)
		}
	}
}

func TestDelivery_Wire_RoundTrip(t *testing.T) {
	in := signals.Delivery{
		Signal: signals.Signal{
			ID: model.NewID[signals.SignalID](), Name: "identity.account-created",
			At:      internalEpoch.Add(time.Second + 5*time.Nanosecond),
			Payload: signals.NewPayload(signals.Ref("person", model.NewID[model.PersonID]())),
		},
		Subscriber: "people", Attempt: 3,
	}
	raw, err := encodeDelivery(in)
	if err != nil {
		t.Fatalf("encodeDelivery: %v", err)
	}
	if err := (Request{Name: NameRedeliver, Key: in.Signal.ID.String() + ":people"}).Validate(); err != nil {
		t.Errorf("the redelivery key is not a valid key: %v", err)
	}
	out, err := decodeDelivery(raw)
	if err != nil {
		t.Fatalf("decodeDelivery: %v", err)
	}
	if out.Signal.ID != in.Signal.ID || out.Signal.Name != in.Signal.Name || !out.Signal.At.Equal(in.Signal.At) ||
		out.Subscriber != in.Subscriber || out.Attempt != in.Attempt || out.Signal.Payload["person"] != in.Signal.Payload["person"] {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
	if err := out.Validate(); err != nil {
		t.Errorf("the decoded delivery is invalid: %v", err)
	}
}

func TestDelivery_Wire_NoPayload_StaysEmpty(t *testing.T) {
	in := signals.Delivery{
		Signal:     signals.Signal{ID: model.NewID[signals.SignalID](), Name: "people.person-deleted", At: internalEpoch},
		Subscriber: "notify", Attempt: 1,
	}
	raw, err := encodeDelivery(in)
	if err != nil {
		t.Fatalf("encodeDelivery: %v", err)
	}
	if !strings.Contains(string(raw), `"payload":{}`) {
		t.Errorf("stored form %s, want an empty payload object, not null", raw)
	}
	out, err := decodeDelivery(raw)
	if err != nil || out.Signal.Payload != nil {
		t.Errorf("decodeDelivery = %+v, %v, want a nil payload", out, err)
	}
}

func TestStatus_StringParseJSON_RoundTrip(t *testing.T) {
	m := map[Status]int{Queued: 1, Running: 2, Done: 3, Failed: 4, Canceled: 5}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back map[Status]int
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal %s: %v", raw, err)
	}
	for st, n := range m {
		if back[st] != n {
			t.Errorf("%s: %d, want %d (%s)", st, back[st], n, raw)
		}
		if got, err := ParseStatus(st.String()); err != nil || got != st {
			t.Errorf("ParseStatus(%q) = %v, %v", st.String(), got, err)
		}
	}
	if _, err := json.Marshal(Status(0)); err == nil {
		t.Error("the zero Status marshaled; it is invalid")
	}
}

func TestTraceparent_IsAW3CHeaderFor(t *testing.T) {
	const id = "4bf92f3577b34da6a3ce929d0e0e4736"
	v := traceparent(id).Get("traceparent")
	if len(v) != 55 || !strings.HasPrefix(v, "00-"+id+"-") || !strings.HasSuffix(v, "-01") {
		t.Errorf("traceparent = %q, want version 00, the trace ID, a parent ID and flags (55 bytes)", v)
	}
}

func TestTally_Outcome_CountsEachOutcomeOnce(t *testing.T) {
	var ts tally
	for _, o := range []string{
		outcomeOK, outcomeDropped, outcomeRetried, outcomeFailed, outcomePanicked, outcomeNoHandler,
		outcomeCanceled, outcomeReadOnly, outcomeLostLease,
	} {
		ts.outcome(o)
	}
	ts.scheduled(2)
	got := ts.snapshot()
	want := Ran{Scheduled: 2, Ran: 2, Failed: 3, Retried: 1, Released: 3}
	if got != want {
		t.Errorf("tally = %+v, want %+v", got, want)
	}
}

func TestDeclared_ListsTheRunnersOwnJobsWithoutIO(t *testing.T) {
	r, _ := internalRunner(t, &slotStore{}) // the nil embedded store would panic on any I/O
	got := r.Declared()
	if len(got) != 2 || got[0].Name != NamePurge || got[1].Name != NameRedeliver {
		t.Fatalf("Declared = %+v, want jobs.purge then signals.redeliver", got)
	}
	if got[0].Every != time.Hour || got[0].Timeout != 10*time.Minute || got[0].Owner != "jobs" {
		t.Errorf("jobs.purge = %+v, want hourly, 10m timeout, owner jobs", got[0])
	}
	if got[1].Every != 0 || got[1].Timeout != DefaultRunTimeout || got[1].Owner != "signals" {
		t.Errorf("signals.redeliver = %+v, want on demand, the default timeout, owner signals", got[1])
	}
}
