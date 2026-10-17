// Package validate provides a handful of input checks that return errs.Invalid naming the
// field, plus an accumulator that reports every problem at once. Each model type and the
// config package write plain Validate() methods that call these helpers.
// It depends only on the standard library and lib/errs.
// Ported from Sojourn (D11).
package validate

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// FieldError is one problem with one input field. Every check in this package returns
// errs.E("validate.<Check>", errs.Invalid, &FieldError{...}), so callers branch with
// errs.Is and the api ErrorMapper extracts fields with errors.As or Fields. Reason is
// safe to show to a user: it never echoes string input (which may be a secret or a database URL);
// it may echo numbers and durations. Code and Params exist so the web shell can translate
// the problem (api.md decision 25): Code is a stable identifier (the Code* constants or a
// caller's own), Params the rule's parameters as strings, never the input.
type FieldError struct {
	Field  string            // the caller's full dotted path: "db.url", "smtp.host"
	Code   string            // a Code* constant or a caller's own; "" when a caller set none
	Params map[string]string // the rule's parameters (Range min/max, OneOf options, URL schemes); nil otherwise
	Reason string            // what is wrong, in English: "must be positive, got -1s"
}

// The codes this package sets, and the one it reserves. A code is a stable API string
// (the web shell translates it under the key field.<code>).
const (
	CodeRequired  = "required"  // NonEmpty
	CodeRange     = "range"     // Range; Params min, max
	CodePositive  = "positive"  // Positive
	CodeOneOf     = "one_of"    // OneOf; Params options
	CodeURL       = "url"       // URL; Params schemes when schemes were given
	CodeHostPort  = "host_port" // HostPort, ListenAddr
	CodeMalformed = "malformed" // reserved for decoders: a body that is not the expected JSON
)

// Error returns "<Field>: <Reason>". Cannot fail.
func (e *FieldError) Error() string {
	if e == nil {
		return "invalid field"
	}
	return e.Field + ": " + e.Reason
}

// Fields returns every *FieldError in err's tree, depth-first, left to right, descending
// through Unwrap() error and Unwrap() []error. nil or no matches returns an empty slice.
// Cannot fail.
func Fields(err error) []*FieldError {
	out := []*FieldError{}
	collect(err, &out)
	return out
}

func collect(err error, out *[]*FieldError) {
	if err == nil {
		return
	}
	if fe, ok := err.(*FieldError); ok { //nolint:errorlint // walking the tree by hand
		if fe != nil {
			*out = append(*out, fe)
		}
		return
	}
	switch u := err.(type) { //nolint:errorlint // walking the tree by hand
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			collect(e, out)
		}
	case interface{ Unwrap() error }:
		collect(u.Unwrap(), out)
	}
}

func invalid(op, field, code string, params map[string]string, format string, args ...any) error {
	return errs.E(op, errs.Invalid, &FieldError{
		Field: field, Code: code, Params: params, Reason: fmt.Sprintf(format, args...),
	})
}

// NonEmpty fails if strings.TrimSpace(value) == "". Code "required".
func NonEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return invalid("validate.NonEmpty", field, CodeRequired, nil, "must not be empty")
	}
	return nil
}

// Range fails if v < lo or v > hi (inclusive bounds). Works for ints, floats, durations.
// Code "range", Params {"min": lo, "max": hi} formatted with %v.
func Range[T cmp.Ordered](field string, v, lo, hi T) error {
	// Written so a NaN float fails too.
	if v >= lo && v <= hi {
		return nil
	}
	params := map[string]string{"min": fmt.Sprintf("%v", lo), "max": fmt.Sprintf("%v", hi)}
	// Never echo string input: it may be a secret. Numbers and durations are safe.
	if reflect.ValueOf(v).Kind() == reflect.String {
		return invalid("validate.Range", field, CodeRange, params, "must be between %v and %v", lo, hi)
	}
	return invalid("validate.Range", field, CodeRange, params, "must be between %v and %v, got %v", lo, hi, v)
}

