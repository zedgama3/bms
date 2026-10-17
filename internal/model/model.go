// Package model is the vocabulary every layer shares: record IDs, sensitivity levels and a
// few small value types. It performs no I/O and depends only on the standard library and
// internal/lib.
package model

import (
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// fieldErr builds an errs.Invalid error wrapping one validate.FieldError. The reason is
// built from the rule, never from the input (decision 12).
func fieldErr(op, field, code string, params map[string]string, format string, args ...any) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{
		Field: field, Code: code, Params: params, Reason: fmt.Sprintf(format, args...),
	})
}
