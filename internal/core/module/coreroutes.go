package module

import (
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// CoreRoutes converts a core area's routes to a route set: Public becomes AccessPublic,
// everything else AccessSignedIn; MaxBodyBytes and Timeout carry over. Core packages cannot
// import this package (module is the top of the core graph), so each returns
// []httpx.Route and app converts it here, once per area.
//
// It fails errs.Invalid when area is not a reserved core area name (IsCoreArea), or when a
// route would fail Route.Validate; the problem fields are named "routes[i].<field>", with
// the field names Route.Validate uses ("method", "pattern", "handler", ...). It reports
// every bad route, not just the first. The openapi fragment is carried over, not checked: the
// api shell compares it with the routes (api.md decision 11).
func CoreRoutes(area string, routes []httpx.Route, openapi []byte) (ModuleRoutes, error) {
	const op = "module.CoreRoutes"
	if !IsCoreArea(area) {
		return ModuleRoutes{}, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "area", Code: validate.CodeOneOf, Reason: "must be a core area name",
		})
	}
	acc := validate.Errors{Op: op}
	out := make([]Route, len(routes))
	for i, r := range routes {
		access := AccessSignedIn
		if r.Public {
			access = AccessPublic
		}
		out[i] = Route{
			Method: r.Method, Pattern: r.Pattern, Access: access, Handler: r.Handler,
			MaxBodyBytes: r.MaxBodyBytes, Timeout: r.Timeout,
		}
		asNested(&acc, fmt.Sprintf("routes[%d]", i), out[i].Validate())
	}
	if err := acc.Err(); err != nil {
		return ModuleRoutes{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return ModuleRoutes{Module: area, Routes: out, OpenAPI: openapi}, nil
}
