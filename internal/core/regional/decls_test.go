package regional_test

// The regional.* declarations' own rules (moved from settings' d40 tests, which drove them
// through Set; here the Decl.Check is called directly, because Set needs a real store and
// lives in settings). Every host and name is invented.

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func declOf(t testing.TB, key string) settings.Decl {
	t.Helper()
	for _, d := range regional.Settings {
		if d.Key == key {
			return d
		}
	}
	t.Fatalf("regional.Settings declares no %q", key)
	return settings.Decl{}
}

// checkOf runs key's Check and returns its field error's code and reason param ("" "" when
// the value passes).
func checkOf(t testing.TB, key, value string) (code, reason string, refused bool) {
	t.Helper()
	d := declOf(t, key)
	if d.Check == nil {
		return "", "", false
	}
	err := d.Check(value)
	if err == nil {
		return "", "", false
	}
	for _, fe := range validate.Fields(err) {
		return fe.Code, fe.Params["reason"], true
	}
	return "rejected", "", true
}

func TestSet_PublicURL_Refusals_Table(t *testing.T) {
	tests := []struct {
		name, in, reason string
	}{
		{"no scheme", "burn.example.test", "scheme"},
		{"ftp", "ftp://burn.example.test", "scheme"},
		{"javascript", "javascript:alert(1)", "scheme"},
		{"scheme-relative", "//burn.example.test", "scheme"},
		{"relative path", "/bms", "scheme"},
		{"no host", "https://", "host"},
		{"no host with path", "https:///bms", "host"},
		{"only a port", "https://:8080", "host"},
		{"port zero", "https://burn.example.test:0", "host"},
		{"port too large", "https://burn.example.test:65536", "host"},
		{"port not a number", "https://burn.example.test:eight", "host"},
		{"userinfo with password", "https://wren:pw@burn.example.test", "userinfo"},
		{"userinfo name only", "https://wren@burn.example.test", "userinfo"},
		{"query", "https://burn.example.test/?a=1", "query"},
		{"empty query", "https://burn.example.test?", "query"},
		{"fragment", "https://burn.example.test/#top", "fragment"},
		{"empty fragment", "https://burn.example.test#", "fragment"},
		{"inner space", "https://burn .example.test", "chars"},
		{"space in path", "https://burn.example.test/a b", "chars"},
		{"tab", "https://burn.example.test/\tx", "chars"},
		{"newline", "https://burn.example.test/\nx", "chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, reason, refused := checkOf(t, "public_url", tc.in)
			if !refused || code != "url" || reason != tc.reason {
				t.Errorf("code %q reason %q refused %v, want url / %s", code, reason, refused, tc.reason)
			}
			if err := declOf(t, "public_url").Check(tc.in); err != nil &&
				(strings.Contains(err.Error(), "wren") || strings.Contains(err.Error(), ":pw@")) {
				t.Error("the error echoes the URL's userinfo")
			}
		})
	}
}

func TestSet_PublicURL_TooLong_Table(t *testing.T) {
	const prefix = "https://burn.example.test/"
	for name, in := range map[string]string{
		"just over": prefix + strings.Repeat("a", settings.MaxURLBytes-len(prefix)+1),
		"far over":  prefix + strings.Repeat("a", 5000),
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, refused := checkOf(t, "public_url", in); !refused || code != "too_long" {
				t.Errorf("code %q refused %v, want too_long", code, refused)
			}
		})
	}
	t.Run("exactly the limit passes", func(t *testing.T) {
		in := prefix + strings.Repeat("a", settings.MaxURLBytes-len(prefix))
		if code, _, refused := checkOf(t, "public_url", in); refused {
			t.Errorf("a URL of exactly MaxURLBytes was refused: %q", code)
		}
	})
}

// A zero-width space (a Unicode format character) is refused as url / chars, in the host, the
// path and at the edges.
func TestSet_PublicURL_FormatCharacters_Refused(t *testing.T) {
	for name, in := range map[string]string{
		"in the host":      "https://burn\u200b.example.test",
		"in the path":      "https://burn.example.test/a\u200bb",
		"after the scheme": "https://\u200bburn.example.test",
		"word joiner":      "https://burn.example.test/\u2060",
	} {
		t.Run(name, func(t *testing.T) {
			code, reason, refused := checkOf(t, "public_url", in)
			if !refused || code != "url" || reason != "chars" {
				t.Errorf("code %q reason %q refused %v, want url / chars", code, reason, refused)
			}
		})
	}
}

func TestSet_PublicURL_EmptyIsUnset(t *testing.T) {
	if _, _, refused := checkOf(t, "public_url", ""); refused {
		t.Error("an empty public_url was refused; empty means unset")
	}
}

func TestSet_Regional_Contrast_Table(t *testing.T) {
	tests := []struct {
		name, key, value string
		ok               bool
	}{
		{"light ok", "color_primary", "#9a3412", true},
		{"light too pale", "color_primary", "#f97316", false},
		{"light white on white", "color_primary", "#ffffff", false},
		{"light just usable black", "color_primary", "#000000", true},
		{"dark ok", "color_primary_dark", "#f97316", true},
		{"dark too dark", "color_primary_dark", "#1e3a8a", false},
		{"not a colour", "color_primary", "orange", false},
		{"short hex", "color_primary", "#fff", false},
		{"empty is the shipped palette", "color_primary", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := declOf(t, tc.key).Check(tc.value)
			if tc.ok {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted")
			}
			for _, fe := range validate.Fields(err) {
				if fe.Code == "contrast" && (fe.Params["min"] != "4.5" || fe.Params["ratio"] == "") {
					t.Errorf("params = %v", fe.Params)
				}
			}
		})
	}
}

