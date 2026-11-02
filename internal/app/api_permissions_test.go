package app

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// TestAPIOptions_AuthzLookupNotBuilt_PermissionsIsNilInterface: when the authz Lookup was not
// built (a command that builds a subset of the parts), api.Options.Permissions is a nil
// interface, so api.New's "never nil" check sees it. A nil *authz.Lookup stored in the
// interface would compare non-nil and pass that check.
func TestAPIOptions_AuthzLookupNotBuilt_PermissionsIsNilInterface(t *testing.T) {
	rec := &recorder{}
	reg := recRegistry(t, rec, "zqalpha")
	a := newAssembly(&process{clock: clock.Real(), parts: nil, version: "dev"}, config.Default())
	a.setTelemetry(telemetrytest.New(t).Telemetry())
	ctx := context.Background()
	if err := a.resolve(ctx, reg, []string{"zqalpha"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if a.authzLookup != nil {
		t.Fatal("the test needs an assembly whose authz Lookup was not built")
	}
	opts, err := a.apiOptions("dev", noDatabase{}, func(context.Context) ([]db.Status, error) { return nil, nil })
	if err != nil {
		t.Fatalf("apiOptions: %v", err)
	}
	if opts.Permissions != nil {
		t.Errorf("Permissions = %#v, want a nil interface", opts.Permissions)
	}
}
