package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Meter creates instruments. Instrument names are tokens of at most 128 bytes following
// OpenTelemetry's naming (dots between segments, no spaces). Safe for concurrent use.
type Meter interface {
	// Counter returns a monotonic counter. Fails with errs.Invalid for a bad name or unit
	// (F20) and errs.Conflict if name exists in this scope as another instrument kind or
	// with another unit (F21).
	Counter(name string, opts ...InstrumentOption) (Counter, error)
	// Histogram returns a histogram; WithBuckets sets explicit boundaries (default: the
	// OpenTelemetry defaults for the unit). Fails as Counter does (F20, F21).
	Histogram(name string, opts ...InstrumentOption) (Histogram, error)
	// Gauge registers observe, called on every collection; each Sample is one value with
	// its attributes. observe must be quick, safe for concurrent use and safe after its
	// owner closed (return nil). A panic inside it is recovered, logged once per
	// instrument and the collection skipped (F23). Fails with errs.Invalid if observe is
	// nil (F22) or the name is bad (F20), errs.Conflict as above (F21).
	Gauge(name string, observe func(ctx context.Context) []Sample, opts ...InstrumentOption) error
}

// Counter adds. It is safe for concurrent use and never blocks. Attributes must be Token,
// Bool, Int, Int64 or Kind: a Text or ID attribute is dropped with a Violation (F19),
// because it would make the series unbounded.
type Counter interface {
	// Add adds n (n >= 0; a negative n is dropped with a Violation, F24).
	Add(ctx context.Context, n int64, attrs ...Attr)
}

// Histogram records. Same attribute rules as Counter.
type Histogram interface {
	Record(ctx context.Context, v float64, attrs ...Attr)
}

// Sample is one gauge observation.
type Sample struct {
	Value float64
	Attrs []Attr
}

type instrumentConfig struct {
	unit    string
	desc    string
	buckets []float64
	hasB    bool
}

// InstrumentOption configures an instrument.
type InstrumentOption func(*instrumentConfig)

// WithUnit sets the UCUM unit: "s", "ms", "By", "{request}", "{connection}". Default "".
func WithUnit(unit string) InstrumentOption {
	return func(c *instrumentConfig) { c.unit = unit }
}

// WithDescription sets the English description shown by backends. No personal data.
func WithDescription(d string) InstrumentOption {
	return func(c *instrumentConfig) { c.desc = d }
}

// WithBuckets sets explicit histogram boundaries, strictly increasing. Histogram only.
func WithBuckets(bounds ...float64) InstrumentOption {
	return func(c *instrumentConfig) {
		c.buckets = append([]float64(nil), bounds...)
		c.hasB = true
	}
}

type instKind uint8

const (
	instCounter instKind = iota + 1
	instHistogram
	instGauge
)

type instInfo struct {
	kind instKind
	unit string
}

// meter is the Meter of one scope.
type meter struct {
	t     *Telemetry
	scope string
	m     metric.Meter

	mu    sync.Mutex
	names map[string]instInfo
}

func (t *Telemetry) newMeter(scope string) *meter {
	return &meter{t: t, scope: scope, m: t.mp.Meter(scope), names: map[string]instInfo{}}
}

