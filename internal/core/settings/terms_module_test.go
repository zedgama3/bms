package settings_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Module-declared terms (Amendment 2026-10-30, D57): F29, F29a, F29b, F31.

func forms(s string) settings.Forms {
	if s == "" {
		return settings.Forms{Singular: "x", Plural: "xs", SingularTitle: "X", PluralTitle: "Xs"}
	}
	title := strings.ToUpper(s[:1]) + s[1:]
	return settings.Forms{Singular: s, Plural: s + "s", SingularTitle: title, PluralTitle: title + "s"}
}

func decl(key string) settings.TermDecl {
	return settings.TermDecl{Key: key, Defaults: map[string]settings.Forms{"en": forms(key)}}
}

// termArea is a module area declaring only terms (a module with no settings still gets one).
func termArea(name string, keys ...string) settings.Area {
	a := settings.Area{Name: name}
	for _, k := range keys {
		a.Terms = append(a.Terms, decl(k))
	}
	return a
}

func withAreas(as ...settings.Area) func(*settings.Options) {
	return func(o *settings.Options) { o.Areas = append(o.Areas, as...) }
}

func TestCoreTerms_GainsLead(t *testing.T) {
	found := false
	for _, term := range settings.CoreTerms {
		if term == "lead" {
			found = true
		}
	}
	if !found {
		t.Fatalf("CoreTerms = %v, want it to contain lead (D57)", settings.CoreTerms)
	}
}

// Decision 7, extended: the shell's boot-fallback term.* entries equal terms/en.json for
// every core term, lead included.
func TestShellTermEntries_EqualEmbeddedDefaults(t *testing.T) {
	b, err := os.ReadFile("terms/en.json")
	if err != nil {
		t.Fatal(err)
	}
	var defaults map[string]settings.Forms
	if err := json.Unmarshal(b, &defaults); err != nil {
		t.Fatal(err)
	}
	sb, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "shell", "messages", "en.json"))
	if err != nil {
		t.Skipf("the shell catalogue is absent: %v", err)
	}
	var shell map[string]string
	if err := json.Unmarshal(sb, &shell); err != nil {
		t.Fatal(err)
	}
	for _, term := range settings.CoreTerms {
		d, ok := defaults[term]
		if !ok {
			t.Errorf("terms/en.json lacks %s", term)
			continue
		}
		want := map[string]string{
			"term." + term:                   d.Singular,
			"term." + term + ".plural":       d.Plural,
			"term." + term + ".title":        d.SingularTitle,
			"term." + term + ".plural_title": d.PluralTitle,
		}
		for k, v := range want {
			if got, ok := shell[k]; !ok || got != v {
				t.Errorf("shell %s = %q (present %v), want %q", k, got, ok, v)
			}
		}
	}
}

