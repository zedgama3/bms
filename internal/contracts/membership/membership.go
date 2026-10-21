// Package membership is the shape of the people.membership extension point: how a module
// that has members tells people which departments each person belongs to, so that a
// department-scoped directory or contacts extent reaches only that department's members.
// It holds no implementation (07-abstractions, "contracts"), so a module imports this
// package and never people.
package membership

import (
	"context"

	"github.com/zedgama3/bms/internal/model"
)

// Point is the extension point's name. app declares it in Env.CoreExtensionPoints with
// Accepts = func(v any) bool { _, ok := v.(Provider); return ok }.
const Point = "people.membership"

// Provider is what a module with members contributes. It runs for the signed-in viewer
// (identity.PersonFrom(ctx)) but reports membership as data, not as a view: it returns the
// departments of every person asked about, whatever the viewer may see, because people
// intersects the answer with the viewer's extent. It records no audit entry (a membership
// is not a personal read of a profile). Batch: one call per use case, len(people) >= 1.
// A person who belongs to no department of the module is absent from the map or has an
// empty slice. Fails with the module's own kinds; people fails the use case closed.
type Provider interface {
	DepartmentsOf(ctx context.Context, people []model.PersonID) (map[model.PersonID][]model.DepartmentID, error)
}
