// Package httpx is the HTTP vocabulary shared by the api shell and every handler in the
// core and the modules: the problem body, the kind-to-status table, and the three calls a
// handler makes (ReadJSON, WriteJSON, Fail). It imports the standard library, lib/errs and
// lib/validate only, so a module may use it without importing the api shell (layer rules 3
// and 4). It holds no state.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// ContentTypeProblem is the media type of every error response (RFC 9457).
const ContentTypeProblem = "application/problem+json"

// Problem is the body of every error response. It carries no prose the client must
// parse: the web shell translates Code and a field's code; a field's Reason is the
// validate.FieldError text, shown only when the shell knows no translation for the code.
// Nothing in it is personal data: reasons never echo string input and Params carry the
// rule's parameters, never the input.
type Problem struct {
	Status int     `json:"status"`           // the HTTP status, repeated for clients that lose it
	Code   string  `json:"code"`             // an errs.Kind name, or one of the shell codes
	Ref    string  `json:"ref,omitempty"`    // telemetry.Ref "XXXX-XXXX"; absent when no trace
	Fields []Field `json:"fields,omitempty"` // for code "invalid": one per validate.FieldError, in order
}

// Field is one validation problem, from validate.FieldError.
type Field struct {
	Field  string            `json:"field"`            // the dotted path the service used: "email", "shift.start"
	Code   string            `json:"code"`             // validate.FieldError.Code; "" when a check set none
	Params map[string]string `json:"params,omitempty"` // validate.FieldError.Params: the rule's parameters, never the input
	Reason string            `json:"reason"`           // the rule in English, never the value; the shell's fallback
}

// Shell codes that are not errs.Kind names. The strings are stable API, pinned by a test.
const (
	CodeReadOnly             = "read_only"              // 503: db.ErrReadOnly
	CodeRateLimited          = "rate_limited"           // 429: ErrRateLimited (intake and identity)
	CodeShuttingDown         = "shutting_down"          // 503: canceled by shutdown, client still connected
	CodePayloadTooLarge      = "payload_too_large"      // 413: *http.MaxBytesError
	CodeUnsupportedMediaType = "unsupported_media_type" // 415: ErrUnsupportedMediaType
	CodeCrossOrigin          = "cross_origin"           // 403: an unsafe request from another origin
	CodeMisdirected          = "misdirected"            // 403: a direct request from an untrusted peer
	CodeMethodNotAllowed     = "method_not_allowed"     // 405: the path exists, the method does not
	CodeNotFound             = "not_found"              // 404: also errs.NotFound
)

// ErrRateLimited is wrapped (errs.Unavailable) by every rejection for rate: the client-error
// intake's and identity's login limiters. The mapper answers 429 rate_limited with
// Retry-After: 60. Check it with errors.Is before the kind, as with db.ErrReadOnly.
var ErrRateLimited = errors.New("too many requests")

// ErrUnsupportedMediaType is wrapped (errs.Invalid) by ReadJSON when the request's
// Content-Type is not JSON; the mapper answers 415. Check it with errors.Is before the kind.
var ErrUnsupportedMediaType = errors.New("request body is not application/json")

