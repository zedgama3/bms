package audit

import "context"

// systemKey and operatorKey are two independent markers, so the order of resolveActor does not
// depend on which was set nearer the leaf of the ctx tree.
type (
	systemKey   struct{}
	operatorKey struct{}
)

// WithSystem marks ctx as running as the system. jobs sets it on every run (beside its own
// InJob marker). A signal delivery needs no marker: audit imports signals and already
// recognises a delivery's ctx. An entry that names no actor is then recorded as the system,
// whatever session or operator marker the ctx also carries. Pure.
func WithSystem(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemKey{}, true)
}

// WithOperator marks ctx as a command the operator runs (create-superuser, export, delete,
// migrate, demo). app sets it on the command's ctx. An entry that names no actor is recorded
// as the operator when the ctx carries no system marker and the ActorFunc names no one (the
// session's person comes first). Pure.
func WithOperator(ctx context.Context) context.Context {
	return context.WithValue(ctx, operatorKey{}, true)
}

func isSystem(ctx context.Context) bool {
	v, ok := ctx.Value(systemKey{}).(bool)
	return ok && v
}

func isOperator(ctx context.Context) bool {
	v, ok := ctx.Value(operatorKey{}).(bool)
	return ok && v
}
