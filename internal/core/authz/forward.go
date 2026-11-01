package authz

import (
	"context"

	"github.com/zedgama3/bms/internal/model"
)

// The decision methods forward to the Lookup so callers of the Service keep compiling; each
// fails as the Lookup's method does.

// Allow is Lookup.Allow.
func (s *Service) Allow(ctx context.Context, p Permission) error { return s.lk.Allow(ctx, p) }

// AllowIn is Lookup.AllowIn.
func (s *Service) AllowIn(ctx context.Context, p Permission, d model.DepartmentID) error {
	return s.lk.AllowIn(ctx, p, d)
}

// ExtentOf is Lookup.ExtentOf.
func (s *Service) ExtentOf(ctx context.Context, p Permission) (Extent, error) {
	return s.lk.ExtentOf(ctx, p)
}

// PermissionsOf is Lookup.PermissionsOf.
func (s *Service) PermissionsOf(ctx context.Context, person model.PersonID) ([]string, error) {
	return s.lk.PermissionsOf(ctx, person)
}

// HoldersOf is Lookup.HoldersOf.
func (s *Service) HoldersOf(ctx context.Context, p Permission) ([]model.PersonID, error) {
	return s.lk.HoldersOf(ctx, p)
}

// TitlesOf is Lookup.TitlesOf.
func (s *Service) TitlesOf(ctx context.Context, person model.PersonID) ([]string, error) {
	return s.lk.TitlesOf(ctx, person)
}

// StarterRoleIDs is Lookup.StarterRoleIDs.
func (s *Service) StarterRoleIDs(ctx context.Context, keys []string) (map[string]RoleID, error) {
	return s.lk.StarterRoleIDs(ctx, keys)
}