// StatusOf is the one kind-to-status table (go-conventions "Errors"): Invalid 400,
// Unauthenticated 401, Permission 403, NotFound 404, Conflict 409, Unavailable 503,
// Timeout 504, Canceled 503, Unsupported 501, Internal 500; a value outside the set is
// 500. Cannot fail.
func StatusOf(k errs.Kind) int {
	switch k {
	case errs.Invalid:
		return http.StatusBadRequest
	case errs.Unauthenticated:
		return http.StatusUnauthorized
	case errs.Permission:
		return http.StatusForbidden
	case errs.NotFound:
		return http.StatusNotFound
	case errs.Conflict:
		return http.StatusConflict
	case errs.Unavailable, errs.Canceled:
		return http.StatusServiceUnavailable
	case errs.Timeout:
		return http.StatusGatewayTimeout
	case errs.Unsupported:
		return http.StatusNotImplemented
	case errs.Internal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// Failer is what the api shell installs in every request's ctx (WithFailer) so that Fail
// reaches the shell's mapper, which adds the reference, recognises the sentinels, marks the
// span and writes the log record. Modules never implement it.
type Failer interface {
	Fail(w http.ResponseWriter, r *http.Request, err error)
}

type (
	failerKey     struct{}
	clientAddrKey struct{}
	schemeKey     struct{}
)

// WithFailer returns ctx carrying f. Consumer: the api shell. Cannot fail.
func WithFailer(ctx context.Context, f Failer) context.Context {
	return context.WithValue(ctx, failerKey{}, f)
}

// Fail answers r with the error response for err and returns; the handler returns right
// after it. With a Failer in r.Context() (every request through the api shell) it
// delegates to it. Without one (a handler under test) it writes a Problem with
// StatusOf(errs.KindOf(err)), the kind's name as code, Fields from validate.Fields, no
// ref, plus 413 for a *http.MaxBytesError and 415 for ErrUnsupportedMediaType, so a
// module's unit test sees the same status the shell would send. A nil err is a caller bug
// and is written as 500 internal. Call it before writing anything to w. Cannot fail.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	if f, ok := r.Context().Value(failerKey{}).(Failer); ok && f != nil {
		f.Fail(w, r, err)
		return
	}
	p := fallbackProblem(err)
	body, merr := json.Marshal(p)
	if merr != nil { // unreachable: Problem holds strings, ints and string maps
		body = []byte(`{"status":500,"code":"internal"}`)
		p.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", ContentTypeProblem)
	w.WriteHeader(p.Status)
	// A failed write means the client is gone; the shell's wrapper records it, and a
	// handler under test has nothing to do about it.
	if _, werr := w.Write(body); werr != nil {
		return
	}
}

func fallbackProblem(err error) Problem {
	if err == nil {
		return Problem{Status: http.StatusInternalServerError, Code: errs.Internal.String()}
	}
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, ErrUnsupportedMediaType):
		return Problem{Status: http.StatusUnsupportedMediaType, Code: CodeUnsupportedMediaType}
	case errors.As(err, &tooBig):
		return Problem{Status: http.StatusRequestEntityTooLarge, Code: CodePayloadTooLarge}
	}
	kind := errs.KindOf(err)
	p := Problem{Status: StatusOf(kind), Code: kind.String()}
	if kind == errs.Invalid {
		for _, fe := range validate.Fields(err) {
			p.Fields = append(p.Fields, Field{Field: fe.Field, Code: fe.Code, Params: fe.Params, Reason: fe.Reason})
		}
	}
	return p
}

// ClientAddrFrom returns the client address the shell derived for this request and false
// when ctx carries none (a handler run outside the shell). It is personal data: use it for
// a rate limit, never log it. Cannot fail.
func ClientAddrFrom(ctx context.Context) (netip.Addr, bool) {
	a, ok := ctx.Value(clientAddrKey{}).(netip.Addr)
	return a, ok && a.IsValid()
}

// SchemeFrom returns the effective scheme, "http" or "https", the shell derived, and false
// when ctx carries none. Cannot fail.
func SchemeFrom(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(schemeKey{}).(string)
	return s, ok && s != ""
}

// WithClientAddr returns ctx carrying the client address. Consumers: the api shell;
// tests of a handler that reads it. Cannot fail.
func WithClientAddr(ctx context.Context, a netip.Addr) context.Context {
	return context.WithValue(ctx, clientAddrKey{}, a)
}

// WithScheme returns ctx carrying the effective scheme. Consumers: the api shell; tests of
// a handler that reads it. Cannot fail.
func WithScheme(ctx context.Context, scheme string) context.Context {
	return context.WithValue(ctx, schemeKey{}, scheme)
}
