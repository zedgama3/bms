package module_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The module aliases are the settings types themselves (module-contract, D57), so a
// manifest's Terms can be handed to settings without conversion.
var (
	_ settings.TermDecl = module.TermDecl{}
	_ settings.Forms    = module.TermForms{}
)

func enForms(s string) map[string]module.TermForms {
	return map[string]module.TermForms{"en": {Singular: s, Plural: s + "s", SingularTitle: strings.ToUpper(s[:1]) + s[1:], PluralTitle: strings.ToUpper(s[:1]) + s[1:] + "s"}}
}

func term(key string) module.TermDecl { return module.TermDecl{Key: key, Defaults: enForms("thing")} }

func withTerms(ts ...module.TermDecl) func(*module.Manifest) {
	return func(m *module.Manifest) { m.Terms = ts }
}

func TestManifestValidate_TermsWellFormed_Passes(t *testing.T) {
	m := mf("alpha", withTerms(term("shift"), term("theme_camp_lead")))
	if err := m.Validate(); err != nil {
		t.Fatalf("valid terms: %v", err)
	}
	if err := mf("alpha", withTerms()).Validate(); err != nil {
		t.Fatalf("no terms: %v", err)
	}
}

// F67
func TestManifest_Terms_Invalid_Table(t *testing.T) {
	blank := func(mut func(*module.TermForms)) module.TermDecl {
		d := module.TermDecl{Key: "shift", Defaults: enForms("shift")}
		f := d.Defaults["en"]
		mut(&f)
		d.Defaults["en"] = f
		return d
	}
	tests := []struct {
		name  string
		terms []module.TermDecl
		field string
		code  string // "" matches any
	}{
		{"reserved department", []module.TermDecl{term("department")}, "terms[0].key", "reserved"},
		{"reserved event", []module.TermDecl{term("event")}, "terms[0].key", "reserved"},
		{"reserved lead (promoted to core, D57)", []module.TermDecl{term("lead")}, "terms[0].key", "reserved"},
		{"reserved second entry", []module.TermDecl{term("shift"), term("participant")}, "terms[1].key", "reserved"},
		{"duplicate in the manifest", []module.TermDecl{term("shift"), term("position"), term("shift")}, "terms[2].key", "duplicate"},
		{"empty key", []module.TermDecl{term("")}, "terms[0].key", "format"},
		{"upper case key", []module.TermDecl{term("Shift")}, "terms[0].key", "format"},
		{"leading digit", []module.TermDecl{term("1shift")}, "terms[0].key", "format"},
		{"double underscore", []module.TermDecl{term("a__b")}, "terms[0].key", "format"},
		{"trailing underscore", []module.TermDecl{term("shift_")}, "terms[0].key", "format"},
		{"hyphen", []module.TermDecl{term("my-shift")}, "terms[0].key", "format"},
		{"33 bytes", []module.TermDecl{term("a" + strings.Repeat("b", 32))}, "terms[0].key", "format"},
		{"en missing", []module.TermDecl{{Key: "shift", Defaults: map[string]module.TermForms{}}}, "terms[0].defaults.en", ""},
		{"defaults nil", []module.TermDecl{{Key: "shift"}}, "terms[0].defaults.en", ""},
		{"singular empty", []module.TermDecl{blank(func(f *module.TermForms) { f.Singular = "" })}, "terms[0].defaults.en.singular", "required"},
		{"plural blank", []module.TermDecl{blank(func(f *module.TermForms) { f.Plural = "  " })}, "terms[0].defaults.en.plural", "required"},
		{"singular_title empty", []module.TermDecl{blank(func(f *module.TermForms) { f.SingularTitle = "" })}, "terms[0].defaults.en.singular_title", "required"},
		{"plural_title empty", []module.TermDecl{blank(func(f *module.TermForms) { f.PluralTitle = "" })}, "terms[0].defaults.en.plural_title", "required"},
		{"form too long", []module.TermDecl{blank(func(f *module.TermForms) { f.Plural = strings.Repeat("x", settings.MaxTermBytes+1) })}, "terms[0].defaults.en.plural", "too_long"},
		{"form with a control character", []module.TermDecl{blank(func(f *module.TermForms) { f.Singular = "sh\x00ift" })}, "terms[0].defaults.en.singular", "control_chars"},
		{"unknown language", []module.TermDecl{{Key: "shift", Defaults: map[string]module.TermForms{"en": enForms("shift")["en"], "fr": enForms("shift")["en"]}}}, "terms[0].defaults.fr", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mf("alpha", withTerms(tc.terms...)).Validate()
			wantField(t, err, tc.field)
			if tc.code == "" {
				return
			}
			found := false
			for _, f := range fieldList(err) {
				if f.Field == tc.field && f.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("field %s has no code %q; got %v", tc.field, tc.code, fieldList(err))
			}
		})
	}
}

func TestManifestValidate_TermsReportEveryProblem(t *testing.T) {
	err := mf("alpha", withTerms(term("department"), term("Bad"), term("ok"), term("ok"))).Validate()
	for _, f := range []string{"terms[0].key", "terms[1].key", "terms[3].key"} {
		wantField(t, err, f)
	}
}

func TestManifestValidate_TermsDoNotReadMigrations(t *testing.T) {
	m := mf("alpha", withTerms(term("shift")))
	m.Migrations = tripFS{t}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func fieldList(err error) []fieldCode {
	var out []fieldCode
	for _, f := range validate.Fields(err) {
		out = append(out, fieldCode{Field: f.Field, Code: f.Code})
	}
	return out
}

type fieldCode struct{ Field, Code string }

func (f fieldCode) String() string { return fmt.Sprintf("%s/%s", f.Field, f.Code) }

// Plan.Settings carries an enabled module's terms, with or without settings (D57).
func TestPlanSettings_CarriesTerms(t *testing.T) {
	r := simple(t,
		mf("alpha", func(m *module.Manifest) {
			m.Settings = []module.SettingDecl{{Key: "greeting", Kind: module.SettingText, Default: "hello"}}
			m.Terms = []module.TermDecl{term("shift")}
		}),
		mf("beta", withTerms(term("position"), term("qualification"))),
		mf("gamma", withTerms(term("disabled"))),
	)
	p := resolve(t, r, enabled("alpha", "beta"))
	byMod := map[string]module.ModuleSettings{}
	for _, ms := range p.Settings() {
		byMod[ms.Module] = ms
	}
	if got := byMod["alpha"]; len(got.Settings) != 1 || len(got.Terms) != 1 || got.Terms[0].Key != "shift" {
		t.Errorf("alpha = %+v", got)
	}
	got, ok := byMod["beta"]
	if !ok {
		t.Fatal("a module with terms and no settings is missing from Plan.Settings()")
	}
	if len(got.Terms) != 2 || got.Terms[0].Key != "position" || got.Terms[1].Key != "qualification" {
		t.Errorf("beta terms = %+v, want position then qualification in declaration order", got.Terms)
	}
	if _, ok := byMod["gamma"]; ok {
		t.Error("a disabled module's terms were returned")
	}
}
