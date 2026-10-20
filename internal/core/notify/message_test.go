package notify_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func okMessage() notify.Message {
	return notify.Message{Template: "volunteers.greet", To: []model.PersonID{newPerson()}}
}

func TestMessageValidate_Valid_Nil(t *testing.T) {
	m := okMessage()
	m.Key = "shift:42.reminder_a-b"
	m.Vars = map[string]string{"shift": "Gate\tone\nline two", "a_1": ""}
	m.Link = "/volunteers/shifts/42?x=1#top"
	wantNoErr(t, m.Validate())
}

func TestMessageValidate_Template_Table(t *testing.T) {
	cases := []struct {
		name, tmpl, code string
	}{
		{"empty", "", "required"},
		{"no dot", "volunteers", "malformed"},
		{"upper case", "Volunteers.greet", "malformed"},
		{"leading digit owner", "1abc.greet", "malformed"},
		{"leading digit ident", "volunteers.1x", "malformed"},
		{"empty ident", "volunteers.", "malformed"},
		{"empty owner", ".greet", "malformed"},
		{"double hyphen", "volunteers.gr--eet", "malformed"},
		{"space", "volunteers.gr eet", "malformed"},
		{"too long", "volunteers." + strings.Repeat("a", 90), "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := okMessage()
			m.Template = notify.Template(tc.tmpl)
			err := m.Validate()
			wantField(t, err, "template", tc.code)
			if err2 := m.Template.Validate(); err2 == nil {
				t.Fatal("Template.Validate accepted it")
			} else {
				wantKind(t, err2, errs.Invalid)
			}
		})
	}
	t.Run("hyphenated names are fine", func(t *testing.T) {
		m := okMessage()
		m.Template = "after-reports.post-burn-2"
		wantNoErr(t, m.Validate())
		if got := m.Template.Owner(); got != "after-reports" {
			t.Fatalf("Owner = %q", got)
		}
	})
}

func TestMessageValidate_To_Table(t *testing.T) {
	ids := func(n int) []model.PersonID {
		out := make([]model.PersonID, n)
		for i := range out {
			out[i] = newPerson()
		}
		return out
	}
	dup := newPerson()
	cases := []struct {
		name string
		to   []model.PersonID
		code string // "" = valid
	}{
		{"nil", nil, "required"},
		{"empty", []model.PersonID{}, "required"},
		{"one", ids(1), ""},
		{"exactly the maximum", ids(notify.MaxRecipients), ""},
		{"one over the maximum", ids(notify.MaxRecipients + 1), "too_many"},
		{"zero id", []model.PersonID{newPerson(), {}}, "malformed"},
		{"only the zero id", []model.PersonID{{}}, "malformed"},
		{"duplicate adjacent", []model.PersonID{dup, dup}, "duplicate"},
		{"duplicate first and last", []model.PersonID{dup, newPerson(), dup}, "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := okMessage()
			m.To = tc.to
			err := m.Validate()
			if tc.code == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "to", tc.code)
			if tc.code == "too_many" && paramOf(err, "to", "too_many", "max") != "100" {
				t.Fatalf("too_many max param = %q", paramOf(err, "to", "too_many", "max"))
			}
		})
	}
}

func TestMessageValidate_Key_Table(t *testing.T) {
	cases := []struct {
		name, key, code string
	}{
		{"empty is optional", "", ""},
		{"all allowed punctuation", "aZ09._:-", ""},
		{"exactly the maximum", strings.Repeat("k", notify.MaxKeyBytes), ""},
		{"one over", strings.Repeat("k", notify.MaxKeyBytes+1), "too_long"},
		{"space", "a b", "malformed"},
		{"slash", "a/b", "malformed"},
		{"newline", "a\nb", "malformed"},
		{"non-ascii", "clé", "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := okMessage()
			m.Key = tc.key
			err := m.Validate()
			if tc.code == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "key", tc.code)
			if tc.code == "too_long" && paramOf(err, "key", "too_long", "max") != "128" {
				t.Fatalf("max param = %q", paramOf(err, "key", "too_long", "max"))
			}
		})
	}
}

