package app_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/model"
)

// expectedKeys are the message keys module-contract decision 8 derives from a manifest.
func expectedKeys(m module.Manifest) []string {
	p := "module." + m.Name
	keys := []string{p + ".title", p + ".description"}
	for _, d := range m.Permissions {
		rest := strings.TrimPrefix(string(d.Name), m.Name+".")
		keys = append(keys, p+".permission."+rest+".title", p+".permission."+rest+".description")
	}
	for _, s := range m.Settings {
		sp := p + ".setting." + s.Key
		keys = append(keys, sp+".title", sp+".description")
		for _, o := range s.Options {
			keys = append(keys, sp+".option."+o)
		}
	}
	for _, n := range m.Publishes {
		keys = append(keys, p+".signal."+strings.TrimPrefix(string(n), m.Name+".")+".title")
	}
	return keys
}

// missingKeys reads the flat catalogue at path and returns the derived keys it lacks.
func missingKeys(m module.Manifest, path string) ([]string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // reads a fixed repository path
	if err != nil {
		return nil, err //nolint:wrapcheck // test helper; the caller reports the path
	}
	var cat map[string]string
	if err := json.Unmarshal(b, &cat); err != nil {
		return nil, errors.Join(errors.New("catalogue is not flat JSON"), err)
	}
	var missing []string
	for _, k := range expectedKeys(m) {
		if _, ok := cat[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing, nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// F30 over the real tree. The invented zq* modules this package registers have no web
// folder and are skipped.
func TestMessageCatalogues_EnglishCoversManifests(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "web", "src")); err != nil {
		t.Skip("web/src is absent")
	}
	for _, m := range module.Default().Manifests() {
		if strings.HasPrefix(m.Name, "zq") {
			continue
		}
		t.Run(m.Name, func(t *testing.T) {
			path := filepath.Join(root, "web", "src", "modules", m.Name, "messages", "en.json")
			missing, err := missingKeys(m, path)
			if err != nil {
				t.Fatalf("module %s: web/src/modules/%s/messages/en.json: %v", m.Name, m.Name, err)
			}
			for _, k := range missing {
				t.Errorf("module %s lacks catalogue key %s", m.Name, k)
			}
		})
	}
}

// The checker itself, over a fixture that lacks keys.
func TestMessageCatalogues_FixtureMissingKey_Fails(t *testing.T) {
	m := module.Manifest{
		Name: "zqfix",
		Permissions: []authz.PermissionDecl{
			{Name: "zqfix.things.read", Scope: authz.ScopeSite, Reaches: model.SensitivityInternal},
		},
		Settings: []module.SettingDecl{
			{Key: "mode", Kind: module.SettingEnum, Default: "fast", Options: []string{"fast", "slow"}},
		},
		Publishes: []signals.Name{"zqfix.thing-made"},
	}
	missing, err := missingKeys(m, filepath.Join("testdata", "catalogue_partial.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"module.zqfix.permission.things.read.description",
		"module.zqfix.setting.mode.description",
		"module.zqfix.setting.mode.option.slow",
		"module.zqfix.signal.thing-made.title",
	}
	slices.Sort(missing)
	if !slices.Equal(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
	if _, err := missingKeys(m, filepath.Join("testdata", "no-such-file.json")); err == nil {
		t.Error("a missing catalogue file must fail")
	}
}
