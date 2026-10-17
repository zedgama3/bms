package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestOptionsValidate_Valid(t *testing.T) {
	r := newRig(t)
	if err := r.opts.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	r.opts.TrustedProxies = prefixes("10.0.0.0/8", "::1/128")
	if err := r.opts.Validate(); err != nil {
		t.Fatalf("Validate with proxies: %v", err)
	}
}

func TestOptionsValidate_ReportsEach(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(*api.Options)
		field string
	}{
		{"addr empty", func(o *api.Options) { o.Addr = "" }, "http.addr"},
		{"addr garbage", func(o *api.Options) { o.Addr = "not an address" }, "http.addr"},
		{"timeout zero", func(o *api.Options) { o.RequestTimeout = 0 }, "http.request_timeout"},
		{"timeout too short", func(o *api.Options) { o.RequestTimeout = 500 * time.Millisecond }, "http.request_timeout"},
		{"timeout too long", func(o *api.Options) { o.RequestTimeout = 11 * time.Minute }, "http.request_timeout"},
		{"body cap too small", func(o *api.Options) { o.MaxBodyBytes = 1023 }, "http.max_body_bytes"},
		{"body cap too big", func(o *api.Options) { o.MaxBodyBytes = 1<<30 + 1 }, "http.max_body_bytes"},
		{"invalid prefix", func(o *api.Options) { o.TrustedProxies = append(o.TrustedProxies, netipZeroPrefix()) }, "http.trusted_proxies"},
		{"nil logger", func(o *api.Options) { o.Logger = nil }, "api.logger"},
		{"nil tracer", func(o *api.Options) { o.Tracer = nil }, "api.tracer"},
		{"nil meter", func(o *api.Options) { o.Meter = nil }, "api.meter"},
		{"nil clock", func(o *api.Options) { o.Clock = nil }, "api.clock"},
		{"version too long", func(o *api.Options) { o.Version = strings.Repeat("v", 65) }, "api.version"},
		{"version with space", func(o *api.Options) { o.Version = "1 2" }, "api.version"},
		{"nil database", func(o *api.Options) { o.Database = nil }, "api.database"},
		{"nil intake", func(o *api.Options) { o.Intake = nil }, "api.intake"},
		{"nil schema", func(o *api.Options) { o.Schema = nil }, "api.schema"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.mut)
			err := r.opts.Validate()
			if !errs.Is(err, errs.Invalid) || !hasField(err, tc.field) {
				t.Fatalf("Validate = %v, want Invalid on %s", err, tc.field)
			}
			if _, nerr := api.New(r.opts); !errs.Is(nerr, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", nerr)
			}
		})
	}
}

func TestOptionsValidate_SeveralAtOnce(t *testing.T) {
	r := newRig(t, func(o *api.Options) {
		o.Addr = ""
		o.RequestTimeout = 0
		o.Logger = nil
		o.Database = nil
	})
	err := r.opts.Validate()
	for _, f := range []string{"http.addr", "http.request_timeout", "api.logger", "api.database"} {
		if !hasField(err, f) {
			t.Errorf("missing field %s in %v", f, err)
		}
	}
}

func TestOptionsValidate_BadRouteSet_NamesRoutesIndex(t *testing.T) {
	r := newRig(t, withRoutes(module.ModuleRoutes{Module: "Bad Name"}))
	err := r.opts.Validate()
	if !errs.Is(err, errs.Invalid) || !hasField(err, "api.routes[0]") {
		t.Fatalf("Validate = %v, want Invalid on api.routes[0]", err)
	}
}

func TestOptionsValidate_ZeroBitPrefix_ReturnsInvalid(t *testing.T) {
	for _, p := range []string{"0.0.0.0/0", "::/0"} {
		t.Run(p, func(t *testing.T) {
			r := newRig(t, func(o *api.Options) { o.TrustedProxies = prefixes(p) })
			err := r.opts.Validate()
			if !errs.Is(err, errs.Invalid) || !hasField(err, "http.trusted_proxies") {
				t.Fatalf("Validate = %v", err)
			}
			if _, nerr := api.New(r.opts); !errs.Is(nerr, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", nerr)
			}
		})
	}
}

