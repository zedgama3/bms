package telemetry_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// denylist is the spec's "The key denylist", pinned here so a change to the package
// constant shows up as a failing test.
var denylist = []string{
	"email", "e_mail", "legal_name", "first_name", "last_name", "full_name", "display_name",
	"playa_name", "phone", "tel", "mobile", "address", "street", "emergency_contact",
	"password", "passwd", "secret", "token", "authorization", "cookie", "set_cookie",
	"session", "body", "request_body", "response_body", "query", "query_string", "url_full",
	"url_query", "sql", "statement", "args", "params", "form", "narrative", "notes",
	"medical", "diagnosis",
}

func TestConstants_Pinned(t *testing.T) {
	if telemetry.MaxTextBytes != 512 || telemetry.MaxLogValueBytes != 2048 {
		t.Errorf("caps changed: %d %d", telemetry.MaxTextBytes, telemetry.MaxLogValueBytes)
	}
}

func TestDenylist_Pinned(t *testing.T) {
	for _, k := range denylist {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, nil)
			a := telemetry.Text(k, marker)
			if a.Key() != k {
				t.Errorf("denied key must be kept, got %q", a.Key())
			}
			sd := h.emit(t, a)
			if sd.Attrs[k] != "[denied]" {
				t.Errorf("value = %q, want [denied]", sd.Attrs[k])
			}
			if !h.obs.hasViolation("denied_key", "") {
				t.Errorf("no denied_key violation: %+v", h.obs.violations())
			}
			if strings.Contains(sd.Attrs[k], marker) {
				t.Error("denied value leaked")
			}
		})
	}
}

func TestDenylist_Matching_Table(t *testing.T) {
	cases := []struct {
		key    string
		denied bool
	}{
		{"email", true},
		{"person.email", true},
		{"http_request_body", true},
		{"user.legal_name", true},
		{"auth.token", true},
		{"db.sql", true},
		{"user_agent", false},
		{"db.name", false},
		{"set_name", false},
		{"module", false},
		{"http.route", false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			h := newHarness(t, nil)
			sd := h.emit(t, telemetry.Text(tc.key, "v"))
			got := sd.Attrs[tc.key] == "[denied]"
			if got != tc.denied {
				t.Errorf("denied = %v, want %v (attrs %v)", got, tc.denied, sd.Attrs)
			}
			if h.obs.hasViolation("denied_key", "") != tc.denied {
				t.Errorf("violation mismatch: %+v", h.obs.violations())
			}
		})
	}
}

func TestAttr_DeniedKey_RedactsAndCountsViolation(t *testing.T) {
	ctors := map[string]telemetry.Attr{
		"text":     telemetry.Text("email", marker),
		"token":    telemetry.Token("email", "abc"),
		"int":      telemetry.Int("phone", 5551234),
		"id":       telemetry.ID("session", model.NewID[model.PersonID]()),
		"duration": telemetry.Duration("password", time.Second),
	}
	for name, a := range ctors {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			sd := h.emit(t, a)
			if sd.Attrs[a.Key()] != "[denied]" {
				t.Errorf("attrs = %v", sd.Attrs)
			}
			if !h.obs.hasViolation("denied_key", "") {
				t.Errorf("violations = %+v", h.obs.violations())
			}
		})
	}
}