// F29b
func TestTermDecl_Validate_Table(t *testing.T) {
	mod := func(f func(*settings.Forms)) settings.TermDecl {
		d := decl("shift")
		x := d.Defaults["en"]
		f(&x)
		d.Defaults["en"] = x
		return d
	}
	tests := []struct {
		name  string
		d     settings.TermDecl
		field string // "" = valid
		code  string // "" = any
	}{
		{"valid", decl("shift"), "", ""},
		{"valid with underscore", decl("theme_camp_lead"), "", ""},
		{"valid 32 bytes", decl("a" + strings.Repeat("b", 31)), "", ""},
		{"valid with es", settings.TermDecl{Key: "shift", Defaults: map[string]settings.Forms{"en": forms("shift"), "es": forms("turno")}}, "", ""},
		{"empty key", decl(""), "key", "format"},
		{"upper case", decl("Shift"), "key", "format"},
		{"digit first", decl("1x"), "key", "format"},
		{"double underscore", decl("a__b"), "key", "format"},
		{"trailing underscore", decl("a_"), "key", "format"},
		{"space", decl("a b"), "key", "format"},
		{"33 bytes", decl("a" + strings.Repeat("b", 32)), "key", "format"},
		{"reserved department", decl("department"), "key", "reserved"},
		{"reserved event", decl("event"), "key", "reserved"},
		{"reserved theme_camp", decl("theme_camp"), "key", "reserved"},
		{"reserved lead", decl("lead"), "key", "reserved"},
		{"reserved _plural suffix (D58)", decl("shift_plural"), "key", "reserved"},
		{"reserved _title suffix (D58)", decl("shift_title"), "key", "reserved"},
		{"reserved _plural_title (ends in _title)", decl("shift_plural_title"), "key", "reserved"},
		{"reserved _singular_title is a form name", decl("shift_singular_title"), "key", "reserved"},
		{"reserved suffix on a one-letter stem", decl("a_plural"), "key", "reserved"},
		{"valid: plural is not _plural", decl("plural"), "", ""},
		{"valid: title is not _title", decl("title"), "", ""},
		{"valid: suffix in the middle", decl("shift_plural_count"), "", ""},
		{"valid: a longer word", decl("shift_titles"), "", ""},
		{"en missing", settings.TermDecl{Key: "shift", Defaults: map[string]settings.Forms{"es": forms("turno")}}, "defaults.en", ""},
		{"defaults nil", settings.TermDecl{Key: "shift"}, "defaults.en", ""},
		{"unknown language", settings.TermDecl{Key: "shift", Defaults: map[string]settings.Forms{"en": forms("shift"), "fr": forms("quart")}}, "defaults.fr", ""},
		{"singular empty", mod(func(f *settings.Forms) { f.Singular = "" }), "defaults.en.singular", "required"},
		{"plural blank", mod(func(f *settings.Forms) { f.Plural = " \t " }), "defaults.en.plural", "required"},
		{"singular_title empty", mod(func(f *settings.Forms) { f.SingularTitle = "" }), "defaults.en.singular_title", "required"},
		{"plural_title empty", mod(func(f *settings.Forms) { f.PluralTitle = "" }), "defaults.en.plural_title", "required"},
		{"60 bytes ok", mod(func(f *settings.Forms) { f.Plural = strings.Repeat("x", 60) }), "", ""},
		{"61 bytes", mod(func(f *settings.Forms) { f.Plural = strings.Repeat("x", 61) }), "defaults.en.plural", "too_long"},
		{"31 two-byte runes", mod(func(f *settings.Forms) { f.Singular = strings.Repeat("é", 31) }), "defaults.en.singular", "too_long"},
		{"NUL", mod(func(f *settings.Forms) { f.Singular = "sh\x00ift" }), "defaults.en.singular", "control_chars"},
		{"newline", mod(func(f *settings.Forms) { f.PluralTitle = "Shi\nfts" }), "defaults.en.plural_title", "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, tc.code)
			if tc.code == "too_long" {
				if got := paramOf(err, tc.field, "too_long", "max"); got != "60" {
					t.Errorf("max = %q, want 60", got)
				}
			}
		})
	}
	t.Run("every problem is reported", func(t *testing.T) {
		d := settings.TermDecl{Key: "Bad", Defaults: map[string]settings.Forms{"fr": {}}}
		err := d.Validate()
		wantField(t, err, "key", "")
		wantField(t, err, "defaults.fr", "")
	})
}

// F29b, the Area half.
func TestAreaValidate_Terms_Table(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		wantNoErr(t, termArea("volunteers", "shift", "position").Validate())
	})
	t.Run("duplicate within the area", func(t *testing.T) {
		wantField(t, termArea("volunteers", "shift", "position", "shift").Validate(), "terms[2].key", "duplicate")
	})
	t.Run("reserved", func(t *testing.T) {
		for _, term := range settings.CoreTerms {
			wantField(t, termArea("volunteers", "shift", term).Validate(), "terms[1].key", "reserved")
		}
	})
	t.Run("reserved suffixes (D58)", func(t *testing.T) {
		for _, key := range []string{"shift_plural", "shift_title"} {
			wantField(t, termArea("volunteers", "shift", key).Validate(), "terms[1].key", "reserved")
		}
	})
	t.Run("a bad declaration is reported under its index", func(t *testing.T) {
		a := termArea("volunteers", "shift")
		a.Terms = append(a.Terms, settings.TermDecl{Key: "position", Defaults: map[string]settings.Forms{"en": {Singular: "position"}}})
		err := a.Validate()
		wantKind(t, err, errs.Invalid)
		if !hasField(err, "terms[1].defaults.en.plural", "required") {
			t.Fatalf("fields = %v", describe(err))
		}
	})
}

// F29a
func TestNew_DuplicateTermAcrossAreas_ReturnsInvalid(t *testing.T) {
	o, _ := validOptions(t, newUnit(t).fake)
	n := len(o.Areas)
	o.Areas = append(o.Areas, termArea("volunteers", "shift", "position"), termArea("incidents", "report", "shift"))
	_, err := settings.New(o)
	wantKind(t, err, errs.Invalid)
	field := "areas[" + strconv.Itoa(n+1) + "].terms[1].key"
	// Options problems may carry a "settings." prefix (like settings.sealing_key).
	if !hasField(err, field, "duplicate") && !hasField(err, "settings."+field, "duplicate") {
		t.Fatalf("want %s duplicate, got %v", field, describe(err))
	}
}

func TestNew_TermsInTwoAreasDifferent_Builds(t *testing.T) {
	o, _ := validOptions(t, newUnit(t).fake)
	o.Areas = append(o.Areas, termArea("volunteers", "shift"), termArea("incidents", "report"))
	_, err := settings.New(o)
	wantNoErr(t, err)
}

