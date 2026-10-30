package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
)

const manifestName = "client-manifest.json"

// ScreenEntry is one screen the router mounts: its route pattern and the name of the
// ScreenSet that owns it ("shell" for the shell's own screens).
type ScreenEntry struct {
	Pattern string `json:"pattern"`
	Set     string `json:"set"`
}

// ActionEntry is one action a ScreenSet declared (ScreenSet.actions) and the set that
// declared it.
type ActionEntry struct {
	Name string `json:"name"`
	Set  string `json:"set"`
}

// Manifest is dist/client-manifest.json: the values a client may send on
// POST /api/client-events, produced from the router's own patterns (telemetry T1.5).
type Manifest struct {
	Screens []ScreenEntry `json:"screens"`
	Actions []ActionEntry `json:"actions"`
}

// ClientManifest returns the build's client manifest, read from dist/client-manifest.json,
// for app to hand to intake as its screen and action allowlists. It fails with errs.NotFound
// when the file is missing (a checkout where `pnpm build` has not run, or an older build),
// and errs.Invalid when it is not the expected JSON or an entry is not a telemetry token
// (a screen pattern must also begin with "/"). app then runs with empty allowlists (the
// fold caps apply), warns once and reports it in `bms check`.
func ClientManifest() (Manifest, error) {
	const op = "web.ClientManifest"
	root, err := fs.Sub(built, "dist")
	if err != nil {
		return Manifest{}, errs.E(op, errs.Internal, fmt.Errorf("open the embedded dist directory: %w", err))
	}
	return manifestFrom(op, root)
}

// manifestFrom reads and validates client-manifest.json from root.
func manifestFrom(op string, root fs.FS) (Manifest, error) {
	data, err := fs.ReadFile(root, manifestName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Manifest{}, errs.E(op, errs.NotFound, fmt.Errorf("no %s in the built shell; run `make web-build`: %w", manifestName, err))
		}
		return Manifest{}, errs.E(op, errs.Internal, fmt.Errorf("read %s: %w", manifestName, err))
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, errs.E(op, errs.Invalid, fmt.Errorf("%s is not the expected JSON: %w", manifestName, err))
	}
	var problems []error
	for i, s := range m.Screens {
		if !strings.HasPrefix(s.Pattern, "/") || !isToken(s.Pattern) {
			problems = append(problems, fmt.Errorf("screens[%d].pattern is not a route pattern", i))
		}
		if !isToken(s.Set) {
			problems = append(problems, fmt.Errorf("screens[%d].set is not a token", i))
		}
	}
	for i, a := range m.Actions {
		if !isToken(a.Name) {
			problems = append(problems, fmt.Errorf("actions[%d].name is not a token", i))
		}
		if !isToken(a.Set) {
			problems = append(problems, fmt.Errorf("actions[%d].set is not a token", i))
		}
	}
	if len(problems) > 0 {
		return Manifest{}, errs.E(op, errs.Invalid, fmt.Errorf("%s: %w", manifestName, errors.Join(problems...)))
	}
	return m, nil
}
