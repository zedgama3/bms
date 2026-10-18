// Package authztest gives modules an authz.Authorizer for their tests. Fake denies
// everything until a test gives a person a permission, so a use case that forgets its
// permission check fails its tests instead of reaching a burner.
package authztest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

type actorKey struct{}

// WithActor returns a ctx whose actor is a, for a Fake built by NewFake. Cannot fail.
func WithActor(ctx context.Context, a authz.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func defaultActor(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

// Fake implements authz.Authorizer in memory. Safe for concurrent use. It follows the same
// decision table as the real service for what a test can set up (site-wide and department
// grants, the superuser, no actor, a signal delivery); it cannot know which permissions
// are declared, so it never answers errs.Invalid for an unknown name.
type Fake struct {
	mu    sync.Mutex
	actor authz.ActorFunc
	super map[model.PersonID]bool
	site  map[model.PersonID]map[authz.Permission]bool
	depts map[model.PersonID]map[authz.Permission]map[model.DepartmentID]bool
}

var _ authz.Authorizer = (*Fake)(nil)

// NewFake returns a Fake that denies everything and reads the actor from WithActor. Cannot
// fail.
func NewFake() *Fake {
	return &Fake{
		actor: defaultActor,
		super: map[model.PersonID]bool{},
		site:  map[model.PersonID]map[authz.Permission]bool{},
		depts: map[model.PersonID]map[authz.Permission]map[model.DepartmentID]bool{},
	}
}

// SetActor replaces how the Fake reads the actor from a ctx, for a test that carries its
// session some other way (a module's tests pass identity's). A nil f restores WithActor.
// Cannot fail.
func (f *Fake) SetActor(a authz.ActorFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a == nil {
		a = defaultActor
	}
	f.actor = a
}

// Grant gives person p site-wide (and so, in every department). Cannot fail.
func (f *Fake) Grant(person model.PersonID, p authz.Permission) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.site[person] == nil {
		f.site[person] = map[authz.Permission]bool{}
	}
	f.site[person][p] = true
}

// GrantIn gives person p for department d only. Cannot fail.
func (f *Fake) GrantIn(person model.PersonID, p authz.Permission, d model.DepartmentID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.depts[person] == nil {
		f.depts[person] = map[authz.Permission]map[model.DepartmentID]bool{}
	}
	if f.depts[person][p] == nil {
		f.depts[person][p] = map[model.DepartmentID]bool{}
	}
	f.depts[person][p][d] = true
}

// Superuser makes person allowed everything, as identity's superuser flag does. Cannot
// fail.
func (f *Fake) Superuser(person model.PersonID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.super[person] = true
}

// who returns the actor, or errs.Unauthenticated (no actor, or inside a signal delivery),
// and whether the superuser path applies.
func (f *Fake) who(ctx context.Context, op string) (authz.Actor, bool, error) {
	f.mu.Lock()
	read := f.actor
	f.mu.Unlock()
	if signals.InDelivery(ctx) {
		return authz.Actor{}, false, errs.E(op, errs.Unauthenticated, errors.New("a signal handler acts as the system"))
	}
	a, ok := read(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return authz.Actor{}, false, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return a, a.Superuser || f.super[a.Person], nil
}

// Allow fails errs.Permission unless the actor was given p with Grant (or is a superuser),
// and errs.Unauthenticated without an actor.
func (f *Fake) Allow(ctx context.Context, p authz.Permission) error {
	const op = "authztest.Fake.Allow"
	a, super, err := f.who(ctx, op)
	if err != nil || super {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.site[a.Person][p] {
		return nil
	}
	return errs.E(op, errs.Permission, errors.New("the permission "+string(p)+" is not granted"))
}

// AllowIn fails errs.Invalid for the zero department; otherwise errs.Permission unless the
// actor holds p site-wide or for d.
func (f *Fake) AllowIn(ctx context.Context, p authz.Permission, d model.DepartmentID) error {
	const op = "authztest.Fake.AllowIn"
	if d.IsZero() {
		return errs.E(op, errs.Invalid, errors.New("the department must be set"))
	}
	a, super, err := f.who(ctx, op)
	if err != nil || super {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.site[a.Person][p] || f.depts[a.Person][p][d] {
		return nil
	}
	return errs.E(op, errs.Permission, errors.New("the permission "+string(p)+" is not granted here"))
}

// ExtentOf returns {Site: true} for a site-wide grant or the superuser, the sorted
// departments otherwise, and a zero Extent when nowhere.
func (f *Fake) ExtentOf(ctx context.Context, p authz.Permission) (authz.Extent, error) {
	const op = "authztest.Fake.ExtentOf"
	a, super, err := f.who(ctx, op)
	if err != nil {
		return authz.Extent{}, err
	}
	if super {
		return authz.Extent{Site: true}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.site[a.Person][p] {
		return authz.Extent{Site: true}, nil
	}
	var out authz.Extent
	for d := range f.depts[a.Person][p] {
		out.Departments = append(out.Departments, d)
	}
	slices.SortFunc(out.Departments, func(x, y model.DepartmentID) int { return strings.Compare(string(x[:]), string(y[:])) })
	return out, nil
}
