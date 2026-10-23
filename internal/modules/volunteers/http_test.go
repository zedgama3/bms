package volunteers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// The route tests drive the module's real handlers through a ServeMux mounted at
// /api/volunteers, with no Failer installed, so httpx.Fail writes its fallback problem body.

const apiBase = "/api/volunteers"

// routeMux mounts every route of the rig's module, validating each.
func (r *rig) routeMux() *http.ServeMux {
	r.t.Helper()
	mux := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		if err := rt.Validate(); err != nil {
			r.t.Fatalf("route %s %s: %v", rt.Method, rt.Pattern, err)
		}
		mux.Handle(rt.Method+" "+apiBase+rt.Pattern, rt.Handler)
	}
	return mux
}

// call serves one request as person p (zero = no session). A non-nil body is sent as JSON.
func (r *rig) call(p model.PersonID, method, path string, body any) *httptest.ResponseRecorder {
	r.t.Helper()
	var rd *bytes.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd, ct = bytes.NewReader([]byte(b)), "application/json"
	default:
		rd, ct = bytes.NewReader(mustJSONT(r.t, b)), "application/json"
	}
	return r.callRaw(p, method, path, rd, ct)
}

func (r *rig) callRaw(p model.PersonID, method, path string, body *bytes.Reader, ct string) *httptest.ResponseRecorder {
	r.t.Helper()
	ctx := ctxT(r.t)
	if !p.IsZero() {
		ctx = asPerson(ctx, p)
	}
	req := httptest.NewRequestWithContext(ctx, method, apiBase+path, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	rec := httptest.NewRecorder()
	r.routeMux().ServeHTTP(rec, req)
	return rec
}

func mustJSONT(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// wantProblem decodes a problem body and checks status and code.
func wantProblem(t testing.TB, rec *httptest.ResponseRecorder, status int, code string) httpx.Problem {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, httpx.ContentTypeProblem) {
		t.Fatalf("Content-Type = %q, want %s", ct, httpx.ContentTypeProblem)
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("not a problem body: %v (%q)", err, rec.Body.String())
	}
	if p.Status != status || p.Code != code {
		t.Fatalf("problem = %+v, want status %d code %q", p, status, code)
	}
	return p
}

// wantField checks that p carries a field problem with the given field and code and returns it.
func wantField(t testing.TB, p httpx.Problem, field, code string) httpx.Field {
	t.Helper()
	for _, f := range p.Fields {
		if f.Field == field && f.Code == code {
			return f
		}
	}
	t.Fatalf("problem %+v has no field %q with code %q", p, field, code)
	return httpx.Field{}
}

// ---- the route table ----

func TestRoutes_TableMatchesTheSpec(t *testing.T) {
	r := newRig(t, opts{})
	type want struct {
		maxBody int64
		timeout bool
	}
	spec := map[string]want{
		"GET /qualifications":                               {},
		"POST /qualifications":                              {},
		"PUT /qualifications/{qualification}":               {},
		"POST /qualifications/{qualification}/archive":      {},
		"GET /people/{person}/qualifications":               {},
		"POST /people/{person}/qualifications":              {},
		"POST /me/qualifications":                           {maxBody: files.UploadCeiling, timeout: true},
		"GET /qualification-records":                        {},
		"POST /qualification-records/{record}/verify":       {},
		"POST /qualification-records/{record}/reject":       {},
		"DELETE /qualification-records/{record}":            {},
		"GET /qualification-records/{record}/certificate":   {timeout: true},
		"GET /departments/{department}/positions":           {},
		"POST /departments/{department}/positions":          {},
		"PUT /positions/{position}":                         {},
		"POST /positions/{position}/archive":                {},
		"PUT /positions/{position}/requirements":            {},
		"GET /departments/{department}/members":             {},
		"POST /departments/{department}/members":            {},
		"DELETE /departments/{department}/members/{person}": {},
		"GET /me/departments":                               {},
		"DELETE /me/departments/{department}":               {},
		"GET /shifts":                                       {},
		"GET /shifts/{shift}":                               {},
		"POST /departments/{department}/shifts":             {},
		"PUT /shifts/{shift}":                               {},
		"POST /shifts/{shift}/cancel":                       {},
		"POST /departments/{department}/shifts/copy":        {},
		"POST /shifts/{shift}/signups":                      {},
		"POST /shifts/{shift}/assign":                       {},
		"DELETE /signups/{signup}":                          {},
		"GET /shifts/{shift}/roster":                        {},
		"GET /me/schedule":                                  {},
		"GET /me/listing":                                   {},
		"PUT /me/listing":                                   {},
	}
	got := r.svc.Routes()
	if len(got) != len(spec) {
		t.Errorf("Routes = %d, spec lists %d", len(got), len(spec))
	}
	seen := map[string]bool{}
	for _, rt := range got {
		key := rt.Method + " " + rt.Pattern
		w, ok := spec[key]
		if !ok {
			t.Errorf("route %s is not in the spec", key)
			continue
		}
		if seen[key] {
			t.Errorf("route %s is registered twice", key)
		}
		seen[key] = true
		if rt.Access != module.AccessSignedIn {
			t.Errorf("%s: access = %v, want signed-in", key, rt.Access)
		}
		if rt.MaxBodyBytes != w.maxBody {
			t.Errorf("%s: MaxBodyBytes = %d, want %d", key, rt.MaxBodyBytes, w.maxBody)
		}
		if w.timeout && rt.Timeout <= 0 {
			t.Errorf("%s: Timeout unset, want the files timeout", key)
		}
		if !w.timeout && rt.Timeout != 0 {
			t.Errorf("%s: Timeout = %v, want the shell's default", key, rt.Timeout)
		}
		if rt.Handler == nil {
			t.Errorf("%s: nil handler", key)
		}
	}
	for key := range spec {
		if !seen[key] {
			t.Errorf("spec route %s is missing", key)
		}
	}
}

func TestRoutes_UploadAndServeUseTheFilesLimits(t *testing.T) {
	r := newRig(t, opts{})
	for _, rt := range r.svc.Routes() {
		switch rt.Method + " " + rt.Pattern {
		case "POST /me/qualifications":
			if rt.MaxBodyBytes != files.UploadCeiling || rt.Timeout != files.UploadTimeout {
				t.Errorf("upload route = %d / %v, want files.UploadCeiling / UploadTimeout", rt.MaxBodyBytes, rt.Timeout)
			}
		case "GET /qualification-records/{record}/certificate":
			if rt.Timeout != files.ServeTimeout {
				t.Errorf("certificate route Timeout = %v, want files.ServeTimeout", rt.Timeout)
			}
		}
	}
}

// ---- failures that need no database ----

func TestHandlers_NoSession_Return401OnEveryRoute(t *testing.T) {
	r := newRig(t, opts{})
	id := model.NewID[model.PersonID]().String()
	for _, rt := range r.svc.Routes() {
		path := rt.Pattern
		for _, name := range []string{"qualification", "person", "record", "department", "position", "shift", "signup"} {
			path = strings.ReplaceAll(path, "{"+name+"}", id)
		}
		t.Run(rt.Method+" "+rt.Pattern, func(t *testing.T) {
			var body any
			if rt.Method == "POST" || rt.Method == "PUT" {
				body = map[string]any{}
			}
			rec := r.call(model.PersonID{}, rt.Method, path, body)
			// A route may refuse a body it cannot parse before it reaches the service; none
			// may answer 2xx or a 403 to someone with no session.
			if rec.Code < 400 || rec.Code == http.StatusForbidden {
				t.Fatalf("status = %d for an anonymous caller (body %q)", rec.Code, rec.Body.String())
			}
			if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnsupportedMediaType {
				t.Errorf("status = %d, want 401 (or a body-parse refusal)", rec.Code)
			}
		})
	}
}

func TestHandlers_NoSession_GetRoutesAre401(t *testing.T) {
	r := newRig(t, opts{})
	id := model.NewID[vol.ShiftID]().String()
	for _, path := range []string{"/shifts", "/shifts/" + id, "/me/schedule", "/me/listing", "/me/departments", "/qualifications"} {
		wantProblem(t, r.call(model.PersonID{}, "GET", path, nil), http.StatusUnauthorized, "unauthenticated")
	}
}

func TestHandlers_MalformedPathID_Returns400Malformed(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	for _, tc := range []struct{ method, path, field string }{
		{"GET", "/shifts/not-a-uuid", "shift"},
		{"POST", "/shifts/not-a-uuid/signups", "shift"},
		{"POST", "/shifts/not-a-uuid/cancel", "shift"},
		{"GET", "/shifts/not-a-uuid/roster", "shift"},
		{"DELETE", "/signups/xyz", "signup"},
		{"GET", "/departments/12345/positions", "department"},
		{"DELETE", "/departments/" + model.NewID[model.DepartmentID]().String() + "/members/bad", "person"},
		{"GET", "/qualification-records/bad/certificate", "record"},
		{"POST", "/qualification-records/bad/verify", "record"},
		{"DELETE", "/me/departments/bad", "department"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			wantField(t, wantProblem(t, r.call(p, tc.method, tc.path, nil), http.StatusBadRequest, "invalid"), tc.field, "malformed")
		})
	}
}

