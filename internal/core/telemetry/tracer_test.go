package telemetry_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestTracerMeter_BadScopeName_ReturnsInvalid(t *testing.T) {
	h := newHarness(t, nil)
	for _, name := range []string{"", "has space", "a@b", ".dot", repeat('s', 65)} {
		if _, err := h.tel.Tracer(name); !errs.Is(err, errs.Invalid) {
			t.Errorf("Tracer(%q): %v", name, err)
		}
		if _, err := h.tel.Meter(name); !errs.Is(err, errs.Invalid) {
			t.Errorf("Meter(%q): %v", name, err)
		}
	}
	for _, name := range []string{"api", "db", "module.hello", repeat('s', 64)} {
		if _, err := h.tel.Tracer(name); err != nil {
			t.Errorf("Tracer(%q): %v", name, err)
		}
		if _, err := h.tel.Meter(name); err != nil {
			t.Errorf("Meter(%q): %v", name, err)
		}
	}
}

func TestTracerMeter_AfterClose_ReturnsInternal(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.tel.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.tel.Tracer("api"); !errs.Is(err, errs.Internal) {
		t.Errorf("Tracer after Close: %v", err)
	}
	if _, err := h.tel.Meter("api"); !errs.Is(err, errs.Internal) {
		t.Errorf("Meter after Close: %v", err)
	}
}

func TestStart_RootWhenNoSpan(t *testing.T) {
	h := newHarness(t, nil)
	tr := h.tracer(t)
	_, a := tr.Start(context.Background(), "root.a")
	_, b := tr.Start(context.Background(), "root.b")
	a.End()
	b.End()
	sa, sb := h.span(t, "root.a"), h.span(t, "root.b")
	if sa.ParentID != "" || sb.ParentID != "" {
		t.Errorf("roots have parents: %q %q", sa.ParentID, sb.ParentID)
	}
	if len(sa.TraceID) != 32 || sa.TraceID == sb.TraceID {
		t.Errorf("trace IDs: %q %q", sa.TraceID, sb.TraceID)
	}
	if sa.Scope != "module.test" {
		t.Errorf("scope = %q", sa.Scope)
	}
}

func TestStart_ChildOfContextSpan(t *testing.T) {
	h := newHarness(t, nil)
	tr := h.tracer(t)
	ctx, parent := tr.Start(context.Background(), "parent")
	_, child := tr.Start(ctx, "child")
	child.End()
	parent.End()
	p, c := h.span(t, "parent"), h.span(t, "child")
	if c.TraceID != p.TraceID || c.ParentID == "" || p.ParentID != "" {
		t.Errorf("parent=%+v child=%+v", p, c)
	}
}

func TestStart_BadName_UsesPlaceholderAndCountsViolation(t *testing.T) {
	bad := []string{
		"", "get /x", "GET  /x", "signal hello.greeted", "GET x", "a b", "GET /x y",
		"GE /x", "ABCDEFGHIJK /x", "_OTHER /x", "name@example", repeat('n', 129),
		"GET /" + repeat('r', 128),
	}
	for _, name := range bad {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			_, sp := h.tracer(t).Start(context.Background(), name)
			sp.End()
			spans := h.obs.spans()
			if len(spans) != 1 || spans[0].Name != "invalid-span-name" {
				t.Fatalf("spans = %+v", spans)
			}
			found := false
			for _, v := range h.obs.violations() {
				if v.Signal == "trace" && v.Rule == "invalid_span_name" {
					found = true
				}
			}
			if !found {
				t.Errorf("violations = %+v", h.obs.violations())
			}
		})
	}
}

