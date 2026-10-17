package telemetry

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Tracer opens spans. Every implementation is safe for concurrent use.
type Tracer interface {
	// Start begins a span named name as a child of the span ctx carries, or a root span
	// when it carries none, and returns a ctx carrying the new span. It never fails and
	// never blocks. name is a token of at most 128 bytes ("db.SELECT",
	// "signal:hello.greeted"), or the HTTP form "<METHOD> <route pattern>": an upper-case
	// method ([A-Z]{3,10}), exactly one space, then a token beginning with "/", the whole
	// at most 128 bytes ("GET /api/people/{id}"; decision 19). The route is always the
	// pattern, never a concrete path. Anything else yields a span named
	// "invalid-span-name" and a counted Violation (F14). The caller must End the span
	// exactly once, usually with defer.
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}

// Span is one unit of work in a trace.
type Span interface {
	// SetAttrs adds attributes, each already policy-checked by its constructor (F15–F18).
	// Cannot fail. No-op after End.
	SetAttrs(attrs ...Attr)
	// RecordError marks the span failed and records error.kind (errs.KindOf), error.op
	// (the outermost errs.Error's Op, when any) and exception.message (err.Error() through
	// Scrub, capped at MaxTextBytes). A nil err is ignored. errs.Canceled does not mark the
	// span failed (the caller left; nothing broke). Cannot fail. No-op after End.
	RecordError(err error)
	// End closes the span and hands it to the exporter. Idempotent. Cannot fail.
	End()
}

// SpanKind follows OpenTelemetry's kinds. The zero value is Internal.
type SpanKind uint8

// The span kinds.
const (
	SpanInternal SpanKind = iota // work inside the process (default)
	SpanServer                   // handling a request: api
	SpanClient                   // calling a dependency: db, later SMTP and S3
	SpanProducer                 // publishing a signal
	SpanConsumer                 // delivering a signal, running a job
)

func (k SpanKind) otel() trace.SpanKind {
	switch k {
	case SpanServer:
		return trace.SpanKindServer
	case SpanClient:
		return trace.SpanKindClient
	case SpanProducer:
		return trace.SpanKindProducer
	case SpanConsumer:
		return trace.SpanKindConsumer
	default:
		return trace.SpanKindInternal
	}
}

type startConfig struct {
	kind   SpanKind
	attrs  []Attr
	header http.Header
}

// StartOption configures Start.
type StartOption func(*startConfig)

// WithKind sets the span kind. Consumers: api (Server), db (Client), signals, jobs.
func WithKind(k SpanKind) StartOption {
	return func(c *startConfig) { c.kind = k }
}

// WithAttrs sets attributes at start, so a sampler could see them. Consumers: api (method
// and route), db (operation).
func WithAttrs(attrs ...Attr) StartOption {
	return func(c *startConfig) { c.attrs = append(c.attrs, attrs...) }
}

// WithParentFrom continues the trace named by a W3C traceparent header in h, as the api's
// request middleware does for the web shell's calls (D20: one trace covers the click and
// the server work). Only the trace ID and parent span ID are taken: the sampled flag and
// any tracestate are ignored, because the server decides sampling (decision 6). A missing
// or malformed header (wrong version, length or characters, all-zero IDs) is ignored and a
// root span starts (F33); nothing is counted, since any client may send anything.
// Consumer: api.
func WithParentFrom(h http.Header) StartOption {
	return func(c *startConfig) { c.header = h }
}

// parseTraceparent extracts the remote parent from a W3C traceparent value: version 00,
// 32-hex trace ID, 16-hex parent ID, 2-hex flags, all-zero IDs refused. The flags are
// cleared in the result.
func parseTraceparent(v string) (trace.SpanContext, bool) {
	// 00-<32>-<16>-<2>
	if len(v) != 55 || v[0] != '0' || v[1] != '0' || v[2] != '-' || v[35] != '-' || v[52] != '-' {
		return trace.SpanContext{}, false
	}
	var tid trace.TraceID
	var sid trace.SpanID
	if !lowerHex(v[3:35]) || !lowerHex(v[36:52]) || !lowerHex(v[53:55]) {
		return trace.SpanContext{}, false
	}
	if _, err := hex.Decode(tid[:], []byte(v[3:35])); err != nil {
		return trace.SpanContext{}, false
	}
	if _, err := hex.Decode(sid[:], []byte(v[36:52])); err != nil {
		return trace.SpanContext{}, false
	}
	if !tid.IsValid() || !sid.IsValid() {
		return trace.SpanContext{}, false
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, Remote: true}), true
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// tracer is the Tracer over one SDK tracer.
type tracer struct {
	t     *Telemetry
	scope string
	tr    trace.Tracer
}

