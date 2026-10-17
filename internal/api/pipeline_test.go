package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

// facts is what the recording route sees in its request.
type facts struct {
	Addr     string `json:"addr"`
	HasAddr  bool   `json:"has_addr"`
	Scheme   string `json:"scheme"`
	Deadline string `json:"deadline"`
	HasDL    bool   `json:"has_deadline"`
	Actor    bool   `json:"actor"`
	Ref      string `json:"ref"`
	TraceID  string `json:"trace_id"`
}

func whoamiSet(t *testing.T) module.ModuleRoutes {
	t.Helper()
	return setOf(t, "delta", route("GET", "/whoami", module.AccessPublic, func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var f facts
		if a, ok := httpx.ClientAddrFrom(ctx); ok {
			f.Addr, f.HasAddr = a.String(), true
		}
		f.Scheme, _ = httpx.SchemeFrom(ctx)
		if d, ok := ctx.Deadline(); ok {
			f.Deadline, f.HasDL = d.Format(time.RFC3339Nano), true
		}
		_, f.Actor = ctx.Value(actorKey{}).(model.PersonID)
		if ref, ok := telemetry.RefFrom(ctx); ok {
			f.Ref = ref.String()
		}
		f.TraceID, _ = telemetry.TraceIDFrom(ctx)
		if err := httpx.WriteJSON(w, 200, f); err != nil {
			t.Errorf("WriteJSON: %v", err)
		}
	}))
}

// deadlineWriter is a recorder that, unlike httptest's, supports SetReadDeadline.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	err       error
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error {
	w.deadlines = append(w.deadlines, t)
	return w.err
}

func TestPipeline_Order(t *testing.T) {
	person := newPerson()
	r := newRig(t, withRoutes(whoamiSet(t)), withAuth(signedIn(person)), func(o *api.Options) {
		o.RequestTimeout = 45 * time.Second
	})
	h := r.server().Handler()
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	h.ServeHTTP(w, newRequest(t, "GET", "/api/delta/whoami", remote("198.51.100.20:5000")))
	after := time.Now()
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var f facts
	if err := jsonUnmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if !f.HasAddr || f.Addr != "198.51.100.20" || f.Scheme != "http" {
		t.Errorf("address/scheme = %q %q", f.Addr, f.Scheme)
	}
	if !f.Actor {
		t.Error("the authenticator's ctx did not reach the handler")
	}
	if f.Ref == "" || f.TraceID == "" {
		t.Errorf("no span in ctx: ref %q trace %q", f.Ref, f.TraceID)
	}
	if !f.HasDL {
		t.Fatal("no ctx deadline")
	}
	dl, err := time.Parse(time.RFC3339Nano, f.Deadline)
	if err != nil {
		t.Fatal(err)
	}
	if dl.Before(before.Add(45*time.Second)) || dl.After(after.Add(45*time.Second)) {
		t.Errorf("deadline %v outside the wall-clock window [%v, %v]", dl, before.Add(45*time.Second), after.Add(45*time.Second))
	}
	if len(w.deadlines) != 1 || !w.deadlines[0].Equal(dl) {
		t.Errorf("SetReadDeadline calls = %v, want one equal to the ctx deadline %v", w.deadlines, dl)
	}
	if w.Header().Get("X-Trace-Id") != f.TraceID {
		t.Errorf("X-Trace-Id %q != span trace %q", w.Header().Get("X-Trace-Id"), f.TraceID)
	}
}

func TestPipeline_SetReadDeadlineFails_500(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder(), err: errors.New("boom")}
	r.server().Handler().ServeHTTP(w, newRequest(t, "GET", "/api/alpha/"))
	wantProblem(t, w.ResponseRecorder, 500, "internal")
	if len(recordsWith(r.cap, slogError)) == 0 {
		t.Error("no error log")
	}
}

func TestPipeline_ReadDeadlineNotSupported_DebugOnce(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	h := r.server().Handler()
	base := len(recordsWith(r.cap, slogDebug))
	if rec := serve(t, h, "GET", "/api/alpha/"); rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	first := len(recordsWith(r.cap, slogDebug)) - base
	if first != 1 {
		t.Fatalf("debug records after first request = %d, want 1", first)
	}
	if rec := serve(t, h, "GET", "/api/alpha/"); rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := len(recordsWith(r.cap, slogDebug)) - base; got != 1 {
		t.Fatalf("debug records after second request = %d, want still 1", got)
	}
	if n := len(recordsWith(r.cap, slogWarn)) + len(recordsWith(r.cap, slogError)); n != 0 {
		t.Fatalf("warn/error records: %d", n)
	}
}

