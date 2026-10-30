package volunteers_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func loadCatalogue(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "modules", "volunteers", "messages", "en.json"))
	if err != nil {
		t.Fatalf("read web catalogue: %v", err)
	}
	var cat map[string]string
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatalf("parse web catalogue: %v", err)
	}
	return cat
}

// A coarse source scan, kept as a cheap backstop that runs without a database: every
// "module.volunteers.section.*" literal anywhere under the module (subpackages included,
// tests excluded) is a key in the web catalogue. It cannot see joined or formatted strings;
// the behavioural check (TestSections_EmittedKeysExistInWebCatalogue, integration) does.
func TestSectionKeys_SourceLiteralsExistInWebCatalogue(t *testing.T) {
	cat := loadCatalogue(t)
	re := regexp.MustCompile("[\"`](module\\.volunteers\\.section\\.[^\"`\\s]*)[\"`]")
	found := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err //nolint:wrapcheck // test walk
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path) //nolint:gosec // path comes from walking the module directory
		if rerr != nil {
			return rerr //nolint:wrapcheck // test walk
		}
		for _, m := range re.FindAllSubmatch(src, -1) {
			found[string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("found no section key literals in the module's Go code; the scan is broken")
	}
	for key := range found {
		if _, ok := cat[key]; !ok {
			t.Errorf("section key %q appears in the module's Go code but is missing from web/src/modules/volunteers/messages/en.json", key)
		}
	}
}