func (tr *tracer) Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	var cfg startConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.header != nil {
		if sc, ok := parseTraceparent(cfg.header.Get("traceparent")); ok {
			ctx = trace.ContextWithRemoteSpanContext(ctx, sc)
		}
	}
	if !spanNameOK(name) {
		tr.t.report(ctx, Violation{Signal: signalTrace, Rule: ruleInvalidSpanName, Key: name})
		name = invalidSpanName
	}
	parent := trace.SpanContextFromContext(ctx)

	s := &span{t: tr.t, scope: tr.scope, name: name, kind: cfg.kind, began: time.Now()}
	if tr.t.obs != nil {
		s.attrs = map[string]string{}
		if parent.SpanID().IsValid() {
			s.parentID = parent.SpanID().String()
		}
	}
	kvs := s.convert(ctx, cfg.attrs)
	ctx, s.sp = tr.tr.Start(ctx, name, trace.WithSpanKind(cfg.kind.otel()), trace.WithAttributes(kvs...))
	return ctx, s
}

// span is the Span over one SDK span.
type span struct {
	t     *Telemetry
	scope string
	name  string
	kind  SpanKind
	began time.Time
	sp    trace.Span

	mu        sync.Mutex
	ended     bool
	attrs     map[string]string // only when an Observer is set
	parentID  string
	failed    bool
	errorKind string
}

// convert applies the policy to attrs, reports their violations and returns the SDK form.
func (s *span) convert(ctx context.Context, attrs []Attr) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		if a.isZero() {
			continue
		}
		if a.viol != "" {
			s.t.report(ctx, Violation{Signal: signalTrace, Rule: a.viol, Key: a.violKey()})
		}
		out = append(out, a.keyValue())
		if s.attrs != nil {
			s.attrs[a.key] = a.text()
		}
	}
	return out
}

func (a Attr) keyValue() attribute.KeyValue {
	switch a.kind {
	case kindInt, kindInt64, kindDuration:
		return attribute.Int64(a.key, a.n)
	case kindFloat:
		return attribute.Float64(a.key, a.f)
	case kindBool:
		return attribute.Bool(a.key, a.n != 0)
	default:
		return attribute.String(a.key, a.s)
	}
}

func (s *span) SetAttrs(attrs ...Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.sp.SetAttributes(s.convert(context.Background(), attrs)...)
}

func (s *span) RecordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	kind := errs.KindOf(err)
	kvs := []attribute.KeyValue{attribute.String("error.kind", kind.String())}
	var ee *errs.Error
	op := ""
	if errors.As(err, &ee) && ee != nil {
		op = ee.Op
		if op != "" {
			kvs = append(kvs, attribute.String("error.op", op))
		}
	}
	msg := capBytes(Scrub(err.Error()), MaxTextBytes)
	kvs = append(kvs, attribute.String("exception.message", msg))
	s.sp.SetAttributes(kvs...)
	s.errorKind = kind.String()
	if s.attrs != nil {
		s.attrs["error.kind"] = kind.String()
		if op != "" {
			s.attrs["error.op"] = op
		}
		s.attrs["exception.message"] = msg
	}
	if kind != errs.Canceled {
		s.failed = true
		s.sp.SetStatus(codes.Error, kind.String())
	}
}

func (s *span) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	var data *SpanData
	if obs := s.t.obs; obs != nil {
		attrs := make(map[string]string, len(s.attrs))
		for k, v := range s.attrs {
			attrs[k] = v
		}
		sc := s.sp.SpanContext()
		id := ""
		if sc.TraceID().IsValid() {
			id = sc.TraceID().String()
		}
		data = &SpanData{
			Scope: s.scope, Name: s.name, Kind: s.kind, TraceID: id, ParentID: s.parentID,
			Attrs: attrs, Failed: s.failed, ErrorKind: s.errorKind,
			DurationMs: time.Since(s.began).Milliseconds(),
		}
	}
	s.sp.End()
	s.mu.Unlock()
	if data != nil {
		s.t.obs.Span(*data)
	}
}

// ---- References ----

// Ref is the code a person sees on an error screen or in an error body and can quote to a
// maintainer: "XXXX-XXXX", the first four bytes of the trace ID as upper-case hex with a
// hyphen (decision 3). It is short enough to read over a radio at an event and long enough
// to be unique among a regional's errors for far longer than logs are kept.
type Ref string

// Validate fails with errs.Invalid unless r is exactly "[0-9A-F]{4}-[0-9A-F]{4}" (F28).
func (r Ref) Validate() error {
	const op = "telemetry.Ref.Validate"
	s := string(r)
	if len(s) != 9 || s[4] != '-' {
		return fieldErr(op, "error_ref", "malformed", "must be XXXX-XXXX in upper-case hexadecimal")
	}
	for i := 0; i < 9; i++ {
		if i == 4 {
			continue
		}
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return fieldErr(op, "error_ref", "malformed", "must be XXXX-XXXX in upper-case hexadecimal")
		}
	}
	return nil
}

// String returns the reference as text.
func (r Ref) String() string { return string(r) }

// RefFrom returns the reference for the trace ctx carries, and false when ctx carries no
// valid span (F27): then the caller omits the field. Cannot fail.
func RefFrom(ctx context.Context) (Ref, bool) {
	id, ok := TraceIDFrom(ctx)
	if !ok {
		return "", false
	}
	return Ref(upper(id[:4]) + "-" + upper(id[4:8])), true
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// TraceIDFrom returns the 32-character lower-case hex trace ID ctx carries, and false when
// it carries none (F27). Cannot fail.
func TraceIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.TraceID().IsValid() {
		return "", false
	}
	return sc.TraceID().String(), true
}
