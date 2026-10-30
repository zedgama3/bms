package app_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
)

// Module terms in the catalogue (app F44, volunteers F168, F169; D57).

func readCatalogue(path string) (map[string]string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // reads a fixed repository path
	if err != nil {
		return nil, err //nolint:wrapcheck // test helper; the caller reports the path
	}
	var cat map[string]string
	if err := json.Unmarshal(b, &cat); err != nil {
		return nil, err //nolint:wrapcheck // test helper
	}
	return cat, nil
}

// termKeyProblems lists every declared term whose four module.<name>.term.* entries are
// missing or differ from the manifest's en defaults.
func termKeyProblems(m module.Manifest, cat map[string]string) []string {
	var out []string
	for _, d := range m.Terms {
		en := d.Defaults["en"]
		p := "module." + m.Name + ".term." + d.Key
		for key, want := range map[string]string{
			p:                   en.Singular,
			p + ".plural":       en.Plural,
			p + ".title":        en.SingularTitle,
			p + ".plural_title": en.PluralTitle,
		} {
			got, ok := cat[key]
			switch {
			case !ok:
				out = append(out, "missing "+key)
			case got != want:
				out = append(out, "differs "+key)
			}
		}
	}
	sort.Strings(out)
	return out
}

var (
	messageTermArg = regexp.MustCompile(`\{term_([a-z0-9_]+)\s*[,}]`)
	templateTermFn = regexp.MustCompile(`\{\{[^}]*\bterm\s+"([a-z0-9_]+)"`)
	termSuffixes   = []string{"_plural_title", "_plural", "_title"}
)

// baseTerm maps an argument or template key (shift_plural_title) to its term (shift), given
// the set of declared terms.
func baseTerm(name string, declared map[string]bool) string {
	if declared[name] {
		return name
	}
	for _, s := range termSuffixes {
		if base, ok := strings.CutSuffix(name, s); ok && declared[base] {
			return base
		}
	}
	return name
}

func allowedTerms(m module.Manifest) map[string]bool {
	ok := map[string]bool{}
	for _, c := range settings.CoreTerms {
		ok[c] = true
	}
	for _, d := range m.Terms {
		ok[d.Key] = true
	}
	return ok
}

// termUseProblems lists term names a module's messages (module.<name>.* entries) or
// templates use that are neither core nor its own.
func termUseProblems(m module.Manifest, cat map[string]string) []string {
	allowed := allowedTerms(m)
	seen := map[string]bool{}
	note := func(where, name string) {
		if !allowed[baseTerm(name, allowed)] {
			seen[where+" uses "+name] = true
		}
	}
	prefix := "module." + m.Name + "."
	for key, msg := range cat {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for _, sub := range messageTermArg.FindAllStringSubmatch(msg, -1) {
			note(key, sub[1])
		}
	}
	if m.Templates != nil {
		if err := fs.WalkDir(m.Templates, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, rerr := fs.ReadFile(m.Templates, path)
			if rerr != nil {
				return fmt.Errorf("read template %s: %w", path, rerr)
			}
			for _, sub := range templateTermFn.FindAllStringSubmatch(string(b), -1) {
				note("template "+path, sub[1])
			}
			return nil
		}); err != nil {
			seen["template walk failed: "+err.Error()] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func realModules(t *testing.T, fn func(t *testing.T, m module.Manifest, cat map[string]string)) {
	t.Helper()
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "web", "src")); err != nil {
		t.Skip("web/src is absent")
	}
	for _, m := range module.Default().Manifests() {
		if strings.HasPrefix(m.Name, "zq") {
			continue
		}
		t.Run(m.Name, func(t *testing.T) {
			cat, err := readCatalogue(filepath.Join(root, "web", "src", "modules", m.Name, "messages", "en.json"))
			if err != nil {
				t.Fatalf("module %s catalogue: %v", m.Name, err)
			}
			fn(t, m, cat)
		})
	}
}

// F44, F168
func TestCatalogue_ModuleTerms_MatchManifestDefaults(t *testing.T) {
	realModules(t, func(t *testing.T, m module.Manifest, cat map[string]string) {
		for _, p := range termKeyProblems(m, cat) {
			t.Errorf("module %s: %s", m.Name, p)
		}
	})
}

// F44, F169
func TestCatalogue_TermsUsed_AreDeclared(t *testing.T) {
	realModules(t, func(t *testing.T, m module.Manifest, cat map[string]string) {
		for _, p := range termUseProblems(m, cat) {
			t.Errorf("module %s: %s, which is neither a core term nor declared by the module", m.Name, p)
		}
	})
}

// The checkers themselves, over fixtures that break each rule.
func TestCatalogue_TermKeyChecker_FixtureFails(t *testing.T) {
	m := module.Manifest{Name: "zqfix", Terms: []module.TermDecl{{Key: "gizmo", Defaults: map[string]module.TermForms{
		"en": {Singular: "gizmo", Plural: "gizmos", SingularTitle: "Gizmo", PluralTitle: "Gizmos"},
	}}}}
	good := map[string]string{
		"module.zqfix.term.gizmo": "gizmo", "module.zqfix.term.gizmo.plural": "gizmos",
		"module.zqfix.term.gizmo.title": "Gizmo", "module.zqfix.term.gizmo.plural_title": "Gizmos",
	}
	if p := termKeyProblems(m, good); len(p) != 0 {
		t.Fatalf("a correct catalogue reported %v", p)
	}
	missing := map[string]string{}
	for k, v := range good {
		missing[k] = v
	}
	delete(missing, "module.zqfix.term.gizmo.plural")
	differs := map[string]string{}
	for k, v := range good {
		differs[k] = v
	}
	differs["module.zqfix.term.gizmo.plural_title"] = "Widgets"
	if p := termKeyProblems(m, missing); !slices.Equal(p, []string{"missing module.zqfix.term.gizmo.plural"}) {
		t.Errorf("missing: %v", p)
	}
	if p := termKeyProblems(m, differs); !slices.Equal(p, []string{"differs module.zqfix.term.gizmo.plural_title"}) {
		t.Errorf("differs: %v", p)
	}
	if p := termKeyProblems(m, nil); len(p) != 4 {
		t.Errorf("empty catalogue: %v", p)
	}
}

func TestCatalogue_TermUseChecker_FixtureFails(t *testing.T) {
	m := module.Manifest{
		Name:  "zqfix",
		Terms: []module.TermDecl{{Key: "gizmo"}},
		Templates: fstest.MapFS{
			"ok.en.tmpl":  {Data: []byte("Subject: x\n\n{{term \"department_plural\"}} {{term \"gizmo_title\"}}")},
			"bad.en.tmpl": {Data: []byte("Subject: x\n\n{{if .Vars.a}}{{term \"shift\"}}{{end}}")},
		},
	}
	cat := map[string]string{
		"module.zqfix.a": "A {term_gizmo} and {term_lead_plural} and {term_department_plural_title}.",
		"module.zqfix.b": "{count, plural, one {{term_gizmo}} other {{count} {term_gizmo_plural}}}",
		"module.zqfix.c": "{term_position_plural}",
		"module.other.d": "{term_shift}", // another module's entry: not this module's problem
	}
	got := termUseProblems(m, cat)
	want := []string{"module.zqfix.c uses position_plural", "template bad.en.tmpl uses shift"}
	if !slices.Equal(got, want) {
		t.Errorf("problems = %v, want %v", got, want)
	}
}