func TestNew_BadDuplicateOrShellSetName_ReturnsInvalid(t *testing.T) {
	ok := func(t *testing.T, name string) module.ModuleRoutes {
		return setOf(t, name, route("GET", "/x", module.AccessPublic, textHandler("x")))
	}
	tests := []struct {
		name string
		sets func(t *testing.T) []module.ModuleRoutes
	}{
		{"upper case", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "Alpha")} }},
		{"too short", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "a")} }},
		{"hyphen", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "al-pha")} }},
		{"empty", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "")} }},
		{"duplicate", func(t *testing.T) []module.ModuleRoutes {
			return []module.ModuleRoutes{ok(t, "alpha"), ok(t, "alpha")}
		}},
		{"core", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "core")} }},
		{"modules", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "modules")} }},
		{"admin", func(t *testing.T) []module.ModuleRoutes { return []module.ModuleRoutes{ok(t, "admin")} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, withRoutes(tc.sets(t)...))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestNew_InvalidCoreRoute_ReturnsInvalid(t *testing.T) {
	good := textHandler("x")
	tests := []struct {
		name string
		rt   module.Route
	}{
		{"bad method", module.Route{Method: "TRACE", Pattern: "/x", Handler: good}},
		{"nil handler", module.Route{Method: "GET", Pattern: "/x"}},
		{"no leading slash", module.Route{Method: "GET", Pattern: "x", Handler: good}},
		{"bad access", module.Route{Method: "GET", Pattern: "/x", Access: 9, Handler: good}},
		{"space in pattern", module.Route{Method: "GET", Pattern: "/x y", Handler: good}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := module.ModuleRoutes{Module: "accounts", Routes: []module.Route{tc.rt}, OpenAPI: []byte(`{"paths":{}}`)}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestNew_ConflictingPatterns_ReturnsConflict(t *testing.T) {
	pub := module.AccessPublic
	set := setOf(t, "alpha",
		route("GET", "/{kind}/x", pub, textHandler("a")),
		route("GET", "/things/{id}", pub, textHandler("b")),
	)
	r := newRig(t, withRoutes(set))
	if _, err := api.New(r.opts); !errs.Is(err, errs.Conflict) {
		t.Fatalf("New = %v, want Conflict", err)
	}
}

func TestNew_MethodSiblings_NoConflict(t *testing.T) {
	pub := module.AccessPublic
	set := setOf(t, "alpha",
		route("GET", "/shifts/{id}", pub, textHandler("one")),
		route("POST", "/shifts/bulk", pub, textHandler("bulk")),
	)
	r := newRig(t, withRoutes(set))
	if _, err := api.New(r.opts); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_RouteEscapesPrefix_ReturnsInvalid(t *testing.T) {
	for _, pattern := range []string{"/../x", "/../../healthz"} {
		t.Run(pattern, func(t *testing.T) {
			rt := route("GET", pattern, module.AccessPublic, textHandler("x"))
			set := module.ModuleRoutes{Module: "accounts", Routes: []module.Route{rt}, OpenAPI: []byte(`{"paths":{}}`)}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestNew_RoutesWithoutFragment_ReturnsInvalid(t *testing.T) {
	rt := route("GET", "/x", module.AccessPublic, textHandler("x"))
	for name, frag := range map[string][]byte{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, withRoutes(module.ModuleRoutes{Module: "alpha", Routes: []module.Route{rt}, OpenAPI: frag}))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestNew_RouteFragmentMismatch_ReturnsInvalid(t *testing.T) {
	pub := module.AccessPublic
	get := route("GET", "/x", pub, textHandler("x"))
	tests := []struct {
		name   string
		routes []module.Route
		frag   string
	}{
		{"route without operation", []module.Route{get}, `{"paths":{}}`},
		{"wrong method", []module.Route{get}, `{"paths":{"/x":{"post":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"wrong path", []module.Route{get}, `{"paths":{"/y":{"get":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"operation without route", []module.Route{get}, `{"paths":{"/x":{"get":{"responses":{"200":{"description":"ok"}}},"post":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"extra path", []module.Route{get}, `{"paths":{"/x":{"get":{"responses":{"200":{"description":"ok"}}}},"/z":{"get":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"head without get route", []module.Route{route("POST", "/x", pub, textHandler("x"))}, `{"paths":{"/x":{"post":{"responses":{"200":{"description":"ok"}}},"head":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"trailing slash differs", []module.Route{route("GET", "/x/", pub, textHandler("x"))}, `{"paths":{"/x":{"get":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"wildcard not translated", []module.Route{route("GET", "/f/{p...}", pub, textHandler("x"))}, `{"paths":{"/f/{p...}":{"get":{"responses":{"200":{"description":"ok"}}}}}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, withRoutes(module.ModuleRoutes{Module: "alpha", Routes: tc.routes, OpenAPI: []byte(tc.frag)}))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestNew_RouteFragmentTranslation_Table(t *testing.T) {
	pub := module.AccessPublic
	op := `{"responses":{"200":{"description":"ok"}}}`
	tests := []struct {
		name    string
		pattern string
		method  string
		frag    string
	}{
		{"root slash", "/", "GET", `{"paths":{"/":{"get":` + op + `}}}`},
		{"trailing slash exact", "/x/", "GET", `{"paths":{"/x/":{"get":` + op + `}}}`},
		{"plain param", "/t/{id}", "GET", `{"paths":{"/t/{id}":{"get":` + op + `}}}`},
		{"wildcard", "/f/{path...}", "GET", `{"paths":{"/f/{path}":{"get":` + op + `}}}`},
		{"head absent is fine", "/h", "GET", `{"paths":{"/h":{"get":` + op + `}}}`},
		{"head present matches get", "/h", "GET", `{"paths":{"/h":{"get":` + op + `,"head":` + op + `}}}`},
		{"post", "/p", "POST", `{"paths":{"/p":{"post":` + op + `}}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := module.ModuleRoutes{
				Module:  "alpha",
				Routes:  []module.Route{route(tc.method, tc.pattern, pub, textHandler("x"))},
				OpenAPI: []byte(tc.frag),
			}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); err != nil {
				t.Fatalf("New: %v", err)
			}
		})
	}
}

func TestNew_MalformedFragment_ReturnsInvalid(t *testing.T) {
	pub := module.AccessPublic
	rt := route("GET", "/x", pub, textHandler("x"))
	refResp := func(ref string) string {
		return `{"paths":{"/x":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"` + ref + `"}}}}}}}}}`
	}
	tests := []struct{ name, frag string }{
		{"not json", `{"paths":`},
		{"array", `[]`},
		{"string", `"x"`},
		{"path without slash", `{"paths":{"x":{"get":{"responses":{"200":{"description":"ok"}}}}}}`},
		{"remote ref", refResp("https://example.com/schema.json#/X")},
		{"file ref", refResp("other.json#/components/schemas/X")},
		{"unresolved ref", refResp("#/components/schemas/NoSuchThing")},
		{"non-schema ref", refResp("#/paths/~1x")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := module.ModuleRoutes{Module: "alpha", Routes: []module.Route{rt}, OpenAPI: []byte(tc.frag)}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func refFragment(ref string, schemas map[string]any) []byte {
	return mustJSON(map[string]any{
		"paths": map[string]any{"/x": map[string]any{"get": map[string]any{"responses": map[string]any{"200": map[string]any{
			"description": "ok",
			"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": ref}}},
		}}}}},
		"components": map[string]any{"schemas": schemas},
	})
}

func TestNew_RefToOtherSetSchema_ReturnsInvalid(t *testing.T) {
	pub := module.AccessPublic
	rt := route("GET", "/x", pub, textHandler("x"))
	beta := module.ModuleRoutes{
		Module: "beta", Routes: []module.Route{rt},
		OpenAPI: refFragment("#/components/schemas/BetaThing", map[string]any{"BetaThing": map[string]any{"type": "object"}}),
	}
	alpha := module.ModuleRoutes{
		Module: "alpha", Routes: []module.Route{rt},
		OpenAPI: refFragment("#/components/schemas/BetaThing", nil),
	}
	r := newRig(t, withRoutes(beta, alpha))
	if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
		t.Fatalf("New = %v, want Invalid (a set must not reference another set's schema)", err)
	}
}

func TestNew_RefToBaseSchema_Resolves(t *testing.T) {
	pub := module.AccessPublic
	rt := route("GET", "/x", pub, textHandler("x"))
	for _, ref := range []string{"#/components/schemas/Problem", "#/components/schemas/ID", "#/components/schemas/AlphaThing"} {
		t.Run(ref, func(t *testing.T) {
			set := module.ModuleRoutes{
				Module: "alpha", Routes: []module.Route{rt},
				OpenAPI: refFragment(ref, map[string]any{"AlphaThing": map[string]any{"type": "object"}}),
			}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); err != nil {
				t.Fatalf("New: %v", err)
			}
		})
	}
}

func TestNew_DuplicateSchemaName_ReturnsConflict(t *testing.T) {
	pub := module.AccessPublic
	mk := func(name string) module.ModuleRoutes {
		rts := []module.Route{route("GET", "/x", pub, textHandler("x"))}
		return module.ModuleRoutes{Module: name, Routes: rts,
			OpenAPI: fragmentFor(t, rts, map[string]any{"Shared": map[string]any{"type": "object"}})}
	}
	r := newRig(t, withRoutes(mk("alpha"), mk("beta")))
	if _, err := api.New(r.opts); !errs.Is(err, errs.Conflict) {
		t.Fatalf("New = %v, want Conflict", err)
	}
}

func TestNew_SchemaNameClashesWithBase_Fails(t *testing.T) {
	pub := module.AccessPublic
	rts := []module.Route{route("GET", "/x", pub, textHandler("x"))}
	set := module.ModuleRoutes{Module: "alpha", Routes: rts,
		OpenAPI: fragmentFor(t, rts, map[string]any{"Problem": map[string]any{"type": "string"}})}
	r := newRig(t, withRoutes(set))
	if _, err := api.New(r.opts); err == nil {
		t.Fatal("New accepted a fragment that redefines the base Problem schema")
	}
}

func TestNew_HistogramCreationFails_ReturnsKind(t *testing.T) {
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	m := c.Meter("api")
	// Same name, different unit: telemetry answers Conflict.
	if _, err := m.Histogram("http.server.request.duration", telemetry.WithUnit("ms")); err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	o := baseOptions(t, c, clk, &fakeDB{})
	o.Meter = m
	if _, err := api.New(o); !errs.Is(err, errs.Conflict) {
		t.Fatalf("New = %v, want Conflict", err)
	}
}

func TestNew_RouteTokenFailsGrammar_ReturnsInvalid(t *testing.T) {
	pub := module.AccessPublic
	for name, pattern := range map[string]string{
		"percent": "/caf%C3%A9",
		"plus":    "/a+b",
		"long":    "/" + strings.Repeat("a", 129),
	} {
		t.Run(name, func(t *testing.T) {
			rt := module.Route{Method: "GET", Pattern: pattern, Access: pub, Handler: textHandler("x")}
			set := module.ModuleRoutes{Module: "alpha", Routes: []module.Route{rt}, OpenAPI: []byte(`{"paths":{}}`)}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestNew_AllowPlaintext_LogsWarningOnce(t *testing.T) {
	r := newRig(t, func(o *api.Options) { o.AllowPlaintext = true })
	r.server()
	warns := recordsWith(r.cap, slogWarn)
	if len(warns) != 1 {
		t.Fatalf("warn records = %d, want 1: %+v", len(warns), warns)
	}
	// Without AllowPlaintext there is no such warning.
	r2 := newRig(t)
	r2.server()
	if n := len(recordsWith(r2.cap, slogWarn)); n != 0 {
		t.Fatalf("unexpected warnings: %d", n)
	}
}

func TestNew_LoopbackBindWithoutLoopbackProxy_WarnsOnce(t *testing.T) {
	r := newRig(t, func(o *api.Options) {
		o.Addr = "127.0.0.1:8080"
		o.TrustedProxies = prefixes("10.0.0.0/8")
	})
	r.server()
	if n := len(recordsWith(r.cap, slogWarn)); n != 1 {
		t.Fatalf("warn records = %d, want 1", n)
	}
	// A loopback proxy listed: no warning.
	r2 := newRig(t, func(o *api.Options) {
		o.Addr = "127.0.0.1:8080"
		o.TrustedProxies = prefixes("127.0.0.1/32")
	})
	r2.server()
	if n := len(recordsWith(r2.cap, slogWarn)); n != 0 {
		t.Fatalf("warn records = %d, want 0", n)
	}
}

func TestNew_DoesNoNetworkIO(t *testing.T) {
	r := newRig(t, func(o *api.Options) { o.Addr = "127.0.0.1:1" })
	r.server()
	if r.db.pingCount() != 0 {
		t.Fatalf("New pinged the database")
	}
}

func TestSpanName_EveryMountedRoute_NoViolation(t *testing.T) {
	pub := module.AccessPublic
	set := setOf(t, "alpha",
		route("GET", "/", pub, textHandler("x")),
		route("GET", "/a/", pub, textHandler("x")),
		route("GET", "/t/{id}", pub, textHandler("x")),
		route("GET", "/f/{path...}", pub, textHandler("x")),
		route("POST", "/p", pub, textHandler("x")),
		route("DELETE", "/a/b_c.d:e-f", pub, textHandler("x")),
	)
	r := newRig(t, withRoutes(set), func(o *api.Options) { o.UI = nil })
	for _, target := range []struct{ m, p string }{
		{"GET", "/api/alpha/"}, {"GET", "/api/alpha/a/"}, {"GET", "/api/alpha/t/7"},
		{"GET", "/api/alpha/f/x/y"}, {"POST", "/api/alpha/p"}, {"DELETE", "/api/alpha/a/b_c.d:e-f"},
		{"GET", "/api/modules"}, {"GET", "/api/openapi.json"}, {"GET", "/api/nothing"},
	} {
		opts := []reqOpt{}
		if target.m != "GET" {
			opts = append(opts, hdr("Sec-Fetch-Site", "same-origin"))
		}
		r.do(target.m, target.p, opts...)
	}
	if v := r.cap.Violations(); len(v) != 0 {
		t.Fatalf("violations: %+v", v)
	}
	if _, ok := spanNamed(r.cap, "GET /api/alpha/"); !ok {
		t.Error("no span named \"GET /api/alpha/\"")
	}
}
