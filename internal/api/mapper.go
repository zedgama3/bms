package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// shellError is an error only the shell raises: a status and a code that are not an
// errs.Kind's own (cross_origin, misdirected, method_not_allowed, ...). It is always wrapped
// with errs.E, so errs.KindOf gives the log its kind.
type shellError struct {
	status int
	code   string
	extra  http.Header
	msg    string
}

func (e *shellError) Error() string { return e.msg }

func shell(op string, kind errs.Kind, status int, code, msg string, extra http.Header) error {
	return errs.E(op, kind, &shellError{status: status, code: code, msg: msg, extra: extra})
}

// outcome is how the mapper answers one error.
type outcome struct {
	status int
	code   string
	extra  http.Header
	quiet  bool // the client is gone: write nothing
}

// classify decides the status and code for err, in the order of "The mapper, step by
// step": the shell's own errors and the four sentinels first, then the kind.
func (s *Server) classify(err error) outcome {
	var se *shellError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &se):
		return outcome{status: se.status, code: se.code, extra: se.extra}
	case errors.Is(err, db.ErrReadOnly):
		return outcome{status: http.StatusServiceUnavailable, code: httpx.CodeReadOnly}
	case errors.Is(err, httpx.ErrRateLimited):
		return outcome{status: http.StatusTooManyRequests, code: httpx.CodeRateLimited,
			extra: http.Header{"Retry-After": {"60"}}}
	case errors.Is(err, httpx.ErrUnsupportedMediaType):
		return outcome{status: http.StatusUnsupportedMediaType, code: httpx.CodeUnsupportedMediaType}
	case errors.As(err, &tooBig):
		return outcome{status: http.StatusRequestEntityTooLarge, code: httpx.CodePayloadTooLarge}
	}
	kind := errs.KindOf(err)
	switch kind {
	case errs.Canceled:
		if s.draining.Load() {
			return outcome{status: http.StatusServiceUnavailable, code: httpx.CodeShuttingDown,
				extra: http.Header{"Connection": {"close"}}}
		}
		return outcome{quiet: true}
	case errs.Unavailable:
		return outcome{status: http.StatusServiceUnavailable, code: kind.String(),
			extra: http.Header{"Retry-After": {"5"}}}
	default:
		return outcome{status: httpx.StatusOf(kind), code: kind.String()}
	}
}

// problemFor builds the body for o: the reference when the ctx has a trace, and the field
// errors for an Invalid or a Conflict (a duplicate names its field, api D31).
func problemFor(o outcome, err error, ref string) httpx.Problem {
	p := httpx.Problem{Status: o.status, Code: o.code, Ref: ref}
	if o.code == errs.Invalid.String() || o.code == errs.Conflict.String() {
		for _, fe := range validate.Fields(err) {
			p.Fields = append(p.Fields, httpx.Field{Field: fe.Field, Code: fe.Code, Params: fe.Params, Reason: fe.Reason})
		}
	}
	return p
}

// writeProblem writes p as application/problem+json with the extra headers. It returns the
// write error, which the response wrapper has also remembered.
func writeProblem(w http.ResponseWriter, p httpx.Problem, extra http.Header) error {
	body, err := json.Marshal(p)
	if err != nil { // unreachable: strings, ints and string maps
		p = httpx.Problem{Status: http.StatusInternalServerError, Code: errs.Internal.String()}
		body = []byte(`{"status":500,"code":"internal"}`)
	}
	h := w.Header()
	for k, v := range extra {
		h[k] = v
	}
	h.Set("Content-Type", httpx.ContentTypeProblem)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(p.Status)
	if _, werr := w.Write(body); werr != nil {
		return werr //nolint:wrapcheck // the transport's error; the wrapper keeps it for the span
	}
	return nil
}

// opOf returns the outermost errs.Error's Op, or "".
func opOf(err error) string {
	var ee *errs.Error
	if errors.As(err, &ee) && ee != nil {
		return ee.Op
	}
	return ""
}

