package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
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
		return false, errs.E("app.lazyDepartments.Active", errs.Unsupported, errors.New("no departments service is wired"))
	}
	return l.a.departments.Active(ctx, d) //nolint:wrapcheck // the service's own error
}

// lazyAuthorizer is settings' authz.Authorizer over a.authz, which authz's part sets after
// settings is built: until then every decision is denied (errs.Permission) and ExtentOf is
// nowhere (a zero Extent, nil), as the services do when they are given no authorizer. It
// does no I/O of its own. settings calls only Allow (every settings permission is
// site-scoped); AllowIn and ExtentOf are implemented so the value is a complete Authorizer.
type lazyAuthorizer struct{ a *assembly }

var _ authz.Authorizer = lazyAuthorizer{}

// Allow asks the authz service when there is one, else fails errs.Permission.
func (l lazyAuthorizer) Allow(ctx context.Context, p authz.Permission) error {
	if l.a.authz == nil {
		return errs.E("app.lazyAuthorizer.Allow", errs.Permission, errors.New("no authorizer is wired, so access is denied"))
	}
	return l.a.authz.Allow(ctx, p) //nolint:wrapcheck // the authorizer's own error
}

// AllowIn asks the authz service when there is one, else fails errs.Permission.
func (l lazyAuthorizer) AllowIn(ctx context.Context, p authz.Permission, d model.DepartmentID) error {
	if l.a.authz == nil {
		return errs.E("app.lazyAuthorizer.AllowIn", errs.Permission, errors.New("no authorizer is wired, so access is denied"))
	}
	return l.a.authz.AllowIn(ctx, p, d) //nolint:wrapcheck // the authorizer's own error
}

// ExtentOf asks the authz service when there is one, else answers nowhere (a zero Extent).
func (l lazyAuthorizer) ExtentOf(ctx context.Context, p authz.Permission) (authz.Extent, error) {
	if l.a.authz == nil {
		return authz.Extent{}, nil
	}
	return l.a.authz.ExtentOf(ctx, p) //nolint:wrapcheck // the authorizer's own error
}

// lazyIdentityPolicy is identity's PolicySource over a.settings, which settings' part sets
// after identity is built: until then the policy is the shipped default, as it is when
// identity is given no source. It does no I/O of its own.
type lazyIdentityPolicy struct{ a *assembly }

// Policy reads the accounts.* settings when there is a settings service, else the defaults.
func (l lazyIdentityPolicy) Policy(ctx context.Context) (identity.Policy, error) {
	if l.a.settings == nil {
		return identity.DefaultPolicy(), nil
	}
	r, err := l.a.settings.Scoped(accountsSettings)
	if err != nil {
		return identity.Policy{}, fmt.Errorf("app.lazyIdentityPolicy: %w", err)
	}
	return settingsPolicy{r: r}.Policy(ctx)
}

// lazyPeoplePolicy is people's PolicySource over a.settings, resolved when called, so the
// part order does not matter to it.
type lazyPeoplePolicy struct{ a *assembly }

// Policy reads the people.* settings when there is a settings service, else the defaults.
func (l lazyPeoplePolicy) Policy(ctx context.Context) (people.Policy, error) {
	if l.a.settings == nil {
		return people.DefaultPolicy(), nil
	}
	r, err := l.a.settings.Scoped(peopleSettings)
	if err != nil {
		return people.Policy{}, fmt.Errorf("app.lazyPeoplePolicy: %w", err)
	}
	return peoplePolicy{r: r}.Policy(ctx)
}