func TestSet_Regional_ContrastKnownPairs_Unit(t *testing.T) {
	// #f97316 on white is about 2.8:1 (the ratio reads down, never up to the minimum).
	err := declOf(t, "color_primary").Check("#f97316")
	var ratio string
	for _, fe := range validate.Fields(err) {
		if fe.Code == "contrast" {
			ratio = fe.Params["ratio"]
		}
	}
	if !strings.HasPrefix(ratio, "2.") && !strings.HasPrefix(ratio, "3.") {
		t.Errorf("ratio for #f97316 on white = %q, want about 2.8", ratio)
	}
}

func TestSet_Regional_NameContactZoneLanguage_Table(t *testing.T) {
	tests := []struct {
		name, key, value, code string // code "" = accepted
	}{
		{"name ok", "name", "Prairie Dog Burn", ""},
		{"name empty", "name", "", "required"},
		{"name control char", "name", "Prairie\x07Dog", "control_chars"},
		{"name too long", "name", strings.Repeat("n", settings.MaxNameBytes+1), "too_long"},
		{"name at the limit", "name", strings.Repeat("n", settings.MaxNameBytes), ""},
		{"contact ok", "contact_email", "hello@burn.example.test", ""},
		{"contact empty", "contact_email", "", ""},
		{"contact malformed", "contact_email", "not-an-address", "malformed"},
		{"zone ok", "timezone", "America/Denver", ""},
		{"zone empty", "timezone", "", ""},
		{"zone unknown", "timezone", "Mars/Olympus", "unknown"},
		{"zone Local", "timezone", "Local", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, refused := checkOf(t, tc.key, tc.value)
			switch {
			case tc.code == "" && refused:
				t.Errorf("refused with %q", code)
			case tc.code != "" && (!refused || code != tc.code):
				t.Errorf("code %q refused %v, want %q", code, refused, tc.code)
			}
		})
	}
}

// The declared defaults are the spec's: the name "BMS", the language "en", the rest unset.
func TestSettings_Defaults(t *testing.T) {
	want := map[string]string{"name": "BMS", "language": "en"}
	for _, d := range regional.Settings {
		if d.Default != want[d.Key] {
			t.Errorf("%s default = %q, want %q", d.Key, d.Default, want[d.Key])
		}
	}
	lang := declOf(t, "language")
	if lang.Kind != settings.Enum || strings.Join(lang.Options, ",") != strings.Join(settings.Languages, ",") {
		t.Errorf("language = %v %v, want an enum of settings.Languages", lang.Kind, lang.Options)
	}
}

// Normalize then Check is what Set does: the canonical form of a valid URL (moved from
// settings: TestSet_PublicURL_Canonicalised_Table and _FixedPoints).
func TestSet_PublicURL_Canonicalised_Table(t *testing.T) {
	d := declOf(t, "public_url")
	if d.Normalize == nil {
		t.Fatal("regional.public_url has no Normalize; its canonical form is lost")
	}
	tests := []struct{ name, in, want string }{
		{"trailing slash", "https://burn.example.test/", "https://burn.example.test"},
		{"many trailing slashes", "https://burn.example.test////", "https://burn.example.test"},
		{"upper-case scheme", "HTTPS://burn.example.test", "https://burn.example.test"},
		{"upper-case host", "https://BURN.Example.TEST", "https://burn.example.test"},
		{"mixed case everywhere", "HtTp://Burn.EXAMPLE.test:8080/", "http://burn.example.test:8080"},
		{"sub-path kept as given", "https://burn.example.test/Bms/", "https://burn.example.test/Bms"},
		{"port 1", "https://burn.example.test:1", "https://burn.example.test:1"},
		{"port 65535", "https://burn.example.test:65535", "https://burn.example.test:65535"},
		{"already canonical", "http://burn.example.test/bms", "http://burn.example.test/bms"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := d.Normalize(tc.in)
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if err := d.Check(got); err != nil {
				t.Errorf("the canonical form %q is refused: %v", got, err)
			}
			if again := d.Normalize(got); again != got {
				t.Errorf("not a fixed point: %q -> %q", got, again)
			}
		})
	}
}

func TestSet_PublicURL_FixedPoints(t *testing.T) {
	d := declOf(t, "public_url")
	for name, in := range map[string]string{
		"ipv6 zone":            "http://[fe80::1%25eth0]:8080",
		"percent-encoded host": "https://ex%C3%A9mple.org",
	} {
		t.Run(name, func(t *testing.T) {
			got := d.Normalize(in)
			// the host is lower-cased as a whole, so %C3 reads back as %c3: the same bytes
			if want := strings.ToLower(in); got != want {
				t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
			}
			if err := d.Check(got); err != nil {
				t.Errorf("refused: %v", err)
			}
			if d.Normalize(got) != got {
				t.Error("saving the read-back value again changes it")
			}
		})
	}
}

// A value Normalize cannot improve is left as it was, so Check reports why it is refused.
func TestSet_PublicURL_NormalizeLeavesRefusedValuesAlone(t *testing.T) {
	d := declOf(t, "public_url")
	for _, in := range []string{"burn.example.test", "https://wren:pw@burn.example.test", "ftp://x.example.test"} {
		if got := d.Normalize(in); got != in {
			t.Errorf("Normalize(%q) = %q; a refused value must come back unchanged", in, got)
		}
	}
}

func TestSet_Regional_ColoursAreLowercasedOnSave(t *testing.T) {
	for _, key := range []string{"color_primary", "color_primary_dark"} {
		d := declOf(t, key)
		if d.Normalize == nil || d.Normalize("#9A3412") != "#9a3412" {
			t.Errorf("%s does not lower-case a colour on save", key)
		}
	}
}