func TestPipeline_SecurityHeaders_Table(t *testing.T) {
	ui := uiFS()
	targets := []struct{ name, method, path string }{
		{"api route", "GET", "/api/alpha/"},
		{"api 404", "GET", "/api/alpha/none"},
		{"api 405", "PUT", "/api/alpha/"},
		{"healthz", "GET", "/healthz"},
		{"readyz", "GET", "/readyz"},
		{"modules", "GET", "/api/modules"},
		{"openapi", "GET", "/api/openapi.json"},
		{"ui index", "GET", "/"},
		{"ui app path", "GET", "/people/1"},
		{"ui asset", "GET", "/assets/app-abc123.js"},
		{"ui 404", "GET", "/missing.js"},
	}
	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
		"X-Frame-Options":              "DENY",
		"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	schemes := []struct {
		name  string
		opts  []reqOpt
		hsts  bool
		trust bool
	}{
		{"http direct", nil, false, false},
		{"https via trusted proxy", []reqOpt{hdr("X-Forwarded-Proto", "https")}, true, true},
		{"http via trusted proxy", []reqOpt{hdr("X-Forwarded-Proto", "http")}, false, true},
		{"https header from untrusted peer", []reqOpt{hdr("X-Forwarded-Proto", "https")}, false, false},
	}
	for _, sc := range schemes {
		for _, tg := range targets {
			t.Run(sc.name+"/"+tg.name, func(t *testing.T) {
				r := newRig(t, withRoutes(alphaSet(t)), func(o *api.Options) {
					o.UI = ui
					if sc.trust {
						o.TrustedProxies = prefixes("127.0.0.1/32")
					} else {
						o.TrustedProxies = prefixes("10.0.0.0/8")
					}
				})
				rec := r.do(tg.method, tg.path, sc.opts...)
				for k, v := range want {
					if got := rec.Header().Get(k); got != v {
						t.Errorf("%s = %q, want %q", k, got, v)
					}
				}
				hsts := rec.Header().Get("Strict-Transport-Security")
				switch {
				case sc.hsts && hsts != "max-age=31536000":
					t.Errorf("HSTS = %q, want exactly max-age=31536000", hsts)
				case !sc.hsts && hsts != "":
					t.Errorf("HSTS = %q on a non-https request", hsts)
				}
				if strings.HasPrefix(tg.path, "/api/") && tg.path != "/api/openapi.json" && tg.path != "/api/modules" {
					if got := rec.Header().Get("Cache-Control"); got != "no-store" {
						t.Errorf("Cache-Control = %q, want no-store", got)
					}
				}
				csp := rec.Header().Get("Content-Security-Policy")
				isHTML := strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html")
				if isHTML && csp == "" {
					t.Error("text/html response without a CSP")
				}
				isUI := !strings.HasPrefix(tg.path, "/api/") && tg.path != "/healthz" && tg.path != "/readyz"
				if isUI && csp == "" {
					t.Error("a UI response (any type, including 404 problems) lacks the CSP")
				}
				if !isUI && !isHTML && csp != "" {
					t.Errorf("CSP on a %q response", rec.Header().Get("Content-Type"))
				}
				// Health routes and UI files open no span, so they have no trace to name; the
				// spec's decision 19 ("every response") is ambiguous there (reported).
				spanless := tg.path == "/healthz" || tg.path == "/readyz" || !strings.HasPrefix(tg.path, "/api/")
				if rec.Header().Get("X-Trace-Id") == "" && !spanless {
					t.Error("no X-Trace-Id")
				}
				for k := range rec.Header() {
					if strings.HasPrefix(strings.ToLower(k), "access-control-") {
						t.Errorf("response carries %s", k)
					}
				}
			})
		}
	}
}

func TestPipeline_NoCORSHeaders_EvenForPreflight(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	rec := r.do("OPTIONS", "/api/alpha/", hdr("Origin", "https://other.example"),
		hdr("Access-Control-Request-Method", "POST"))
	for k := range rec.Header() {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("response carries %s", k)
		}
	}
}

