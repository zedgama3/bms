package app

import (
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// boundaries.md step 13: identity and datarights scope their own Notifier from the notify
// service, so they are built after notify. A missing service is a wiring bug (Internal), not a
// nil panic.
func TestBuildIdentity_NotifyNil_ReturnsInternal(t *testing.T) {
	a := builtCore(t)
	a.notify = nil
	if err := buildIdentity(t.Context(), a); !errs.Is(err, errs.Internal) {
		t.Errorf("buildIdentity err = %v, want Internal", err)
	}
}

func TestBuildDatarights_NotifyNil_ReturnsInternal(t *testing.T) {
	a := builtCore(t)
	a.notify = nil
	if err := buildDatarights(t.Context(), a); !errs.Is(err, errs.Internal) {
		t.Errorf("buildDatarights err = %v, want Internal", err)
	}
}

// Both Lookups exist after the build, before any part uses them.
func TestBuildLookups_BothAreBuiltAgainstDBNone(t *testing.T) {
	a := builtCore(t)
	if a.identityLookup == nil || a.peopleLookup == nil {
		t.Errorf("identityLookup = %v, peopleLookup = %v, want both built", a.identityLookup, a.peopleLookup)
	}
}
