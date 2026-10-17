package telemetrytest_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
)

const marker = "ZQ9MARKER7"

// wrapTB records failures instead of failing the real test, and runs cleanups on demand.
type wrapTB struct {
	testing.TB
	mu       sync.Mutex
	failed   bool
	cleanups []func()
}

func (w *wrapTB) Cleanup(f func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cleanups = append(w.cleanups, f)
}

func (w *wrapTB) mark() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failed = true
}

func (w *wrapTB) Error(...any)          { w.mark() }
func (w *wrapTB) Errorf(string, ...any) { w.mark() }
func (w *wrapTB) Fatal(...any)          { w.mark() }
func (w *wrapTB) Fatalf(string, ...any) { w.mark() }
func (w *wrapTB) Fail()                 { w.mark() }
func (w *wrapTB) FailNow()              { w.mark() }
func (w *wrapTB) Failed() bool          { w.mu.Lock(); defer w.mu.Unlock(); return w.failed }

func (w *wrapTB) runCleanups() {
	w.mu.Lock()
	fs := w.cleanups
	w.cleanups = nil
	w.mu.Unlock()
	for i := len(fs) - 1; i >= 0; i-- {
		fs[i]()
	}
}

func TestCapture_FailsOnViolationAtCleanup(t *testing.T) {
	ctx := context.Background()

	bad := &wrapTB{TB: t}
	c := telemetrytest.New(bad)
	c.Logger().InfoContext(ctx, "leaky", slog.String("email", marker))
	if len(c.Violations()) == 0 {
		t.Fatal("denied key produced no violation")
	}
	if bad.Failed() {
		t.Error("the test must fail at cleanup, not earlier")
	}
	bad.runCleanups()
	if !bad.Failed() {
		t.Error("a violation did not fail the test at cleanup")
	}

	good := &wrapTB{TB: t}
	c2 := telemetrytest.New(good)
	c2.Logger().InfoContext(ctx, "fine", slog.String("module", "hello"))
	good.runCleanups()
	if good.Failed() {
		t.Error("a clean run failed the test")
	}
}

func TestCapture_Accessors(t *testing.T) {
	c := telemetrytest.New(t)
	if c.Telemetry() == nil || c.Telemetry().Exporting() {
		t.Fatal("Telemetry must be non-nil and not exporting")
	}
	ctx, sp := c.Tracer("module.test").Start(context.Background(), "op")
	c.Logger().DebugContext(ctx, "debug is captured", slog.Int("n", 1))
	cnt, err := c.Meter("module.test").Counter("bms.test.n")
	if err != nil {
		t.Fatal(err)
	}
	cnt.Add(ctx, 2)
	sp.End()

	recs := c.Records()
	if len(recs) != 1 || recs[0].Level != slog.LevelDebug {
		t.Errorf("records = %+v", recs)
	}
	if spans := c.Spans(); len(spans) != 1 || spans[0].Name != "op" {
		t.Errorf("spans = %+v", spans)
	}
	if ms := c.Measurements(); len(ms) != 1 || ms[0].Value != 2 {
		t.Errorf("measurements = %+v", ms)
	}
	if len(c.Violations()) != 0 {
		t.Errorf("violations = %+v", c.Violations())
	}
	out := c.Stdout()
	if strings.HasPrefix(strings.TrimSpace(out), "{") || !strings.Contains(out, "debug is captured") {
		t.Errorf("stdout should be text format: %q", out)
	}

	// Accessors return copies.
	recs[0].Message = "mutated"
	if c.Records()[0].Message == "mutated" {
		t.Error("Records returned the internal slice")
	}
}

func TestCapture_ContainsSearchesEverything(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(c *telemetrytest.Capture){
		"log message":    func(c *telemetrytest.Capture) { c.Logger().InfoContext(ctx, "hello "+marker) },
		"log attr value": func(c *telemetrytest.Capture) { c.Logger().InfoContext(ctx, "m", slog.String("module", marker)) },
		"span name": func(c *telemetrytest.Capture) {
			_, sp := c.Tracer("module.test").Start(ctx, marker)
			sp.End()
		},
		"span attr": func(c *telemetrytest.Capture) {
			_, sp := c.Tracer("module.test").Start(ctx, "op")
			sp.SetAttrs(telemetry.Token("route", marker))
			sp.End()
		},
		"metric attr": func(c *telemetrytest.Capture) {
			cnt, err := c.Meter("module.test").Counter("bms.test.c")
			if err != nil {
				t.Fatal(err)
			}
			cnt.Add(ctx, 1, telemetry.Token("outcome", marker))
		},
	}
	for name, emit := range cases {
		t.Run(name, func(t *testing.T) {
			c := telemetrytest.New(t)
			if c.Contains(marker) {
				t.Fatal("marker present before anything was emitted")
			}
			emit(c)
			if !c.Contains(marker) {
				t.Errorf("Contains missed the marker in %s", name)
			}
			if c.Contains("ABSENT-NEEDLE") {
				t.Error("Contains matched an absent needle")
			}
		})
	}
}

func TestCapture_SafeForConcurrentUse(t *testing.T) {
	c := telemetrytest.New(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, sp := c.Tracer("module.test").Start(context.Background(), "op")
			c.Logger().InfoContext(ctx, "tick")
			sp.End()
			if !c.Contains("tick") {
				t.Error("missing tick")
			}
		}()
	}
	wg.Wait()
	if len(c.Spans()) != 16 || len(c.Records()) != 16 {
		t.Errorf("spans=%d records=%d", len(c.Spans()), len(c.Records()))
	}
}
