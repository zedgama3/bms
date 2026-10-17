package module_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
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
