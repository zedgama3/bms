package people

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/zedgama3/bms/internal/contracts/membership"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// membershipTimeout is how long people waits for one membership provider in one use case.
// A provider that needs longer is too slow for a directory read; the use case then fails
// closed with errs.Timeout.
const membershipTimeout = 2 * time.Second

// MembershipProvider is one module's contribution to people.membership, handed to Link by
// app in build order.
type MembershipProvider struct {
	Module   string
	Provider membership.Provider
}

// reach says which people one permission's extent reaches (decision 9 as amended in D44).
type reach struct {
	all bool                        // the whole directory: a site extent, or no provider to narrow it
	set map[model.PersonID]struct{} // otherwise only these
}

// has reports whether id is reached.
func (r reach) has(id model.PersonID) bool {
	if r.all {
		return true
	}
	_, ok := r.set[id]
	return ok
}

// reachOf returns who among ids the actor's extent of p reaches. A zero extent reaches
// nobody. A site extent reaches everybody. A department extent of people.directory.read
// reaches everybody while no membership provider is registered (decision 9), but a
// department extent of people.contacts.read reaches nobody then (D45: fail closed, there is
// no way to say who belongs to the department). With providers, a department extent
// reaches only the people the providers report as members of one of its departments. A
// provider that fails
// fails the use case, so a membership that cannot be read never widens access. Fails with
// the authorizer's and the providers' kinds, and errs.Timeout when a provider is too slow.
func (s *Service) reachOf(ctx context.Context, p authz.Permission, ids []model.PersonID) (reach, error) {
	const op = "people.Service.reachOf"
	ext, err := s.authz.ExtentOf(ctx, p)
	if err != nil {
		return reach{}, wrap(op, err)
	}
	if ext.IsZero() {
		return reach{set: map[model.PersonID]struct{}{}}, nil
	}
	st, _ := s.links()
	if ext.Site {
		return reach{all: true}, nil
	}
	if len(st.membership) == 0 {
		if p == PermissionContactsRead {
			return reach{set: map[model.PersonID]struct{}{}}, nil
		}
		return reach{all: true}, nil
	}
	set := make(map[model.PersonID]struct{}, len(ids))
	if len(ids) == 0 {
		return reach{set: set}, nil
	}
	for _, mp := range st.membership {
		got, err := s.departmentsOf(ctx, mp, ids)
		if err != nil {
			return reach{}, wrap(op, err)
		}
		for person, depts := range got {
			if slices.ContainsFunc(depts, ext.Covers) {
				set[person] = struct{}{}
			}
		}
	}
	return reach{set: set}, nil
}

// departmentsOf calls one provider under membershipTimeout.
func (s *Service) departmentsOf(ctx context.Context, mp MembershipProvider, ids []model.PersonID) (map[model.PersonID][]model.DepartmentID, error) {
	const op = "people.Service.departmentsOf"
	pctx, cancel := context.WithTimeout(ctx, membershipTimeout)
	defer cancel()
	got, err := mp.Provider.DepartmentsOf(pctx, ids)
	if err != nil {
		return nil, fmt.Errorf("%s: module %q: %w", op, mp.Module, err)
	}
	if cerr := pctx.Err(); cerr != nil {
		return nil, errs.E(op, errs.KindOf(cerr), fmt.Errorf("module %q: %w", mp.Module, cerr))
	}
	return got, nil
}
