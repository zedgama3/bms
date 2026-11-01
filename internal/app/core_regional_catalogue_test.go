package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zedgama3/bms/internal/core/regional"
)

// regional.md, Test plan: core.regional.setting.<key>.title|description for every key, and
// .option.<opt> for every option of an enum key (language).
func TestCatalogue_RegionalSettingKeysExist(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "web", "src", "core", "settings", "messages", "en.json")) //nolint:gosec // a fixed repository path
	if err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	var cat map[string]string
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatalf("parse the catalogue: %v", err)
	}
	for _, d := range regional.Settings {
		base := "core.regional.setting." + d.Key
		for _, suffix := range []string{".title", ".description"} {
			if cat[base+suffix] == "" {
				t.Errorf("%s is missing from the catalogue", base+suffix)
			}
		}
		for _, opt := range d.Options {
			if cat[base+".option."+opt] == "" {
				t.Errorf("%s is missing from the catalogue", base+".option."+opt)
			}
		}
	}
}
