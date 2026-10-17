package telemetry_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// marker and markerEmail are the leak markers from the spec's test plan (invented data).
const (
	marker      = "ZQ9MARKER7"
	markerEmail = "zq9@marker.example"
)

var epoch = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// syncBuf is a goroutine-safe buffer used as Options.Stdout.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.b.Write(p)
	if err != nil {
		return n, fmt.Errorf("syncBuf: %w", err)
	}
	return n, nil
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// failWriter always fails, like a closed terminal.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// obs is an Observer that keeps copies.
type obs struct {
	mu   sync.Mutex
	recs []telemetry.LogRecord
	sps  []telemetry.SpanData
	ms   []telemetry.Measurement
	vs   []telemetry.Violation
}

var _ telemetry.Observer = (*obs)(nil)

func (o *obs) Record(r telemetry.LogRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.recs = append(o.recs, r)
}

func (o *obs) Span(s telemetry.SpanData) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sps = append(o.sps, s)
}

func (o *obs) Measurement(m telemetry.Measurement) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ms = append(o.ms, m)
}

func (o *obs) Violation(v telemetry.Violation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.vs = append(o.vs, v)
}

func (o *obs) records() []telemetry.LogRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]telemetry.LogRecord(nil), o.recs...)
}

func (o *obs) spans() []telemetry.SpanData {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]telemetry.SpanData(nil), o.sps...)
}

func (o *obs) measurements() []telemetry.Measurement {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]telemetry.Measurement(nil), o.ms...)
}

func (o *obs) violations() []telemetry.Violation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]telemetry.Violation(nil), o.vs...)
}

// hasViolation reports whether a violation with the rule (and key, when non-empty) was seen.
func (o *obs) hasViolation(rule, key string) bool {
	for _, v := range o.violations() {
		if v.Rule == rule && (key == "" || v.Key == key) {
			return true
		}
	}
	return false
}

// harness is a Telemetry built with an in-memory observer and no environment.
type harness struct {
	tel *telemetry.Telemetry
	obs *obs
	buf *syncBuf
	clk *clock.Fake
}

func baseOptions(buf io.Writer, clk clock.Clock, env map[string]string, o telemetry.Observer) telemetry.Options {
	return telemetry.Options{
		Level:    slog.LevelDebug,
		Format:   telemetry.FormatJSON,
		Instance: "test-copy",
		Version:  "v1.2.3",
		Stdout:   buf,
		Clock:    clk,
		Environ: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
		Observer: o,
	}
}

// newHarness builds a Telemetry; mut may change the options first. Close is registered as
// cleanup, with a deadline.
func newHarness(t *testing.T, mut func(*telemetry.Options)) *harness {
	t.Helper()
	h := &harness{obs: &obs{}, buf: &syncBuf{}, clk: clock.NewFake(epoch)}
	opts := baseOptions(h.buf, h.clk, nil, h.obs)
	if mut != nil {
		mut(&opts)
	}
	tel, err := telemetry.New(context.Background(), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.tel = tel
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tel.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return h
}

func (h *harness) tracer(t *testing.T) telemetry.Tracer {
	t.Helper()
	tr, err := h.tel.Tracer("module.test")
	if err != nil {
		t.Fatalf("Tracer: %v", err)
	}
	return tr
}

func (h *harness) meter(t *testing.T) telemetry.Meter {
	t.Helper()
	m, err := h.tel.Meter("module.test")
	if err != nil {
		t.Fatalf("Meter: %v", err)
	}
	return m
}

// span returns the first captured span with the name.
func (h *harness) span(t *testing.T, name string) telemetry.SpanData {
	t.Helper()
	for _, s := range h.obs.spans() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no span named %q among %d spans", name, len(h.obs.spans()))
	return telemetry.SpanData{}
}

// emit starts a span, applies attrs, ends it, and returns the captured data.
func (h *harness) emit(t *testing.T, attrs ...telemetry.Attr) telemetry.SpanData {
	t.Helper()
	_, sp := h.tracer(t).Start(context.Background(), "attr.probe")
	sp.SetAttrs(attrs...)
	sp.End()
	return h.span(t, "attr.probe")
}

// fieldNames returns the field of every validate.FieldError in err.
func fieldNames(err error) []string {
	var out []string
	for _, f := range validate.Fields(err) {
		out = append(out, f.Field)
	}
	return out
}

func hasField(err error, field string) bool {
	for _, f := range fieldNames(err) {
		if f == field {
			return true
		}
	}
	return false
}

func repeat(c byte, n int) string { return string(bytes.Repeat([]byte{c}, n)) }
