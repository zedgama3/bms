package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// Tests for POST /api/client-events, bms.api.errors and RouteTokens (Amendment T1, D54;
// api.md F65 to F72, telemetry.md F74 and F75).

// eventsRig is a rig whose Options.Usage is built over the rig's capture and clock.
func eventsRig(t *testing.T, enabled bool, mut ...func(*api.Options)) *rig {
	t.Helper()
	r := newRig(t, mut...)
	u, err := intake.NewUsage(intake.UsageOptions{
		Logger: r.cap.Logger(), Meter: r.cap.Meter("usage"), Clock: r.clk, Enabled: enabled,
		Screens: []string{"/", "/people/{id}"}, Actions: []string{"shift.filter"},
	})
	if err != nil {
		t.Fatalf("intake.NewUsage: %v", err)
	}
	r.opts.Usage = u
	return r
}

func eventsBody(n int) string {
	var sb strings.Builder
	sb.WriteString(`{"events":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"kind":"screen_view","screen":"/","from":"_entry","occurred_at":%q}`, epoch.Add(-time.Minute).Format(time.RFC3339))
	}
	sb.WriteString(`]}`)
	return sb.String()
}

const eventsPath = "/api/client-events"

func TestClientEvents_Accepted202NoBody(t *testing.T) {
	r := eventsRig(t, true)
	rec := r.do("POST", eventsPath, jsonBody(eventsBody(3)))
	if rec.Code != 202 || rec.Body.Len() != 0 {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	var views float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.client.screen_views" {
			views += m.Value
		}
	}
	if views != 3 {
		t.Errorf("screen_views counted = %v, want 3", views)
	}
}

func TestClientEvents_Invalid_Returns400(t *testing.T) {
	cases := map[string]string{
		"empty list":    `{"events":[]}`,
		"no events key": `{}`,
		"bad kind":      `{"events":[{"kind":"nope","screen":"/","occurred_at":"2026-10-17T11:59:00Z"}]}`,
		"malformed":     `{"events": [`,
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			r := eventsRig(t, true)
			p := wantProblem(t, r.do("POST", eventsPath, jsonBody(b)), 400, "invalid")
			if name == "bad kind" {
				if len(p.Fields) == 0 || !strings.HasPrefix(p.Fields[0].Field, "events[0].") {
					t.Errorf("fields = %+v", p.Fields)
				}
			}
			if name == "empty list" && (len(p.Fields) == 0 || p.Fields[0].Field != "events") {
				t.Errorf("fields = %+v", p.Fields)
			}
		})
	}
}

func TestClientEvents_TooLarge_413(t *testing.T) {
	r := eventsRig(t, true)
	big := `{"events":[{"kind":"screen_view","screen":"/` + strings.Repeat("a", 70_000) + `","occurred_at":"2026-10-17T11:59:00Z"}]}`
	wantProblem(t, r.do("POST", eventsPath, jsonBody(big)), 413, httpx.CodePayloadTooLarge)
}

func TestClientEvents_WrongContentType_415(t *testing.T) {
	r := eventsRig(t, true)
	wantProblem(t, r.do("POST", eventsPath, body("text/plain", eventsBody(1))), 415, httpx.CodeUnsupportedMediaType)
	r2 := eventsRig(t, true)
	wantProblem(t, r2.do("POST", eventsPath, body("", eventsBody(1))), 415, httpx.CodeUnsupportedMediaType)
}

func TestClientEvents_RateLimited_429(t *testing.T) {
	r := eventsRig(t, true)
	for i, n := range []int{50, 10} {
		if rec := r.do("POST", eventsPath, jsonBody(eventsBody(n))); rec.Code != 202 {
			t.Fatalf("batch %d = %d", i, rec.Code)
		}
	}
	rec := r.do("POST", eventsPath, jsonBody(eventsBody(1)))
	wantProblem(t, rec, 429, httpx.CodeRateLimited)
	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	// A different client address is not limited.
	if rec := r.do("POST", eventsPath, jsonBody(eventsBody(1)), remote("203.0.113.99:1234")); rec.Code != 202 {
		t.Errorf("other address = %d", rec.Code)
	}
}

