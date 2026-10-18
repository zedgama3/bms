package app

import (
	"context"
	"errors"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The adapters in this file are the seams of parts built before the part they wait for: audit
// and identity are built before authz and people, so they hold these small values, which
// read the assembly's fields when called and answer "nothing yet" until the later part has
// set them. They do no I/O of their own.

// lazyPermissions is the PermissionChecker of audit and identity over a.permissions, which
// authz's part sets: until then it denies, as the services do when they are given none.
type lazyPermissions struct{ a *assembly }

// Allow asks the authorizer when there is one, else fails errs.Permission.
func (l lazyPermissions) Allow(ctx context.Context, p authz.Permission) error {
	if l.a.permissions == nil {
		return errs.E("app.Allow", errs.Permission, errors.New("no authorizer is wired, so access is denied"))
	}
	return l.a.permissions.Allow(ctx, p) //nolint:wrapcheck // the authorizer's own error
}

// lazyTitles is audit's TitleSource over a.titles, which authz's part sets: until then
// nobody has titles.
type lazyTitles struct{ a *assembly }

// TitlesOf asks the source when there is one, else returns nil, nil.
func (l lazyTitles) TitlesOf(ctx context.Context, person model.PersonID) ([]string, error) {
	if l.a.titles == nil {
		return nil, nil
	}
	return l.a.titles.TitlesOf(ctx, person) //nolint:wrapcheck // the source's own error
}

// lazyLister is identity's PermissionLister over a.lister, which authz's part sets: until
// then the list is empty.
type lazyLister struct{ a *assembly }

// PermissionsOf asks the lister when there is one, else returns an empty list.
func (l lazyLister) PermissionsOf(ctx context.Context, person model.PersonID) ([]string, error) {
	if l.a.lister == nil {
		return []string{}, nil
	}
	return l.a.lister.PermissionsOf(ctx, person) //nolint:wrapcheck // the lister's own error
}

// lazyNames is identity's NameSource over a.names, which people's part sets: until then the
// playa name is "".
type lazyNames struct{ a *assembly }

// PlayaNameOf asks the source when there is one, else returns "".
func (l lazyNames) PlayaNameOf(ctx context.Context, person model.PersonID) (string, error) {
	if l.a.names == nil {
		return "", nil
	}
	return l.a.names.PlayaNameOf(ctx, person) //nolint:wrapcheck // the source's own error
}

// authzActor is the ActorFunc of authz and people over identity's ctx accessors: the person
// of a full session and whether it is the superuser's. It is false without a session and for
// a restricted one (identity.PersonFrom's rule); the services also refuse the actor inside
// a signal delivery. It does no I/O.
func authzActor(ctx context.Context) (authz.Actor, bool) {
	p, ok := identity.PersonFrom(ctx)
	if !ok {
		return authz.Actor{}, false
	}
	s, _ := identity.SessionFrom(ctx)
	return authz.Actor{Person: p, Superuser: s.Superuser}, true
}

// lazyDepartments is authz's DepartmentSource over a.departments, which the departments part
// sets after authz is built: until then a department grant is Unsupported, as it is when
// authz is given no source (departments-events.md; D37).
type lazyDepartments struct{ a *assembly }

// Active asks the departments service when there is one, else fails errs.Unsupported.
func (l lazyDepartments) Active(ctx context.Context, d model.DepartmentID) (bool, error) {
	if l.a.departments == nil {
		return false, errs.E("app.Active", errs.Unsupported, errors.New("no departments service is wired"))
	}
	return l.a.departments.Active(ctx, d) //nolint:wrapcheck // the service's own error
}
