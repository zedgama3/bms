package auditlog_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/auditlog"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/model"
)

func TestPermissions_AreSiteWideAndReachSensitive(t *testing.T) {
	if auditlog.PermissionSearch != "audit.search" || auditlog.PermissionExport != "audit.export" {
		t.Fatalf("permission names differ: %q %q", auditlog.PermissionSearch, auditlog.PermissionExport)
	}
	want := map[authz.Permission]bool{auditlog.PermissionSearch: false, auditlog.PermissionExport: false}
	for _, d := range auditlog.Permissions {
		if _, ok := want[d.Name]; !ok {
			t.Errorf("unexpected permission %q", d.Name)
			continue
		}
		want[d.Name] = true
		if d.Scope != authz.ScopeSite {
			t.Errorf("%s scope = %v, want site", d.Name, d.Scope)
		}
		if d.Reaches != model.SensitivitySensitive {
			t.Errorf("%s reaches %v, want sensitive", d.Name, d.Reaches)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s: declaration invalid", d.Name)
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("permission %q not declared", n)
		}
	}
}
