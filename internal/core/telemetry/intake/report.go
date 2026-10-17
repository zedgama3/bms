package intake

import (
	"fmt"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Kind is what the web shell caught (D20).
type Kind string

// The report kinds.
const (
	KindCrash     Kind = "crash"     // an uncaught error
	KindRejection Kind = "rejection" // an unhandled promise rejection
	KindScreen    Kind = "screen"    // a React error boundary caught a crashed screen
	KindAPI       Kind = "api"       // an API call failed (network error or an error body)
)

// Report is one error the web shell caught. JSON field names are the wire form; unknown
// fields are ignored, so an older server accepts a newer shell.
type Report struct {
	Kind       Kind      `json:"kind"`
	Message    string    `json:"message"`               // free text; scrubbed; truncated at MaxMessageBytes
	Stack      string    `json:"stack,omitempty"`       // free text; scrubbed; truncated at MaxStackBytes
	Screen     string    `json:"screen,omitempty"`      // the route pattern, e.g. "/people/{id}"; a Token
	OccurredAt time.Time `json:"occurred_at"`           // RFC 3339; within OccurredWithin of now
	Count      int       `json:"count,omitempty"`       // how many times the shell saw it before sending; 0 means 1
	AppVersion string    `json:"app_version,omitempty"` // the web bundle's version; a Token of at most 64 bytes
	ErrorRef   string    `json:"error_ref,omitempty"`   // the Ref from a failed call's error body
	TraceID    string    `json:"trace_id,omitempty"`    // the traceparent trace ID the shell sent with that call; 32 hex
	API        *APICall  `json:"api,omitempty"`         // required when Kind is api; otherwise must be absent
}

// APICall describes the failed call for KindAPI. Never the URL, the query string, the
// request or response body.
type APICall struct {
	Method string `json:"method"` // GET, POST, PUT, PATCH, DELETE
	Route  string `json:"route"`  // the route pattern the shell called; a Token
	Status int    `json:"status"` // 0 for a network failure, else 100..599
	Code   string `json:"code"`   // the error body's code (an errs.Kind name) or ""
}

// Batch is the request body of POST /api/client-errors.
type Batch struct {
	Reports []Report `json:"reports"`
}

// codes of the field errors intake adds beyond lib/validate's.
const (
	codeMalformed = "malformed"
	codeUnexpect  = "unexpected"
)

// Validate checks shape (F38–F44) and fails with errs.Invalid joining a validate.FieldError
// per problem, with the field names the batch form gives them minus the "reports[i]."
// prefix. It does not truncate (Accept does that after Scrub) and does not compare
// occurred_at with the clock (Accept does); a zero occurred_at fails here.
func (r Report) Validate() error {
	acc := validate.Errors{Op: "intake.Report.Validate"}
	r.check(func(field, code, reason string) { acc.Addf(field, code, "%s", reason) })
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem, fields
// "reports" and "reports[i].<field>" (F37–F44).
func (b Batch) Validate() error {
	acc := validate.Errors{Op: "intake.Batch.Validate"}
	if n := len(b.Reports); n < 1 || n > MaxReports {
		acc.Add(validate.Range("reports", n, 1, MaxReports))
	}
	for i, r := range b.Reports {
		prefix := fmt.Sprintf("reports[%d].", i)
		r.check(func(field, code, reason string) { acc.Addf(prefix+field, code, "%s", reason) })
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// check reports every shape problem of r through add.
func (r Report) check(add func(field, code, reason string)) {
	switch r.Kind {
	case KindCrash, KindRejection, KindScreen, KindAPI:
	default:
		add("kind", validate.CodeOneOf, "must be one of: crash, rejection, screen, api")
	}
	if strings.TrimSpace(r.Message) == "" {
		add("message", validate.CodeRequired, "must not be empty")
	}
	if r.Screen != "" && !telemetry.IsToken(r.Screen) {
		add("screen", codeMalformed, "must be a route pattern")
	}
	if r.OccurredAt.IsZero() {
		add("occurred_at", validate.CodeRequired, "must be set")
	}
	if r.Count < 0 || r.Count > MaxCount {
		add("count", validate.CodeRange, fmt.Sprintf("must be between 0 and %d", MaxCount))
	}
	if r.AppVersion != "" && (len(r.AppVersion) > 64 || !telemetry.IsToken(r.AppVersion)) {
		add("app_version", codeMalformed, "must be a token of at most 64 bytes")
	}
	if r.ErrorRef != "" && telemetry.Ref(r.ErrorRef).Validate() != nil {
		add("error_ref", codeMalformed, "must be XXXX-XXXX in upper-case hexadecimal")
	}
	if r.TraceID != "" && !hex32(r.TraceID) {
		add("trace_id", codeMalformed, "must be 32 lower-case hexadecimal digits")
	}
	switch {
	case r.Kind == KindAPI && r.API == nil:
		add("api", validate.CodeRequired, "must be set when kind is api")
	case r.Kind != KindAPI && r.API != nil:
		add("api", codeUnexpect, "must be absent unless kind is api")
	case r.API != nil:
		r.API.check(add)
	}
}

func (c APICall) check(add func(field, code, reason string)) {
	switch c.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		add("api.method", validate.CodeOneOf, "must be one of: GET, POST, PUT, PATCH, DELETE")
	}
	if !telemetry.IsToken(c.Route) {
		add("api.route", codeMalformed, "must be a route pattern")
	}
	if c.Status != 0 && (c.Status < 100 || c.Status > 599) {
		add("api.status", validate.CodeRange, "must be 0 or between 100 and 599")
	}
	if c.Code != "" && !telemetry.IsToken(c.Code) {
		add("api.code", codeMalformed, "must be a token or empty")
	}
}

func hex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
