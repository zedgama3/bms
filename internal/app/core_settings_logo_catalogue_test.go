package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Every core.settings.logo.* key the spec names under UI exists in the catalogue.
func TestCatalogue_LogoKeysExist(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "web", "src", "core", "settings", "messages", "en.json")) //nolint:gosec // a fixed repository path
	if err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	var cat map[string]string
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatalf("parse the catalogue: %v", err)
	}
	for _, k := range []string{
		"heading", "current_alt", "unavailable", "none", "field", "hint", "upload", "replace",
		"uploaded", "remove", "remove_confirm", "removed", "unsupported",
	} {
		if cat["core.settings.logo."+k] == "" {
			t.Errorf("core.settings.logo.%s is missing from the catalogue", k)
		}
	}
}
