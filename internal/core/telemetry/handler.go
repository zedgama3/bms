package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// sink is where a cleaned record goes: the stdout handler, and the OTLP bridge when logs
// are exported. Both see the same bytes (decision 7).
type sink struct {
	stdout slog.Handler
	bridge slog.Handler // nil when logs are not exported
}

// op is one pre-applied step of With/WithGroup, replayed on every record. Replaying
// instead of delegating keeps trace_id and span_id at the top level whatever groups are
// open, and keeps the stdout and OTLP copies identical.
type op struct {
	group string // non-empty: open this group
	attrs []slog.Attr
}

// policyHandler is the one log handler (decision 7): it checks keys, scrubs and caps
// values, adds trace_id and span_id, then writes to the sink. It never logs by itself
// except through Telemetry's violation report, which uses fixed, valid keys.
type policyHandler struct {
	t   *Telemetry
	ops []op
}

func (h *policyHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.t.level
}

func (h *policyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	var clean []slog.Attr
	for _, a := range attrs {
		clean = append(clean, h.t.cleanAttr(context.Background(), a)...)
	}
	return &policyHandler{t: h.t, ops: append(append([]op(nil), h.ops...), op{attrs: clean})}
}

func (h *policyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	name = h.t.cleanGroupName(context.Background(), name)
	return &policyHandler{t: h.t, ops: append(append([]op(nil), h.ops...), op{group: name})}
}

func (h *policyHandler) Handle(ctx context.Context, r slog.Record) error {
	var tail []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		tail = append(tail, h.t.cleanAttr(ctx, a)...)
		return true
	})
	var attrs []slog.Attr
	traceID := ""
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		traceID = sc.TraceID().String()
		attrs = append(attrs,
			slog.String("trace_id", traceID),
			slog.String("span_id", sc.SpanID().String()))
	}
	attrs = append(attrs, nest(h.ops, tail)...)

	msg := capBytes(Scrub(r.Message), MaxLogValueBytes)
	out := slog.NewRecord(r.Time, r.Level, msg, r.PC)
	out.AddAttrs(attrs...)

	if obs := h.t.obs; obs != nil {
		flat := make(map[string]string, len(attrs))
		flatten("", attrs, flat)
		obs.Record(LogRecord{Time: r.Time, Level: r.Level, Message: msg, Attrs: flat, TraceID: traceID})
	}

	var err error
	if serr := h.t.sink.stdout.Handle(ctx, out); serr != nil {
		h.t.countLogWriteFailure(ctx)
		err = errs.E("telemetry.Handler.Handle", errs.Unavailable, serr)
	}
	if h.t.sink.bridge != nil {
		if berr := h.t.sink.bridge.Handle(ctx, out.Clone()); berr != nil {
			err = errors.Join(err, errs.E("telemetry.Handler.Handle", errs.Unavailable, berr))
		}
	}
	return err
}

// nest wraps tail in the ops' groups and prepends their attributes, innermost last.
func nest(ops []op, tail []slog.Attr) []slog.Attr {
	cur := tail
	for i := len(ops) - 1; i >= 0; i-- {
		o := ops[i]
		if o.group != "" {
			if len(cur) == 0 {
				cur = nil
				continue
			}
			cur = []slog.Attr{{Key: o.group, Value: slog.GroupValue(cur...)}}
			continue
		}
		cur = append(append([]slog.Attr(nil), o.attrs...), cur...)
	}
	return cur
}

// flatten renders attrs as "group.key" -> text.
func flatten(prefix string, attrs []slog.Attr, out map[string]string) {
	for _, a := range attrs {
		if a.Value.Kind() == slog.KindGroup {
			p := prefix
			if a.Key != "" {
				p += a.Key + "."
			}
			flatten(p, a.Value.Group(), out)
			continue
		}
		out[prefix+a.Key] = a.Value.String()
	}
}

// cleanGroupName applies the key rules to a group name.
func (t *Telemetry) cleanGroupName(ctx context.Context, name string) string {
	if !keyOK(name) {
		t.report(ctx, Violation{Signal: signalLog, Rule: ruleInvalidKey, Key: name})
		return "invalid_group"
	}
	if keyDenied(name) {
		t.report(ctx, Violation{Signal: signalLog, Rule: ruleDeniedKey, Key: name})
		return "denied_group"
	}
	return name
}

// cleanAttr applies the policy to one slog attribute: LogValuers are resolved, keys are
// checked against the grammar and the denylist, strings are scrubbed and capped.
func (t *Telemetry) cleanAttr(ctx context.Context, a slog.Attr) []slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return nil
	}
	if a.Value.Kind() == slog.KindGroup {
		kids := a.Value.Group()
		if len(kids) == 0 {
			return nil
		}
		var clean []slog.Attr
		for _, k := range kids {
			clean = append(clean, t.cleanAttr(ctx, k)...)
		}
		if a.Key == "" { // inlined group
			return clean
		}
		name := t.cleanGroupName(ctx, a.Key)
		return []slog.Attr{{Key: name, Value: slog.GroupValue(clean...)}}
	}
	if !keyOK(a.Key) {
		t.report(ctx, Violation{Signal: signalLog, Rule: ruleInvalidKey, Key: a.Key})
		return []slog.Attr{slog.String(invalidKeyAttr, capKey(a.Key))}
	}
	if keyDenied(a.Key) {
		t.report(ctx, Violation{Signal: signalLog, Rule: ruleDeniedKey, Key: a.Key})
		return []slog.Attr{slog.String(a.Key, markerDenied)}
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(capBytes(Scrub(a.Value.String()), logValueCap(a.Key)))
	case slog.KindAny:
		a.Value = slog.StringValue(capBytes(Scrub(renderAny(a.Value.Any())), logValueCap(a.Key)))
	default:
		// numbers, booleans, durations and times carry no free text.
	}
	return []slog.Attr{a}
}

// renderAny turns an arbitrary logged value into text so it can be scrubbed: the handler
// cannot look inside a struct or a map, and writing one as JSON would bypass the scrubber.
func renderAny(v any) string {
	// fmt recovers from a panicking Error or String method (a typed nil receiver) and
	// prints "<nil>" or a PANIC marker, so this cannot panic.
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}
