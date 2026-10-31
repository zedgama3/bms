package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// departmentsPart is core part 6: the regional's standing departments and the Directory
// modules label their records with (departments-events.md; app.md "How later core services
// plug in", row 6; D37). It is built after authz, whose service it takes, and fills the
// DepartmentSource seam authz was built with.
var departmentsPart = part{
	name:    "departments",
	declare: declareDepartments,
	build:   buildDepartments,
}

// departmentsArea is the route set, audit scope and signal scope of departments:
// /api/departments.
const departmentsArea = "departments"

// declareDepartments adds the departments migration set, permissions and signals. No I/O.
func declareDepartments(a *assembly) error {
	if err := a.addCoreSet(departments.Set); err != nil {
		return err
	}
	a.corePerms = append(a.corePerms, departments.Permissions...)
	a.coreSignals = append(a.coreSignals, departments.PublishedSignals...)
	return nil
}

// buildDepartments constructs the service (no I/O), shares it as Deps.Departments and mounts
// the departments routes.
func buildDepartments(_ context.Context, a *assembly) error {
	const op = "app.departments.build"
	if a.deps.audit == nil || a.deps.authz == nil {
		return errs.E(op, errs.Internal, errors.New("departments is built after audit and authz"))
	}
	rec, err := a.deps.audit.Scoped(departmentsArea)
	if err != nil {
		return fmt.Errorf("%s: audit scope: %w", op, err)
	}
	bus, err := a.disp.Scope(departmentsArea, departments.PublishedSignals, nil)
	if err != nil {
		return fmt.Errorf("%s: signals scope: %w", op, err)
	}
	tr, m, err := a.scopes(departmentsArea)
	if err != nil {
		return err
	}
	svc, err := departments.New(departments.Options{
		DB:      a.dbc,
		Clock:   a.clock,
		Logger:  a.component(departmentsArea),
		Tracer:  tr,
		Meter:   m,
		Audit:   rec,
		Signals: bus,
		Authz:   a.deps.authz,
		Actor:   authzActor,
	})
	if err != nil {
		return err //nolint:wrapcheck // departments.New names itself
	}
	a.departments = svc
	a.deps.departments = svc
	if err := a.addCoreRoutes(departmentsArea, svc.Routes(), svc.OpenAPI()); err != nil {
		return err
	}
	return nil
}