func TestMessageValidate_Vars_Table(t *testing.T) {
	many := func(n int) map[string]string {
		m := map[string]string{}
		for i := 0; i < n; i++ {
			m["k"+strings.Repeat("a", i/26)+string(rune('a'+i%26))] = "v"
		}
		return m
	}
	cases := []struct {
		name  string
		vars  map[string]string
		field string // "" = valid
		code  string
	}{
		{"nil", nil, "", ""},
		{"exactly the maximum count", many(notify.MaxVars), "", ""},
		{"one over the count", many(notify.MaxVars + 1), "vars", "too_many"},
		{"key upper case", map[string]string{"Shift": "x"}, "vars.Shift", "malformed"},
		{"key leading digit", map[string]string{"1a": "x"}, "vars.1a", "malformed"},
		{"key with hyphen", map[string]string{"a-b": "x"}, "vars.a-b", "malformed"},
		{"key empty", map[string]string{"": "x"}, "", "malformed"},
		{"key at 32 bytes", map[string]string{"a" + strings.Repeat("b", 31): "x"}, "", ""},
		{"key at 33 bytes", map[string]string{"a" + strings.Repeat("b", 32): "x"}, "vars.a" + strings.Repeat("b", 32), "malformed"},
		{"value at the maximum", map[string]string{"a": strings.Repeat("x", notify.MaxVarBytes)}, "", ""},
		{"value one over", map[string]string{"a": strings.Repeat("x", notify.MaxVarBytes+1)}, "vars.a", "too_long"},
		{"value bad utf-8", map[string]string{"a": "bad\xff\xfe"}, "vars.a", "malformed"},
		{"value bell", map[string]string{"a": "x\x07y"}, "vars.a", "control_chars"},
		{"value NUL", map[string]string{"a": "x\x00y"}, "vars.a", "control_chars"},
		{"value carriage return", map[string]string{"a": "x\ry"}, "vars.a", "control_chars"},
		{"value newline and tab are fine", map[string]string{"a": "x\n\ty"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := okMessage()
			m.Vars = tc.vars
			err := m.Validate()
			if tc.code == "" {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
			if tc.field == "" {
				if len(validate.Fields(err)) == 0 {
					t.Fatalf("no field errors in %v", err)
				}
				return
			}
			// A malformed KEY cannot safely be named in a field path (it is caller input), so
			// the spec's "vars" or "vars.<key>" are both accepted for it (spec ambiguity, F4).
			if tc.code == "malformed" && strings.HasPrefix(tc.field, "vars.") && !hasField(err, tc.field, tc.code) {
				wantField(t, err, "vars", "malformed")
				return
			}
			wantField(t, err, tc.field, tc.code)
		})
	}
}

func TestMessageValidate_Link_Table(t *testing.T) {
	cases := []struct {
		name, link, code string
	}{
		{"empty is optional", "", ""},
		{"root", "/", ""},
		{"path with query", "/a/b?x=1&y=2#z", ""},
		{"exactly the maximum", "/" + strings.Repeat("a", notify.MaxLinkBytes-1), ""},
		{"one over", "/" + strings.Repeat("a", notify.MaxLinkBytes), "too_long"},
		{"https scheme", "https://evil.example/x", "malformed"},
		{"javascript scheme", "javascript:alert(1)", "malformed"},
		{"protocol relative", "//evil.example/x", "malformed"},
		{"no leading slash", "volunteers/shifts", "malformed"},
		{"newline", "/a\nb", "control_chars"},
		{"NUL", "/a\x00b", "control_chars"},
		{"carriage return", "/a\rb", "control_chars"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := okMessage()
			m.Link = tc.link
			err := m.Validate()
			if tc.code == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "link", tc.code)
		})
	}
}

