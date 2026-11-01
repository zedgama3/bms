//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Layer rules from files.md "Summary" and Proposed change 14, checked on the production
// sources (test files are skipped): files and objects import only what the spec lists.
func importsOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, im := range f.Imports {
			out[strings.Trim(im.Path.Value, `"`)] = append(out[strings.Trim(im.Path.Value, `"`)], p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", dir, err)
	}
	return out
}

const mod = "github.com/zedgama3/bms/internal/"

func TestImports_FilesAndObjectsRespectTheLayers(t *testing.T) {
	forbidFiles := []string{"core/identity", "core/module", "core/jobs", "api", "app", "config", "modules/", "core/objects/disk", "core/objects/s3", "core/people"}
	forbidObjects := []string{"core/files", "core/db", "core/audit", "core/authz", "core/identity", "core/settings", "core/module", "core/jobs", "api", "app", "config", "modules/"}
	check := func(dir string, forbid []string, skipSub ...string) {
		for imp, srcs := range importsOf(t, dir) {
			for _, bad := range forbid {
				if strings.HasPrefix(imp, mod+bad) {
					ok := false
					for _, s := range srcs {
						for _, sub := range skipSub {
							if strings.Contains(s, sub) {
								ok = true
							}
						}
					}
					if !ok {
						t.Errorf("%s imports %s (forbidden: %s)", srcs[0], imp, bad)
					}
				}
			}
		}
	}
	check("../files", forbidFiles, "/filestest/", "/postgres/")
	check("../objects", forbidObjects)
	// Only objects/s3 may import the minio client; nothing else under core may.
	for _, dir := range []string{"../files", "../objects/disk", "../objects/objectstest", "../objects"} {
		for imp, srcs := range importsOf(t, dir) {
			if strings.HasPrefix(imp, "github.com/minio/minio-go") && !strings.Contains(srcs[0], "/objects/s3/") {
				t.Errorf("%s imports %s; only objects/s3 may", srcs[0], imp)
			}
		}
	}
}

func TestImports_FilesUsesObjectsOnlyThroughTheContract(t *testing.T) {
	for imp, srcs := range importsOf(t, "../files") {
		if imp == mod+"core/objects/disk" || imp == mod+"core/objects/s3" {
			t.Errorf("%s imports %s; only app may name an implementation", srcs[0], imp)
		}
	}
}
