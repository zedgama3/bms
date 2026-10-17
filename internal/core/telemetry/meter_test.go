package telemetry_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func (h *harness) measurement(t *testing.T, instrument string) telemetry.Measurement {
	t.Helper()
	for _, m := range h.obs.measurements() {
		if m.Instrument == instrument {
			return m
		}
	}
	t.Fatalf("no measurement for %q among %+v", instrument, h.obs.measurements())
	return telemetry.Measurement{}
}

func TestCounterHistogramGauge_RoundTrip(t *testing.T) {
	h := newHarness(t, nil)
	m := h.meter(t)
	ctx := context.Background()

	c, err := m.Counter("bms.test.count", telemetry.WithUnit("{thing}"), telemetry.WithDescription("things counted"))
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(ctx, 3, telemetry.Token("outcome", "ok"))
	got := h.measurement(t, "bms.test.count")
	if got.Value != 3 || got.Scope != "module.test" || got.Attrs["outcome"] != "ok" {
		t.Errorf("counter = %+v", got)
	}
	c.Add(ctx, 0)

	hist, err := m.Histogram("bms.test.duration", telemetry.WithUnit("s"), telemetry.WithBuckets(0.1, 1, 10))
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	hist.Record(ctx, 0.25, telemetry.Token("http.route", "/api/x"))
	got = h.measurement(t, "bms.test.duration")
	if got.Value != 0.25 || got.Attrs["http.route"] != "/api/x" {
		t.Errorf("histogram = %+v", got)
	}

	if err := m.Gauge("bms.test.level", func(context.Context) []telemetry.Sample {
		return []telemetry.Sample{{Value: 2, Attrs: []telemetry.Attr{telemetry.Token("state", "idle")}}}
	}, telemetry.WithUnit("{connection}")); err != nil {
		t.Errorf("Gauge: %v", err)
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}

func TestMetricAttrs_TokenBoolIntKindAllowed(t *testing.T) {
	h := newHarness(t, nil)
	c, err := h.meter(t).Counter("bms.test.attrs")
	if err != nil {
		t.Fatal(err)
	}
	c.Add(context.Background(), 1,
		telemetry.Token("route", "/x"), telemetry.Bool("ok", true), telemetry.Int("n", 4),
		telemetry.Int64("big", 5), telemetry.Kind(errs.Conflict))
	got := h.measurement(t, "bms.test.attrs")
	want := map[string]string{"route": "/x", "ok": "true", "n": "4", "big": "5", "error.kind": errs.Conflict.String()}
	for k, v := range want {
		if got.Attrs[k] != v {
			t.Errorf("%s = %q, want %q", k, got.Attrs[k], v)
		}
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}

func TestMetric_TextOrIDAttr_DroppedAndCounted(t *testing.T) {
	h := newHarness(t, nil)
	m := h.meter(t)
	c, err := m.Counter("bms.test.c")
	if err != nil {
		t.Fatal(err)
	}
	hist, err := m.Histogram("bms.test.h")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c.Add(ctx, 1, telemetry.Text("msg", marker), telemetry.Token("route", "/x"))
	hist.Record(ctx, 1, telemetry.ID("person_id", model.NewID[model.PersonID]()))

	cm := h.measurement(t, "bms.test.c")
	if _, ok := cm.Attrs["msg"]; ok || cm.Attrs["route"] != "/x" {
		t.Errorf("counter attrs = %v", cm.Attrs)
	}
	hm := h.measurement(t, "bms.test.h")
	if _, ok := hm.Attrs["person_id"]; ok {
		t.Errorf("histogram attrs = %v", hm.Attrs)
	}
	if !h.obs.hasViolation("text_on_metric", "") || !h.obs.hasViolation("id_on_metric", "") {
		t.Errorf("violations = %+v", h.obs.violations())
	}
	for _, v := range h.obs.violations() {
		if v.Signal != "metric" {
			t.Errorf("signal = %q", v.Signal)
		}
	}
}

func TestMeter_BadNameUnitOrBuckets_ReturnsInvalid(t *testing.T) {
	h := newHarness(t, nil)
	m := h.meter(t)
	for _, name := range []string{"", "has space", "a@b", repeat('n', 129)} {
		if _, err := m.Counter(name); !errs.Is(err, errs.Invalid) {
			t.Errorf("Counter(%q): %v", name, err)
		}
		if _, err := m.Histogram(name); !errs.Is(err, errs.Invalid) {
			t.Errorf("Histogram(%q): %v", name, err)
		}
		if err := m.Gauge(name, func(context.Context) []telemetry.Sample { return nil }); !errs.Is(err, errs.Invalid) {
			t.Errorf("Gauge(%q): %v", name, err)
		}
	}
	if _, err := m.Counter("bms.test.unit", telemetry.WithUnit("bad unit")); !errs.Is(err, errs.Invalid) {
		t.Errorf("bad unit: %v", err)
	}
	for _, b := range [][]float64{{2, 1}, {1, 1}, {1, 3, 2}} {
		if _, err := m.Histogram("bms.test.buckets", telemetry.WithBuckets(b...)); !errs.Is(err, errs.Invalid) {
			t.Errorf("buckets %v: %v", b, err)
		}
	}
	if _, err := m.Counter(strings.Repeat("a", 128)); err != nil {
		t.Errorf("128-byte name should be valid: %v", err)
	}
}

func TestMeter_DuplicateNameDifferentKind_ReturnsConflict(t *testing.T) {
	h := newHarness(t, nil)
	m := h.meter(t)
	if _, err := m.Counter("bms.test.dup", telemetry.WithUnit("s")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Counter("bms.test.dup", telemetry.WithUnit("s")); err != nil {
		t.Errorf("same kind and unit twice must succeed: %v", err)
	}
	if _, err := m.Histogram("bms.test.dup", telemetry.WithUnit("s")); !errs.Is(err, errs.Conflict) {
		t.Errorf("other kind: %v", err)
	}
	if _, err := m.Counter("bms.test.dup", telemetry.WithUnit("ms")); !errs.Is(err, errs.Conflict) {
		t.Errorf("other unit: %v", err)
	}
	err := m.Gauge("bms.test.dup", func(context.Context) []telemetry.Sample { return nil }, telemetry.WithUnit("s"))
	if !errs.Is(err, errs.Conflict) {
		t.Errorf("gauge over counter: %v", err)
	}
	other, err := h.tel.Meter("module.other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Histogram("bms.test.dup", telemetry.WithUnit("s")); err != nil {
		t.Errorf("the same name in another scope is allowed: %v", err)
	}
}

func TestGauge_NilObserve_ReturnsInvalid(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.meter(t).Gauge("bms.test.nilobs", nil); !errs.Is(err, errs.Invalid) {
		t.Errorf("err = %v", err)
	}
}

func TestCounterAdd_Negative_DroppedAndCounted(t *testing.T) {
	h := newHarness(t, nil)
	c, err := h.meter(t).Counter("bms.test.neg")
	if err != nil {
		t.Fatal(err)
	}
	c.Add(context.Background(), -1)
	for _, m := range h.obs.measurements() {
		if m.Instrument == "bms.test.neg" {
			t.Errorf("negative add was recorded: %+v", m)
		}
	}
	found := false
	for _, v := range h.obs.violations() {
		if v.Signal == "metric" && v.Rule == "negative_count" {
			found = true
		}
	}
	if !found {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}

func TestMeter_SafeForConcurrentUse(t *testing.T) {
	h := newHarness(t, nil)
	m := h.meter(t)
	c, err := m.Counter("bms.test.conc")
	if err != nil {
		t.Fatal(err)
	}
	hist, err := m.Histogram("bms.test.conc.h")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Add(context.Background(), 1, telemetry.Token("k", "v"))
			hist.Record(context.Background(), 0.5)
		}()
	}
	wg.Wait()
	n := 0
	for _, x := range h.obs.measurements() {
		if x.Instrument == "bms.test.conc" {
			n++
		}
	}
	if n != 32 {
		t.Errorf("measurements = %d", n)
	}
}

func TestGauge_ObservePanics_RecoveredAndLoggedOnce(t *testing.T) {
	h := newHarness(t, nil)
	m := h.meter(t)
	ctx := context.Background()
	if err := m.Gauge("bms.test.good", func(context.Context) []telemetry.Sample {
		return []telemetry.Sample{{Value: 7, Attrs: []telemetry.Attr{telemetry.Token("state", "idle")}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Gauge("bms.test.panics", func(context.Context) []telemetry.Sample {
		panic("observer bug " + marker)
	}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := h.tel.Collect(ctx); err != nil {
			t.Fatalf("Collect must survive a panicking observer: %v", err)
		}
	}
	good := h.measurement(t, "bms.test.good")
	if good.Value != 7 || good.Attrs["state"] != "idle" {
		t.Errorf("good gauge = %+v", good)
	}
	for _, x := range h.obs.measurements() {
		if x.Instrument == "bms.test.panics" {
			t.Errorf("panicking gauge produced %+v", x)
		}
	}
	errorsLogged := 0
	for _, r := range h.obs.records() {
		if r.Level == slog.LevelError {
			errorsLogged++
		}
	}
	if errorsLogged != 1 {
		t.Errorf("error records after 3 collections = %d, want 1", errorsLogged)
	}
}