func TestMessageValidate_ManyProblems_AllReportedTogether(t *testing.T) {
	m := notify.Message{
		Template: "", To: nil, Key: "bad key",
		Vars: map[string]string{"BAD": "x", "ok": "x\x07"}, Link: "https://x",
	}
	err := m.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"template", "to", "key", "vars", "vars.ok", "link"} {
		if !hasField(err, f, "") {
			t.Errorf("missing field error %q in %v", f, describe(err))
		}
	}
}

func TestMessageValidate_ErrorsNeverEchoValues(t *testing.T) {
	const marker = "ECHOMARK-4417"
	m := notify.Message{
		Template: notify.Template("Bad " + marker),
		To:       []model.PersonID{{}},
		Key:      "bad key " + marker,
		Vars:     map[string]string{"k" + marker: "v", "ok": "x\x07" + marker},
		Link:     "https://" + marker,
	}
	err := m.Validate()
	wantKind(t, err, errs.Invalid)
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("validation error echoes caller input: %v", err)
	}
	for _, fe := range validate.Fields(err) {
		if strings.Contains(fe.Reason, marker) {
			t.Fatalf("field reason echoes caller input: %q", fe.Reason)
		}
	}
}

func TestLanguageValidate_Table(t *testing.T) {
	cases := []struct {
		in   string
		okay bool
	}{
		{"", true}, {"en", true}, {"es", true}, {"es-MX", true}, {"zh-Hant-TW", true}, {"fil", true},
		{"EN", false}, {"e", false}, {"en_US", false}, {"en-", false}, {"english", false},
		{"en US", false}, {"en-" + strings.Repeat("a", 9), false},
		{"en-" + strings.Repeat("a", 33), false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			err := notify.Language(tc.in).Validate()
			if tc.okay {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "", "malformed")
		})
	}
	t.Run("longest accepted is 35 bytes", func(t *testing.T) {
		ok := "en-" + strings.Repeat("a", 8) + "-" + strings.Repeat("b", 8) + "-" + strings.Repeat("c", 8) + "-ab"
		if len(ok) > 35 {
			t.Fatalf("test bug: %d", len(ok))
		}
		wantNoErr(t, notify.Language(ok).Validate())
		wantKind(t, notify.Language(ok+"abcdef").Validate(), errs.Invalid)
	})
}

func TestStatus_StringAndParse_RoundTrip(t *testing.T) {
	// The spec names a Status constant and the Sent counter struct both "Sent"; a Go package
	// cannot declare both, so the sent status is reached through its text.
	for _, name := range []string{"queued", "sending", "sent", "failed"} {
		s, err := notify.ParseStatus(name)
		wantNoErr(t, err)
		str := s.String()
		if str != name {
			t.Fatalf("Status(%q).String() = %q", name, str)
		}
		if str == "" {
			t.Fatalf("status %d has no name", int(s))
		}
		got, err := notify.ParseStatus(str)
		wantNoErr(t, err)
		if got != s {
			t.Fatalf("ParseStatus(%q) = %v, want %v", str, got, s)
		}
	}
	if notify.Queued != 1 {
		t.Fatalf("Queued = %d, the zero value must stay invalid", int(notify.Queued))
	}
	for _, bad := range []string{"", "nope", "QUEUED "} {
		_, err := notify.ParseStatus(bad)
		wantKind(t, err, errs.Invalid)
	}
}

func TestMessageValidate_TTL_Table(t *testing.T) {
	cases := []struct {
		name string
		ttl  time.Duration
		bad  bool
	}{
		{"zero means MaxAge", 0, false},
		{"one nanosecond", time.Nanosecond, false},
		{"one hour", time.Hour, false},
		{"exactly MaxAge", notify.MaxAge, false},
		{"one nanosecond over MaxAge", notify.MaxAge + time.Nanosecond, true},
		{"a year", 365 * 24 * time.Hour, true},
		{"negative", -time.Second, true},
		{"minimum duration", time.Duration(-1 << 63), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := okMessage()
			m.TTL = tc.ttl
			err := m.Validate()
			if !tc.bad {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "ttl", "range")
		})
	}
	if notify.MaxAge != 7*24*time.Hour {
		t.Fatalf("MaxAge = %v, want 7 days", notify.MaxAge)
	}
}
