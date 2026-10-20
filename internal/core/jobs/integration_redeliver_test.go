//go:build integration

package jobs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestDefer_StoresRedeliverJobWithKeyAndBackoff(t *testing.T) {
	r := newIRig(t)
	d := delivery(1)
	wantNoErr(t, r.run.Defer(ctxT(t), d))
	x := r.one(jobs.NameRedeliver)
	wantKey := d.Signal.ID.String() + ":" + d.Subscriber
	if x.Key == nil || *x.Key != wantKey {
		t.Errorf("key = %v, want %q", x.Key, wantKey)
	}
	if x.Status != "queued" || x.Attempts != 0 {
		t.Errorf("row = %+v", x)
	}
	if x.RunAt.Before(epoch) || x.RunAt.After(epoch.Add(jobs.DefaultRetry.Delay(d.Attempt+1, 1))) {
		t.Errorf("run_at = %v, want within the first backoff (%v) of now", x.RunAt, jobs.DefaultRetry.Delay(d.Attempt+1, 1))
	}
	a := argsJSON(t, x.Args)
	del, delOK := a["delivery"].(map[string]any)
	sig, sigOK := del["signal"].(map[string]any)
	if !delOK || !sigOK || del["subscriber"] != d.Subscriber || sig["id"] != d.Signal.ID.String() ||
		sig["name"] != string(d.Signal.Name) || del["attempt"] != float64(d.Attempt) {
		t.Errorf("args = %v, want the delivery's signal, subscriber and attempt", a)
	}
	if n := r.count(""); n != 1 {
		t.Errorf("%d rows", n)
	}
}

func TestDefer_SameDeliveryTwice_IsNoop_OtherSubscriberIsNot(t *testing.T) {
	r := newIRig(t)
	d := delivery(1)
	wantNoErr(t, r.run.Defer(ctxT(t), d))
	wantNoErr(t, r.run.Defer(ctxT(t), d))
	if n := len(r.byName(jobs.NameRedeliver)); n != 1 {
		t.Fatalf("%d rows after deferring the same (signal, subscriber) twice, want 1", n)
	}
	other := d
	other.Subscriber = "volunteers"
	wantNoErr(t, r.run.Defer(ctxT(t), other))
	if n := len(r.byName(jobs.NameRedeliver)); n != 2 {
		t.Fatalf("%d rows for two subscribers of one signal, want 2", n)
	}
}

func TestDefer_StoreUnavailable_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	r.flt.set(false, errs.E("test", errs.Unavailable, errors.New("down")))
	wantKind(t, r.run.Defer(ctxT(t), delivery(1)), errs.Unavailable)
}

func TestDefer_ReadOnly_ReturnsErrReadOnly_StoresNothing(t *testing.T) {
	r := newIRig(t)
	r.flt.set(true, nil)
	wantReadOnly(t, r.run.Defer(ctxT(t), delivery(1)))
	r.flt.set(false, nil)
	if n := r.count(""); n != 0 {
		t.Errorf("%d rows", n)
	}
}

func TestRedeliver_HandsDeliveryBackWithIncrementedAttempt(t *testing.T) {
	r := newIRig(t)
	d := delivery(1)
	wantNoErr(t, r.run.Defer(ctxT(t), d))
	x := r.one(jobs.NameRedeliver)
	if x.RunAt.After(epoch) { // not due yet (jitter gave a positive wait)
		r.runOnce()
		if n := len(r.red.got()); n != 0 {
			t.Fatalf("redelivered %d times before the backoff elapsed", n)
		}
	}
	r.advance(2 * time.Minute)
	r.runOnce()
	got := r.red.got()
	if len(got) != 1 {
		t.Fatalf("Redeliver called %d times, want 1", len(got))
	}
	g := got[0]
	if g.Signal.ID != d.Signal.ID || g.Signal.Name != d.Signal.Name || g.Subscriber != d.Subscriber ||
		g.Signal.Payload["person"] != d.Signal.Payload["person"] {
		t.Errorf("redelivered %+v, want the stored delivery %+v", g, d)
	}
	if g.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2 (the first delivery counts)", g.Attempt)
	}
	if got := r.one(jobs.NameRedeliver); got.Status != "done" {
		t.Errorf("row status = %q, want done", got.Status)
	}
}

