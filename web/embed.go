// Package web embeds the built web shell for app (api.Options.UI). It imports the standard
// library and lib/errs only; only app imports it (web-shell.md, "Imports and boundaries").
//
// The files come from `pnpm build` (make web-build). A checkout where that has not run holds
// only dist/.gitkeep, so the package compiles everywhere and FS reports errs.NotFound.
package web

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
)

//go:embed all:dist
var built embed.FS

const (
	indexName   = "index.html"
	versionName = "version.txt"
	// maxVersionBytes is telemetry's limit on app_version.
	maxVersionBytes = 64
)

// FS returns the built shell rooted at dist/ (index.html at the root, hashed files under
// assets/), for api.Options.UI. It fails with errs.NotFound when dist/ holds no index.html
// (a checkout where `pnpm build` has not run): app then starts without a UI, logs one
// warning, and non-API paths answer 404 (api decision 12). Cannot fail otherwise.
func FS() (fs.FS, error) {
	const op = "web.FS"
	root, err := fs.Sub(built, "dist")
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("open the embedded dist directory: %w", err))
	}
	return checkedFS(op, root)
}

// Version returns the bundle's version, written by the build to dist/version.txt (the same
// string Vite injects as __BMS_VERSION__ and the shell sends as app_version). It fails with
// errs.NotFound when the file is missing and errs.Invalid when its content is not a
// telemetry token of at most 64 bytes.
func Version() (string, error) {
	const op = "web.Version"
	root, err := fs.Sub(built, "dist")
	if err != nil {
		return "", errs.E(op, errs.Internal, fmt.Errorf("open the embedded dist directory: %w", err))
	}
	return versionFrom(op, root)
}

// checkedFS returns root when it holds a regular index.html, else errs.NotFound.
func checkedFS(op string, root fs.FS) (fs.FS, error) {
	info, err := fs.Stat(root, indexName)
	switch {
	case err == nil && info.Mode().IsRegular():
		return noDotFiles{root}, nil
	case err == nil:
		return nil, errs.E(op, errs.NotFound, fmt.Errorf("%s is not a regular file; run `make web-build`", indexName))
	case errors.Is(err, fs.ErrNotExist):
		return nil, errs.E(op, errs.NotFound, fmt.Errorf("no %s in the built shell; run `make web-build`: %w", indexName, err))
	default:
		return nil, errs.E(op, errs.Internal, fmt.Errorf("stat %s: %w", indexName, err))
	}
}

// noDotFiles hides every path with a dot-prefixed element (.vite/manifest.json, .gitkeep) from
// whoever serves the files: they are build artefacts, not part of the application.
type noDotFiles struct{ fs.FS }

// Open implements fs.FS.
func (d noDotFiles) Open(name string) (fs.File, error) {
	for _, el := range strings.Split(name, "/") {
		if len(el) > 1 && el[0] == '.' {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
	}
	f, err := d.FS.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // an fs.FS returns fs errors as they are
	}
	return f, nil
}

// versionFrom reads and validates version.txt from root.
func versionFrom(op string, root fs.FS) (string, error) {
	data, err := fs.ReadFile(root, versionName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", errs.E(op, errs.NotFound, fmt.Errorf("no %s in the built shell; run `make web-build`: %w", versionName, err))
		}
		return "", errs.E(op, errs.Internal, fmt.Errorf("read %s: %w", versionName, err))
	}
	v := strings.TrimSpace(string(data))
	if len(v) > maxVersionBytes {
		return "", errs.E(op, errs.Invalid, fmt.Errorf("%s is %d bytes; the limit is %d", versionName, len(v), maxVersionBytes))
	}
	if !isToken(v) {
		return "", errs.E(op, errs.Invalid, fmt.Errorf("%s does not hold a version token ([A-Za-z0-9/_][A-Za-z0-9._:/{}-]*)", versionName))
	}
	return v, nil
}

// isToken reports whether s fits telemetry's token grammar "[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}".
// The grammar is repeated here because this package may import only the standard library
// and lib/errs; web's tests pin the two cases that matter (web-shell.md F31).
func isToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '/', c == '_':
		case i > 0 && (c == '.' || c == ':' || c == '{' || c == '}' || c == '-'):
		default:
			return false
		}
	}
	return true
}
