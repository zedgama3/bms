package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

// unmatchedRoute is the route token of a request no route matched: one series, not one per
// probed path.
const unmatchedRoute = "/{unmatched}"

// route is one mounted pattern with what the per-route layers need.
type route struct {
	set       string // the set that owns it, for messages
	pattern   string // as registered with the mux: "GET /api/alpha/{$}", or "/api/"
	token     string // the route token: the path with a trailing {$} rendered as /
	access    module.Access
	handler   http.Handler
	unmatched bool // the fallback: its 4xx are debug noise
}

// routeLimits are the per-route raises of the body cap and the request deadline
// (module.Route.MaxBodyBytes and Timeout). A zero field keeps the shell's value.
type routeLimits struct {
	maxBody int64
	timeout time.Duration
}

// actorKey carries the authenticated person in ctx for the shell's own handlers. The
// principal's form for services is identity's; this one is private to api.
type actorKey struct{}

func actorFrom(ctx context.Context) model.PersonID {
	if p, ok := ctx.Value(actorKey{}).(model.PersonID); ok {
		return p
	}
	return model.PersonID{}
}

// knownMethods are the methods the shell lets reach a span, a metric or a log.
var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true,
}

// otherMethod stands for any method outside knownMethods (OpenTelemetry's _OTHER). The
// original method is never recorded.
const otherMethod = "_OTHER"

func normalizeMethod(m string) string {
	if knownMethods[m] {
		return m
	}
	return otherMethod
}

// spanName is "<METHOD> <route token>", or "HTTP <route token>" for an unknown method.
func spanName(method, token string) string {
	if method == otherMethod {
		return "HTTP " + token
	}
	return method + " " + token
}

// reqState is one request's view of the per-route layers. It is the httpx.Failer.
type reqState struct {
	s         *Server
	rt        *route
	method    string          // normalised
	ctx       context.Context //nolint:containedctx // one request's context, used for logs and the reference
	span      telemetry.Span
	rw        *respWriter
	actor     model.PersonID
	start     time.Time
	abandoned bool // the client went away; nothing was written
}

// serveRoute runs the per-route layers around rt.handler: span and histogram, the mapper,
// inner recovery, the cookie guard, cross-origin protection and authentication.
func (s *Server) serveRoute(rt *route, w http.ResponseWriter, r *http.Request) {
	rw, ok := w.(*respWriter)
	if !ok { // a route reached without the pipeline: a bug in the router
		rw = &respWriter{ResponseWriter: w, s: s, scheme: "http"}
	}
	method := normalizeMethod(r.Method)
	start := s.opts.Clock.Now()
	ctx, span := s.opts.Tracer.Start(r.Context(), spanName(method, rt.token),
		telemetry.WithKind(telemetry.SpanServer),
		telemetry.WithParentFrom(r.Header),
		telemetry.WithAttrs(
			telemetry.Token("http.request.method", method),
			telemetry.Token("http.route", rt.token),
		))
	st := &reqState{s: s, rt: rt, method: method, span: span, rw: rw, start: start}
	if id, ok := telemetry.TraceIDFrom(ctx); ok {
		rw.Header().Set("X-Trace-Id", id)
	}
	ctx = httpx.WithFailer(ctx, st)
	st.ctx = ctx
	r = r.WithContext(ctx)
	rw.failCookie = st.cookieFailed

	defer func() {
		rec := recover()
		st.complete(rec)
		if rec == http.ErrAbortHandler { //nolint:errorlint // net/http compares the panic value itself
			panic(rec)
		}
	}()

	r, proceed := st.layers(r) // cross-origin protection, then authentication on every routed request
	if !proceed {
		return
	}
	rt.handler.ServeHTTP(rw, r)
}

// layers runs cross-origin protection and authentication. It reports false when it
// answered the request.
func (st *reqState) layers(r *http.Request) (*http.Request, bool) {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if err := st.s.cop.Check(r); err != nil {
			st.Fail(st.rw, r, shell("api.crossOrigin", errs.Permission, http.StatusForbidden,
				httpx.CodeCrossOrigin, "cross-origin request refused", nil))
			return r, false
		}
	}
	return st.authenticate(r)
}

