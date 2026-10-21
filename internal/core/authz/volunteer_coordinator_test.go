package authz_test

import (
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
)

// D44: the ninth starter role. Only the key is checked here; the role row, its
// permissions and the Seed behaviour are in integration_coordinator_test.go.

func TestRoleVolunteerCoordinator_KeyIsTheSpecifiedIdent(t *testing.T) {
	if authz.RoleVolunteerCoordinator != "volunteer-coordinator" {
		t.Fatalf("RoleVolunteerCoordinator = %q", authz.RoleVolunteerCoordinator)
	}
}

// The spec: SiteAdminRoles is unchanged (the six admin keys); the coordinator is not an
// administrator and must not reach core.modules.read through it.
func TestSiteAdminRoles_UnchangedByCoordinator(t *testing.T) {
	if len(authz.SiteAdminRoles) != 6 {
		t.Fatalf("SiteAdminRoles = %v, want the six admin keys", authz.SiteAdminRoles)
	}
	if slices.Contains(authz.SiteAdminRoles, authz.RoleVolunteerCoordinator) {
		t.Error("SiteAdminRoles contains volunteer-coordinator")
	}
	if slices.Contains(authz.SiteAdminRoles, authz.RoleParticipant) || slices.Contains(authz.SiteAdminRoles, authz.RoleDepartmentAdmin) {
		t.Errorf("SiteAdminRoles = %v includes a non-admin role", authz.SiteAdminRoles)
	}
}