// check validates the options and registers the name; the caller creates the instrument.
func (m *meter) check(op, name string, kind instKind, opts []InstrumentOption) (instrumentConfig, error) {
	var cfg instrumentConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if !IsToken(name) || name[0] < 'A' || (name[0] > 'Z' && name[0] < 'a') || name[0] > 'z' {
		return cfg, errs.E(op, errs.Invalid, errors.New("instrument name must be a token starting with a letter"))
	}
	if !unitOK(cfg.unit) {
		return cfg, errs.E(op, errs.Invalid, errors.New("unit must be a UCUM-shaped string without spaces"))
	}
	if cfg.hasB {
		if kind != instHistogram {
			return cfg, errs.E(op, errs.Invalid, errors.New("buckets apply to histograms only"))
		}
		for i, b := range cfg.buckets {
			if math.IsNaN(b) || math.IsInf(b, 0) || (i > 0 && b <= cfg.buckets[i-1]) {
				return cfg, errs.E(op, errs.Invalid, errors.New("buckets must be finite and strictly increasing"))
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.names[name]; ok {
		if prev.kind != kind || prev.unit != cfg.unit {
			return cfg, errs.E(op, errs.Conflict,
				fmt.Errorf("instrument %q already exists in scope %q as another kind or unit", name, m.scope))
		}
		return cfg, nil
	}
	m.names[name] = instInfo{kind: kind, unit: cfg.unit}
	return cfg, nil
}

func unitOK(u string) bool {
	if len(u) > 63 {
		return false
	}
	for i := 0; i < len(u); i++ {
		if u[i] <= ' ' || u[i] > '~' {
			return false
		}
	}
	return true
}

func (m *meter) Counter(name string, opts ...InstrumentOption) (Counter, error) {
	const op = "telemetry.Meter.Counter"
	cfg, err := m.check(op, name, instCounter, opts)
	if err != nil {
		return nil, err
	}
	c, err := m.m.Int64Counter(name, metric.WithUnit(cfg.unit), metric.WithDescription(cfg.desc))
	if err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	return &counter{m: m, name: name, c: c}, nil
}

func (m *meter) Histogram(name string, opts ...InstrumentOption) (Histogram, error) {
	const op = "telemetry.Meter.Histogram"
	cfg, err := m.check(op, name, instHistogram, opts)
	if err != nil {
		return nil, err
	}
	mo := []metric.Float64HistogramOption{metric.WithUnit(cfg.unit), metric.WithDescription(cfg.desc)}
	if cfg.hasB {
		mo = append(mo, metric.WithExplicitBucketBoundaries(cfg.buckets...))
	}
	h, err := m.m.Float64Histogram(name, mo...)
	if err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	return &histogram{m: m, name: name, h: h}, nil
}

func (m *meter) Gauge(name string, observe func(ctx context.Context) []Sample, opts ...InstrumentOption) error {
	const op = "telemetry.Meter.Gauge"
	if observe == nil {
		return errs.E(op, errs.Invalid, errors.New("observe must not be nil"))
	}
	cfg, err := m.check(op, name, instGauge, opts)
	if err != nil {
		return err
	}
	var warned sync.Once
	_, err = m.m.Float64ObservableGauge(name,
		metric.WithUnit(cfg.unit), metric.WithDescription(cfg.desc),
		metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) (cbErr error) {
			defer func() {
				if r := recover(); r != nil {
					warned.Do(func() {
						m.t.logger.ErrorContext(ctx, "gauge observer panicked; collection skipped",
							slog.String("instrument", name), slog.String("scope", m.scope))
					})
					cbErr = nil
				}
			}()
			for _, s := range observe(ctx) {
				kvs, text := m.metricAttrs(ctx, s.Attrs)
				if obs := m.t.obs; obs != nil {
					obs.Measurement(Measurement{Scope: m.scope, Instrument: name, Value: s.Value, Attrs: text})
				}
				o.Observe(s.Value, metric.WithAttributes(kvs...))
			}
			return nil
		}))
	if err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	return nil
}

// metricAttrs applies the metric rules: Token, Bool, Int, Int64 and Kind pass; Text and ID
// are dropped with a Violation (F19); so are floats and durations, which are unbounded too.
func (m *meter) metricAttrs(ctx context.Context, attrs []Attr) ([]attribute.KeyValue, map[string]string) {
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	var text map[string]string
	if m.t.obs != nil {
		text = make(map[string]string, len(attrs))
	}
	for _, a := range attrs {
		if a.isZero() {
			continue
		}
		if a.viol != "" {
			m.t.report(ctx, Violation{Signal: signalMetric, Rule: a.viol, Key: a.violKey()})
		}
		rule := ""
		switch a.kind {
		case kindText:
			rule = ruleTextOnMetric
		case kindID:
			rule = ruleIDOnMetric
		case kindFloat, kindDuration:
			rule = ruleInvalidMetricVal
		}
		if rule != "" {
			if a.viol == "" {
				m.t.report(ctx, Violation{Signal: signalMetric, Rule: rule, Key: a.key})
			}
			continue
		}
		kvs = append(kvs, a.keyValue())
		if text != nil {
			text[a.key] = a.text()
		}
	}
	return kvs, text
}

type counter struct {
	m    *meter
	name string
	c    metric.Int64Counter
}

func (c *counter) Add(ctx context.Context, n int64, attrs ...Attr) {
	kvs, text := c.m.metricAttrs(ctx, attrs)
	if n < 0 {
		c.m.t.report(ctx, Violation{Signal: signalMetric, Rule: ruleNegativeCount, Key: c.name})
		return
	}
	if obs := c.m.t.obs; obs != nil {
		obs.Measurement(Measurement{Scope: c.m.scope, Instrument: c.name, Value: float64(n), Attrs: text})
	}
	c.c.Add(ctx, n, metric.WithAttributes(kvs...))
}

type histogram struct {
	m    *meter
	name string
	h    metric.Float64Histogram
}

func (h *histogram) Record(ctx context.Context, v float64, attrs ...Attr) {
	kvs, text := h.m.metricAttrs(ctx, attrs)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	if obs := h.m.t.obs; obs != nil {
		obs.Measurement(Measurement{Scope: h.m.scope, Instrument: h.name, Value: v, Attrs: text})
	}
	h.h.Record(ctx, v, metric.WithAttributes(kvs...))
}

// scopeOK reports whether name is a legal scope name: a token of at most 64 bytes.
func scopeOK(name string) bool {
	return len(name) <= 64 && IsToken(name) && !strings.ContainsAny(name, " ")
}