func TestPipeline_XTraceIDMatchesSpan(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	rec := r.do("GET", "/api/alpha/")
	id := rec.Header().Get("X-Trace-Id")
	if len(id) != 32 {
		t.Fatalf("X-Trace-Id = %q", id)
	}
	s, ok := spanNamed(r.cap, "GET /api/alpha/")
	if !ok || s.TraceID != id {
		t.Fatalf("span %+v does not match trace id %s", s, id)
	}
	// An error body's ref is derived from the same trace.
	rec = r.do("GET", "/api/alpha/nope")
	p := problemOf(t, rec)
	tid := rec.Header().Get("X-Trace-Id")
	if p.Ref == "" || !strings.EqualFold(strings.ReplaceAll(p.Ref, "-", ""), tid[:8]) {
		t.Fatalf("ref %q does not come from trace %q", p.Ref, tid)
	}
}

func TestPipeline_XReadOnlyHeader(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	for _, p := range []string{"/api/alpha/", "/api/alpha/nope", "/healthz", "/readyz", "/api/modules"} {
		if got := r.do("GET", p).Header().Get("X-Read-Only"); got != "" {
			t.Errorf("%s: X-Read-Only = %q on a read-write copy", p, got)
		}
	}
	r.db.set(nil, true)
	for _, p := range []string{"/api/alpha/", "/api/alpha/nope", "/healthz", "/readyz", "/api/modules"} {
		if got := r.do("GET", p).Header().Get("X-Read-Only"); got != "true" {
			t.Errorf("%s: X-Read-Only = %q, want true", p, got)
		}
	}
	r.db.set(nil, false)
	if got := r.do("GET", "/api/alpha/").Header().Get("X-Read-Only"); got != "" {
		t.Errorf("header stuck after the copy became writable: %q", got)
	}
}

func TestPipeline_ClientAddr_Table(t *testing.T) {
	tests := []struct {
		name       string
		proxies    []string
		remote     string
		xff        []string
		xfp        []string
		wantAddr   string
		wantScheme string
	}{
		{"no proxies: peer", nil, "203.0.113.5:1000", []string{"1.2.3.4"}, []string{"https"}, "203.0.113.5", "http"},
		{"untrusted peer with forged headers", []string{"10.0.0.0/8"}, "127.0.0.1:1000", []string{"6.6.6.6"}, []string{"https"}, "127.0.0.1", "http"},
		{"trusted peer, no header", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, nil, "10.0.0.1", "http"},
		{"trusted peer, one untrusted entry", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7"}, nil, "198.51.100.7", "http"},
		{"chain client, proxyA, proxyB", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7, 10.0.0.2, 10.0.0.3"}, nil, "198.51.100.7", "http"},
		{"all entries trusted: peer", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"10.0.0.2, 10.0.0.3"}, nil, "10.0.0.1", "http"},
		{"two lines joined in order", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7", "10.0.0.2"}, nil, "198.51.100.7", "http"},
		{"forged prefix not believed", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"6.6.6.6", "198.51.100.7"}, nil, "198.51.100.7", "http"},
		{"forged prefix before proxy", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"6.6.6.6, 198.51.100.7, 10.0.0.2"}, nil, "198.51.100.7", "http"},
		{"spaces trimmed", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"  198.51.100.7  ,   10.0.0.2 "}, nil, "198.51.100.7", "http"},
		{"IPv4 entry with port", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7:4321"}, nil, "198.51.100.7", "http"},
		{"malformed rightmost: peer", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7, garbage"}, nil, "10.0.0.1", "http"},
		{"malformed in middle: hop to its right", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7, garbage, 10.0.0.2"}, nil, "10.0.0.2", "http"},
		{"empty entry is malformed", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7,,10.0.0.2"}, nil, "10.0.0.2", "http"},
		{"IPv4-mapped peer trusted by v4 prefix", []string{"10.0.0.0/8"}, "[::ffff:10.0.0.1]:1000", nil, nil, "10.0.0.1", "http"},
		{"IPv4-mapped entry unmapped before test", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"198.51.100.7, ::ffff:10.0.0.2"}, nil, "198.51.100.7", "http"},
		{"IPv4-mapped client entry is unmapped", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"::ffff:198.51.100.7"}, nil, "198.51.100.7", "http"},
		{"IPv6 client", []string{"10.0.0.0/8"}, "10.0.0.1:1000", []string{"2001:db8::7"}, nil, "2001:db8::7", "http"},
		{"proto https", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"https"}, "10.0.0.1", "https"},
		{"proto HTTPS", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"HTTPS"}, "10.0.0.1", "https"},
		{"proto last entry wins: http", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"https, http"}, "10.0.0.1", "http"},
		{"proto last entry wins: https", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"http, https"}, "10.0.0.1", "https"},
		{"proto last line wins", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"http", "https"}, "10.0.0.1", "https"},
		{"proto wss", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"wss"}, "10.0.0.1", "http"},
		{"proto on", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{"on"}, "10.0.0.1", "http"},
		{"proto empty", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, []string{""}, "10.0.0.1", "http"},
		{"proto absent", []string{"10.0.0.0/8"}, "10.0.0.1:1000", nil, nil, "10.0.0.1", "http"},
		{"proto from untrusted peer", []string{"10.0.0.0/8"}, "127.0.0.1:1000", nil, []string{"https"}, "127.0.0.1", "http"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, withRoutes(whoamiSet(t)), func(o *api.Options) {
				o.TrustedProxies = prefixes(tc.proxies...)
				if len(tc.proxies) == 0 {
					o.TrustedProxies = nil
				}
			})
			opts := []reqOpt{remote(tc.remote)}
			for _, v := range tc.xff {
				opts = append(opts, hdr("X-Forwarded-For", v))
			}
			for _, v := range tc.xfp {
				opts = append(opts, hdr("X-Forwarded-Proto", v))
			}
			rec := r.do("GET", "/api/delta/whoami", opts...)
			if rec.Code != 200 {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			var f facts
			if err := jsonUnmarshal(rec.Body.Bytes(), &f); err != nil {
				t.Fatal(err)
			}
			if f.Addr != tc.wantAddr || f.Scheme != tc.wantScheme {
				t.Fatalf("client = %q scheme = %q, want %q %q", f.Addr, f.Scheme, tc.wantAddr, tc.wantScheme)
			}
			if r.cap.Contains(tc.wantAddr) && tc.wantAddr != "127.0.0.1" {
				t.Errorf("client address %s reached telemetry", tc.wantAddr)
			}
		})
	}
}

