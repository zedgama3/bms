package notify

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The field codes notify adds to the shell's vocabulary (validate has the rest).
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeOneOf     = validate.CodeOneOf
	codeTooLong   = "too_long"      // params: max
	codeTooMany   = "too_many"      // params: max
	codeControl   = "control_chars" // a control character
	codeDuplicate = "duplicate"
	codeUnknown   = "unknown" // a template nobody registered
	codeScope     = "scope"   // another owner's template
	codeParse     = "parse"   // a template that does not parse or render
	codeConflict  = "conflict"
)

// fieldError returns an errs.Invalid error carrying one FieldError.
func fieldError(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// addField records one problem with params on v.
func addField(v *validate.Errors, field, code string, params map[string]string, format string, args ...any) {
	v.Add(errs.E("", errs.Invalid, &validate.FieldError{
		Field: field, Code: code, Params: params, Reason: fmt.Sprintf(format, args...),
	}))
}

func maxParam(n int) map[string]string { return map[string]string{"max": strconv.Itoa(n)} }

// seamError reports a failure of a seam (settings, people, identity) with its kind but not
// its text, which could carry an address or a name the seam looked up. Cancellation and
// deadline stay visible to errors.Is.
func seamError(op, seam string, err error) error {
	if err == nil {
		return nil
	}
	cause := errors.New(seam + " failed")
	switch {
	case errors.Is(err, context.Canceled):
		cause = fmt.Errorf("%s failed: %w", seam, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		cause = fmt.Errorf("%s failed: %w", seam, context.DeadlineExceeded)
	}
	return errs.E(op, errs.KindOf(err), cause)
}

// channelError reports a failure of the channel or its factory with its kind and, when it
// has one, its sentinel (ErrRejected, ErrAuth, ErrTemporary), but not its text, which could
// echo the address, the subject or the body.
func channelError(op string, err error) error {
	if err == nil {
		return nil
	}
	cause := errors.New("the channel failed")
	for _, s := range []error{ErrRejected, ErrAuth, ErrTemporary, ErrNotConfigured, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, s) {
			cause = s
			break
		}
	}
	return errs.E(op, errs.KindOf(err), cause)
}

// wrap adds op to err and keeps its kind and chain. nil stays nil.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
