package module_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

type routeCase struct {
	name string
	r    module.Route
	ok   bool
}

func routeTable() []routeCase {
	return []routeCase{
		{"get root", route("GET", "/"), true},
		{"get simple", route("GET", "/greeting"), true},
		{"post", route("POST", "/greetings"), true},
		{"put", route("PUT", "/a"), true},
		{"patch", route("PATCH", "/a"), true},
		{"delete", route("DELETE", "/a/{id}"), true},
		{"exact marker", route("GET", "/x/{$}"), true},
		{"subtree", route("GET", "/files/{path...}"), true},
		{"dots colons hyphens underscores", route("GET", "/a.b:c-d_e"), true},
		{"public access", module.Route{Method: "GET", Pattern: "/p", Access: module.AccessPublic, Handler: okHandler}, true},
		{"lower-case method", route("get", "/a"), false},
		{"unknown method", route("TRACE", "/a"), false},
		{"head not one of five", route("HEAD", "/a"), false},
		{"empty method", route("", "/a"), false},
		{"no leading slash", route("GET", "a"), false},
		{"empty pattern", route("GET", ""), false},
		{"space", route("GET", "/a b"), false},
		{"percent", route("GET", "/caf%C3%A9"), false},
		{"plus", route("GET", "/a+b"), false},
		{"comma", route("GET", "/a,b"), false},
		{"semicolon", route("GET", "/a;b"), false},
		{"non-ascii", route("GET", "/café"), false},
		{"nil handler", module.Route{Method: "GET", Pattern: "/a"}, false},
		{"access out of range", module.Route{Method: "GET", Pattern: "/a", Access: 9, Handler: okHandler}, false},
	}
}

func TestRouteValidate_Table(t *testing.T) {
	for _, tc := range routeTable() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.ok {
				wantKind(t, err, errs.Invalid)
			}
		})
	}
}

func TestAccess_ZeroValueIsSignedIn(t *testing.T) {
	var a module.Access
	if a != module.AccessSignedIn {
		t.Fatal("the zero Access must be AccessSignedIn")
	}
	if module.AccessPublic == module.AccessSignedIn {
		t.Fatal("AccessPublic must differ from the zero value")
	}
}

// module-contract F33 (D44): MaxBodyBytes and Timeout outside their range are Invalid, with
// a FieldError on max_body_bytes or timeout; the ceilings themselves and zero pass.
func TestRoute_Validate_LimitsAboveCeiling_ReturnsInvalid(t *testing.T) {
	with := func(f func(*module.Route)) module.Route {
		r := route("POST", "/up")
		f(&r)
		return r
	}
	tests := []struct {
		name  string
		r     module.Route
		field string // empty = valid
	}{
		{"zero is the shell's", with(func(*module.Route) {}), ""},
		{"body at ceiling", with(func(r *module.Route) { r.MaxBodyBytes = module.MaxRouteBodyBytes }), ""},
		{"body above ceiling", with(func(r *module.Route) { r.MaxBodyBytes = module.MaxRouteBodyBytes + 1 }), "max_body_bytes"},
		{"body negative", with(func(r *module.Route) { r.MaxBodyBytes = -1 }), "max_body_bytes"},
		{"timeout at ceiling", with(func(r *module.Route) { r.Timeout = module.MaxRouteTimeout }), ""},
		{"timeout above ceiling", with(func(r *module.Route) { r.Timeout = module.MaxRouteTimeout + 1 }), "timeout"},
		{"timeout negative", with(func(r *module.Route) { r.Timeout = -1 }), "timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
			found := false
			for _, f := range validate.Fields(err) {
				found = found || f.Field == tc.field
			}
			if !found {
				t.Errorf("no FieldError on %q in %v", tc.field, err)
			}
		})
	}
}
