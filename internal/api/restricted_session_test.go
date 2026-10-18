package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/model"
)

type restrictedKey struct{}

// restrictedAuth is an Authenticator for a restricted session: it returns a ctx carrying a
// value and the zero person, with no error.
type restrictedAuth struct{}

func (restrictedAuth) Authenticate(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
	return context.WithValue(ctx, restrictedKey{}, "restricted"), model.PersonID{}, nil
}

// identity decision 9: the ctx the Authenticator returns replaces the request's even when
// the person is zero, so a public route (identity's own) sees the restricted session, while
// every signed-in route answers 401.
func TestAuthenticate_ZeroPersonWithCtx_PublicRouteSeesIt_SignedInRouteIs401(t *testing.T) {
	var sawPublic, sawSigned string
	set := setOf(t, "alpha",
		route("GET", "/pub", module.AccessPublic, func(w http.ResponseWriter, r *http.Request) {
			if v, ok := r.Context().Value(restrictedKey{}).(string); ok {
				sawPublic = v
			}
			w.WriteHeader(http.StatusNoContent)
		}),
		route("GET", "/priv", module.AccessSignedIn, func(w http.ResponseWriter, r *http.Request) {
			if v, ok := r.Context().Value(restrictedKey{}).(string); ok {
				sawSigned = v
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	r := newRig(t, withRoutes(set), withAuth(restrictedAuth{}))
	if rec := r.do("GET", "/api/alpha/pub"); rec.Code != http.StatusNoContent {
		t.Fatalf("public route status = %d", rec.Code)
	}
	if sawPublic != "restricted" {
		t.Error("the public route's handler did not see the ctx the Authenticator returned")
	}
	wantProblem(t, r.do("GET", "/api/alpha/priv"), 401, "unauthenticated")
	if sawSigned != "" {
		t.Error("a signed-in handler ran for a restricted session")
	}
}