func TestClientEvents_Off_501(t *testing.T) {
	r := eventsRig(t, false)
	wantProblem(t, r.do("POST", eventsPath, jsonBody(eventsBody(1))), 501, errs.Unsupported.String())
	// The route is still mounted and documented.
	rec := r.do("GET", "/api/openapi.json")
	if !strings.Contains(rec.Body.String(), eventsPath) {
		t.Error("the OpenAPI document must not depend on the usage switch")
	}
}

func TestClientEvents_NilUsage_501(t *testing.T) {
	r := newRig(t) // baseOptions leaves Usage nil
	wantProblem(t, r.do("POST", eventsPath, jsonBody(eventsBody(1))), 501, errs.Unsupported.String())
}

func TestClientEvents_Public_NoSessionOrStaleSession_202(t *testing.T) {
	r := eventsRig(t, true, withAuth(noSession()))
	if rec := r.do("POST", eventsPath, jsonBody(eventsBody(1))); rec.Code != 202 {
		t.Fatalf("no session = %d", rec.Code)
	}
	r2 := eventsRig(t, true, withAuth(authFails(errs.E("x", errs.Unauthenticated, errors.New("stale")))))
	if rec := r2.do("POST", eventsPath, jsonBody(eventsBody(1)), authCookie()); rec.Code != 202 {
		t.Fatalf("stale session = %d", rec.Code)
	}
}

