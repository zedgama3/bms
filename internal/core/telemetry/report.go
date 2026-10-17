package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// gate lets one event through per key per period, by the injected clock. It holds at most
// maxTrackedWarnings keys; when full it forgets them all (a warning may then repeat early,
// which is harmless).
type gate struct {
	clk    interface{ Now() time.Time }
	period time.Duration
	mu     sync.Mutex
	last   map[string]time.Time
}

func newGate(clk interface{ Now() time.Time }, period time.Duration) *gate {
	return &gate{clk: clk, period: period, last: map[string]time.Time{}}
}

// allow reports whether key may fire now, and records that it did.
func (g *gate) allow(key string) bool {
	now := g.clk.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if at, ok := g.last[key]; ok && now.Sub(at) < g.period {
		return false
	}
	if len(g.last) >= maxTrackedWarnings {
		clear(g.last)
	}
	g.last[key] = now
	return true
}

// report counts a policy violation, tells the Observer, and warns once per
// (signal, rule, key) per hour (decision 13). The record that carries the bad value is
// still written; this is the pointer to it.
func (t *Telemetry) report(ctx context.Context, v Violation) {
	v.Key = capKey(v.Key)
	if t.obs != nil {
		t.obs.Violation(v)
	}
	if t.policyViolations != nil {
		t.policyViolations.Add(ctx, 1, Token("signal", v.Signal), Token("rule", v.Rule))
	}
	if t.logger != nil && t.violationGate.allow(v.Signal+"\x00"+v.Rule+"\x00"+v.Key) {
		t.logger.WarnContext(ctx, "telemetry policy violation",
			slog.String("signal", v.Signal), slog.String("rule", v.Rule), slog.String("key", v.Key))
	}
}

func (t *Telemetry) countLogWriteFailure(ctx context.Context) {
	if t.logWriteFailures != nil {
		t.logWriteFailures.Add(ctx, 1)
	}
}

var urlRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://\S+`)

// exportError marks an error already counted and warned about by an exporter wrapper, so
// the SDK's global error handler does not warn about it a second time.
type exportError struct{ err error }

func (e *exportError) Error() string { return e.err.Error() }
func (e *exportError) Unwrap() error { return e.err }

// exportFailed counts a failed export of n records and warns once per signal per minute.
// The text never carries a URL (it may hold credentials).
func (t *Telemetry) exportFailed(ctx context.Context, signal string, n int, err error) error {
	if t.exportFailures != nil {
		t.exportFailures.Add(ctx, 1, Token("signal", signal))
	}
	if t.dropped != nil && n > 0 {
		t.dropped.Add(ctx, int64(n), Token("signal", signal))
	}
	if t.logger != nil && t.exportGate.allow(signal) {
		t.logger.WarnContext(ctx, "telemetry export failed",
			slog.String("signal", signal), slog.String("error", urlRE.ReplaceAllString(err.Error(), "[url]")))
	}
	return &exportError{err: err}
}

// sdkError is the SDK's global error handler: it warns, rate limited per message, about
// anything the wrappers did not already report.
func (t *Telemetry) sdkError(err error) {
	if err == nil {
		return
	}
	var ee *exportError
	if errors.As(err, &ee) {
		return
	}
	msg := urlRE.ReplaceAllString(err.Error(), "[url]")
	if t.logger != nil && t.sdkGate.allow(msg) {
		t.logger.Warn("opentelemetry error", slog.String("error", msg))
	}
}

// ---- exporter wrappers ----

type spanExporter struct {
	inner sdktrace.SpanExporter
	t     *Telemetry
}

func (e *spanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := e.inner.ExportSpans(ctx, spans); err != nil {
		return e.t.exportFailed(ctx, "traces", len(spans), err)
	}
	return nil
}

func (e *spanExporter) Shutdown(ctx context.Context) error { return e.inner.Shutdown(ctx) } //nolint:wrapcheck // pass-through

type metricExporter struct {
	inner sdkmetric.Exporter
	t     *Telemetry
}

func (e *metricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return e.inner.Temporality(k)
}

func (e *metricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return e.inner.Aggregation(k)
}

func (e *metricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if err := e.inner.Export(ctx, rm); err != nil {
		return e.t.exportFailed(ctx, "metrics", 0, err)
	}
	return nil
}

func (e *metricExporter) ForceFlush(ctx context.Context) error { return e.inner.ForceFlush(ctx) } //nolint:wrapcheck // pass-through
func (e *metricExporter) Shutdown(ctx context.Context) error   { return e.inner.Shutdown(ctx) }   //nolint:wrapcheck // pass-through

type logExporter struct {
	inner sdklog.Exporter
	t     *Telemetry
}

func (e *logExporter) Export(ctx context.Context, recs []sdklog.Record) error {
	if err := e.inner.Export(ctx, recs); err != nil {
		return e.t.exportFailed(ctx, "logs", len(recs), err)
	}
	return nil
}

func (e *logExporter) ForceFlush(ctx context.Context) error { return e.inner.ForceFlush(ctx) } //nolint:wrapcheck // pass-through
func (e *logExporter) Shutdown(ctx context.Context) error   { return e.inner.Shutdown(ctx) }   //nolint:wrapcheck // pass-through