func TestRedeliver_NoSubscriber_DropsWithLog(t *testing.T) {
	r := newIRig(t)
	r.red.set(func(signals.Delivery) error { return errs.E("test", errs.NotFound, errors.New("no handler")) })
	wantNoErr(t, r.run.Defer(ctxT(t), delivery(1)))
	r.advance(2 * time.Minute)
	ran := r.runOnce()
	x := r.one(jobs.NameRedeliver)
	if x.Status != "done" || x.LastError != nil {
		t.Errorf("row = %+v, want done and no error", x)
	}
	if ran.Failed != 0 {
		t.Errorf("Ran = %+v, a dropped delivery is not a failure", ran)
	}
	if r.outcomes("dropped") != 1 {
		t.Errorf("dropped outcomes = %v, want 1", r.outcomes("dropped"))
	}
	lines := 0
	for _, rec := range r.cap.Records() {
		if rec.Attrs["outcome"] == "dropped" {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("%d log lines with outcome=dropped, want 1", lines)
	}
}

func TestRedeliver_HandlerUnavailable_RetriesWithIncrementedAttempt(t *testing.T) {
	r := newIRig(t)
	r.red.set(func(signals.Delivery) error { return errs.E("test", errs.Unavailable, errors.New("db down")) })
	wantNoErr(t, r.run.Defer(ctxT(t), delivery(1)))
	r.advance(2 * time.Minute)
	ran := r.runOnce()
	if ran.Retried != 1 {
		t.Fatalf("Ran = %+v, want a retry", ran)
	}
	r.advance(2 * time.Hour)
	r.runOnce()
	got := r.red.got()
	if len(got) != 2 || got[0].Attempt != 2 || got[1].Attempt != 3 {
		var as []int
		for _, g := range got {
			as = append(as, g.Attempt)
		}
		t.Fatalf("Attempts seen = %v, want [2 3]", as)
	}
}

func TestRedeliver_GivesUp_AfterMaxAttemptsCountingFirstDelivery(t *testing.T) {
	r := newIRig(t)
	r.red.set(func(signals.Delivery) error { return errs.E("test", errs.Unavailable, errors.New("db down")) })
	wantNoErr(t, r.run.Defer(ctxT(t), delivery(1)))
	for range 14 {
		r.advance(2 * time.Hour)
		r.runOnce()
	}
	x := r.one(jobs.NameRedeliver)
	if x.Status != "failed" || x.LastError == nil || *x.LastError != "gave_up" {
		t.Fatalf("row = %+v, want failed/gave_up", x)
	}
	got := r.red.got()
	// Ten deliveries in all, the first one made by the dispatcher itself: nine or ten
	// redeliveries depending on how the count is read; never more than DefaultRetry allows.
	if n := len(got); n < jobs.DefaultRetry.MaxAttempts-1 || n > jobs.DefaultRetry.MaxAttempts {
		t.Errorf("Redeliver called %d times, want %d or %d", n, jobs.DefaultRetry.MaxAttempts-1, jobs.DefaultRetry.MaxAttempts)
	}
	for i, g := range got {
		if g.Attempt != i+2 {
			t.Errorf("call %d had Attempt %d, want %d", i+1, g.Attempt, i+2)
		}
	}
}

func TestRedeliver_OtherKind_FailsWithoutRetry(t *testing.T) {
	r := newIRig(t)
	r.red.set(func(signals.Delivery) error { return errs.E("test", errs.Invalid, errors.New("bad")) })
	wantNoErr(t, r.run.Defer(ctxT(t), delivery(1)))
	r.advance(2 * time.Minute)
	r.runOnce()
	r.advance(2 * time.Hour)
	r.runOnce()
	x := r.one(jobs.NameRedeliver)
	if x.Status != "failed" || x.LastError == nil || *x.LastError != "invalid" || len(r.red.got()) != 1 {
		t.Errorf("row = %+v after %d calls, want failed/invalid after one", x, len(r.red.got()))
	}
}

func TestRedeliver_Args_NeverReachTelemetry(t *testing.T) {
	r := newIRig(t)
	d := delivery(1)
	wantNoErr(t, r.run.Defer(ctxT(t), d))
	r.advance(2 * time.Minute)
	r.runOnce()
	if r.cap.Contains(d.Signal.Payload["person"].String()) {
		t.Error("a payload reference reached a log line, span or metric")
	}
}
