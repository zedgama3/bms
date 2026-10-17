// Package telemetrytest gives tests a Telemetry that keeps everything in memory and fails
// the test on any policy violation. No environment, no collector, no goroutines left.
package telemetrytest

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// closeTimeout bounds the cleanup Close; with no exporter it returns at once.
const closeTimeout = 5 * time.Second

// Capture is an Observer with accessors. New builds a telemetry.Telemetry with FormatText
// into a buffer, LevelDebug, a clock.Fake, an Environ that returns nothing (so nothing is
// exported) and this Capture as Observer; it registers t.Cleanup to Close it and to fail t
// if Violations is non-empty. Safe for concurrent use.
type Capture struct {
	tb  testing.TB
	tel *telemetry.Telemetry
	out *lockedBuffer

	mu         sync.Mutex
	records    []telemetry.LogRecord
	spans      []telemetry.SpanData
	measures   []telemetry.Measurement
	violations []telemetry.Violation
}

var _ telemetry.Observer = (*Capture)(nil)

// lockedBuffer is a bytes.Buffer safe for concurrent use.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // bytes.Buffer.Write never fails
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// New cannot fail: a construction error stops the test with t.Fatal.
func New(t testing.TB) *Capture {
	t.Helper()
	c := &Capture{tb: t, out: &lockedBuffer{}}
	tel, err := telemetry.New(context.Background(), telemetry.Options{
		Level:    slog.LevelDebug,
		Format:   telemetry.FormatText,
		Instance: "test",
		Version:  "test",
		Stdout:   c.out,
		Clock:    clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		Environ:  func(string) (string, bool) { return "", false },
		Observer: c,
	})
	if err != nil {
		t.Fatalf("telemetrytest.New: %v", err)
	}
	c.tel = tel
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		if err := tel.Close(ctx); err != nil {
			t.Errorf("telemetrytest: close: %v", err)
		}
		if vs := c.Violations(); len(vs) > 0 {
			t.Errorf("telemetry policy violations: %s", describe(vs))
		}
	})
	return c
}

func describe(vs []telemetry.Violation) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmt.Sprintf("%s/%s(%s)", v.Signal, v.Rule, v.Key)
	}
	return strings.Join(parts, ", ")
}

// Telemetry returns the instance.
func (c *Capture) Telemetry() *telemetry.Telemetry { return c.tel }

// Logger is a shortcut for Telemetry().Logger().
func (c *Capture) Logger() *slog.Logger { return c.tel.Logger() }

// Tracer is a shortcut for Telemetry().Tracer(name); a bad name stops the test.
func (c *Capture) Tracer(name string) telemetry.Tracer {
	c.tb.Helper()
	tr, err := c.tel.Tracer(name)
	if err != nil {
		c.tb.Fatalf("telemetrytest: Tracer(%q): %v", name, err)
	}
	return tr
}

// Meter is a shortcut for Telemetry().Meter(name); a bad name stops the test.
func (c *Capture) Meter(name string) telemetry.Meter {
	c.tb.Helper()
	m, err := c.tel.Meter(name)
	if err != nil {
		c.tb.Fatalf("telemetrytest: Meter(%q): %v", name, err)
	}
	return m
}

// Record implements telemetry.Observer.
func (c *Capture) Record(r telemetry.LogRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, copyRecord(r))
}

// Span implements telemetry.Observer.
func (c *Capture) Span(s telemetry.SpanData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s.Attrs = copyMap(s.Attrs)
	c.spans = append(c.spans, s)
}

// Measurement implements telemetry.Observer.
func (c *Capture) Measurement(m telemetry.Measurement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m.Attrs = copyMap(m.Attrs)
	c.measures = append(c.measures, m)
}

// Violation implements telemetry.Observer.
func (c *Capture) Violation(v telemetry.Violation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.violations = append(c.violations, v)
}

// Records returns a copy of the log records captured so far.
func (c *Capture) Records() []telemetry.LogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]telemetry.LogRecord, len(c.records))
	for i, r := range c.records {
		out[i] = copyRecord(r)
	}
	return out
}

// Spans returns a copy of the ended spans captured so far.
func (c *Capture) Spans() []telemetry.SpanData {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]telemetry.SpanData, len(c.spans))
	for i, s := range c.spans {
		s.Attrs = copyMap(s.Attrs)
		out[i] = s
	}
	return out
}

// Measurements returns a copy of the measurements captured so far.
func (c *Capture) Measurements() []telemetry.Measurement {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]telemetry.Measurement, len(c.measures))
	for i, m := range c.measures {
		m.Attrs = copyMap(m.Attrs)
		out[i] = m
	}
	return out
}

// Violations returns a copy of the policy violations captured so far.
func (c *Capture) Violations() []telemetry.Violation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]telemetry.Violation(nil), c.violations...)
}

// Contains reports whether needle occurs in any captured message, attribute value, span
// name or the stdout buffer: the one-line leak check every spec's test plan uses
// (`!c.Contains(marker)`). Cannot fail.
func (c *Capture) Contains(needle string) bool {
	if strings.Contains(c.out.String(), needle) {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if strings.Contains(r.Message, needle) || anyValue(r.Attrs, needle) {
			return true
		}
	}
	for _, s := range c.spans {
		if strings.Contains(s.Name, needle) || anyValue(s.Attrs, needle) {
			return true
		}
	}
	for _, m := range c.measures {
		if strings.Contains(m.Instrument, needle) || anyValue(m.Attrs, needle) {
			return true
		}
	}
	for _, v := range c.violations {
		if strings.Contains(v.Key, needle) {
			return true
		}
	}
	return false
}

// Stdout returns what the stdout handler wrote. For format tests.
func (c *Capture) Stdout() string { return c.out.String() }

func anyValue(m map[string]string, needle string) bool {
	for _, v := range m {
		if strings.Contains(v, needle) {
			return true
		}
	}
	return false
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyRecord(r telemetry.LogRecord) telemetry.LogRecord {
	r.Attrs = copyMap(r.Attrs)
	return r
}
