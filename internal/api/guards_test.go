package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

// ---- authentication ----

func TestAuth_SignedInRouteNoSession_401(t *testing.T) {
	for name, a := range map[string]api.Authenticator{"nil authenticator": nil, "no session": noSession()} {
		t.Run(name, func(t *testing.T) {
			var mut []func(*api.Options)
			if a != nil {
				mut = append(mut, withAuth(a))
			}
			r := newRig(t, append([]func(*api.Options){withRoutes(alphaSet(t))}, mut...)...)
			wantProblem(t, r.do("GET", "/api/alpha/private"), 401, "unauthenticated")
			for _, l := range r.cap.Records() {
				if l.Level > slogDebug {
					t.Errorf("a 401 with no session logged above debug: %+v", l)
				}
			}
		})
	}
}

func TestAuth_SignedInRouteBadSession_401(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)),
		withAuth(authFails(errs.E("identity.Authenticate", errs.Unauthenticated, errors.New("expired")))))
	wantProblem(t, r.do("GET", "/api/alpha/private", hdr("Cookie", "__Host-bms_session="+marker)), 401, "unauthenticated")
	for _, l := range r.cap.Records() {
		if l.Level > slogDebug {
			t.Errorf("anonymous 401 logged above debug: %+v", l)
		}
	}
	if r.cap.Contains(marker) {
		t.Error("the cookie value reached telemetry")
	}
}

func TestAuth_PublicRouteInvalidSession_ServedAnonymously(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)),
		withAuth(authFails(errs.E("identity.Authenticate", errs.Unauthenticated, errors.New("stale")))))
	if rec := r.do("GET", "/api/alpha/"); rec.Code != 200 {
		t.Fatalf("public route with a stale session = %d", rec.Code)
	}
}

func TestAuth_AuthenticatorUnavailable_503OnSignedInAnonymousOnPublic(t *testing.T) {
	r := newRig(t, withRoutes(alphaSet(t)),
		withAuth(authFails(errs.E("identity.Authenticate", errs.Unavailable, errors.New("db down")))))
	wantProblem(t, r.do("GET", "/api/alpha/private"), 503, "unavailable")
	before := len(recordsWith(r.cap, slogWarn))
	if rec := r.do("GET", "/api/alpha/"); rec.Code != 200 {
		t.Fatalf("public route = %d, want served anonymously", rec.Code)
	}
	if len(recordsWith(r.cap, slogWarn)) <= before {
		t.Error("no warn log for the authenticator failure on a public route")
	}
	r2 := newRig(t, withRoutes(alphaSet(t)),
		withAuth(authFails(errs.E("identity.Authenticate", errs.Timeout, errors.New("slow")))))
	wantProblem(t, r2.do("GET", "/api/alpha/private"), 504, "timeout")
}

