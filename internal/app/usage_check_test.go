package app

import (
	"bytes"
	"errors"
	"io/fs"
	"net"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/web"
)

// Tests for the client-telemetry wiring of Amendment T1 (D54): app.md F41 to F43.

const usageTestKey = "ZQ9MARKER7kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"

func stubManifest(t *testing.T, m web.Manifest, err error) {
	t.Helper()
	old := webManifest
	webManifest = func() (web.Manifest, error) { return m, err }
	t.Cleanup(func() { webManifest = old })
}

func stubUI(t *testing.T, built bool) {
	t.Helper()
	old := webFS
	webFS = func() (fs.FS, error) {
		if !built {
			return nil, errs.E("web.FS", errs.NotFound, errors.New("no build"))
		}
		return fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, nil
	}
	t.Cleanup(func() { webFS = old })
}

func twoScreensThreeActions() web.Manifest {
	return web.Manifest{
		Screens: []web.ScreenEntry{{Pattern: "/", Set: "shell"}, {Pattern: "/people/{id}", Set: "people"}},
		Actions: []web.ActionEntry{{Name: "a.one", Set: "x"}, {Name: "a.two", Set: "x"}, {Name: "a.three", Set: "x"}},
	}
}

// runCheckWith runs `bms check` against a database nothing listens on, so the status is
// ExitFailure whatever else is reported, and returns stdout and the reported problem count.
func runCheckWith(t *testing.T, extraEnv ...string) (out string, problems int) {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	env := append([]string{
		"BMS_DB_URL=postgres://zquser:pw@" + port + "/zqdb?sslmode=disable",
		"BMS_SESSION_KEY=" + usageTestKey,
		"BMS_SETTINGS_SEALING_KEY=" + usageTestKey,
		"BMS_DATA_DIR=" + t.TempDir(),
		"BMS_LOG_FORMAT=json",
		"BMS_DB_CONNECT_TIMEOUT=2s",
	}, extraEnv...)
	var so, se bytes.Buffer
	_ = Main(t.Context(), []string{"check"}, env, &so, &se)
	m := regexp.MustCompile(`result: (\d+) problem`).FindStringSubmatch(so.String())
	if m == nil {
		t.Fatalf("no problem count in the output:\n%s", so.String())
	}
	problems, err = strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return so.String(), problems
}

func TestCheck_PrintsUsageCounts(t *testing.T) { // F43
	stubUI(t, true)
	stubManifest(t, twoScreensThreeActions(), nil)

	out, _ := runCheckWith(t)
	if !strings.Contains(out, "usage counts: on (2 screens, 3 actions)") {
		t.Errorf("stdout lacks the on line:\n%s", out)
	}

	out, _ = runCheckWith(t, "BMS_TELEMETRY_USAGE=false")
	if !strings.Contains(out, "usage counts: off") || strings.Contains(out, "usage counts: on") {
		t.Errorf("stdout lacks the off line:\n%s", out)
	}

	stubManifest(t, web.Manifest{}, errs.E("web.ClientManifest", errs.NotFound, errors.New("none")))
	out, _ = runCheckWith(t)
	if !strings.Contains(out, "usage counts: on (no manifest; fold caps apply)") {
		t.Errorf("stdout lacks the no-manifest line:\n%s", out)
	}
	out, _ = runCheckWith(t, "BMS_TELEMETRY_USAGE=false")
	if !strings.Contains(out, "usage counts: off") {
		t.Errorf("off must win over a missing manifest:\n%s", out)
	}
}

func TestCheck_ClientManifestMissing_Fails(t *testing.T) { // F41
	stubUI(t, true)
	stubManifest(t, twoScreensThreeActions(), nil)
	_, okProblems := runCheckWith(t)

	for name, err := range map[string]error{
		"not found": errs.E("web.ClientManifest", errs.NotFound, errors.New("none")),
		"invalid":   errs.E("web.ClientManifest", errs.Invalid, errors.New("bad")),
	} {
		t.Run("with a UI/"+name, func(t *testing.T) {
			stubManifest(t, web.Manifest{}, err)
			out, problems := runCheckWith(t)
			if !strings.Contains(out, "client manifest: missing") {
				t.Errorf("stdout lacks the missing line:\n%s", out)
			}
			if problems != okProblems+1 {
				t.Errorf("problems = %d, want %d (the missing manifest is one more failure)", problems, okProblems+1)
			}
		})
	}

	t.Run("no UI is a note, not a failure", func(t *testing.T) {
		stubUI(t, false)
		stubManifest(t, twoScreensThreeActions(), nil)
		_, withManifest := runCheckWith(t)
		stubManifest(t, web.Manifest{}, errs.E("web.ClientManifest", errs.NotFound, errors.New("none")))
		out, without := runCheckWith(t)
		if !strings.Contains(out, "client manifest: missing") {
			t.Errorf("stdout lacks the note:\n%s", out)
		}
		if without != withManifest {
			t.Errorf("problems = %d, want %d: with no UI a missing manifest is only a note", without, withManifest)
		}
	})
}

func TestOpenAPI_UsageBuiltDisabled(t *testing.T) { // F42
	run := func() string {
		var so, se bytes.Buffer
		if code := Main(t.Context(), []string{"openapi"}, nil, &so, &se); code != ExitOK {
			t.Fatalf("openapi exit = %d\n%s", code, se.String())
		}
		return so.String()
	}
	stubManifest(t, web.Manifest{}, errs.E("web.ClientManifest", errs.NotFound, errors.New("none")))
	without := run()
	stubManifest(t, twoScreensThreeActions(), nil)
	with := run()
	if without != with {
		t.Error("the OpenAPI document depends on the client manifest")
	}
	if !strings.Contains(with, "/api/client-events") {
		t.Error("the OpenAPI document lacks /api/client-events")
	}
	if strings.Contains(with, "a.one") {
		t.Error("the manifest's actions leaked into the document")
	}
}
