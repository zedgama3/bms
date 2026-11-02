package app

import (
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// boundaries.md step 8: identity and datarights take their Notifier from notify's scoped set, so
// they are built after notify. A missing set is a wiring bug (Internal), not a nil panic.
func TestBuildIdentity_NotifiersNil_ReturnsInternal(t *testing.T) {
	a := builtCore(t)
	a.notifiers = nil
	if err := buildIdentity(t.Context(), a); !errs.Is(err, errs.Internal) {
		t.Errorf("buildIdentity err = %v, want Internal", err)
	}
}

func TestBuildDatarights_NotifiersNil_ReturnsInternal(t *testing.T) {
	a := builtCore(t)
	a.notifiers = nil
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
