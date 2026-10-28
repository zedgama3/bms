// Package rules holds what jobs and jobstest share, so the real runner and its fake apply
// one rule: the marker a handler's context carries and the scope check. It imports lib only.
package rules

import (
	"context"
	"strings"

	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

type inJobKey struct{}

// runIDKey carries the ID of the row whose run the ctx belongs to.
type runIDKey struct{}

// WithJob returns a child of ctx that InJob reports true for. A non-zero id is the row the
// run belongs to, which RunID reports; Enqueue uses it to tell a request the run made for its
// own key from another caller's (D52 a).
func WithJob(ctx context.Context, id rows.ID) context.Context {
	ctx = context.WithValue(ctx, inJobKey{}, true)
	if id.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, runIDKey{}, id)
}

// InJob reports whether ctx is a handler's ctx. Cannot fail.
func InJob(ctx context.Context) bool {
	v, ok := ctx.Value(inJobKey{}).(bool)
	return ok && v
}

// RunID returns the ID of the row whose run ctx belongs to, and false outside a run (or when
// the run has no row). Cannot fail.
func RunID(ctx context.Context) (rows.ID, bool) {
	id, ok := ctx.Value(runIDKey{}).(rows.ID)
	return id, ok
}

// CodeScope is the field code of a name outside the caller's scope.
const CodeScope = "scope"

// Owner returns the part of name before the first dot, "" when there is none.
func Owner(name string) string {
	owner, _, found := strings.Cut(name, ".")
	if !found {
		return ""
	}
	return owner
}

// Scope fails errs.Invalid (field "name", code "scope") unless name belongs to owner. op names
// the caller.
func Scope(op, owner, name string) error {
	if Owner(name) == owner {
		return nil
	}
	return errs.E(op, errs.Invalid, &validate.FieldError{
		Field: "name", Code: CodeScope,
		Reason: "this scope may use only names that begin with \"" + owner + ".\"",
	})
}
