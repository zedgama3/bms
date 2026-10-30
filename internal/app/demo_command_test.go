package app_test

// The `bms demo` command without a database: plan/design/demo.md (D53) and app.md F38.

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/app"
)

// F38 (app.md): an unknown subcommand or extra arguments is usage, exit 2. Usage is decided
// before any database is opened.
func TestDemoCommand_Usage_Exit2(t *testing.T) {
	for _, args := range [][]string{
		{"demo"},
		{"demo", "bogus"},
		{"demo", "seed", "extra"},
		{"demo", "reset", "extra"},
		{"demo", "seed", "reset"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			env := baseEnv(t, closedDBURL(t))
			for _, e := range []string{"BMS_DEMO_ENABLED=true", ""} {
				if e != "" {
					env = append(env, e)
				}
				r := run(t.Context(), t, env, args...)
				wantCode(t, r, app.ExitUsage)
				noMarker(t, r)
			}
		})
	}
}

func TestHelp_ListsTheDemoCommand(t *testing.T) {
	r := run(t.Context(), t, baseEnv(t, closedDBURL(t)), "help")
	wantCode(t, r, app.ExitOK)
	if !strings.Contains(r.out+r.errOut, "demo") {
		t.Errorf("help does not list the demo command:\n%s", r.out)
	}
}

// Config (F2): a demo public URL without demo mode is an operator mistake, exit 2, before any I/O.
func TestDemoCommand_PublicURLWithoutDemo_Exit2(t *testing.T) {
	env := baseEnv(t, closedDBURL(t), "BMS_DEMO_PUBLIC_URL=https://demo.example.org")
	for _, args := range [][]string{{"demo", "seed"}, {"check"}, {"serve"}} {
		r := run(t.Context(), t, env, args...)
		wantCode(t, r, app.ExitUsage)
		if !strings.Contains(r.errOut+r.out, "BMS_DEMO_PUBLIC_URL") {
			t.Errorf("%v: stderr does not name BMS_DEMO_PUBLIC_URL:\n%s", args, r.errOut)
		}
	}
}

func TestDemoCommand_MalformedEnabled_Exit2(t *testing.T) {
	r := run(t.Context(), t, baseEnv(t, closedDBURL(t), "BMS_DEMO_ENABLED=maybe"), "demo", "seed")
	wantCode(t, r, app.ExitUsage)
	if !strings.Contains(r.errOut, "BMS_DEMO_ENABLED") {
		t.Errorf("stderr does not name BMS_DEMO_ENABLED:\n%s", r.errOut)
	}
}