func TestKeyGrammar_Table(t *testing.T) {
	valid := []string{"a", "http.route", "a_b1", "x9", repeat('k', 64)}
	invalid := []string{"", "Upper", "1abc", "a-b", "a b", ".x", "_x", "k@x", repeat('k', 65)}
	for _, k := range valid {
		t.Run("valid/"+k, func(t *testing.T) {
			h := newHarness(t, nil)
			a := telemetry.Int(k, 1)
			if a.Key() != k {
				t.Errorf("Key() = %q", a.Key())
			}
			sd := h.emit(t, a)
			if sd.Attrs[k] != "1" {
				t.Errorf("attrs = %v", sd.Attrs)
			}
			if len(h.obs.violations()) != 0 {
				t.Errorf("violations = %+v", h.obs.violations())
			}
		})
	}
	for _, k := range invalid {
		t.Run("invalid/"+k, func(t *testing.T) {
			h := newHarness(t, nil)
			a := telemetry.Int(k, 1)
			if a.Key() != "telemetry.invalid_key" {
				t.Errorf("Key() = %q, want telemetry.invalid_key", a.Key())
			}
			sd := h.emit(t, a)
			if _, ok := sd.Attrs["telemetry.invalid_key"]; !ok {
				t.Errorf("attrs = %v", sd.Attrs)
			}
			if !h.obs.hasViolation("invalid_key", "") {
				t.Errorf("violations = %+v", h.obs.violations())
			}
		})
	}
}

func TestAttr_InvalidKey_ReplacedAndCounted(t *testing.T) {
	h := newHarness(t, nil)
	sd := h.emit(t, telemetry.Text("Bad Key", "v"))
	if sd.Attrs["telemetry.invalid_key"] != "Bad Key" {
		t.Errorf("attrs = %v", sd.Attrs)
	}
	if !h.obs.hasViolation("invalid_key", "") {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}

func TestTokenGrammar_Table(t *testing.T) {
	cases := []struct {
		value string
		ok    bool
	}{
		{"/api/people/{id}", true},
		{"/files/{path...}", true},
		{"GET", true},
		{"_OTHER", true},
		{"module.name", true},
		{"signal:hello.greeted", true},
		{"db.SELECT", true},
		{"0", true},
		{repeat('a', 128), true},
		{markerEmail, false},
		{"https://h.example/p?x=1", false},
		{"two words", false},
		{".leading-dot", false},
		{"-leading-dash", false},
		{"", false},
		{repeat('a', 129), false},
		{"a@b", false},
		{"q?x", false},
	}
	for _, tc := range cases {
		name := tc.value
		if len(name) > 20 {
			name = name[:20] + "..."
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			sd := h.emit(t, telemetry.Token("probe", tc.value))
			if tc.ok {
				if sd.Attrs["probe"] != tc.value {
					t.Errorf("value = %q", sd.Attrs["probe"])
				}
				if len(h.obs.violations()) != 0 {
					t.Errorf("violations = %+v", h.obs.violations())
				}
				return
			}
			if sd.Attrs["probe"] != "[invalid-token]" {
				t.Errorf("value = %q, want [invalid-token]", sd.Attrs["probe"])
			}
			if !h.obs.hasViolation("invalid_token", "") {
				t.Errorf("violations = %+v", h.obs.violations())
			}
			if strings.Contains(sd.Attrs["probe"], "marker") {
				t.Error("rejected value leaked")
			}
		})
	}
}

func TestToken_InvalidValue_ReplacedAndCounted(t *testing.T) {
	h := newHarness(t, nil)
	sd := h.emit(t, telemetry.Token("route", "has space"), telemetry.Module("bad value"), telemetry.Module("hello"))
	if sd.Attrs["route"] != "[invalid-token]" {
		t.Errorf("route = %q", sd.Attrs["route"])
	}
	// The last Module wins; check the invalid one separately.
	h2 := newHarness(t, nil)
	sd2 := h2.emit(t, telemetry.Module("bad value"))
	if sd2.Attrs["module"] != "[invalid-token]" {
		t.Errorf("module = %q", sd2.Attrs["module"])
	}
	if sd.Attrs["module"] != "hello" {
		t.Errorf("valid module = %q", sd.Attrs["module"])
	}
	if !h.obs.hasViolation("invalid_token", "") || !h2.obs.hasViolation("invalid_token", "") {
		t.Error("missing invalid_token violation")
	}
}

