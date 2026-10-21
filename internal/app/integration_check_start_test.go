//go:build integration

package app_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/app"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Q-JOBS10 (D44): a module whose Start fails shows in the jobs section and does not fail
// the check; serve still stops (TestServe_ModuleStartFails_StopsAndExit1).
func TestCheck_ModuleStartFails_StillExit0AndJobsSectionShowsIt(t *testing.T) {
	env, _ := integrationEnv(t)
	mustMigrate(t, env)
	ctl.Lock()
	ctl.startErr = kindErr(errs.Unavailable)
	ctl.Unlock()
	r := run(t.Context(), t, env, "check")
	wantCode(t, r, app.ExitOK)
	noMarker(t, r)
	if !strings.Contains(r.out, "jobs") {
		t.Fatalf("stdout has no jobs section:\n%s", r.out)
	}
	_, after, _ := strings.Cut(r.out, "jobs")
	if !strings.Contains(strings.ToLower(after), "start") {
		t.Errorf("the jobs section does not mention the failing Start:\n%s", r.out)
	}
	if !strings.Contains(r.out, "result: ok") {
		t.Errorf("stdout lacks the ok result line:\n%s", r.out)
	}
	assertNoLogLines(t, "stdout", r.out)
}
