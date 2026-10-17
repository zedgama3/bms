package web

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestFS_WithoutBuild_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	_, err := checkedFS("web.FS", fstest.MapFS{".gitkeep": {Data: nil}})
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("kind = %v (%v), want NotFound", errs.KindOf(err), err)
	}
}

func TestFS_IndexIsADirectory_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	_, err := checkedFS("web.FS", fstest.MapFS{"index.html/x": {Data: []byte("x")}})
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("kind = %v (%v), want NotFound", errs.KindOf(err), err)
	}
}

func TestFS_WithBuild_ServesIndexAndAssets(t *testing.T) {
	t.Parallel()
	built := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>")},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc.css": {Data: []byte("a{}")},
		"version.txt":          {Data: []byte("v1.2.3")},
	}
	root, err := checkedFS("web.FS", built)
	if err != nil {
		t.Fatalf("checkedFS: %v", err)
	}
	for _, name := range []string{"index.html", "assets/index-abc.js", "assets/index-abc.css"} {
		if _, err := fs.Stat(root, name); err != nil {
			t.Errorf("Stat(%q): %v", name, err)
		}
	}
}

func TestFS_HidesDotPaths(t *testing.T) {
	t.Parallel()
	root, err := checkedFS("web.FS", fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html>")},
		".gitkeep":              {Data: nil},
		".vite/manifest.json":   {Data: []byte("{}")},
		"assets/.hidden/x.js":   {Data: []byte("x")},
		"assets/index-abc12.js": {Data: []byte("x")},
	})
	if err != nil {
		t.Fatalf("checkedFS: %v", err)
	}
	for _, name := range []string{".gitkeep", ".vite/manifest.json", "assets/.hidden/x.js"} {
		if _, err := fs.Stat(root, name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Stat(%q) = %v, want ErrNotExist", name, err)
		}
	}
	for _, name := range []string{"index.html", "assets/index-abc12.js"} {
		if _, err := fs.Stat(root, name); err != nil {
			t.Errorf("Stat(%q): %v", name, err)
		}
	}
}

func TestVersion_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files fstest.MapFS
		want  string
		kind  errs.Kind
	}{
		{"dev", fstest.MapFS{"version.txt": {Data: []byte("dev")}}, "dev", 0},
		{"describe", fstest.MapFS{"version.txt": {Data: []byte("v0.1.0-3-gabc1234")}}, "v0.1.0-3-gabc1234", 0},
		{"trailing newline is trimmed", fstest.MapFS{"version.txt": {Data: []byte("v1\n")}}, "v1", 0},
		{"exactly 64 bytes", fstest.MapFS{"version.txt": {Data: []byte(strings.Repeat("a", 64))}}, strings.Repeat("a", 64), 0},
		{"missing", fstest.MapFS{}, "", errs.NotFound},
		{"empty", fstest.MapFS{"version.txt": {Data: []byte("")}}, "", errs.Invalid},
		{"65 bytes", fstest.MapFS{"version.txt": {Data: []byte(strings.Repeat("a", 65))}}, "", errs.Invalid},
		{"space inside", fstest.MapFS{"version.txt": {Data: []byte("v1 beta")}}, "", errs.Invalid},
		{"starts with a dash", fstest.MapFS{"version.txt": {Data: []byte("-v1")}}, "", errs.Invalid},
		{"query character", fstest.MapFS{"version.txt": {Data: []byte("v1?x")}}, "", errs.Invalid},
		{"at sign", fstest.MapFS{"version.txt": {Data: []byte("a@b")}}, "", errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := versionFrom("web.Version", tc.files)
			if tc.kind == 0 {
				if err != nil || got != tc.want {
					t.Fatalf("versionFrom = %q, %v; want %q, nil", got, err, tc.want)
				}
				return
			}
			if !errs.Is(err, tc.kind) {
				t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, tc.kind)
			}
		})
	}
}

func TestVersion_ReadError_ClassifiesInternal(t *testing.T) {
	t.Parallel()
	_, err := versionFrom("web.Version", failingFS{})
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("kind = %v (%v), want Internal", errs.KindOf(err), err)
	}
}

// failingFS fails every Open with something other than fs.ErrNotExist.
type failingFS struct{}

func (failingFS) Open(string) (fs.File, error) { return nil, errors.New("disk on fire") }

// TestFS_ServesHashedAssetsAndIndex and TestVersion_MatchesBuild run over the real embedded
// dist/ and skip in a checkout where `pnpm build` has not run.
func TestFS_ServesHashedAssetsAndIndex(t *testing.T) {
	t.Parallel()
	root, err := FS()
	if errs.Is(err, errs.NotFound) {
		t.Skip("web/dist has no build; run `make web-build`")
	}
	if err != nil {
		t.Fatalf("FS: %v", err)
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if strings.Contains(string(index), "<script>") || strings.Contains(string(index), " style=") {
		t.Error("index.html has an inline script or style; the CSP forbids both")
	}
	assets, err := fs.Glob(root, "assets/*")
	if err != nil || len(assets) == 0 {
		t.Fatalf("assets/* = %v, %v; want hashed files", assets, err)
	}
	hashed := regexp.MustCompile(`^assets/.+-[A-Za-z0-9_-]{6,}\.(js|css|svg)$`)
	for _, a := range assets {
		if !hashed.MatchString(a) {
			t.Errorf("asset %q is not content-hashed", a)
		}
	}
}

func TestVersion_MatchesBuild(t *testing.T) {
	t.Parallel()
	v, err := Version()
	if errs.Is(err, errs.NotFound) {
		t.Skip("web/dist has no build; run `make web-build`")
	}
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v == "" {
		t.Error("Version() is empty")
	}
}

// docHosts are the only origins the built files may name: documentation links and XML
// namespaces inside library error messages. Anything else could be a third-party request
// (decision 15; connect-src 'self' makes the CSP the runtime guarantee).
var docHosts = map[string]bool{
	"www.w3.org":         true,
	"react.dev":          true,
	"reactrouter.com":    true,
	"github.com":         true,
	"formatjs.github.io": true,
	"tailwindcss.com":    true,
	"localhost":          true,
}

func TestBundle_NoExternalURLs(t *testing.T) {
	t.Parallel()
	dist := os.DirFS("dist")
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		t.Skip("web/dist has no build; run `make web-build`")
	}
	origin := regexp.MustCompile(`https?://([A-Za-z0-9.-]+)`)
	text := regexp.MustCompile(`\.(js|css|html)$`)
	err := fs.WalkDir(dist, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		if d.IsDir() || !text.MatchString(path) {
			return nil
		}
		data, err := fs.ReadFile(dist, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for _, m := range origin.FindAllStringSubmatch(string(data), -1) {
			if !docHosts[m[1]] {
				t.Errorf("%s names the origin %s", path, m[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk dist: %v", err)
	}
}