func TestPipeline_SpanNamedByRoute_WithParentFromTraceparent(t *testing.T) {
	person := newPerson()
	r := newRig(t, withRoutes(alphaSet(t)), withAuth(signedIn(person)))
	const tid = "0123456789abcdef0123456789abcdef"
	rec := r.do("GET", "/api/alpha/things/7?q="+marker,
		hdr("Traceparent", "00-"+tid+"-0123456789abcdef-01"))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	s, ok := spanNamed(r.cap, "GET /api/alpha/things/{id}")
	if !ok {
		t.Fatalf("spans: %+v", r.cap.Spans())
	}
	if s.TraceID != tid || s.ParentID != "0123456789abcdef" {
		t.Errorf("trace/parent = %s/%s", s.TraceID, s.ParentID)
	}
	if s.Kind != telemetry.SpanServer {
		t.Errorf("kind = %v", s.Kind)
	}
	for k, want := range map[string]string{
		"http.request.method":       "GET",
		"http.route":                "/api/alpha/things/{id}",
		"http.response.status_code": "200",
		"person_id":                 person.String(),
	} {
		if got := s.Attrs[k]; got != want {
			t.Errorf("attr %s = %q, want %q", k, got, want)
		}
	}
	if s.Failed {
		t.Error("a 200 span is marked failed")
	}
	if _, ok := s.Attrs["duration_ms"]; !ok {
		t.Error("no duration_ms attribute")
	}
	if rec.Header().Get("X-Trace-Id") != tid {
		t.Errorf("X-Trace-Id = %q, want the traceparent's trace", rec.Header().Get("X-Trace-Id"))
	}
	if r.cap.Contains(marker) {
		t.Error("the query string reached telemetry")
	}

	r.do("GET", "/api/alpha/")
	if _, ok := spanNamed(r.cap, "GET /api/alpha/"); !ok {
		t.Error("root route span must be named \"GET /api/alpha/\" (no {$})")
	}
	for _, sp := range r.cap.Spans() {
		if strings.Contains(sp.Name, "$") || strings.Contains(sp.Attrs["http.route"], "$") {
			t.Errorf("span carries {$}: %+v", sp)
		}
	}
}

func TestPipeline_SpanFailedFor5xx(t *testing.T) {
	set := setOf(t, "alpha", route("GET", "/boom", module.AccessPublic, failWith(errs.E("alpha.boom", errs.Internal, errors.New("x")))))
	r := newRig(t, withRoutes(set))
	r.do("GET", "/api/alpha/boom")
	s, ok := spanNamed(r.cap, "GET /api/alpha/boom")
	if !ok || !s.Failed || s.ErrorKind != "internal" {
		t.Fatalf("span = %+v", s)
	}
	if s.Attrs["http.response.status_code"] != "500" {
		t.Errorf("status attr = %q", s.Attrs["http.response.status_code"])
	}
}