// F75 / F69: the session's person is never read, put on a span, logged or measured.
func TestClientEvents_SignedIn_NoPersonAnywhere(t *testing.T) {
	person := newPerson()
	r := eventsRig(t, true, withAuth(signedIn(person)))
	rec := r.do("POST", eventsPath+"?q="+marker, jsonBody(eventsBody(2)),
		hdr("Cookie", "s="+marker), hdr("User-Agent", "Mozilla/5.0 "+marker+" Mobile"), remote("203.0.113.55:4000"))
	if rec.Code != 202 {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, needle := range []string{person.String(), marker, "203.0.113.55", "Mobile"} {
		if r.cap.Contains(needle) {
			t.Errorf("%q reached telemetry", needle)
		}
	}
	for _, s := range r.cap.Spans() {
		if _, ok := s.Attrs["person_id"]; ok {
			t.Errorf("span %q has person_id", s.Name)
		}
	}
	for _, rec := range r.cap.Records() {
		if _, ok := rec.Attrs["person_id"]; ok {
			t.Errorf("record %q has person_id", rec.Message)
		}
	}
	// The same person on the error route is still recorded as today: the rule is per route.
	r.do("POST", "/api/client-errors", jsonBody(validBatch(1)))
	if !r.cap.Contains(person.String()) {
		t.Error("the error route should keep its person_id (unchanged)")
	}
}

// Misbehaving request text never reaches telemetry on a refused batch either.
func TestClientEvents_Refused_NoLeak(t *testing.T) {
	r := eventsRig(t, true)
	bad := fmt.Sprintf(`{"events":[{"kind":"screen_view","screen":"/%s","occurred_at":"2026-10-17T11:59:00Z"},{"kind":"%s"}]}`, marker, emailMarker)
	r.do("POST", eventsPath+"?x="+marker, jsonBody(bad))
	if r.cap.Contains(marker) || r.cap.Contains(emailMarker) {
		t.Error("request text reached telemetry")
	}
}

func TestMapper_CountsErrorsByCode(t *testing.T) {
	set := setOf(t, "alpha",
		route("GET", "/boom", module.AccessPublic, failWith(errs.E("alpha.boom", errs.Internal, errors.New("x")))),
		route("GET", "/missing", module.AccessPublic, failWith(errs.E("alpha.missing", errs.NotFound, errors.New("x")))),
		route("GET", "/fine", module.AccessPublic, textHandler("ok")),
	)
	r := newRig(t, withRoutes(set))
	r.do("GET", "/api/alpha/boom")
	r.do("GET", "/api/alpha/boom")
	r.do("GET", "/api/alpha/missing?q="+marker)
	r.do("GET", "/api/alpha/fine")
	r.db.set(errors.New("down"), false)
	r.do("GET", "/readyz")
	r.do("GET", "/healthz")

	type key struct{ method, route, code string }
	got := map[key]float64{}
	for _, m := range r.cap.Measurements() {
		if m.Instrument != "bms.api.errors" {
			continue
		}
		if len(m.Attrs) != 3 {
			t.Errorf("attrs = %v, want exactly http.request.method, http.route, code", m.Attrs)
		}
		got[key{m.Attrs["http.request.method"], m.Attrs["http.route"], m.Attrs["code"]}] += m.Value
	}
	if got[key{"GET", "/api/alpha/boom", "internal"}] != 2 {
		t.Errorf("boom = %v (all: %v)", got[key{"GET", "/api/alpha/boom", "internal"}], got)
	}
	if got[key{"GET", "/api/alpha/missing", "not_found"}] != 1 {
		t.Errorf("missing = %v (all: %v)", got[key{"GET", "/api/alpha/missing", "not_found"}], got)
	}
	for k := range got {
		if k.route == "/readyz" || k.route == "/healthz" || k.route == "/api/alpha/fine" {
			t.Errorf("counted %v", k)
		}
	}
	if r.cap.Contains(marker) {
		t.Error("the query string reached a metric")
	}
}

func TestRouteTokens_MatchSpanNames(t *testing.T) {
	routes := []module.ModuleRoutes{alphaSet(t)}
	tokens := api.RouteTokens(routes)
	set := map[string]bool{}
	for _, tk := range tokens {
		if set[tk] {
			t.Errorf("duplicate token %q", tk)
		}
		set[tk] = true
		if strings.Contains(tk, "$") {
			t.Errorf("token carries {$}: %q", tk)
		}
	}
	for _, want := range []string{"/api/client-errors", "/api/client-events", "/api/alpha/", "/api/alpha/things/{id}", "/{unmatched}"} {
		if !set[want] {
			t.Errorf("RouteTokens lacks %q (have %v)", want, tokens)
		}
	}
	if again := api.RouteTokens(routes); strings.Join(again, "|") != strings.Join(tokens, "|") {
		t.Error("RouteTokens is not pure")
	}
	if len(api.RouteTokens(nil)) == 0 {
		t.Error("the shell's own routes are tokens even with no module routes")
	}
	// Every route a request span is named with is a token.
	r := eventsRig(t, true, withRoutes(alphaSet(t)))
	r.do("GET", "/api/alpha/")
	r.do("GET", "/api/alpha/things/"+uuidFor())
	r.do("GET", "/api/alpha/files/a/b")
	r.do("POST", eventsPath, jsonBody(eventsBody(1)))
	r.do("POST", "/api/client-errors", jsonBody(validBatch(1)))
	r.do("GET", "/api/nowhere/at/all")
	spans := requestSpans(r.cap)
	if len(spans) == 0 {
		t.Fatal("no request spans")
	}
	for _, s := range spans {
		if rt := s.Attrs["http.route"]; !set[rt] {
			t.Errorf("span %q route %q is not in RouteTokens", s.Name, rt)
		}
	}
}

func uuidFor() string { return "3f2b8c1e-5d4a-4b6f-9a7e-1c2d3e4f5a6b" }

func TestClientEvents_OpenAPIDocumentsRoute(t *testing.T) {
	r := eventsRig(t, true)
	rec := r.do("GET", "/api/openapi.json")
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi: %v", err)
	}
	op, ok := doc.Paths[eventsPath]["post"]
	if !ok {
		t.Fatalf("POST %s is not documented", eventsPath)
	}
	for _, code := range []string{`"202"`, `"400"`, `"413"`, `"415"`, `"429"`, `"501"`} {
		if !strings.Contains(string(op), code) {
			t.Errorf("response %s is not documented", code)
		}
	}
}
