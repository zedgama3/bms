package module

import (
	"net/http"

	"github.com/zedgama3/bms/internal/lib/validate"
)

// RouteProvider is implemented by a module with an HTTP API. The api shell mounts every
// route under /api/<name>/ with its pipeline (authentication per Access, cross-origin
// protection for unsafe methods, the request span) and the error mapper; handlers use
// httpx.ReadJSON, call the service, httpx.WriteJSON, and httpx.Fail on error
// (add-module). Permission checks happen in the service, never here. A RouteProvider
// must also implement APIDescriber: api requires the fragment (api.md decision 11; F34).
type RouteProvider interface {
	// Routes returns the module's routes. Cannot fail: handlers are built already. Build
	// validates each route (F33) and keeps the list; api reads it from Set.Routes.
	Routes() []Route
}

// Route is one HTTP route relative to /api/<name>. api mounts it as
// "<Method> /api/<name><Pattern>" (api.md decision 3): a Pattern ending in "/" is mounted
// exactly ("{$}" appended), so "/" answers only /api/<name>/; a module that wants a
// subtree says "{path...}". The mounted pattern must fit telemetry's token grammar
// ("[A-Za-z0-9/_][A-Za-z0-9._:/{}-]*"; Validate enforces the character set, so a module
// learns in its own tests) and the whole span name "<Method> /api/<name><Pattern>" must
// be at most 128 bytes (api F48). A GET or HEAD route never changes state: the
// cross-origin check does not cover it and browsers prefetch it.
type Route struct {
	Method  string       // "GET", "POST", "PUT", "PATCH" or "DELETE"
	Pattern string       // net/http ServeMux path syntax, starting with "/", e.g. "/shifts/{id}"
	Access  Access       // who may reach it before any permission check
	Handler http.Handler // must not be nil
}

// Access says whether a route needs a signed-in person. The zero value is the safe one.
type Access uint8

// The route access levels.
const (
	AccessSignedIn Access = iota // a valid session is required (default)
	AccessPublic                 // reachable without a session, e.g. a published camp list
)

// Validate fails with errs.Invalid joining one validate.FieldError per problem, on the
// fields "method", "pattern", "access" and "handler": Method is not one of the five,
// Pattern does not start with "/" or contains a byte outside "[A-Za-z0-9._:/{}$-]" (so no
// space, "%", "+", "," or ";"; "$" only for ServeMux's "{$}"), Access is out of range, or
// Handler is nil.
func (r Route) Validate() error {
	acc := validate.Errors{Op: "module.Route.Validate"}
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		acc.Addf("method", validate.CodeOneOf, "must be one of: GET, POST, PUT, PATCH, DELETE")
	}
	switch {
	case len(r.Pattern) == 0 || r.Pattern[0] != '/':
		acc.Addf("pattern", codeMalformed, `must start with "/"`)
	case !routeBytesOK(r.Pattern):
		acc.Addf("pattern", codeMalformed, "may contain only A-Z a-z 0-9 . _ : / { } $ and -")
	}
	if r.Access != AccessSignedIn && r.Access != AccessPublic {
		acc.Addf("access", validate.CodeOneOf, "must be signed-in or public")
	}
	if r.Handler == nil {
		acc.Addf("handler", validate.CodeRequired, "must not be nil")
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func routeBytesOK(p string) bool {
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == ':', c == '/', c == '{', c == '}', c == '$', c == '-':
		default:
			return false
		}
	}
	return true
}
