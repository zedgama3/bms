package telemetry

import (
	"log/slog"
	"time"
)

// Observer receives a copy of what telemetry emits, after the policy ran. telemetrytest
// implements it; production passes nil. Methods must be safe for concurrent use and must
// not block.
type Observer interface {
	Record(r LogRecord)
	Span(s SpanData)
	Measurement(m Measurement)
	Violation(v Violation)
}

// LogRecord is a plain copy of a log record for observers and tests.
type LogRecord struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]string // flattened "group.key" -> value as text
	TraceID string            // 32 lower-case hex, or ""
}

// SpanData is a plain copy of an ended span for observers and tests.
type SpanData struct {
	Scope      string
	Name       string
	Kind       SpanKind
	TraceID    string
	ParentID   string // span id of the parent, "" for a root
	Attrs      map[string]string
	Failed     bool
	ErrorKind  string
	DurationMs int64
}

// Measurement is a plain copy of one counter increment, histogram record or gauge sample.
type Measurement struct {
	Scope      string
	Instrument string
	Value      float64
	Attrs      map[string]string
}