func TestHandlers_MalformedQuery_Returns400Malformed(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	for _, tc := range []struct{ path, field string }{
		{"/shifts?department=zzz", "department"},
		{"/shifts?event=zzz", "event"},
		{"/shifts?position=zzz", "position"},
		{"/shifts?from=yesterday", "from"},
		{"/shifts?to=tomorrow", "to"},
		{"/shifts?canceled=maybe", "canceled"},
		{"/shifts?limit=lots", "limit"},
		{"/me/schedule?from=x", "from"},
		{"/me/schedule?past=perhaps", "past"},
		{"/qualifications?archived=sometimes", "archived"},
		{"/qualifications?department=nope", "department"},
		{"/qualification-records?department=nope", "department"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			wantField(t, wantProblem(t, r.call(p, "GET", tc.path, nil), http.StatusBadRequest, "invalid"), tc.field, "malformed")
		})
	}
}

func TestHandlers_BadBodies_Return400Or415(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	d := model.NewID[model.DepartmentID]().String()
	sh := model.NewID[vol.ShiftID]().String()
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"empty body", "POST", "/qualifications", "", http.StatusBadRequest, "invalid"},
		{"broken JSON", "POST", "/departments/" + d + "/shifts", "{not json", http.StatusBadRequest, "invalid"},
		{"empty assign", "POST", "/shifts/" + sh + "/assign", "", http.StatusBadRequest, "invalid"},
		{"empty listing", "PUT", "/me/listing", "", http.StatusBadRequest, "invalid"},
		{"empty copy", "POST", "/departments/" + d + "/shifts/copy", "", http.StatusBadRequest, "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantProblem(t, r.call(p, tc.method, tc.path, tc.body), tc.status, tc.code)
		})
	}
	t.Run("wrong content type", func(t *testing.T) {
		rec := r.callRaw(p, "PUT", "/me/listing", bytes.NewReader([]byte(`{"listed":true}`)), "text/plain")
		wantProblem(t, rec, http.StatusUnsupportedMediaType, "unsupported_media_type")
	})
}

func TestHandlers_UploadNotMultipart_Returns415(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	wantProblem(t, r.call(p, "POST", "/me/qualifications", `{"qualification":"x"}`), http.StatusUnsupportedMediaType, "unsupported_media_type")
}