// capText scrubs s and cuts it to telemetry.MaxTextBytes, as telemetry.Text does for a
// span.
func capText(s string) string {
	s = telemetry.Scrub(s)
	if len(s) <= telemetry.MaxTextBytes {
		return s
	}
	cut := telemetry.MaxTextBytes - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// Fail is the shell's mapper (httpx.Failer): it answers r with the problem for err, marks
// the span and writes the one log record decision 4 asks for.
func (st *reqState) Fail(w http.ResponseWriter, _ *http.Request, err error) {
	if err == nil {
		err = errs.E("api.Fail", errs.Internal, errors.New("Fail called with a nil error"))
	}
	if st.rw.wroteHeader {
		st.s.log.LogAttrs(st.ctx, slog.LevelError, "error after response started", st.logAttrs(err, 0, false)...)
		st.span.RecordError(spanError(err, st.rw.status))
		return
	}
	st.respond(w, err)
}

// respond classifies err, writes the answer to dst and records it. It returns the status
// written, or 0 when nothing was written.
func (st *reqState) respond(dst http.ResponseWriter, err error) int {
	o := st.s.classify(err)
	if o.quiet {
		st.span.RecordError(spanError(err, 0))
		st.abandoned = true
		st.s.log.LogAttrs(st.ctx, slog.LevelDebug, "the client went away before the request finished",
			st.logAttrs(err, 0, false)...)
		return 0
	}
	ref := ""
	if v, ok := telemetry.RefFrom(st.ctx); ok {
		ref = v.String()
	}
	if werr := writeProblem(dst, problemFor(o, err, ref), o.extra); werr != nil {
		st.rw.noteWriteError(werr)
	}
	if st.rw.discard {
		return 0 // the cookie guard answered 500 instead; it recorded and logged that response
	}
	st.span.RecordError(spanError(err, o.status))
	st.logError(err, o, ref)
	return o.status
}

// spanError is what the span records for err. A 5xx keeps its (scrubbed) message; below
// that the message may restate the caller's input, so only the op and kind are kept.
func spanError(err error, status int) error {
	if status >= http.StatusInternalServerError {
		return err
	}
	return errs.E(opOf(err), errs.KindOf(err), errors.New("request refused"))
}

// logError writes the single log record for an error response (decision 4).
func (st *reqState) logError(err error, o outcome, ref string) {
	level := slog.LevelDebug
	switch {
	case o.status >= http.StatusInternalServerError:
		level = slog.LevelError
	case !st.rt.unmatched && !st.actor.IsZero():
		level = slog.LevelWarn
	}
	attrs := st.logAttrs(err, o.status, o.status >= http.StatusInternalServerError)
	if ref != "" {
		attrs = append(attrs, slog.String("error_ref", ref))
	}
	st.s.log.LogAttrs(st.ctx, level, "request failed", attrs...)
}

// logAttrs are the fields every error record carries. error.message only for a 5xx, whose
// text names no input; never for a 4xx.
func (st *reqState) logAttrs(err error, status int, withMessage bool) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("error.kind", errs.KindOf(err).String()),
		slog.String("op", opOf(err)),
		slog.String("http.route", st.rt.token),
		slog.String("http.request.method", st.method),
		slog.Int("status", status),
		slog.Int64("duration_ms", st.s.opts.Clock.Since(st.start).Milliseconds()),
	}
	if !st.actor.IsZero() {
		attrs = append(attrs, slog.String("person_id", st.actor.String()))
	}
	if withMessage {
		attrs = append(attrs, slog.String("error.message", capText(err.Error())))
	}
	return attrs
}

// panicError is the cause recorded for a recovered panic.
type panicError struct{ v any }

func (p panicError) Error() string { return fmt.Sprintf("panic: %v", p.v) }

// handlePanic maps a panic recovered around a handler: the span records it, a 500 is
// written if nothing was, and the error record carries the stack.
func (st *reqState) handlePanic(rec any) {
	err := errs.E("api.panic", errs.Internal, panicError{rec})
	st.span.RecordError(err)
	wrote := st.rw.wroteHeader
	var ref string
	if v, ok := telemetry.RefFrom(st.ctx); ok {
		ref = v.String()
	}
	if !wrote {
		o := outcome{status: http.StatusInternalServerError, code: errs.Internal.String()}
		if werr := writeProblem(st.rw, problemFor(o, err, ref), nil); werr != nil {
			st.rw.noteWriteError(werr)
		}
	}
	attrs := st.logAttrs(err, http.StatusInternalServerError, true)
	attrs = append(attrs, slog.String("stack", string(debug.Stack())))
	if ref != "" {
		attrs = append(attrs, slog.String("error_ref", ref))
	}
	st.s.log.LogAttrs(st.ctx, slog.LevelError, "handler panicked", attrs...)
}