func TestScrub_Table(t *testing.T) {
	changed := []struct{ in, want string }{
		{"contact " + markerEmail + " today", "contact [email] today"},
		{"a." + markerEmail + ".", ""}, // only checked for no leak below
		{"/path?x=1", "/path?[query]"},
		{"https://h.example/p?x", "https://h.example/p?[query]"},
		{"see https://h.example/p?x=1&y=2 next", "see https://h.example/p?[query] next"},
	}
	for _, tc := range changed {
		got := telemetry.Scrub(tc.in)
		if tc.want != "" && got != tc.want {
			t.Errorf("Scrub(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.Contains(got, "zq9@") {
			t.Errorf("Scrub(%q) leaked: %q", tc.in, got)
		}
		if again := telemetry.Scrub(got); again != got {
			t.Errorf("Scrub not idempotent: %q -> %q", got, again)
		}
	}
	if got := telemetry.Scrub("mailto:" + markerEmail); strings.Contains(got, "zq9") || !strings.Contains(got, "[email]") {
		t.Errorf("mailto not scrubbed: %q", got)
	}
	unchanged := []string{
		"", "plain words", "a@b", "user@host:5432/db",
		"123e4567-e89b-12d3-a456-426614174000", "2026-10-17T12:00:00Z", "why? really",
		"/path/no/query",
	}
	for _, s := range unchanged {
		if got := telemetry.Scrub(s); got != s {
			t.Errorf("Scrub(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestText_ScrubsAndTruncates(t *testing.T) {
	h := newHarness(t, nil)
	long := repeat('a', 800)
	sd := h.emit(t,
		telemetry.Text("short", "mail "+markerEmail+" and /p?x="+marker),
		telemetry.Text("long", long),
		telemetry.Text("multibyte", strings.Repeat("é", 400)),
	)
	if got := sd.Attrs["short"]; !strings.Contains(got, "[email]") || strings.Contains(got, "zq9@") || strings.Contains(got, marker) {
		t.Errorf("short = %q", got)
	}
	got := sd.Attrs["long"]
	if len(got) > telemetry.MaxTextBytes || !strings.HasSuffix(got, "…") || len(got) >= len(long) {
		t.Errorf("long len=%d suffix=%q", len(got), got[len(got)-3:])
	}
	if mb := sd.Attrs["multibyte"]; !utf8.ValidString(mb) || len(mb) > telemetry.MaxTextBytes {
		t.Errorf("multibyte not cut cleanly: len=%d valid=%v", len(mb), utf8.ValidString(mb))
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("Text use is expected, got violations %+v", h.obs.violations())
	}
}

func TestID_ZeroIsIgnored(t *testing.T) {
	h := newHarness(t, nil)
	var zero model.PersonID
	z := telemetry.ID("person_id", zero)
	if z.Key() != "" {
		t.Errorf("zero ID should give the zero Attr, key %q", z.Key())
	}
	id := model.NewID[model.PersonID]()
	sd := h.emit(t, z, telemetry.ID("person_id", id), telemetry.Attr{})
	if sd.Attrs["person_id"] != id.String() || len(sd.Attrs) != 1 {
		t.Errorf("attrs = %v", sd.Attrs)
	}
}

func TestAttr_Constructors_SuccessPaths(t *testing.T) {
	h := newHarness(t, nil)
	sd := h.emit(t,
		telemetry.Int("n", 42),
		telemetry.Int64("big", 1<<40),
		telemetry.Float("ratio", 1.5),
		telemetry.Bool("ok", true),
		telemetry.Duration("elapsed_ms", 1500*time.Millisecond),
		telemetry.Kind(errs.Conflict),
		telemetry.Module("hello"),
	)
	want := map[string]string{
		"n": "42", "big": "1099511627776", "ok": "true", "elapsed_ms": "1500",
		"error.kind": errs.Conflict.String(), "module": "hello",
	}
	for k, v := range want {
		if sd.Attrs[k] != v {
			t.Errorf("%s = %q, want %q", k, sd.Attrs[k], v)
		}
	}
	if !strings.Contains(sd.Attrs["ratio"], "1.5") {
		t.Errorf("ratio = %q", sd.Attrs["ratio"])
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}