func TestPipeline_HistogramSample_TokensOnly(t *testing.T) {
	person := newPerson()
	r := newRig(t, withRoutes(alphaSet(t)), withAuth(signedIn(person)))
	r.do("GET", "/api/alpha/things/7?q="+marker, hdr("Cookie", "s="+marker))
	var found bool
	for _, m := range r.cap.Measurements() {
		if m.Instrument != "http.server.request.duration" {
			continue
		}
		found = true
		vals := map[string]bool{}
		for _, v := range m.Attrs {
			vals[v] = true
		}
		for _, want := range []string{"GET", "/api/alpha/things/{id}", "200"} {
			if !vals[want] {
				t.Errorf("histogram attrs %v lack %q", m.Attrs, want)
			}
		}
		if len(m.Attrs) != 3 {
			t.Errorf("histogram attrs = %v, want exactly method, route, status", m.Attrs)
		}
		if m.Scope != "api" || m.Value < 0 {
			t.Errorf("measurement = %+v", m)
		}
	}
	if !found {
		t.Fatal("no http.server.request.duration sample")
	}
	for _, m := range r.cap.Measurements() {
		for k, v := range m.Attrs {
			if strings.Contains(v, person.String()) || strings.Contains(v, marker) {
				t.Errorf("an ID or request text reached measurement attr %s=%q", k, v)
			}
		}
	}
}

func TestPipeline_HealthRoutes_NoSpanNoSample(t *testing.T) {
	r := newRig(t)
	r.do("GET", "/healthz")
	r.do("GET", "/readyz")
	if n := len(requestSpans(r.cap)); n != 0 {
		t.Errorf("health routes opened %d spans", n)
	}
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "http.server.request.duration" {
			t.Errorf("health routes recorded a sample: %+v", m)
		}
	}
}

func TestPipeline_SuccessWritesNoLogLine(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	base := len(r.cap.Records())
	r.do("GET", "/api/alpha/")
	r.do("GET", "/api/alpha/things/1")
	r.do("GET", "/healthz")
	r.do("GET", "/api/modules")
	for _, rec := range r.cap.Records()[base:] {
		if rec.Level >= slogInfo {
			t.Errorf("successful request logged: %+v", rec)
		}
	}
}

func TestPipeline_UnknownMethod_NormalisedToOther(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	for _, m := range []string{marker, "TRACE", "PROPFIND", "get"} {
		rec := r.do(m, "/api/alpha/")
		if rec.Code != 405 && rec.Code != 404 {
			t.Errorf("%s: status = %d, want 405 or 404", m, rec.Code)
		}
	}
	if r.cap.Contains(marker) {
		t.Error("the original method reached a span, log or metric")
	}
	s, ok := spanNamed(r.cap, "HTTP /{unmatched}")
	if !ok {
		t.Fatalf("no span HTTP /{unmatched}: %+v", r.cap.Spans())
	}
	if s.Attrs["http.request.method"] != "_OTHER" {
		t.Errorf("method attr = %q", s.Attrs["http.request.method"])
	}
	seen := false
	for _, m := range r.cap.Measurements() {
		for _, v := range m.Attrs {
			if v == "_OTHER" {
				seen = true
			}
		}
	}
	if !seen {
		t.Error("histogram did not carry _OTHER")
	}
	if v := r.cap.Violations(); len(v) != 0 {
		t.Errorf("violations: %+v", v)
	}
}

func TestPipeline_KnownMethodsKeepTheirName(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	r.do("OPTIONS", "/api/alpha/nothing")
	if _, ok := spanNamed(r.cap, "OPTIONS /{unmatched}"); !ok {
		t.Errorf("spans: %+v", r.cap.Spans())
	}
}

