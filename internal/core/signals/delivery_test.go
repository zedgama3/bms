package signals_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// multi wires alpha (publisher of x) to the named subscribers, in order.
func multi(t *testing.T, f *fixture, subs map[string]signals.Handler, order []string) signals.Bus {
	t.Helper()
	a := f.scope("alpha", []signals.Name{nameX}, nil)
	for _, o := range order {
		b := f.scope(o, nil, []signals.Name{nameX})
		subscribe(t, b, nameX, subs[o])
	}
	return a
}

func fail(err error) signals.Handler {
	return func(context.Context, signals.Signal) error { return err }
}

func TestDeliver_HandlerFails_LogsCountsAndContinues(t *testing.T) {
	for _, k := range []errs.Kind{errs.Invalid, errs.Internal, errs.Permission, errs.NotFound, errs.Conflict} {
		t.Run(k.String(), func(t *testing.T) {
			f := newFixture(t)
			var l log
			a := multi(t, f, map[string]signals.Handler{
				"beta":  fail(errs.E("beta.handle", k, errBoom)),
				"gamma": l.mark("gamma"),
			}, []string{"beta", "gamma"})
			noErr(t, a.Publish(context.Background(), nameX, person())) // never reaches the publisher
			if got := l.all(); len(got) != 1 {
				t.Errorf("later subscriber did not run: %v", got)
			}
			o := outcomes(f.cap)
			if o["beta"] != "failed" || o["gamma"] != "ok" {
				t.Errorf("outcomes = %v", o)
			}
			errsLogged := recordsAt(f.cap, slog.LevelError)
			if len(errsLogged) != 1 {
				t.Fatalf("error records = %d, want exactly one (logged once)", len(errsLogged))
			}
			r := errsLogged[0]
			if r.Attrs["signal"] != string(nameX) || r.Attrs["subscriber"] != "beta" || r.Attrs["signal_id"] == "" {
				t.Errorf("record attrs = %v", r.Attrs)
			}
			if r.Attrs["error.kind"] != k.String() {
				t.Errorf("error.kind = %q, want %q", r.Attrs["error.kind"], k.String())
			}
			if _, ok := r.Attrs["attempt"]; !ok {
				t.Errorf("no attempt attribute: %v", r.Attrs)
			}
			failedSpans := 0
			for _, s := range spansNamed(f.cap, "deliver:alpha.x") {
				if s.Failed {
					failedSpans++
					if s.Attrs["module"] != "beta" || s.Attrs["outcome"] != "failed" {
						t.Errorf("failed span attrs = %v", s.Attrs)
					}
				}
			}
			if failedSpans != 1 {
				t.Errorf("failed spans = %d", failedSpans)
			}
		})
	}
}

func TestDeliver_RetryableWithoutDeferrer_IsFinal(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		t.Run(k.String(), func(t *testing.T) {
			f := newFixture(t)
			a := multi(t, f, map[string]signals.Handler{"beta": fail(errs.E("beta.h", k, errBoom))}, []string{"beta"})
			noErr(t, a.Publish(context.Background(), nameX, nil))
			if outcomes(f.cap)["beta"] != "failed" {
				t.Errorf("outcomes = %v", outcomes(f.cap))
			}
			if !hasMessage(recordsAt(f.cap, slog.LevelError), "no deferrer") {
				t.Errorf("no error record saying no deferrer: %v", f.cap.Records())
			}
		})
	}
}

func TestDeliver_RetryableWithDeferrer_Defers(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		t.Run(k.String(), func(t *testing.T) {
			d := &fakeDeferrer{}
			f := newFixture(t, func(o *signals.Options) { o.Deferrer = d })
			a := multi(t, f, map[string]signals.Handler{"beta": fail(errs.E("beta.h", k, errBoom))}, []string{"beta"})
			noErr(t, a.Publish(context.Background(), nameX, person()))
			got := d.deliveries()
			if len(got) != 1 {
				t.Fatalf("deferred %d deliveries", len(got))
			}
			if got[0].Subscriber != "beta" || got[0].Attempt != 1 || got[0].Signal.Name != nameX {
				t.Errorf("delivery = %+v", got[0])
			}
			noErr(t, got[0].Validate())
			if outcomes(f.cap)["beta"] != "deferred" {
				t.Errorf("outcomes = %v", outcomes(f.cap))
			}
			if len(recordsAt(f.cap, slog.LevelError)) != 0 {
				t.Errorf("deferral logged at error: %v", recordsAt(f.cap, slog.LevelError))
			}
			if len(recordsAt(f.cap, slog.LevelInfo)) == 0 {
				t.Error("deferral not logged at info")
			}
		})
	}
}

