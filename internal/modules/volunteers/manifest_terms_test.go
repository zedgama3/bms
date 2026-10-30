package volunteers_test

import (
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/modules/volunteers"
)

// F167 (Amendment V3, D57): the module declares exactly shift, position and qualification,
// English only, all four forms; lead is a core term, not this module's.
func TestManifest_Terms_DeclaresShiftPositionQualification(t *testing.T) {
	want := []struct {
		key                          string
		singular, plural, sTitle, pT string
	}{
		{"shift", "shift", "shifts", "Shift", "Shifts"},
		{"position", "position", "positions", "Position", "Positions"},
		{"qualification", "qualification", "qualifications", "Qualification", "Qualifications"},
	}
	got := volunteers.Manifest.Terms
	if len(got) != len(want) {
		t.Fatalf("%d terms declared, want %d (shift, position, qualification)", len(got), len(want))
	}
	for i, w := range want {
		d := got[i]
		if d.Key != w.key {
			t.Errorf("terms[%d].Key = %q, want %q", i, d.Key, w.key)
			continue
		}
		if len(d.Defaults) != 1 {
			t.Errorf("%s declares languages %v, want en only", w.key, termLangsOf(d.Defaults))
		}
		en, ok := d.Defaults["en"]
		if !ok {
			t.Errorf("%s has no en defaults", w.key)
			continue
		}
		if en.Singular != w.singular || en.Plural != w.plural || en.SingularTitle != w.sTitle || en.PluralTitle != w.pT {
			t.Errorf("%s en = %+v", w.key, en)
		}
	}
}

func termLangsOf[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestManifest_Terms_AreValidAndNotCore(t *testing.T) {
	if err := volunteers.Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
	for _, d := range volunteers.Manifest.Terms {
		wantNoErrT(t, d.Validate())
		if slices.Contains(settings.CoreTerms, d.Key) {
			t.Errorf("term %q is a core term; declaring it too is a start-up error", d.Key)
		}
	}
	if slices.ContainsFunc(volunteers.Manifest.Terms, func(d module.TermDecl) bool { return d.Key == "lead" }) {
		t.Error("lead is a core term (D57), not this module's")
	}
	if !slices.Contains(settings.CoreTerms, "lead") {
		t.Error("settings.CoreTerms lacks lead")
	}
}

// The module's terms and core's are one flat namespace: settings accepts the module's area.
func TestManifest_Terms_SettingsAcceptsTheArea(t *testing.T) {
	a := settings.Area{Name: "volunteers", Terms: volunteers.Manifest.Terms}
	wantNoErrT(t, a.Validate())
}

var (
	templateTermFn = regexp.MustCompile(`\{\{[^}]*\bterm\s+"([a-z0-9_]+)"`)
	fixedWord      = regexp.MustCompile(`(?i)\b(shifts?|positions?|qualifications?|leads?)\b`)
)

// V3.4: the templates name their words through term, and only core terms or this module's.
func TestTemplates_UseOnlyCoreAndOwnTerms(t *testing.T) {
	allowed := map[string]bool{}
	for _, c := range settings.CoreTerms {
		allowed[c] = true
	}
	for _, d := range volunteers.Manifest.Terms {
		allowed[d.Key] = true
	}
	n := 0
	err := fs.WalkDir(volunteers.Manifest.Templates, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(volunteers.Manifest.Templates, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		n++
		for _, m := range templateTermFn.FindAllStringSubmatch(string(b), -1) {
			name := m[1]
			base := name
			for _, s := range []string{"_plural_title", "_plural", "_title"} {
				if b, ok := strings.CutSuffix(name, s); ok && allowed[b] {
					base = b
				}
			}
			if !allowed[base] {
				t.Errorf("%s uses term %q, which is neither core nor this module's", path, name)
			}
		}
		return nil
	})
	wantNoErrT(t, err)
	if n == 0 {
		t.Fatal("the manifest carries no templates")
	}
}

// V3.4: a fixed "shift", "position", "qualification" or "lead" in running text of a
// template is a regional word hard-coded. Variable names (.Vars.is_lead) are not text.
func TestTemplates_NoFixedRegionalWords(t *testing.T) {
	err := fs.WalkDir(volunteers.Manifest.Templates, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(volunteers.Manifest.Templates, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		text := string(b)
		// Strip actions ({{ ... }}) and the Vars: header, which name variables, not words.
		text = regexp.MustCompile(`\{\{.*?\}\}`).ReplaceAllString(text, " ")
		var lines []string
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "Vars:") {
				continue
			}
			lines = append(lines, l)
		}
		for _, m := range fixedWord.FindAllString(strings.Join(lines, "\n"), -1) {
			t.Errorf("%s says %q as a fixed word; use {{term ...}}", path, m)
		}
		return nil
	})
	wantNoErrT(t, err)
}

func wantNoErrT(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