func TestPipeline_HandlerPanics_500WithRefAndStackLogged(t *testing.T) {
	set := setOf(t, "alpha",
		route("GET", "/panic", module.AccessPublic, func(http.ResponseWriter, *http.Request) { panic("kaboom") }),
		route("GET", "/ok", module.AccessPublic, textHandler("ok")),
	)
	r := newRig(t, withRoutes(set))
	rec := r.do("GET", "/api/alpha/panic")
	p := wantProblem(t, rec, 500, "internal")
	if p.Ref == "" {
		t.Error("no ref on a panic response")
	}
	var stack bool
	for _, l := range recordsWith(r.cap, slogError) {
		if l.Attrs["stack"] != "" {
			stack = true
		}
	}
	if !stack {
		t.Errorf("no error record with a stack: %+v", r.cap.Records())
	}
	if s, ok := spanNamed(r.cap, "GET /api/alpha/panic"); !ok || !s.Failed {
		t.Errorf("span = %+v", s)
	}
	if rec := r.do("GET", "/api/alpha/ok"); rec.Code != 200 {
		t.Errorf("server stopped serving after a panic: %d", rec.Code)
	}
}

func TestPipeline_PanicAfterHeader_DoesNotRewriteResponse(t *testing.T) {
	set := setOf(t, "alpha", route("GET", "/late", module.AccessPublic, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("late")
	}))
	r := newRig(t, withRoutes(set))
	rec := r.do("GET", "/api/alpha/late")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want the already-written 202", rec.Code)
	}
	if len(recordsWith(r.cap, slogError)) == 0 {
		t.Error("no error log")
	}
}

func TestPipeline_ErrAbortHandler_Repanics(t *testing.T) {
	set := setOf(t, "alpha", route("GET", "/abort", module.AccessPublic, func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	r := newRig(t, withRoutes(set))
	h := r.server().Handler()
	var got any
	func() {
		defer func() { got = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), newRequest(t, "GET", "/api/alpha/abort"))
	}()
	if !errors.Is(asError(got), http.ErrAbortHandler) {
		t.Fatalf("recovered %v, want http.ErrAbortHandler", got)
	}
	if n := len(recordsWith(r.cap, slogError)); n != 0 {
		t.Errorf("ErrAbortHandler logged %d error records", n)
	}
}

func TestPipeline_UntrustedPeer_403AndLogsPrefixOnce(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)), func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") })
	prefixLogs := func() []telemetry.LogRecord {
		var out []telemetry.LogRecord
		for _, l := range recordsWith(r.cap, slogWarn) {
			if l.Attrs["peer_prefix"] != "" {
				out = append(out, l)
			}
		}
		return out
	}
	rec := r.do("GET", "/api/alpha/", remote("203.0.113.5:1000"))
	wantProblem(t, rec, 403, httpx.CodeMisdirected)
	if logs := prefixLogs(); len(logs) != 1 || logs[0].Attrs["peer_prefix"] != "203.0.113.0/24" {
		t.Fatalf("logs = %+v", logs)
	}
	// Same prefix, another device, another path: no new line.
	wantProblem(t, r.do("GET", "/api/alpha/things/1", remote("203.0.113.99:1000")), 403, httpx.CodeMisdirected)
	if n := len(prefixLogs()); n != 1 {
		t.Fatalf("second request from the same /24 logged again: %d", n)
	}
	// Another prefix logs.
	r.do("GET", "/api/alpha/", remote("198.51.100.5:1000"))
	if n := len(prefixLogs()); n != 2 {
		t.Fatalf("new prefix: %d lines, want 2", n)
	}
	// IPv6 is cut at /64.
	r.do("GET", "/api/alpha/", remote("[2001:db8:1:2::5]:1000"))
	logs := prefixLogs()
	if len(logs) != 3 || logs[2].Attrs["peer_prefix"] != "2001:db8:1:2::/64" {
		t.Fatalf("v6 logs = %+v", logs)
	}
	// An hour later the same prefix is logged again.
	if err := r.clk.Advance(time.Hour + time.Second); err != nil {
		t.Fatal(err)
	}
	r.do("GET", "/api/alpha/", remote("203.0.113.6:1000"))
	if n := len(prefixLogs()); n != 4 {
		t.Fatalf("after an hour: %d lines, want 4", n)
	}
	for _, a := range []string{"203.0.113.5", "203.0.113.99", "198.51.100.5", "2001:db8:1:2::5"} {
		if r.cap.Contains(a) {
			t.Errorf("whole peer address %s was logged", a)
		}
	}
}

func TestPipeline_UntrustedPeer_HealthRoutesServed(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)), func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") })
	for _, p := range []string{"/healthz", "/readyz"} {
		if rec := r.do("GET", p, remote("203.0.113.5:1000")); rec.Code != 200 {
			t.Errorf("%s from an untrusted peer = %d", p, rec.Code)
		}
	}
}