func TestDeliver_NonRetryableNeverDeferred(t *testing.T) {
	d := &fakeDeferrer{}
	f := newFixture(t, func(o *signals.Options) { o.Deferrer = d })
	a := multi(t, f, map[string]signals.Handler{"beta": fail(errs.E("beta.h", errs.Internal, errBoom))}, []string{"beta"})
	noErr(t, a.Publish(context.Background(), nameX, nil))
	if len(d.deliveries()) != 0 {
		t.Error("non-retryable failure was deferred")
	}
}

func TestDeliver_DeferFails_LogsBothAndIsFinal(t *testing.T) {
	d := &fakeDeferrer{err: errs.E("jobs.Defer", errs.Unavailable, errDeferMarker)}
	f := newFixture(t, func(o *signals.Options) { o.Deferrer = d })
	a := multi(t, f, map[string]signals.Handler{
		"beta": fail(errs.E("beta.h", errs.Unavailable, errHandlerMarker)),
	}, []string{"beta"})
	noErr(t, a.Publish(context.Background(), nameX, nil))
	if outcomes(f.cap)["beta"] != "failed" {
		t.Errorf("outcomes = %v", outcomes(f.cap))
	}
	if len(recordsAt(f.cap, slog.LevelError)) == 0 {
		t.Error("no error record")
	}
	if !f.cap.Contains("handlermarker") || !f.cap.Contains("defermarker") {
		t.Error("log does not carry both the handler's and Defer's error")
	}
}

type markerError string

func (e markerError) Error() string { return string(e) }

const (
	errHandlerMarker = markerError("handlermarker")
	errDeferMarker   = markerError("defermarker")
)

func TestDeliver_ReadOnly_WarnsNotDeferred(t *testing.T) {
	d := &fakeDeferrer{}
	f := newFixture(t, func(o *signals.Options) { o.Deferrer = d })
	a := multi(t, f, map[string]signals.Handler{
		"beta": fail(errs.E("beta.h", errs.Unavailable, db.ErrReadOnly)),
	}, []string{"beta"})
	noErr(t, a.Publish(context.Background(), nameX, nil))
	if outcomes(f.cap)["beta"] != "read_only" {
		t.Errorf("outcomes = %v", outcomes(f.cap))
	}
	if len(d.deliveries()) != 0 {
		t.Error("read-only failure was deferred")
	}
	if len(recordsAt(f.cap, slog.LevelWarn)) == 0 {
		t.Error("no warn record")
	}
	if len(recordsAt(f.cap, slog.LevelError)) != 0 {
		t.Errorf("read-only logged at error: %v", recordsAt(f.cap, slog.LevelError))
	}
}

func TestDeliver_HandlerOverruns_WarnsWithDuration(t *testing.T) {
	const timeout = 300 * time.Millisecond
	f := newFixture(t, func(o *signals.Options) { o.HandlerTimeout = timeout })
	a := multi(t, f, map[string]signals.Handler{
		"beta": func(ctx context.Context, _ signals.Signal) error {
			dl, ok := ctx.Deadline()
			if !ok {
				t.Error("handler ctx has no deadline")
			} else if rem := time.Until(dl); rem > timeout || rem <= 0 {
				t.Errorf("remaining = %v, want within (0, %v]", rem, timeout)
			}
			return f.clk.Advance(timeout + time.Second)
		},
	}, []string{"beta"})
	noErr(t, a.Publish(context.Background(), nameX, nil))
	var found *telemetry.LogRecord
	for _, r := range recordsAt(f.cap, slog.LevelWarn) {
		if r.Attrs["duration_ms"] != "" && strings.Contains(r.Message, "overran") {
			found = &r
		}
	}
	if found == nil {
		t.Fatalf("no overran warn record with duration_ms: %v", f.cap.Records())
	}
	if found.Attrs["duration_ms"] != "1300" {
		t.Errorf("duration_ms = %q, want 1300", found.Attrs["duration_ms"])
	}
}

