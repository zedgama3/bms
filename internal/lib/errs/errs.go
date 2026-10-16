// Package errs classifies errors by kind so callers decide by kind, never by message.
// It depends only on the standard library.
// Ported from Sojourn (D11).
package errs

import (
	"context"
	"errors"
	"strconv"
)

// Kind is the category of a failure. The zero value is Internal, so an unclassified
// error is treated as a bug rather than as something the caller did wrong.
type Kind uint8

// The closed set of error kinds. Adding or renaming one is a breaking change to the API
// error codes, because Kind.String is used as the code.
const (
	Internal        Kind = iota // a bug or broken invariant; the default when nothing better is known
	Invalid                     // caller gave bad input or configuration
	Unauthenticated             // no or bad identity
	Permission                  // the person lacks the permission for this action or record
	NotFound                    // the thing doesn't exist
	Conflict                    // state conflict or duplicate
	Unavailable                 // dependency down or unreachable; retrying may help
	Timeout                     // a deadline passed before the operation finished
	Canceled                    // the caller gave up (context canceled); retrying is wrong
	Unsupported                 // feature or module turned off in this deployment
)

// kindNames holds the stable lower_snake_case name of each defined kind, indexed by Kind.
var kindNames = [...]string{
	Internal:        "internal",
	Invalid:         "invalid",
	Unauthenticated: "unauthenticated",
	Permission:      "permission",
	NotFound:        "not_found",
	Conflict:        "conflict",
	Unavailable:     "unavailable",
	Timeout:         "timeout",
	Canceled:        "canceled",
	Unsupported:     "unsupported",
}

// defined reports whether k is one of the ten defined kinds.
func (k Kind) defined() bool {
	return int(k) < len(kindNames)
}

// String returns the kind's stable lower_snake_case name, for log fields and API error
// codes: "internal", "invalid", "unauthenticated", "permission", "not_found", "conflict",
// "unavailable", "timeout", "canceled", "unsupported". A value outside the defined set
// returns "Kind(<n>)". It never panics.
func (k Kind) String() string {
	if !k.defined() {
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
	return kindNames[k]
}

// Error is an error with an operation name, a kind and an optional cause.
// Build it with E rather than a literal; E normalises the kind and drops nil causes.
type Error struct {
	Op   string // where it happened: "pkg.Type.Method" or "pkg.Func", e.g. "volunteers.postgres.CreateShift"
	Kind Kind   // the classification; read it through KindOf, which also handles unkinded errors
	Err  error  // the cause; nil only when built as a literal
}

// Error returns "<Op>: <Err>". If Op is empty the prefix is omitted. If Err is nil the
// kind's name stands in for the cause. Because Err may itself be an *Error, messages chain
// naturally: "volunteers.Service.Enroll: volunteers.postgres.CreateShift: dial tcp ...: connection refused".
// The kind is not repeated in the text; log it as a structured field instead. It never
// panics, including on a nil receiver.
func (e *Error) Error() string {
	if e == nil {
		return Internal.String()
	}
	cause := e.Kind.String()
	if e.Err != nil {
		cause = e.Err.Error()
	}
	if e.Op == "" {
		return cause
	}
	return e.Op + ": " + cause
}

// Unwrap returns the cause so errors.Is and errors.As see through this error.
// It returns nil for a nil receiver or when there is no cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// E wraps err with an operation name and a kind.
//
// It returns nil when err is nil, so `return errs.E(op, kind, f())` is correct on success.
// A kind outside the defined set is recorded as Internal. The returned error's KindOf is
// kind, even when err already carries a different kind (the outer decision wins).
func E(op string, kind Kind, err error) error {
	if err == nil {
		return nil
	}
	if !kind.defined() {
		kind = Internal
	}
	return &Error{Op: op, Kind: kind, Err: err}
}

// KindOf reports the kind of err. It walks the error tree depth-first, left to right (the
// same order errors.Is uses) and returns the kind of the first *Error it meets. That first
// *Error decides even when it is unusable: a typed-nil *Error, or one whose Kind is outside
// the defined set, reports Internal and later kinded errors are not consulted. If there is
// no *Error: context.Canceled anywhere in the tree gives Canceled, context.DeadlineExceeded
// gives Timeout, and anything else gives Internal. When both sentinels appear, Canceled
// wins, because retrying abandoned work is never right. KindOf(nil) is Internal, because
// asking the kind of a nil error is itself a broken invariant; check err != nil first.
func KindOf(err error) Kind {
	if err == nil {
		return Internal
	}
	var e *Error
	// errors.As visits the tree in the same order as errors.Is and stops at the first
	// *Error, which may be a typed nil.
	if errors.As(err, &e) {
		if e == nil || !e.Kind.defined() {
			return Internal
		}
		return e.Kind
	}
	switch {
	case errors.Is(err, context.Canceled):
		return Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return Timeout
	default:
		return Internal
	}
}

// Is reports whether KindOf(err) == kind. It is false for a nil err regardless of kind,
// so it is safe to call before the nil check. Use it in place of comparing messages.
func Is(err error, kind Kind) bool {
	if err == nil {
		return false
	}
	return KindOf(err) == kind
}
