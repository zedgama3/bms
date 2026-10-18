// Package eventsetup is the shape of the events.event-setup extension point: what a module
// does when the event admin creates a new event from an earlier one, and how events asks
// for it. It holds no implementation (07-abstractions, "contracts"), so a module imports
// this package and never events.
package eventsetup

import (
	"context"

	"github.com/zedgama3/bms/internal/model"
)

// Point is the extension point's name. app declares it in Env.CoreExtensionPoints with
// Accepts = func(v any) bool { _, ok := v.(Provider); return ok }.
const Point = "events.event-setup"

// Request names the new event and the one to copy from. Both are non-zero and differ.
type Request struct {
	Event    model.EventID
	CopyFrom model.EventID
}

// Result is what a provider did; Items counts the records it created or copied.
type Result struct {
	Items int
}

// Provider is a module's set-up for a new event. It runs in the caller's ctx (the event
// admin), under events' SetupTimeout, after the event is committed. It checks the caller's
// permission and fails errs.Permission when they may not copy its data. It is idempotent
// per (Event, module), because RunSetup calls it again. It never imports events: it only
// receives IDs. A failure is reported to the admin and never undoes the event.
type Provider interface {
	SetUp(ctx context.Context, req Request) (Result, error)
}
