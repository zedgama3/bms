package authz

import (
	"context"
	"slices"
)

// ListPermissions returns the catalogue sorted by name. It needs access.roles.read
// site-wide, or any of access.grants.read, access.grants.manage and
// access.department-grants.manage in any extent (then only the permissions that can be
// granted for a department). Fails errs.Permission, errs.Unauthenticated and store kinds.
func (s *Service) ListPermissions(ctx context.Context) (_ []PermissionInfo, err error) {
	const op = "authz.Service.ListPermissions"
	ctx, span := s.start(ctx, "list-permissions")
	defer endSpan(span, &err)
	view, err := s.viewer(ctx, op)
	if err != nil {
		return nil, err
	}
	out := make([]PermissionInfo, 0, len(s.lk.cat.list))
	for _, d := range s.lk.cat.list {
		if !view.full && d.Scope != ScopeDepartment {
			continue
		}
		d.StarterRoles = slices.Clone(d.StarterRoles)
		out = append(out, PermissionInfo{PermissionDecl: d})
	}
	return out, nil
}

// viewer is the caller of a catalogue or role read: full with access.roles.read, otherwise
// limited to department material when a grants permission is held in any extent.
type viewer struct{ full bool }

func (s *Service) viewer(ctx context.Context, op string) (viewer, error) {
	a, err := s.actor(ctx, op)
	if err != nil {
		return viewer{}, err
	}
	if a.Superuser {
		return viewer{full: true}, nil
	}
	acc, err := s.lk.load(ctx, a.Person, "")
	if err != nil {
		return viewer{}, err
	}
	if acc.site(PermissionRolesRead) {
		return viewer{full: true}, nil
	}
	if acc.anywhere(PermissionGrantsRead) || acc.anywhere(PermissionGrantsManage) || acc.anywhere(PermissionDepartmentGrantsManage) {
		return viewer{}, nil
	}
	return viewer{}, denied(op, PermissionRolesRead, "the permission")
}
