package app

// D56 review follow-ups of plan/design/demo.md: F44.

import (
	"testing"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// F44: demo mode on but the demo service not built: files' policy source fails closed with
// Unavailable and never falls through to the settings-backed source.
func TestDemoPolicy_ServiceMissing_FailsClosed(t *testing.T) {
	a := &assembly{cfg: config.Config{Demo: config.Demo{Enabled: true}}}
	// a.demo and a.filesPolicy are nil: a fall-through would panic instead of returning.
	_, err := lazyFilesPolicy{a: a}.Policy(t.Context())
	if err == nil {
		t.Fatal("got nil error, want Unavailable")
	}
	if !errs.Is(err, errs.Unavailable) {
		t.Fatalf("kind = %v (%v), want Unavailable", errs.KindOf(err), err)
	}
}