func TestStart_HTTPSpanName_Accepted(t *testing.T) {
	good := []string{
		"GET /api/people/{id}", "POST /api/client-errors", "HTTP /api/x", "DELETE /",
		"db.SELECT", "signal:hello.greeted", "deliver:hello.greeted", "_OTHER", repeat('n', 128),
	}
	for _, name := range good {
		h := newHarness(t, nil)
		_, sp := h.tracer(t).Start(context.Background(), name)
		sp.End()
		spans := h.obs.spans()
		if len(spans) != 1 || spans[0].Name != name {
			t.Errorf("%q: spans = %+v", name, spans)
		}
		if len(h.obs.violations()) != 0 {
			t.Errorf("%q: violations = %+v", name, h.obs.violations())
		}
	}
}

func TestSpan_KindsAndStartAttrs(t *testing.T) {
	kinds := []telemetry.SpanKind{
		telemetry.SpanInternal, telemetry.SpanServer, telemetry.SpanClient,
		telemetry.SpanProducer, telemetry.SpanConsumer,
	}
	for _, k := range kinds {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			h := newHarness(t, nil)
			_, sp := h.tracer(t).Start(context.Background(), "kinded",
				telemetry.WithKind(k),
				telemetry.WithAttrs(telemetry.Token("http.route", "/x"), telemetry.Int("n", 3)))
			sp.SetAttrs(telemetry.Bool("ok", true))
			sp.End()
			sd := h.span(t, "kinded")
			if sd.Kind != k {
				t.Errorf("kind = %v", sd.Kind)
			}
			if sd.Attrs["http.route"] != "/x" || sd.Attrs["n"] != "3" || sd.Attrs["ok"] != "true" {
				t.Errorf("attrs = %v", sd.Attrs)
			}
		})
	}
	h := newHarness(t, nil)
	_, sp := h.tracer(t).Start(context.Background(), "default.kind")
	sp.End()
	if h.span(t, "default.kind").Kind != telemetry.SpanInternal {
		t.Error("default kind must be Internal")
	}
}