// authenticate implements decision 5. A public route treats an invalid session as none; a
// signed-in route answers 401 without one.
func (st *reqState) authenticate(r *http.Request) (*http.Request, bool) {
	const op = "api.authenticate"
	signedIn := st.rt.access == module.AccessSignedIn
	ctx := r.Context()
	if a := st.s.opts.Authenticator; a != nil {
		nctx, pid, err := a.Authenticate(ctx, r)
		switch {
		case err != nil:
			if signedIn {
				st.Fail(st.rw, r, err)
				return r, false
			}
			if !errs.Is(err, errs.Unauthenticated) {
				st.s.log.LogAttrs(ctx, slog.LevelWarn, "authentication failed; serving the request anonymously",
					slog.String("error.kind", errs.KindOf(err).String()),
					slog.String("op", opOf(err)),
					slog.String("http.route", st.rt.token),
				)
			}
		case nctx == nil:
			st.Fail(st.rw, r, errs.E(op, errs.Internal, errNilContext))
			return r, false
		default:
			// The returned ctx always replaces the request's, even with no person: a
			// restricted session (a temporary password) is in ctx for identity's own routes.
			ctx = nctx
			st.actor = pid
			st.ctx = ctx
			r = r.WithContext(ctx)
		}
	}
	if !st.actor.IsZero() {
		ctx = context.WithValue(ctx, actorKey{}, st.actor)
		st.span.SetAttrs(telemetry.ID("person_id", st.actor))
		st.ctx = ctx
		r = r.WithContext(ctx)
	}
	if signedIn && st.actor.IsZero() {
		st.Fail(st.rw, r, errs.E(op, errs.Unauthenticated, errors.New("a signed-in session is required")))
		return r, false
	}
	return r, true
}

// cookieFailed answers 500 internal for a response that set a cookie against the rules
// (decision 7). It writes straight to the connection: the guard is already past.
func (st *reqState) cookieFailed(name string) {
	err := errs.E("api.cookieGuard", errs.Internal,
		&cookieError{name: name})
	if status := st.respond(st.rw.ResponseWriter, err); status != 0 {
		st.rw.markSent(status)
	}
}

// cookieError names the offending cookie, never its value.
type cookieError struct{ name string }

func (e *cookieError) Error() string {
	return "a response set the cookie " + e.name + " against the cookie rules"
}

// complete closes the request: it handles a panic, runs the cookie guard for a handler
// that wrote nothing, and sets the span's result attributes, the histogram sample and End.
func (st *reqState) complete(rec any) {
	switch {
	case rec == http.ErrAbortHandler: //nolint:errorlint // net/http compares the panic value itself
		st.abandoned = true
		st.span.RecordError(errs.E("api.abort", errs.Canceled, errors.New("handler aborted the response")))
	case rec != nil:
		st.handlePanic(rec)
	default:
		st.rw.finish()
	}
	status, wrote, werr := st.rw.status, st.rw.wroteHeader, st.rw.writeErr
	switch {
	case wrote:
	case st.abandoned:
		status = statusClientClosed
	default:
		status = http.StatusOK // nothing written: net/http sends 200
	}
	d := st.s.opts.Clock.Since(st.start)
	attrs := []telemetry.Attr{
		telemetry.Int("http.response.status_code", status),
		telemetry.Duration("duration_ms", d),
	}
	if werr != nil {
		attrs = append(attrs, telemetry.Bool("http.response.aborted", true))
		kind := errs.Canceled
		if isTimeout(werr) {
			kind = errs.Timeout
		}
		// the transport's text can name the peer's address, so only the kind is kept
		st.span.RecordError(errs.E("api.respWriter.Write", kind, errors.New("response write failed")))
	}
	st.span.SetAttrs(attrs...)
	st.s.hist.Record(st.ctx, d.Seconds(),
		telemetry.Token("http.request.method", st.method),
		telemetry.Token("http.route", st.rt.token),
		telemetry.Int("http.response.status_code", status),
	)
	st.span.End()
}

// statusClientClosed is the status recorded for a request whose client left before any
// answer (nginx's 499); nothing is sent.
const statusClientClosed = 499