func TestDeliver_HandlerPanics_RecoveredLoggedAndContinues(t *testing.T) {
	f := newFixture(t)
	var l log
	a := multi(t, f, map[string]signals.Handler{
		"beta":  func(context.Context, signals.Signal) error { panic("kaboom") },
		"gamma": l.mark("gamma"),
	}, []string{"beta", "gamma"})
	noErr(t, a.Publish(context.Background(), nameX, nil))
	if len(l.all()) != 1 {
		t.Error("later subscriber did not run after a panic")
	}
	o := outcomes(f.cap)
	if o["beta"] != "panicked" || o["gamma"] != "ok" {
		t.Errorf("outcomes = %v", o)
	}
	rs := recordsAt(f.cap, slog.LevelError)
	if len(rs) == 0 {
		t.Fatal("no error record")
	}
	hasStack := false
	for _, r := range rs {
		if r.Attrs["stack"] != "" {
			hasStack = true
		}
	}
	if !hasStack {
		t.Errorf("no stack attribute: %v", rs)
	}
}

func TestDeliver_CtxHasNoRequestCancellation(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var l log
	var errAfter error
	a := multi(t, f, map[string]signals.Handler{
		"beta": func(context.Context, signals.Signal) error { cancel(); return nil },
		"gamma": func(hctx context.Context, _ signals.Signal) error {
			errAfter = hctx.Err()
			l.add("gamma")
			return nil
		},
	}, []string{"beta", "gamma"})
	noErr(t, a.Publish(ctx, nameX, nil))
	if len(l.all()) != 1 || errAfter != nil {
		t.Errorf("gamma ran=%v ctx.Err=%v", l.all(), errAfter)
	}
	if outcomes(f.cap)["gamma"] != "ok" {
		t.Errorf("outcomes = %v", outcomes(f.cap))
	}
}

func TestDeliver_CtxHasDeadlineAndKeepsValues(t *testing.T) {
	type key struct{}
	f := newFixture(t)
	var hasDL, hasVal bool
	a := multi(t, f, map[string]signals.Handler{
		"beta": func(ctx context.Context, _ signals.Signal) error {
			_, hasDL = ctx.Deadline()
			hasVal = ctx.Value(key{}) == "v"
			return nil
		},
	}, []string{"beta"})
	noErr(t, a.Publish(context.WithValue(context.Background(), key{}, "v"), nameX, nil))
	if !hasDL || !hasVal {
		t.Errorf("deadline=%v values kept=%v", hasDL, hasVal)
	}
}

func TestDeliver_CtxCarriesNoTransaction(t *testing.T) {
	f := newFixture(t)
	a := multi(t, f, map[string]signals.Handler{
		"beta": func(ctx context.Context, _ signals.Signal) error {
			_, err := f.fdb.Exec(ctx, "SELECT 'in-handler'")
			return err //nolint:wrapcheck // test
		},
	}, []string{"beta"})
	err := f.tx(context.Background(), func(ctx context.Context) error {
		_, err := f.fdb.Exec(ctx, "SELECT 'in-publisher'")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		return a.Publish(ctx, nameX, nil)
	})
	noErr(t, err)
	seen := map[string]bool{}
	for _, c := range f.fdb.Calls() {
		seen[c.SQL] = c.InTx
	}
	if inTx, ok := seen["SELECT 'in-handler'"]; !ok || inTx {
		t.Errorf("handler call recorded=%v InTx=%v, want recorded with InTx=false", ok, inTx)
	}
	if !seen["SELECT 'in-publisher'"] {
		t.Error("publisher call should be InTx")
	}
}

func TestDeliver_InDeliveryTrueInsideFalseOutside(t *testing.T) {
	f := newFixture(t)
	if signals.InDelivery(context.Background()) {
		t.Error("InDelivery true outside a handler")
	}
	var inside, nested bool
	a := multi(t, f, map[string]signals.Handler{
		"beta": func(ctx context.Context, _ signals.Signal) error {
			inside = signals.InDelivery(ctx)
			return f.tx(ctx, func(ctx context.Context) error {
				nested = signals.InDelivery(ctx)
				return nil
			})
		},
	}, []string{"beta"})
	ctx := context.Background()
	noErr(t, f.tx(ctx, func(ctx context.Context) error {
		if signals.InDelivery(ctx) {
			t.Error("InDelivery true in the publisher")
		}
		return a.Publish(ctx, nameX, nil)
	}))
	if !inside || !nested {
		t.Errorf("inside=%v nested=%v", inside, nested)
	}
}