func TestSpan_RecordError_KindOpMessage(t *testing.T) {
	h := newHarness(t, nil)
	tr := h.tracer(t)
	run := func(name string, err error) telemetry.SpanData {
		_, sp := tr.Start(context.Background(), name)
		sp.RecordError(err)
		sp.End()
		return h.span(t, name)
	}

	inner := errs.E("people.Get", errs.NotFound, errors.New("no row for "+markerEmail))
	wrapped := fmt.Errorf("handler: %w", inner)
	sd := run("failed.one", wrapped)
	if !sd.Failed || sd.ErrorKind != errs.NotFound.String() {
		t.Errorf("failed=%v kind=%q", sd.Failed, sd.ErrorKind)
	}
	if sd.Attrs["error.kind"] != errs.NotFound.String() || sd.Attrs["error.op"] != "people.Get" {
		t.Errorf("attrs = %v", sd.Attrs)
	}
	if m := sd.Attrs["exception.message"]; !strings.Contains(m, "[email]") || strings.Contains(m, "zq9@") {
		t.Errorf("message not scrubbed: %q", m)
	}

	sd = run("failed.long", errors.New(repeat('x', 2000)))
	if m := sd.Attrs["exception.message"]; len(m) > telemetry.MaxTextBytes {
		t.Errorf("message not capped: %d", len(m))
	}
	if !sd.Failed || sd.ErrorKind != errs.Internal.String() {
		t.Errorf("unclassified error: failed=%v kind=%q", sd.Failed, sd.ErrorKind)
	}

	sd = run("canceled", errs.E("x.Do", errs.Canceled, context.Canceled))
	if sd.Failed {
		t.Error("Canceled must not mark the span failed")
	}

	sd = run("nil.error", nil)
	if sd.Failed || sd.Attrs["error.kind"] != "" {
		t.Errorf("nil error recorded: %+v", sd)
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}

func TestWithParentFrom_Table(t *testing.T) {
	const (
		tid = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
		pid = "1122334455667788"
	)
	cases := []struct {
		name   string
		header map[string]string
		joined bool
	}{
		{"valid sampled", map[string]string{"Traceparent": "00-" + tid + "-" + pid + "-01"}, true},
		{"valid flags cleared", map[string]string{"Traceparent": "00-" + tid + "-" + pid + "-00"}, true},
		{"valid with tracestate", map[string]string{"Traceparent": "00-" + tid + "-" + pid + "-01", "Tracestate": "vendor=opaque"}, true},
		{"missing", nil, false},
		{"bad version", map[string]string{"Traceparent": "ff-" + tid + "-" + pid + "-01"}, false},
		{"short trace id", map[string]string{"Traceparent": "00-" + tid[:30] + "-" + pid + "-01"}, false},
		{"short parent id", map[string]string{"Traceparent": "00-" + tid + "-" + pid[:14] + "-01"}, false},
		{"non hex", map[string]string{"Traceparent": "00-" + strings.Repeat("g", 32) + "-" + pid + "-01"}, false},
		{"upper case hex", map[string]string{"Traceparent": "00-" + strings.ToUpper(tid) + "-" + pid + "-01"}, false},
		{"zero trace id", map[string]string{"Traceparent": "00-" + strings.Repeat("0", 32) + "-" + pid + "-01"}, false},
		{"zero parent id", map[string]string{"Traceparent": "00-" + tid + "-" + strings.Repeat("0", 16) + "-01"}, false},
		{"garbage", map[string]string{"Traceparent": "not-a-traceparent"}, false},
		{"empty", map[string]string{"Traceparent": ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			hdr := http.Header{}
			for k, v := range tc.header {
				hdr.Set(k, v)
			}
			ctx, sp := h.tracer(t).Start(context.Background(), "GET /api/x", telemetry.WithParentFrom(hdr))
			sp.End()
			sd := h.span(t, "GET /api/x")
			if tc.joined {
				if sd.TraceID != tid || sd.ParentID != pid {
					t.Errorf("trace=%q parent=%q", sd.TraceID, sd.ParentID)
				}
				if got, ok := telemetry.TraceIDFrom(ctx); !ok || got != tid {
					t.Errorf("TraceIDFrom = %q %v", got, ok)
				}
			} else if sd.TraceID == tid || sd.ParentID != "" || len(sd.TraceID) != 32 {
				t.Errorf("expected a fresh root, got trace=%q parent=%q", sd.TraceID, sd.ParentID)
			}
			if len(h.obs.violations()) != 0 {
				t.Errorf("a bad traceparent must not be counted: %+v", h.obs.violations())
			}
		})
	}
}

func TestSpan_AfterEnd_NoOp(t *testing.T) {
	h := newHarness(t, nil)
	_, sp := h.tracer(t).Start(context.Background(), "ended")
	sp.SetAttrs(telemetry.Int("before", 1))
	sp.End()
	sp.End()
	sp.SetAttrs(telemetry.Int("after", 2))
	sp.RecordError(errors.New("late"))
	sp.End()
	spans := h.obs.spans()
	if len(spans) != 1 {
		t.Fatalf("End delivered %d times", len(spans))
	}
	if spans[0].Failed || spans[0].Attrs["after"] != "" || spans[0].Attrs["before"] != "1" {
		t.Errorf("span = %+v", spans[0])
	}
}

func TestSpan_SafeForConcurrentUse(t *testing.T) {
	h := newHarness(t, nil)
	tr := h.tracer(t)
	ctx, root := tr.Start(context.Background(), "root")
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, sp := tr.Start(ctx, "worker")
			sp.SetAttrs(telemetry.Int("i", i))
			sp.RecordError(errs.E("w.Do", errs.Unavailable, errors.New("x")))
			h.tel.Logger().InfoContext(c, "tick", slog.Int("i", i))
			sp.End()
		}()
	}
	wg.Wait()
	root.End()
	if n := len(h.obs.spans()); n != 33 {
		t.Errorf("spans = %d", n)
	}
}
