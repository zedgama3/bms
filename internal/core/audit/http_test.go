package audit_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

const apiBase = "/api/audit"

// muxFor mounts the audit routes where app does, on a ServeMux with no Failer, so
// httpx.Fail writes its fallback status table. A request carries the session actor given.
func muxFor(t *testing.T, l *audit.Log) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	for _, r := range l.Routes() {
		if r.Handler == nil {
			t.Fatalf("route %s %s has no handler", r.Method, r.Pattern)
		}
		mux.Handle(r.Method+" "+apiBase+r.Pattern, r.Handler)
	}
	return mux
}

func get(t *testing.T, h http.Handler, target string, a *audit.Actor) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if a != nil {
		ctx = withSession(ctx, *a)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil))
	return rec
}

func problemOf(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, httpx.ContentTypeProblem) {
		t.Fatalf("Content-Type = %q, want a problem body (status %d)", ct, rec.Code)
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("not a problem body: %v", err)
	}
	return p
}

func TestRoutes_AreTheTwoSpecified(t *testing.T) {
	f := newFx(t)
	var got []string
	for _, r := range f.log.Routes() {
		got = append(got, r.Method+" "+r.Pattern)
	}
	sort.Strings(got)
	want := []string{"GET /entries", "GET /people/{person}/views"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("routes = %v, want %v", got, want)
	}
}

func TestOpenAPI_ParsesAndNamesExactlyTheRoutes(t *testing.T) {
	f := newFx(t)
	var frag struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(f.log.OpenAPI(), &frag); err != nil {
		t.Fatalf("fragment is not JSON: %v", err)
	}
	want := map[string]string{}
	for _, r := range f.log.Routes() {
		want[r.Pattern] = strings.ToLower(r.Method)
	}
	if len(frag.Paths) != len(want) {
		t.Errorf("fragment has %d paths, routes %d", len(frag.Paths), len(want))
	}
	for p, m := range want {
		ops, ok := frag.Paths[p]
		if !ok {
			t.Errorf("path %s missing from the fragment", p)
			continue
		}
		if _, ok := ops[m]; !ok || len(ops) != 1 {
			t.Errorf("path %s operations = %v, want only %s", p, keys(ops), m)
		}
	}
	a := f.log.OpenAPI()
	if len(a) == 0 {
		t.Fatal("empty fragment")
	}
	a[0] = 'X'
	if b := f.log.OpenAPI(); b[0] == 'X' {
		t.Error("OpenAPI shares its backing array with callers")
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestHandlers_NoSession_Return401_NoPermission_Return403(t *testing.T) {
	// M1: with a nil PermissionChecker the routes are mounted and answer 403.
	f := newFx(t, func(o *audit.Options) { o.Permissions = nil })
	mux := muxFor(t, f.log)
	targets := []string{apiBase + "/entries", apiBase + "/people/" + bob.String() + "/views"}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			rec := get(t, mux, target, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("no session: status %d, want 401", rec.Code)
			}
			if p := problemOf(t, rec); p.Code != errs.Unauthenticated.String() {
				t.Errorf("code = %q", p.Code)
			}
			a := person(alice)
			rec = get(t, mux, target, &a)
			if rec.Code != http.StatusForbidden {
				t.Errorf("signed in, nil checker: status %d, want 403", rec.Code)
			}
			su := superuser(alice)
			rec = get(t, mux, target, &su)
			if rec.Code != http.StatusForbidden {
				t.Errorf("superuser, nil checker: status %d, want 403 (fail closed)", rec.Code)
			}
			if p := problemOf(t, rec); p.Code != errs.Permission.String() {
				t.Errorf("code = %q", p.Code)
			}
		})
	}
}

func TestEntriesHandler_Allowed_Returns200Json(t *testing.T) {
	f := readerFx(t)
	mux := muxFor(t, f.log)
	a := person(carol)
	q := url.Values{"subject": {bob.String()}, "limit": {"10"}}
	rec := get(t, mux, apiBase+"/entries?"+q.Encode(), &a)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a JSON object: %v", err)
	}
	if n := len(entriesNamed(t, f, "audit.searched")); n != 1 {
		t.Errorf("audit.searched entries = %d, want 1", n)
	}
}

func TestEntriesHandler_BadQuery_Returns400(t *testing.T) {
	f := readerFx(t)
	mux := muxFor(t, f.log)
	a := person(carol)
	for _, tc := range []struct{ name, query, field string }{
		{"limit not a number", "limit=abc", "query.limit"},
		{"limit negative", "limit=-1", "query.limit"},
		{"limit over max", "limit=201", "query.limit"},
		{"limit twice", "limit=1&limit=2", "query.limit"},
		{"after garbage", "after=%25%25%25", "query.after"},
		{"actor not a uuid", "actor=not-a-uuid", "query.actor"},
		{"subject not a uuid", "subject=1234", "query.subject"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, mux, apiBase+"/entries?"+tc.query, &a)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			p := problemOf(t, rec)
			if p.Code != errs.Invalid.String() {
				t.Errorf("code = %q", p.Code)
			}
			found := false
			for _, fe := range p.Fields {
				found = found || fe.Field == tc.field
			}
			if !found {
				t.Errorf("fields = %+v, want %s", p.Fields, tc.field)
			}
			if strings.Contains(rec.Body.String(), "not-a-uuid") {
				t.Error("the problem body echoes the input")
			}
		})
	}
	if n := len(f.spoolLines(t)); n != 0 {
		t.Errorf("bad queries left %d audit entries", n)
	}
}

func TestViewsHandler_Allowed_ReturnsJSONLinesAttachment(t *testing.T) {
	f := readerFx(t)
	mux := muxFor(t, f.log)
	a := person(carol)
	rec := get(t, mux, apiBase+"/people/"+bob.String()+"/views", &a)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/x-ndjson") {
		t.Errorf("Content-Type = %q, want application/x-ndjson", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
	if got := entriesNamed(t, f, "audit.exported"); len(got) != 1 || str(got[0]["subject"]) != bob.String() {
		t.Errorf("audit.exported entries = %v", got)
	}
}

func TestViewsHandler_BadPerson_Returns400(t *testing.T) {
	f := readerFx(t)
	mux := muxFor(t, f.log)
	a := person(carol)
	for _, p := range []string{"not-a-uuid", (model.PersonID{}).String()} {
		rec := get(t, mux, apiBase+"/people/"+p+"/views", &a)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("person %q: status %d, want 400", p, rec.Code)
		}
	}
	if n := len(entriesNamed(t, f, "audit.exported")); n != 0 {
		t.Errorf("a rejected export was recorded %d times", n)
	}
}