// Positive fails if d <= 0. Code "positive".
func Positive(field string, d time.Duration) error {
	if d <= 0 {
		return invalid("validate.Positive", field, CodePositive, nil, "must be positive, got %s", d)
	}
	return nil
}

// OneOf fails if value is not exactly one of allowed. Fails with errs.Internal if allowed
// is empty (a caller bug, not bad input). Code "one_of", Params {"options": allowed
// joined with ", "}.
func OneOf(field, value string, allowed ...string) error {
	if len(allowed) == 0 {
		return errs.E("validate.OneOf", errs.Internal,
			fmt.Errorf("%s: no allowed values given", field))
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	options := strings.Join(allowed, ", ")
	return invalid("validate.OneOf", field, CodeOneOf, map[string]string{"options": options},
		"must be one of: %s", options)
}

// URL fails if value does not parse as an absolute URL with a non-empty host, or if
// schemes is non-empty and the scheme (case-insensitive) is not in it. Code "url";
// Params {"schemes": joined with ", "} when schemes were given.
func URL(field, value string, schemes ...string) error {
	const op = "validate.URL"
	var params map[string]string
	if len(schemes) > 0 {
		params = map[string]string{"schemes": strings.Join(schemes, ", ")}
	}
	u, err := url.Parse(value)
	if err != nil { // the error text echoes the input, so it is dropped
		return invalid(op, field, CodeURL, params, "must be a valid URL")
	}
	if u.Scheme == "" || u.Hostname() == "" {
		return invalid(op, field, CodeURL, params, "must be an absolute URL with a scheme and host")
	}
	if len(schemes) > 0 {
		for _, s := range schemes {
			if strings.EqualFold(u.Scheme, s) {
				return nil
			}
		}
		return invalid(op, field, CodeURL, params, "URL scheme must be one of: %s", params["schemes"])
	}
	return nil
}

// HostPort fails unless value is "host:port" or "[v6]:port" with a non-empty host and a
// numeric port in 1..65535. Code "host_port".
func HostPort(field, value string) error {
	return hostPort("validate.HostPort", field, value, false)
}

// ListenAddr is HostPort with an empty host allowed (":8080" means all interfaces). Code
// "host_port".
func ListenAddr(field, value string) error {
	return hostPort("validate.ListenAddr", field, value, true)
}

func hostPort(op, field, value string, allowEmptyHost bool) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil { // the error text echoes the input, so it is dropped
		return invalid(op, field, CodeHostPort, nil, "must be host:port")
	}
	if host == "" && !allowEmptyHost {
		return invalid(op, field, CodeHostPort, nil, "host must not be empty")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || port[0] == '+' || port[0] == '-' {
		return invalid(op, field, CodeHostPort, nil, "port must be a number between 1 and 65535")
	}
	return nil
}

// Errors accumulates problems. The zero value is ready to use. Not safe for concurrent use.
type Errors struct {
	Op       string // operation for errs.E; "validate" if empty
	problems []error
}

// Add records err; a nil err is ignored. Cannot fail.
func (e *Errors) Add(err error) {
	if err != nil {
		e.problems = append(e.problems, err)
	}
}

// Addf records errs.Invalid wrapping &FieldError{Field: field, Code: code, Reason:
// fmt.Sprintf(format, args...)} with no Params. code is a Code* constant or a caller's own
// stable string; "" means the web shell shows the reason. The problem carries no op of its
// own (Err adds the accumulator's op once), so messages read "<Op>: <field>: <reason>".
// Cannot fail.
func (e *Errors) Addf(field, code, format string, args ...any) {
	e.problems = append(e.problems, errs.E("", errs.Invalid,
		&FieldError{Field: field, Code: code, Reason: fmt.Sprintf(format, args...)}))
}

// Err returns nil if nothing was added, otherwise
// errs.E(Op, errs.Invalid, errors.Join(problems...)). Cannot fail.
func (e *Errors) Err() error {
	if len(e.problems) == 0 {
		return nil
	}
	return errs.E(e.op(), errs.Invalid, errors.Join(e.problems...))
}

func (e *Errors) op() string {
	if e.Op == "" {
		return "validate"
	}
	return e.Op
}
