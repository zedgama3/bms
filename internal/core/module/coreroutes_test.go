package module_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func coreRouteHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func TestCoreRoutes_PublicBecomesAccessPublic(t *testing.T) {
	spec := []byte(`{"openapi":"3.1.0"}`)
	in := []httpx.Route{
		{Method: http.MethodGet, Pattern: "/public", Public: true, Handler: coreRouteHandler()},
		{Method: http.MethodPost, Pattern: "/private", Handler: coreRouteHandler(), MaxBodyBytes: 1 << 20, Timeout: time.Minute},
	}
	got, err := module.CoreRoutes("people", in, spec)
	if err != nil {
		t.Fatalf("CoreRoutes: %v", err)
	}
	if got.Module != "people" {
		t.Errorf("Module = %q, want people", got.Module)
	}
	if string(got.OpenAPI) != string(spec) {
		t.Errorf("OpenAPI = %q, want %q", got.OpenAPI, spec)
	}
	if len(got.Routes) != 2 {
		t.Fatalf("len(Routes) = %d, want 2", len(got.Routes))
	}
	if r := got.Routes[0]; r.Access != module.AccessPublic || r.Method != http.MethodGet || r.Pattern != "/public" || r.Handler == nil {
		t.Errorf("route 0 = %+v, want public GET /public", r)
	}
	r := got.Routes[1]
	if r.Access != module.AccessSignedIn || r.Method != http.MethodPost || r.Pattern != "/private" {
		t.Errorf("route 1 = %+v, want signed-in POST /private", r)
	}
	if r.MaxBodyBytes != 1<<20 || r.Timeout != time.Minute {
		t.Errorf("route 1 limits = %d, %s; want carried over", r.MaxBodyBytes, r.Timeout)
	}
}

func TestCoreRoutes_Empty_ReturnsNoRoutes(t *testing.T) {
	got, err := module.CoreRoutes("people", nil, nil)
	if err != nil {
		t.Fatalf("CoreRoutes: %v", err)
	}
	if len(got.Routes) != 0 {
		t.Errorf("len(Routes) = %d, want 0", len(got.Routes))
	}
}

func TestCoreRoutes_BadRoute_ReturnsInvalid(t *testing.T) {
	good := httpx.Route{Method: http.MethodGet, Pattern: "/ok", Handler: coreRouteHandler()}
	tests := []struct {
		name  string
		route httpx.Route
		field string
	}{
		{"bad method", httpx.Route{Method: "TRACE", Pattern: "/x", Handler: coreRouteHandler()}, "routes[1].method"},
		{"empty method", httpx.Route{Pattern: "/x", Handler: coreRouteHandler()}, "routes[1].method"},
		{"pattern without slash", httpx.Route{Method: "GET", Pattern: "x", Handler: coreRouteHandler()}, "routes[1].pattern"},
		{"pattern with space", httpx.Route{Method: "GET", Pattern: "/a b", Handler: coreRouteHandler()}, "routes[1].pattern"},
		{"nil handler", httpx.Route{Method: "GET", Pattern: "/x"}, "routes[1].handler"},
		{"negative timeout", httpx.Route{Method: "GET", Pattern: "/x", Handler: coreRouteHandler(), Timeout: -time.Second}, "routes[1].timeout"},
		{"timeout over ceiling", httpx.Route{Method: "GET", Pattern: "/x", Handler: coreRouteHandler(), Timeout: module.MaxRouteTimeout + time.Second}, "routes[1].timeout"},
		{"body over ceiling", httpx.Route{Method: "GET", Pattern: "/x", Handler: coreRouteHandler(), MaxBodyBytes: module.MaxRouteBodyBytes + 1}, "routes[1].max_body_bytes"},
		{"negative body", httpx.Route{Method: "GET", Pattern: "/x", Handler: coreRouteHandler(), MaxBodyBytes: -1}, "routes[1].max_body_bytes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := module.CoreRoutes("people", []httpx.Route{good, tc.route}, nil)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				if fe.Field == tc.field {
					found = true
				}
			}
			if !found {
				t.Errorf("no field %q in %v", tc.field, validate.Fields(err))
			}
		})
	}
}

func TestCoreRoutes_NotACoreArea_ReturnsInvalid(t *testing.T) {
	r := []httpx.Route{{Method: http.MethodGet, Pattern: "/x", Handler: coreRouteHandler()}}
	for _, area := range []string{"", "hello", "volunteers", "People", " people", "api"} {
		t.Run("area "+area, func(t *testing.T) {
			_, err := module.CoreRoutes(area, r, nil)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				if fe.Field == "area" {
					found = true
				}
			}
			if !found {
				t.Errorf("no field %q in %v", "area", validate.Fields(err))
			}
		})
	}
}