func TestNew_TermReservedOrInvalid_ReturnsInvalid(t *testing.T) {
	for _, key := range []string{"lead", "department", "Bad Key", ""} {
		t.Run(key, func(t *testing.T) {
			o, _ := validOptions(t, newUnit(t).fake)
			o.Areas = append(o.Areas, termArea("volunteers", key))
			_, err := settings.New(o)
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestNew_TermsOnCoreAreaAlsoCount(t *testing.T) {
	o, _ := validOptions(t, newUnit(t).fake)
	core := termArea("incidents", "shift")
	core.Core = true
	o.Areas = append(o.Areas, core, termArea("volunteers", "shift"))
	_, err := settings.New(o)
	wantKind(t, err, errs.Invalid)
}

// F29: a term declared by no area is refused, before any statement.
func TestSetTerms_UndeclaredModuleTerm_ReturnsInvalid(t *testing.T) {
	t.Run("no area declares it", func(t *testing.T) {
		u := newUnit(t)
		_, err := u.svc.SetTerms(u.manager(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty")}}))
		wantField(t, err, "terms.shift", "unknown")
		u.noStatements(t, "an undeclared module term")
	})
	t.Run("another term of the area is declared", func(t *testing.T) {
		u := newUnit(t, withAreas(termArea("volunteers", "shift")))
		_, err := u.svc.SetTerms(u.manager(), "en", termsOf(map[string]settings.FormChanges{"position": {Singular: str("job")}}))
		wantField(t, err, "terms.position", "unknown")
		u.noStatements(t, "an undeclared module term")
	})
	t.Run("a reset naming an unknown form of a module term", func(t *testing.T) {
		u := newUnit(t, withAreas(termArea("volunteers", "shift")))
		_, err := u.svc.SetTerms(u.manager(), "en", termsOf(map[string]settings.FormChanges{"shift": {Reset: []string{"bogus"}}}))
		wantField(t, err, "terms.shift.reset", "unknown")
		u.noStatements(t, "an unknown form")
	})
	t.Run("a declared module term is not unknown", func(t *testing.T) {
		u := newUnit(t, withAreas(termArea("volunteers", "shift")))
		_, err := u.svc.SetTerms(u.manager(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty")}}))
		if hasField(err, "terms.shift", "unknown") {
			t.Errorf("a declared term reported unknown: %v", describe(err))
		}
	})
	t.Run("lead is a core term", func(t *testing.T) {
		u := newUnit(t)
		_, err := u.svc.SetTerms(u.manager(), "en", termsOf(map[string]settings.FormChanges{"lead": {Singular: str("captain")}}))
		if hasField(err, "terms.lead", "unknown") {
			t.Errorf("lead reported unknown: %v", describe(err))
		}
	})
}

func TestSetTerms_ModuleTermFormShape_ReturnsInvalid(t *testing.T) {
	u := newUnit(t, withAreas(termArea("volunteers", "shift")))
	ctx := u.manager()
	_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("  "), Plural: str(repeat("a", 61))}}))
	wantField(t, err, "terms.shift.singular", "required")
	wantField(t, err, "terms.shift.plural", "too_long")
	u.noStatements(t, "a bad module term form")
}

// F31: the bound is the number of declared terms.
func TestSetTerms_TooManyTerms_BoundIsDeclaredCount(t *testing.T) {
	u := newUnit(t, withAreas(termArea("volunteers", "shift", "position")))
	ctx := u.manager()
	declared := len(settings.CoreTerms) + 2
	batch := func(n int) settings.TermChanges {
		m := map[string]settings.FormChanges{}
		for i := range n {
			m["t"+strconv.Itoa(i)] = settings.FormChanges{Singular: str("x")}
		}
		return termsOf(m)
	}
	_, err := u.svc.SetTerms(ctx, "en", batch(declared+1))
	wantField(t, err, "terms", "too_many")
	if got := paramOf(err, "terms", "too_many", "max"); got != strconv.Itoa(declared) {
		t.Errorf("max = %q, want %d", got, declared)
	}
	// Exactly the declared count is not too many (it fails later: the names are unknown).
	_, err = u.svc.SetTerms(ctx, "en", batch(declared))
	if hasField(err, "terms", "too_many") {
		t.Errorf("%d terms reported too_many", declared)
	}
	u.noStatements(t, "a bad term batch")
}

func TestSetTerms_EmptyBatch_StillRequired(t *testing.T) {
	u := newUnit(t, withAreas(termArea("volunteers", "shift")))
	_, err := u.svc.SetTerms(u.manager(), "en", settings.TermChanges{})
	wantField(t, err, "terms", "required")
	u.noStatements(t, "an empty batch")
}

// Permission rules are unchanged for module terms: no session, no write.
func TestSetTerms_ModuleTerm_NoSessionOrPermission(t *testing.T) {
	u := newUnit(t, withAreas(termArea("volunteers", "shift")))
	c := termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty")}})
	_, err := u.svc.SetTerms(t.Context(), "en", c)
	wantKind(t, err, errs.Unauthenticated)
	reader := asPerson(t.Context(), u.person(settings.PermissionRead))
	_, err = u.svc.SetTerms(reader, "en", c)
	wantKind(t, err, errs.Permission)
	u.noStatements(t, "denied term writes")
}