func TestAuth_SignedIn_ActorReachesHandlerAndSpan(t *testing.T) {
	person := newPerson()
	r := newRig(t, withRoutes(alphaSet(t)), withAuth(signedIn(person)))
	rec := r.do("GET", "/api/alpha/private")
	if rec.Code != 200 || decodeMap(t, rec.Body.Bytes())["actor"] != true {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	// Public routes attribute a signed-in person too.
	r.do("GET", "/api/alpha/")
	s, ok := spanNamed(r.cap, "GET /api/alpha/")
	if !ok || s.Attrs["person_id"] != person.String() {
		t.Fatalf("public-route span = %+v", s)
	}
}

func TestAuth_CalledOnEveryRoutedRequest(t *testing.T) {
	calls := 0
	a := authFn(func(ctx context.Context, _ *http.Request) (context.Context, model.PersonID, error) {
		calls++
		return ctx, model.PersonID{}, nil
	})
	r := newRig(t, withRoutes(alphaSet(t)), withAuth(a))
	r.do("GET", "/api/alpha/")
	r.do("GET", "/api/alpha/things/1")
	r.do("GET", "/api/alpha/private")
	if calls != 3 {
		t.Fatalf("Authenticate calls = %d, want 3", calls)
	}
}

func TestAuth_AuthenticatorNilCtx_500(t *testing.T) {
	bad := authFn(func(context.Context, *http.Request) (context.Context, model.PersonID, error) {
		return nil, model.PersonID{}, nil //nolint:nilnil // the bug under test
	})
	r := newRig(t, withRoutes(alphaSet(t)), withAuth(bad))
	wantProblem(t, r.do("GET", "/api/alpha/"), 500, "internal")
	wantProblem(t, r.do("GET", "/api/alpha/private"), 500, "internal")
	if len(recordsWith(r.cap, slogError)) == 0 {
		t.Error("no error log")
	}
}

// ---- cross-origin protection ----

func methodSet(t *testing.T) module.ModuleRoutes {
	t.Helper()
	pub := module.AccessPublic
	return setOf(t, "beta",
		route("GET", "/m", pub, textHandler("ok")),
		route("POST", "/m", pub, textHandler("ok")),
		route("PUT", "/m", pub, textHandler("ok")),
		route("PATCH", "/m", pub, textHandler("ok")),
		route("DELETE", "/m", pub, textHandler("ok")),
	)
}

func TestCrossOrigin_Table(t *testing.T) {
	tests := []struct {
		name string
		opts []reqOpt
		want int
	}{
		{"cross-site rejected", []reqOpt{hdr("Sec-Fetch-Site", "cross-site")}, 403},
		{"same-site rejected", []reqOpt{hdr("Sec-Fetch-Site", "same-site")}, 403},
		{"origin null rejected", []reqOpt{hdr("Origin", "null")}, 403},
		{"origin host differs rejected", []reqOpt{hdr("Origin", "https://evil.example")}, 403},
		{"origin host matches", []reqOpt{hdr("Origin", "http://bms.test")}, 200},
		{"same-origin accepted", []reqOpt{hdr("Sec-Fetch-Site", "same-origin")}, 200},
		{"none accepted", []reqOpt{hdr("Sec-Fetch-Site", "none")}, 200},
		{"no headers accepted", nil, 200},
		{"cross-site beats a matching origin", []reqOpt{hdr("Sec-Fetch-Site", "cross-site"), hdr("Origin", "http://bms.test")}, 403},
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, tc := range tests {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				r := newRig(t, withRoutes(methodSet(t)))
				rec := r.do(method, "/api/beta/m", tc.opts...)
				if tc.want == 403 {
					wantProblem(t, rec, 403, httpx.CodeCrossOrigin)
				} else if rec.Code != 200 {
					t.Fatalf("status = %d", rec.Code)
				}
				for k := range rec.Header() {
					if strings.HasPrefix(strings.ToLower(k), "access-control-") {
						t.Errorf("response carries %s", k)
					}
				}
			})
		}
	}
	t.Run("GET is never checked", func(t *testing.T) {
		r := newRig(t, withRoutes(methodSet(t)))
		if rec := r.do("GET", "/api/beta/m", hdr("Sec-Fetch-Site", "cross-site")); rec.Code != 200 {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestCrossOrigin_LogOmitsOrigin(t *testing.T) {
	r := newRig(t, withRoutes(methodSet(t)))
	r.do("POST", "/api/beta/m", hdr("Origin", "https://"+marker+".example"))
	r.do("POST", "/api/beta/m", hdr("Origin", marker), hdr("Sec-Fetch-Site", "cross-site"))
	if r.cap.Contains(marker) {
		t.Error("the Origin value reached telemetry")
	}
}

// ---- cookie guard ----

const (
	momentWriteHeader = iota
	momentImplicit
	momentNothing
)

func cookieRig(t *testing.T, raw *string, moment *int, mut ...func(*api.Options)) *rig {
	t.Helper()
	set := setOf(t, "alpha", route("GET", "/c", module.AccessPublic, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", *raw)
		switch *moment {
		case momentWriteHeader:
			w.WriteHeader(http.StatusOK)
		case momentImplicit:
			if _, err := w.Write([]byte("body")); err != nil {
				return
			}
		}
	}))
	return newRig(t, append([]func(*api.Options){withRoutes(set)}, mut...)...)
}

var momentNames = map[int]string{momentWriteHeader: "WriteHeader", momentImplicit: "implicit write", momentNothing: "nothing written"}

func TestCookieGuard_Table(t *testing.T) {
	const v = "sessionvalue" + marker
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"all attributes, __Host- prefix", "__Host-bms_session=" + v + "; Path=/; Secure; HttpOnly; SameSite=Lax", true},
		{"all attributes, plain name", "pref=" + v + "; Path=/; Secure; HttpOnly; SameSite=Lax", true},
		{"missing Secure", "__Host-bms_session=" + v + "; Path=/; HttpOnly; SameSite=Lax", false},
		{"missing HttpOnly", "__Host-bms_session=" + v + "; Path=/; Secure; SameSite=Lax", false},
		{"missing SameSite", "__Host-bms_session=" + v + "; Path=/; Secure; HttpOnly", false},
		{"SameSite=None", "__Host-bms_session=" + v + "; Path=/; Secure; HttpOnly; SameSite=None", false},
		{"missing Path", "__Host-bms_session=" + v + "; Secure; HttpOnly; SameSite=Lax", false},
		{"Path not root", "__Host-bms_session=" + v + "; Path=/api; Secure; HttpOnly; SameSite=Lax", false},
		{"Domain set", "__Host-bms_session=" + v + "; Path=/; Domain=example.com; Secure; HttpOnly; SameSite=Lax", false},
		{"unparseable", "no-equals-sign" + v, false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		for _, m := range []int{momentWriteHeader, momentImplicit, momentNothing} {
			t.Run(tc.name+"/"+momentNames[m], func(t *testing.T) {
				raw, moment := tc.raw, m
				r := cookieRig(t, &raw, &moment)
				rec := r.do("GET", "/api/alpha/c")
				if tc.ok {
					if rec.Code != 200 || rec.Header().Get("Set-Cookie") != tc.raw {
						t.Fatalf("valid cookie refused: %d %v", rec.Code, rec.Header())
					}
					return
				}
				wantProblem(t, rec, 500, "internal")
				if strings.Contains(rec.Body.String(), v) || strings.Contains(fmt.Sprint(rec.Header()), v) {
					t.Errorf("the refused cookie's value is in the response: %v", rec.Header())
				}
				if len(recordsWith(r.cap, slogError)) == 0 {
					t.Error("no error log naming the cookie")
				}
				if r.cap.Contains(v) {
					t.Error("the cookie value was logged")
				}
			})
		}
	}
}

func TestCookieGuard_NamesCookieNeverValue(t *testing.T) {
	raw := "__Host-guardtest=" + marker + "; Path=/; HttpOnly; SameSite=Lax"
	moment := momentWriteHeader
	r := cookieRig(t, &raw, &moment)
	r.do("GET", "/api/alpha/c")
	if r.cap.Contains(marker) {
		t.Error("the value was logged")
	}
	if !r.cap.Contains("__Host-guardtest") {
		t.Error("the cookie's name should be logged so a developer can find the bug")
	}
}

func TestCookieGuard_HostPrefixCookiePasses(t *testing.T) {
	raw := "__Host-bms_session=abc; Path=/; Secure; HttpOnly; SameSite=Lax"
	moment := momentImplicit
	r := cookieRig(t, &raw, &moment)
	if rec := r.do("GET", "/api/alpha/c"); rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCookieGuard_DevCookie_Table(t *testing.T) {
	const dev = "bms_session_dev=tok; Path=/; HttpOnly; SameSite=Lax"
	const devSecure = "bms_session_dev=tok; Path=/; Secure; HttpOnly; SameSite=Lax"
	const host = "__Host-bms_session=tok; Path=/; HttpOnly; SameSite=Lax"
	httpOpts := []reqOpt{}
	httpsOpts := []reqOpt{hdr("X-Forwarded-Proto", "https")}
	tests := []struct {
		name      string
		raw       string
		plaintext bool
		opts      []reqOpt
		ok        bool
	}{
		{"dev cookie: plaintext allowed, http", dev, true, httpOpts, true},
		{"dev cookie: plaintext allowed, https", dev, true, httpsOpts, false},
		{"dev cookie: not allowed, http", dev, false, httpOpts, false},
		{"dev cookie: not allowed, https", dev, false, httpsOpts, false},
		{"dev cookie lacking HttpOnly", "bms_session_dev=tok; Path=/; SameSite=Lax", true, httpOpts, false},
		{"dev cookie lacking SameSite", "bms_session_dev=tok; Path=/; HttpOnly", true, httpOpts, false},
		{"dev cookie with Domain", dev + "; Domain=example.com", true, httpOpts, false},
		{"dev cookie with Secure, https", devSecure, true, httpsOpts, true},
		{"dev cookie with Secure, http", devSecure, true, httpOpts, true},
		{"dev cookie with Secure, not allowed, https", devSecure, false, httpsOpts, false},
		{"dev cookie with Secure, not allowed, http", devSecure, false, httpOpts, false},
		{"host cookie without Secure: plaintext http", host, true, httpOpts, false},
		{"host cookie without Secure: plaintext https", host, true, httpsOpts, false},
		{"host cookie without Secure: strict http", host, false, httpOpts, false},
		{"host cookie without Secure: strict https", host, false, httpsOpts, false},
		{"other cookie without Secure: plaintext http", "pref=1; Path=/; HttpOnly; SameSite=Lax", true, httpOpts, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, moment := tc.raw, momentWriteHeader
			r := cookieRig(t, &raw, &moment, func(o *api.Options) {
				o.AllowPlaintext = tc.plaintext
				o.TrustedProxies = prefixes("127.0.0.1/32")
			})
			rec := r.do("GET", "/api/alpha/c", tc.opts...)
			if tc.ok && rec.Code != 200 {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !tc.ok {
				wantProblem(t, rec, 500, "internal")
				if c := rec.Header().Get("Set-Cookie"); c != "" {
					t.Errorf("refused cookie must be dropped, got %q", c)
				}
			}
		})
	}
}