func TestDeliver_SpansAndCountersPerOutcome(t *testing.T) {
	d := &fakeDeferrer{}
	f := newFixture(t, func(o *signals.Options) { o.Deferrer = d })
	a := multi(t, f, map[string]signals.Handler{
		"beta":    func(context.Context, signals.Signal) error { return nil },
		"gamma":   fail(errs.E("g", errs.Internal, errBoom)),
		"delta":   fail(errs.E("d", errs.Unavailable, errBoom)),
		"epsilon": fail(errs.E("e", errs.Unavailable, db.ErrReadOnly)),
		"zeta":    func(context.Context, signals.Signal) error { panic("x") },
	}, []string{"beta", "gamma", "delta", "epsilon", "zeta"})

	rctx, rspan := f.cap.Tracer("test").Start(context.Background(), "request")
	err := f.tx(rctx, func(ctx context.Context) error { return a.Publish(ctx, nameX, person()) })
	rspan.End()
	noErr(t, err)

	want := map[string]string{"beta": "ok", "gamma": "failed", "delta": "deferred", "epsilon": "read_only", "zeta": "panicked"}
	got := outcomes(f.cap)
	for m, o := range want {
		if got[m] != o {
			t.Errorf("outcome[%s] = %q, want %q", m, got[m], o)
		}
	}
	for _, m := range delivered(f.cap) {
		if m.Attrs["signal"] != string(nameX) || m.Value != 1 {
			t.Errorf("delivered measurement = %+v", m)
		}
		if len(m.Attrs) != 3 {
			t.Errorf("delivered attrs = %v, want signal, module, outcome only", m.Attrs)
		}
	}
	for _, m := range measures(f.cap, "bms.signals.published") {
		if len(m.Attrs) != 2 {
			t.Errorf("published attrs = %v", m.Attrs)
		}
	}

	var request telemetry.SpanData
	for _, s := range f.cap.Spans() {
		if s.Name == "request" {
			request = s
		}
	}
	prod := spansNamed(f.cap, "signal:alpha.x")
	cons := spansNamed(f.cap, "deliver:alpha.x")
	if len(prod) != 1 || prod[0].Kind != telemetry.SpanProducer {
		t.Fatalf("producer spans = %+v", prod)
	}
	if len(cons) != len(want) {
		t.Fatalf("consumer spans = %d, want %d", len(cons), len(want))
	}
	for _, s := range cons {
		if s.Kind != telemetry.SpanConsumer {
			t.Errorf("kind = %v", s.Kind)
		}
		if s.TraceID != request.TraceID {
			t.Errorf("consumer span in another trace")
		}
		if s.ParentID == "" || s.ParentID != prod[0].ParentID {
			t.Errorf("consumer parent %q, want the request span %q", s.ParentID, prod[0].ParentID)
		}
		if want[s.Attrs["module"]] != s.Attrs["outcome"] {
			t.Errorf("span attrs = %v", s.Attrs)
		}
		if s.Attrs["signal"] != string(nameX) || s.Attrs["signal_id"] == "" || s.Attrs["attempt"] != "1" {
			t.Errorf("span attrs = %v", s.Attrs)
		}
		if _, ok := s.Attrs["duration_ms"]; !ok {
			t.Errorf("no duration_ms: %v", s.Attrs)
		}
	}
}

func TestDeliver_NeverLogsPayloadOrSpansIt(t *testing.T) {
	f := newFixture(t)
	a := multi(t, f, map[string]signals.Handler{
		"beta":  fail(errs.E("b", errs.Internal, errBoom)),
		"gamma": func(context.Context, signals.Signal) error { panic("x") },
		"delta": fail(errs.E("d", errs.Unavailable, errBoom)),
	}, []string{"beta", "gamma", "delta"})
	id := model.NewID[model.PersonID]()
	noErr(t, a.Publish(context.Background(), nameX, signals.NewPayload(signals.Ref("person", id))))
	if f.cap.Contains(id.String()) {
		t.Error("the payload's ID reached a log, span or metric")
	}
}
