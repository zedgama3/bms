//go:build integration

package regional_test

// The public document, the stylesheet and the profile over a real database and a real
// settings.Service (moved from settings' integration tests: TestGetPublic_*,
// TestGetThemeCSS_*, TestRegional_DefaultsAndOverrides). Every value is invented.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
)

type recorded struct {
	status int
	body   string
	header http.Header
	fail   *failRec
}

// get serves GET path with no session (the three GETs are public) unless ctx carries one.
func (r *irig) get(ctx context.Context, path string, hdr ...string) *recorded {
	r.t.Helper()
	w, f := r.do(ctx, http.MethodGet, path, "", hdr...)
	return &recorded{status: w.Code, body: w.Body.String(), header: w.Header(), fail: f}
}

func TestGetPublic_ServesWithoutASession(t *testing.T) {
	r := newIRig(t)
	r.mustSet("regional.name", "Camp Quill")
	res := r.get(t.Context(), "/public")
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, failures %v", res.status, res.fail.last())
	}
	var pub regional.Public
	wantNoErr(t, json.Unmarshal([]byte(res.body), &pub))
	if pub.Name != "Camp Quill" || pub.Language != "en" || pub.Theme || len(pub.Terms["en"]) == 0 {
		t.Errorf("public = %+v", pub)
	}
	if !slices.Equal(pub.Languages, settings.Languages) {
		t.Errorf("languages = %v, want %v", pub.Languages, settings.Languages)
	}
	if res.header.Get("ETag") == "" {
		t.Error("no ETag")
	}
}

// The shell reads this document before anyone signs in, and its JSON must not change
// (regional.md Open question 1): exactly these keys, and never the colours (they are in
// theme.css) or any other area's value.
func TestGetPublic_FieldSetIsPinned(t *testing.T) {
	r := newIRig(t)
	r.mustSet("regional.name", "Camp Quill", "regional.color_primary", "#9a3412")
	res := r.get(t.Context(), "/public")
	if res.status != http.StatusOK {
		t.Fatalf("status = %d (%v)", res.status, res.fail.last())
	}
	var raw map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal([]byte(res.body), &raw))
	want := []string{"contact_email", "language", "languages", "logo", "name", "public_url", "terms", "theme", "timezone"}
	var got []string
	for k := range raw {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("public document keys = %v, want %v", got, want)
	}
	if strings.Contains(res.body, "9a3412") {
		t.Error("the public document carries a brand colour; they belong in theme.css")
	}
	var theme bool
	wantNoErr(t, json.Unmarshal(raw["theme"], &theme))
	if !theme {
		t.Error("theme is false with a colour set")
	}
}

func TestGetPublic_IfNoneMatch_Returns304(t *testing.T) {
	r := newIRig(t)
	for _, path := range []string{"/public", "/theme.css"} {
		t.Run(path, func(t *testing.T) {
			first := r.get(t.Context(), path)
			etag := first.header.Get("ETag")
			if first.status != http.StatusOK || etag == "" {
				t.Fatalf("status %d, ETag %q", first.status, etag)
			}
			second := r.get(t.Context(), path, "If-None-Match", etag)
			if second.status != http.StatusNotModified || second.body != "" {
				t.Errorf("status = %d, body %q; want 304 and no body", second.status, second.body)
			}
			if second.fail.last() != nil {
				t.Errorf("a 304 went through the failer: %v", second.fail.last())
			}
			stale := r.get(t.Context(), path, "If-None-Match", `"not-the-etag"`)
			if stale.status != http.StatusOK {
				t.Errorf("a different ETag answered %d", stale.status)
			}
		})
	}
	// A settings write changes the ETag of /public.
	old := r.get(t.Context(), "/public").header.Get("ETag")
	r.mustSet("regional.name", "Camp Quill")
	after := r.get(t.Context(), "/public", "If-None-Match", old)
	if after.status != http.StatusOK || after.header.Get("ETag") == old {
		t.Errorf("after a write: status %d, ETag %q (old %q)", after.status, after.header.Get("ETag"), old)
	}
}

func TestGetThemeCSS_ContentTypeAndBody(t *testing.T) {
	r := newIRig(t)
	res := r.get(t.Context(), "/theme.css")
	if res.status != http.StatusOK || res.body != "" {
		t.Errorf("default: status %d body %q", res.status, res.body)
	}
	if ct := res.header.Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	r.mustSet("regional.color_primary", "#9a3412")
	res = r.get(t.Context(), "/theme.css")
	if !strings.Contains(res.body, "--primary: #9a3412") || res.header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("themed: %q (%q)", res.body, res.header.Get("Content-Type"))
	}
	pub, err := r.svc.Public(t.Context())
	wantNoErr(t, err)
	if !pub.Theme {
		t.Error("Public.Theme false with a colour set")
	}
}

