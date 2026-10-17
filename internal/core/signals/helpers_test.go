package signals_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const (
	testTimeout = 5 * time.Second
	nameX       = signals.Name("alpha.x")
	nameY       = signals.Name("alpha.y")
	nameOther   = signals.Name("beta.other")
)

var errBoom = errors.New("boom")

// fixture is a dispatcher over fakes, with captured telemetry.
type fixture struct {
	t    *testing.T
	cap  *telemetrytest.Capture
	clk  *clock.Fake
	fdb  *dbtest.Fake
	disp *signals.Dispatcher
}

func baseOptions(c *telemetrytest.Capture, clk clock.Clock) signals.Options {
	return signals.Options{
		Logger:         c.Logger(),
		Tracer:         c.Tracer("signals"),
		Meter:          c.Meter("signals"),
		Clock:          clk,
		HandlerTimeout: testTimeout,
	}
}

func newFixture(t *testing.T, mods ...func(*signals.Options)) *fixture {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC))
	opts := baseOptions(c, clk)
	for _, m := range mods {
		m(&opts)
	}
	d, err := signals.New(opts)
	if err != nil {
		t.Fatalf("signals.New: %v", err)
	}
	return &fixture{t: t, cap: c, clk: clk, fdb: dbtest.NewFake(), disp: d}
}

// scope fails the test when Scope fails.
func (f *fixture) scope(owner string, pub, sub []signals.Name) signals.Bus {
	f.t.Helper()
	b, err := f.disp.Scope(owner, pub, sub)
	if err != nil {
		f.t.Fatalf("Scope(%q): %v", owner, err)
	}
	return b
}

// subscribe fails the test when Subscribe fails.
func subscribe(t *testing.T, b signals.Bus, n signals.Name, h signals.Handler) {
	t.Helper()
	if err := b.Subscribe(n, h); err != nil {
		t.Fatalf("Subscribe(%s): %v", n, err)
	}
}

// tx runs fn in a write transaction of the fixture's fake database.
func (f *fixture) tx(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := f.fdb.Transact(ctx, db.Write, fn); err != nil {
		return err //nolint:wrapcheck // test helper passes the error on unchanged
	}
	return nil
}

// log is a concurrency-safe ordered list of strings.
type log struct {
	mu sync.Mutex
	s  []string
}

func (l *log) add(v string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.s = append(l.s, v)
}

func (l *log) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.s...)
}

// mark returns a handler that appends tag and returns nil.
func (l *log) mark(tag string) signals.Handler {
	return func(context.Context, signals.Signal) error {
		l.add(tag)
		return nil
	}
}

func person() signals.Payload {
	return signals.NewPayload(signals.Ref("person", model.NewID[model.PersonID]()))
}

func validSignal(name signals.Name) signals.Signal {
	return signals.Signal{
		ID:      model.NewID[signals.SignalID](),
		Name:    name,
		At:      time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		Payload: person(),
	}
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error of kind %v, got nil", k)
	}
	if !errs.Is(err, k) {
		t.Fatalf("want kind %v, got %v (%v)", k, errs.KindOf(err), err)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// delivered returns the bms.signals.delivered measurements.
func delivered(c *telemetrytest.Capture) []telemetry.Measurement {
	return measures(c, "bms.signals.delivered")
}

func measures(c *telemetrytest.Capture, instrument string) []telemetry.Measurement {
	var out []telemetry.Measurement
	for _, m := range c.Measurements() {
		if m.Instrument == instrument {
			out = append(out, m)
		}
	}
	return out
}

// outcomes maps subscriber module to outcome from the delivered counter.
func outcomes(c *telemetrytest.Capture) map[string]string {
	out := map[string]string{}
	for _, m := range delivered(c) {
		out[m.Attrs["module"]] = m.Attrs["outcome"]
	}
	return out
}

func spansNamed(c *telemetrytest.Capture, name string) []telemetry.SpanData {
	var out []telemetry.SpanData
	for _, s := range c.Spans() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func recordsAt(c *telemetrytest.Capture, lvl slog.Level) []telemetry.LogRecord {
	var out []telemetry.LogRecord
	for _, r := range c.Records() {
		if r.Level == lvl {
			out = append(out, r)
		}
	}
	return out
}

func hasMessage(rs []telemetry.LogRecord, sub string) bool {
	for _, r := range rs {
		if strings.Contains(r.Message, sub) {
			return true
		}
	}
	return false
}

// fakeDeferrer records deliveries and returns err.
type fakeDeferrer struct {
	mu    sync.Mutex
	got   []signals.Delivery
	err   error
	calls int
}

func (d *fakeDeferrer) Defer(_ context.Context, dl signals.Delivery) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.got = append(d.got, dl)
	return d.err
}

func (d *fakeDeferrer) deliveries() []signals.Delivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]signals.Delivery(nil), d.got...)
}
