package app

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// version is set at link time: -ldflags "-X github.com/zedgama3/bms/internal/app.version=..."
// (the Makefile passes `git describe --tags --always --dirty`).
var version string

// maxVersionBytes is the longest version telemetry and api accept.
const maxVersionBytes = 64

// devVersion is what an unstamped build reports.
const devVersion = "dev"

// Version returns the build's version: the value -ldflags sets on the unexported variable
// `version`, else the main module's version from debug.ReadBuildInfo when it is not
// "(devel)", else "dev". It fails with errs.Invalid when the injected value is not a
// telemetry token of at most 64 bytes (F2): the build is wrong, not the operator.
// Consumers: Main (every command's start-up line, telemetry.Options.Version,
// api.Options.Version), `bms version`, the web-version comparison in serve.
func Version() (string, error) {
	const op = "app.Version"
	if version != "" {
		if err := checkVersion(version); err != nil {
			return "", errs.E(op, errs.Invalid, err)
		}
		return version, nil
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v, usable := fromBuildInfo(bi.Main.Version); usable {
			return v, nil
		}
	}
	return devVersion, nil
}

// checkVersion reports whether v is a telemetry token of at most maxVersionBytes bytes.
func checkVersion(v string) error {
	if len(v) > maxVersionBytes || !telemetry.IsToken(v) {
		return fmt.Errorf("the build's version is not a token of at most %d bytes (check -ldflags -X ...internal/app.version)", maxVersionBytes)
	}
	return nil
}

// fromBuildInfo turns the main module's version into a usable one. The toolchain stamps
// "v0.0.0-<date>-<hash>+dirty" into a build made inside a repository; "+" is not a token
// character, so it becomes "-". "(devel)", empty or anything still not a token is not
// usable: Version then falls back to "dev" rather than refusing to start a binary nobody
// stamped.
func fromBuildInfo(raw string) (string, bool) {
	v := strings.ReplaceAll(raw, "+", "-")
	if v == "" || v == "(devel)" || checkVersion(v) != nil {
		return "", false
	}
	return v, true
}