// Profile over the real settings service: the declared defaults, then the saved values, and
// the public URL saved in its canonical form.
func TestRegional_DefaultsAndOverrides(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	got, err := r.svc.Profile(ctx)
	wantNoErr(t, err)
	if want := (regional.Profile{Name: "BMS", Language: "en"}); got != want {
		t.Errorf("defaults = %+v, want %+v", got, want)
	}
	pub, err := r.svc.Public(ctx)
	wantNoErr(t, err)
	if !slices.Equal(pub.Languages, settings.Languages) {
		t.Errorf("Languages = %v", pub.Languages)
	}
	pub.Languages[0] = "zz" // a copy: the next call is unaffected
	if again := first(r.svc.Public(ctx)); again.Languages[0] != "en" {
		t.Error("Public shares its Languages slice with the caller")
	}

	r.mustSet("regional.name", "Camp Quill", "regional.contact_email", "hello@example.org",
		"regional.language", "es", "regional.timezone", "America/Denver",
		"regional.public_url", "HTTPS://Burn.Example.test/", "regional.color_primary", "#9a3412")
	got, err = r.svc.Profile(ctx)
	wantNoErr(t, err)
	want := regional.Profile{
		Name: "Camp Quill", ContactEmail: "hello@example.org", Language: "es", Timezone: "America/Denver",
		PublicURL: "https://burn.example.test", ColorPrimary: "#9a3412",
	}
	if got != want {
		t.Errorf("Profile = %+v, want %+v", got, want)
	}
}

// A settings write moves the document and a logo change moves it too, but a logo change is
// not a settings write: the stored revision stays where the settings service left it.
func TestSetLogo_DoesNotWriteSettingRows(t *testing.T) {
	r := newRealLogoI(t, filesDefault())
	r.mustSet("regional.name", "Camp Quill")
	rows := r.count(`SELECT count(*) FROM core.setting`)
	r.mustSetLogo("logo.png", pngOf(t, 1))
	if got := r.count(`SELECT count(*) FROM core.setting`); got != rows {
		t.Errorf("core.setting rows %d -> %d by a logo upload", rows, got)
	}
	if p := first(r.svc.Profile(ctxT(t))); p.Name != "Camp Quill" {
		t.Errorf("Profile.Name = %q after a logo upload", p.Name)
	}
}

// The three public GETs and the two writes, through the real mux with real sessions
// (TestRoutes_ArePublicWhereSettingsWere's behaviour on a real settings service).
func TestRoutes_PublicGETsNeedNoSession_OnARealDatabase(t *testing.T) {
	r := newIRig(t)
	for _, p := range []string{"/public", "/theme.css"} {
		if res := r.get(t.Context(), p); res.status != http.StatusOK {
			t.Errorf("GET %s with no session = %d (%v)", p, res.status, res.fail.last())
		}
	}
	if res := r.get(t.Context(), "/logo"); res.status != http.StatusNotFound {
		t.Errorf("GET /logo with no logo = %d, want 404", res.status)
	}
}

// The document carries the terms in every language, four forms each: defaults with the
// regional's overrides (moved from settings' Public.Terms assertions, now that regional
// builds the document from settings.TermsFor).
func TestGetPublic_TermsFollowTheSettingsTerms(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	str := func(s string) *string { return &s }
	_, err := r.stg.SetTerms(r.actx(), "en", settings.TermChanges{Terms: map[string]settings.FormChanges{
		"department": {Singular: str("team"), Plural: str("teams"), SingularTitle: str("Team"), PluralTitle: str("Teams")},
	}})
	wantNoErr(t, err)
	pub, err := r.svc.Public(ctx)
	wantNoErr(t, err)
	for k, want := range map[string]string{
		"term_department": "team", "term_department_plural": "teams",
		"term_department_title": "Team", "term_department_plural_title": "Teams",
		"term_event": "event", "term_event_plural": "events", "term_event_title": "Event", "term_event_plural_title": "Events",
	} {
		if pub.Terms["en"][k] != want {
			t.Errorf("Public.Terms[en][%s] = %q, want %q", k, pub.Terms["en"][k], want)
		}
	}
	for _, lang := range settings.Languages {
		for _, term := range settings.CoreTerms {
			for _, suffix := range []string{"", "_plural", "_title", "_plural_title"} {
				if pub.Terms[lang]["term_"+term+suffix] == "" {
					t.Errorf("Public.Terms[%s][term_%s%s] is empty", lang, term, suffix)
				}
			}
		}
	}
	if pub.Terms["es"]["term_department"] == "team" {
		t.Error("an en override leaked into es")
	}
}

// Reads keep working on a read-only copy: the public document, the stylesheet and the logo's
// bytes; the writes are refused (the logo's, before the body is read).
func TestPublic_ReadOnlyCopy_ReadsWork(t *testing.T) {
	base := newLogoI(t)
	base.mustSetLogo("logo.png", pngOf(t, 1))
	// A second Service over a database that reports read-only.
	f := &readOnlyDB{DB: base.pool}
	rr := newIRigOn(t, f, base.pool, 0, func(o *regional.Options) { o.Files = base.spy })
	f.ro.Store(true)
	for _, p := range []string{"/public", "/theme.css", "/logo"} {
		if res := rr.get(t.Context(), p); res.status != http.StatusOK {
			t.Errorf("GET %s on a read-only copy = %d (%v)", p, res.status, res.fail.last())
		}
	}
	_, err := rr.svc.RemoveLogo(rr.actx())
	wantReadOnly(t, err)
}