func TestPipeline_Peers_ServedWhenAllowed(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*api.Options)
		remote  string
		want    int
		wantMis bool
	}{
		{"trusted peer", func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") }, "10.1.2.3:1", 200, false},
		{"loopback peer", func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") }, "127.0.0.1:1", 200, false},
		{"loopback v6", func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") }, "[::1]:1", 200, false},
		{"allow plaintext: anyone", func(o *api.Options) {
			o.TrustedProxies = prefixes("10.0.0.0/8")
			o.AllowPlaintext = true
		}, "203.0.113.5:1", 200, false},
		{"no trusted proxies: anyone", func(*api.Options) {}, "203.0.113.5:1", 200, false},
		{"untrusted", func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") }, "203.0.113.5:1", 403, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, withRoutes(alphaSet(t)), tc.mut)
			rec := r.do("GET", "/api/alpha/", remote(tc.remote))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestPipeline_UntrustedPeer_PrefixCapAggregates(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)), func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") })
	peer := func(i int) reqOpt { return remote(fmt.Sprintf("172.%d.%d.1:1000", 16+i/256, i%256)) }
	r.server()
	startup := len(recordsWith(r.cap, slogWarn)) // New may warn about the loopback bind
	warns := func() int { return len(recordsWith(r.cap, slogWarn)) - startup }
	for i := 0; i < 1001; i++ {
		wantProblem(t, r.do("GET", "/api/alpha/", peer(i)), 403, httpx.CodeMisdirected)
	}
	if got := warns(); got != 1001 {
		t.Fatalf("warn lines after 1,001 prefixes = %d, want 1,000 + 1 aggregate", got)
	}
	for i := 1001; i < 1011; i++ {
		r.do("GET", "/api/alpha/", peer(i))
	}
	if got := warns(); got != 1001 {
		t.Fatalf("more untracked prefixes within the minute logged: %d lines", got)
	}
	if err := r.clk.Advance(time.Minute + time.Second); err != nil {
		t.Fatal(err)
	}
	r.do("GET", "/api/alpha/", peer(1100))
	if got := warns(); got != 1002 {
		t.Fatalf("after a minute: %d lines, want 1,002", got)
	}
}

func TestPipeline_UntrustedLoopbackForwardedHeader_WarnsOnce(t *testing.T) {
	r := newRig(t, withRoutes(whoamiSet(t)), func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") })
	r.server()
	startup := len(recordsWith(r.cap, slogWarn)) // New may warn about the loopback bind
	for i := 0; i < 3; i++ {
		rec := r.do("GET", "/api/delta/whoami", remote("127.0.0.1:5000"),
			hdr("X-Forwarded-For", "198.51.100.77"), hdr("X-Forwarded-Proto", "https"))
		var f facts
		if err := jsonUnmarshal(rec.Body.Bytes(), &f); err != nil {
			t.Fatal(err)
		}
		if f.Addr != "127.0.0.1" || f.Scheme != "http" {
			t.Fatalf("headers from an untrusted loopback peer were believed: %+v", f)
		}
	}
	if n := len(recordsWith(r.cap, slogWarn)) - startup; n != 1 {
		t.Fatalf("warn records = %d, want exactly 1 per process", n)
	}
	if r.cap.Contains("198.51.100.77") {
		t.Error("the forwarded address was logged")
	}
}

func TestPipeline_BadRemoteAddr_500(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	for _, a := range []string{"garbage", "", "203.0.113.5", "[::1"} {
		t.Run(a, func(t *testing.T) {
			wantProblem(t, r.do("GET", "/api/alpha/", remote(a)), 500, "internal")
		})
	}
}

func TestPipeline_BodyOverCap_413(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)), func(o *api.Options) { o.MaxBodyBytes = 1024 })
	big := `{"name":"` + strings.Repeat("a", 2000) + `"}`
	wantProblem(t, r.do("POST", "/api/alpha/echo", jsonBody(big)), 413, httpx.CodePayloadTooLarge)
	if rec := r.do("POST", "/api/alpha/echo", jsonBody(`{"name":"ok"}`)); rec.Code != 200 {
		t.Fatalf("a small body = %d", rec.Code)
	}
}

func TestPipeline_ReadJSONErrorsThroughShell(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	wantProblem(t, r.do("POST", "/api/alpha/echo", body("text/plain", `{"name":"a"}`)), 415, httpx.CodeUnsupportedMediaType)
	p := wantProblem(t, r.do("POST", "/api/alpha/echo", jsonBody(`{"name": `+marker)), 400, "invalid")
	if len(p.Fields) == 0 || p.Fields[0].Code != "malformed" {
		t.Fatalf("fields = %+v", p.Fields)
	}
	p = wantProblem(t, r.do("POST", "/api/alpha/echo", jsonBody(``)), 400, "invalid")
	if len(p.Fields) != 1 || p.Fields[0].Field != "body" || p.Fields[0].Code != "required" {
		t.Fatalf("fields = %+v", p.Fields)
	}
}

func TestPipeline_NeverLogsQueryBodyCookieOrAgent(t *testing.T) {
	person := newPerson()
	r := newRig(t, withRoutes(alphaSet(t)), withAuth(signedIn(person)),
		func(o *api.Options) { o.TrustedProxies = prefixes("10.0.0.0/8") })
	common := []reqOpt{
		hdr("Cookie", "__Host-bms_session="+marker),
		hdr("User-Agent", "agent/"+marker),
		hdr("Authorization", "Bearer "+marker),
		hdr("Referer", "https://"+marker+".example/?e="+emailMarker),
		hdr("X-Forwarded-For", marker+", "+emailMarker),
		hdr("Origin", "https://"+marker+".example"),
		host(marker + ".example"),
	}
	q := "?q=" + marker + "&email=" + emailMarker
	r.do("POST", "/api/alpha/echo"+q, append([]reqOpt{jsonBody(`{"name": ` + marker + emailMarker)}, common...)...)
	r.do("POST", "/api/alpha/echo"+q, append([]reqOpt{body("text/plain", marker)}, common...)...)
	r.do("POST", "/api/alpha/echo"+q, append([]reqOpt{jsonBody(`{}`), hdr("Sec-Fetch-Site", "cross-site")}, common...)...)
	r.do("GET", "/api/alpha/"+marker+q, common...)
	r.do("GET", "/api/alpha/private"+q, common...)
	r.do("GET", "/api/alpha/things/"+marker+q, common...)
	r.do("GET", "/api/alpha/files/"+emailMarker+q, common...)
	r.do(marker, "/api/alpha/"+q, common...)
	r.do("PUT", "/api/alpha/"+q, common...)
	r.do("GET", "/"+marker+q, common...)
	r.do("POST", "/api/client-errors"+q, append([]reqOpt{body("text/plain", marker)}, common...)...)
	r.do("POST", "/api/client-errors"+q, append([]reqOpt{jsonBody(`{"reports":[]}`)}, common...)...)
	for _, m := range []string{marker, emailMarker} {
		if r.cap.Contains(m) {
			t.Errorf("marker %q reached a log, span or metric", m)
		}
	}
	if len(r.cap.Records()) == 0 {
		t.Error("expected at least one log record from the forced errors")
	}
}

func TestPipeline_ConcurrentRequests(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)))
	h := r.server().Handler()
	done := make(chan int, 40)
	for i := 0; i < 40; i++ {
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, newRequest(t, "GET", "/api/alpha/things/1"))
			done <- rec.Code
		}()
	}
	for i := 0; i < 40; i++ {
		if c := <-done; c != 200 {
			t.Errorf("status = %d", c)
		}
	}
}

func TestPipeline_WriteError_MarksSpanAborted(t *testing.T) {
	var werr error
	set := setOf(t, "alpha", route("GET", "/x", module.AccessPublic, func(w http.ResponseWriter, _ *http.Request) {
		werr = httpx.WriteJSON(w, 200, map[string]string{"a": "b"})
	}))
	r := newRig(t, withRoutes(set))
	h := r.server().Handler()
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(w, newRequest(t, "GET", "/api/alpha/x"))
	if !errs.Is(werr, errs.Canceled) {
		t.Fatalf("WriteJSON err = %v, want Canceled", werr)
	}
	s, ok := spanNamed(r.cap, "GET /api/alpha/x")
	if !ok {
		t.Fatal("no span")
	}
	if s.Attrs["http.response.aborted"] != "true" {
		t.Errorf("attrs = %v, want http.response.aborted=true", s.Attrs)
	}
	if len(errorRecords(r.cap)) != 0 || len(recordsWith(r.cap, slogError)) != 0 {
		t.Errorf("a failed write must not be logged: %+v", r.cap.Records())
	}
}

type failingWriter struct{ *httptest.ResponseRecorder }

func (*failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
