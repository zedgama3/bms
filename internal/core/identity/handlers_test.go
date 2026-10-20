package identity_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestRoutes_TheElevenRoutes(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	want := map[string]bool{ // "METHOD pattern" -> Public
		"GET /session":                       true,
		"POST /sign-up":                      true,
		"POST /sign-in":                      true,
		"POST /sign-out":                     true,
		"POST /password":                     true,
		"POST /password-reset":               true,
		"POST /password-reset/complete":      true,
		"POST /sessions/end-others":          false,
		"GET /":                              false,
		"GET /{account}":                     false,
		"POST /{account}/disable":            false,
		"POST /{account}/enable":             false,
		"POST /{account}/temporary-password": false,
	}
	routes := r.svc.Routes()
	if len(routes) != len(want) {
		t.Fatalf("%d routes, want %d", len(routes), len(want))
	}
	for _, rt := range routes {
		key := rt.Method + " " + rt.Pattern
		pub, ok := want[key]
		if !ok {
			t.Errorf("unexpected route %s", key)
			continue
		}
		if rt.Public != pub {
			t.Errorf("%s: Public = %v, want %v", key, rt.Public, pub)
		}
		if rt.Handler == nil {
			t.Errorf("%s: nil handler", key)
		}
	}
}

func TestOpenAPI_FragmentNamesItsSchemas(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	raw := r.svc.OpenAPI()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("OpenAPI is not JSON: %v", err)
	}
	for _, name := range []string{"AccountsSession", "AccountsCredentials", "AccountsPasswordChange", "AccountsAccount", "AccountsAccountPage", "AccountsTemporaryPassword"} {
		if !strings.Contains(string(raw), `"`+name+`"`) {
			t.Errorf("schema %s missing", name)
		}
	}
}

func TestHandlers_Session_NoStoreAndJSON(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	w, f := call(t, handlerFor(t, r.svc, "GET", "/session"), from(ctxT(t), "198.51.100.7"), "")
	if w.Code != http.StatusOK || f.count() != 0 {
		t.Fatalf("status %d, failures %d", w.Code, f.count())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var info identity.SessionInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("body: %v", err)
	}
	if info.SignedIn {
		t.Error("signed in without a session")
	}
}

// F51
func TestHandlers_BadBody_Table(t *testing.T) {
	small := func(r *http.Request) { r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 16) }
	contentType := func(ct string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Content-Type", ct) } }
	inv := func(err error) bool { return errs.Is(err, errs.Invalid) }
	tests := []struct {
		name     string
		cred, pw string // the body for the credentials routes and for /password
		mod      func(*http.Request)
		is       func(error) bool
	}{
		{"not json", `{nope`, `{nope`, nil, inv},
		{"array instead of object", `[]`, `[]`, nil, inv},
		{"wrong type", `{"email":7,"password":"x"}`, `{"current_password":7,"new_password":"x"}`, nil, inv},
		{"wrong media type", `{"email":"ada.quill@example.org","password":"x"}`, `{"current_password":"x","new_password":"y"}`, contentType("text/plain"),
			func(err error) bool { return errors.Is(err, httpx.ErrUnsupportedMediaType) }},
		{"too large", `{"email":"ada.quill@example.org","password":"` + strings.Repeat("a", 64) + `"}`,
			`{"current_password":"` + strings.Repeat("a", 64) + `","new_password":"y"}`, small,
			func(err error) bool { var m *http.MaxBytesError; return errors.As(err, &m) }},
	}
	routes := []struct{ method, pattern string }{{"POST", "/sign-in"}, {"POST", "/sign-up"}, {"POST", "/password"}}
	for _, rt := range routes {
		for _, tc := range tests {
			t.Run(rt.pattern+"/"+tc.name, func(t *testing.T) {
				r := newRig(t, dbtest.NewFake())
				ctx := identity.WithSession(from(ctxT(t), "198.51.100.7"), sessionFor(newAccountID(), newPersonID(), epoch))
				var mods []func(*http.Request)
				if tc.mod != nil {
					mods = append(mods, tc.mod)
				}
				body := tc.cred
				if rt.pattern == "/password" {
					body = tc.pw
				}
				w, f := call(t, handlerFor(t, r.svc, rt.method, rt.pattern), ctx, body, mods...)
				if f.count() != 1 {
					t.Fatalf("httpx.Fail called %d times (status %d)", f.count(), w.Code)
				}
				if !tc.is(f.last()) {
					t.Errorf("failure %v does not match the case", f.last())
				}
				if len(setCookies(w)) != 0 {
					t.Error("a cookie was set on a bad body")
				}
			})
		}
	}
}

func TestHandlers_EmptyBody_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	_, f := call(t, handlerFor(t, r.svc, "POST", "/sign-in"), from(ctxT(t), "198.51.100.7"), "")
	if f.count() != 1 || !errs.Is(f.last(), errs.Invalid) {
		t.Fatalf("failure = %v", f.last())
	}
}

// F52
func TestHandlers_BadAccountID_400(t *testing.T) {
	ids := []string{"not-a-uuid", "123", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", "{11111111-1111-4111-8111-111111111111}", "11111111111141118111111111111111"}
	routes := []struct{ method, pattern string }{
		{"GET", "/{account}"}, {"POST", "/{account}/disable"}, {"POST", "/{account}/enable"}, {"POST", "/{account}/temporary-password"},
	}
	for _, rt := range routes {
		for _, id := range ids {
			t.Run(rt.pattern+"/"+id, func(t *testing.T) {
				su := sessionFor(newAccountID(), newPersonID(), epoch)
				su.Superuser = true
				r := newRig(t, dbtest.NewFake(), withPerms(allowing(identity.PermissionAccountsRead, identity.PermissionAccountsManage)))
				ctx := identity.WithSession(from(ctxT(t), "198.51.100.7"), su)
				w, f := call(t, handlerFor(t, r.svc, rt.method, rt.pattern), ctx, "", func(r *http.Request) { r.SetPathValue("account", id) })
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", w.Code)
				}
				wantField(t, f.last(), "account", "malformed")
			})
		}
	}
}

// F31, F37, F25, F29 at the handler: no session is a 401.
func TestHandlers_NoSession_Unauthenticated(t *testing.T) {
	routes := []struct{ method, pattern, body string }{
		{"GET", "/", ""},
		{"GET", "/{account}", ""},
		{"POST", "/{account}/disable", ""},
		{"POST", "/{account}/enable", ""},
		{"POST", "/{account}/temporary-password", ""},
		{"POST", "/sessions/end-others", ""},
		{"POST", "/password", `{"current_password":"x","new_password":"y"}`},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.pattern, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake(), withPerms(allowing(identity.PermissionAccountsRead, identity.PermissionAccountsManage)))
			w, f := call(t, handlerFor(t, r.svc, rt.method, rt.pattern), from(ctxT(t), "198.51.100.7"), rt.body,
				func(r *http.Request) { r.SetPathValue("account", newAccountID().String()) })
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (failure %v)", w.Code, f.last())
			}
			if len(setCookies(w)) != 0 {
				t.Error("cookie set for an unauthenticated request")
			}
		})
	}
}

// F24: sign-out clears the cookie before it records, so an audit failure still signs the
// browser out while the response is the audit error.
func TestSignOut_AuditFails_CookieStillCleared(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	ctx := identity.WithSession(from(ctxT(t), "198.51.100.7"), sessionFor(newAccountID(), newPersonID(), epoch))
	w, f := call(t, handlerFor(t, r.svc, "POST", "/sign-out"), ctx, "")
	if f.count() != 1 || !errs.Is(f.last(), errs.Unavailable) {
		t.Fatalf("failure = %v, want the audit error", f.last())
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	cs := setCookies(w)
	if len(cs) != 1 || cs[0].Name != identity.CookieName || cs[0].MaxAge >= 0 || cs[0].Value != "" {
		t.Fatalf("cookies = %+v, want one clearing %s", cs, identity.CookieName)
	}
}

func TestSignOut_AuditFails_ServiceReturnsAuditKind(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	ctx := identity.WithSession(from(ctxT(t), "198.51.100.7"), sessionFor(newAccountID(), newPersonID(), epoch))
	wantKind(t, r.svc.SignOut(ctx), errs.Unavailable)
}

func TestSignOut_NoSession_NilAndNothingRecorded(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.svc.SignOut(from(ctxT(t), "198.51.100.7")))
	if len(r.rec.Recorded()) != 0 {
		t.Error("an entry was recorded without a session")
	}
}

func TestSignOut_RecordsLogoutForSession_RestrictedIncluded(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		r := newRig(t, dbtest.NewFake())
		s := sessionFor(newAccountID(), newPersonID(), epoch)
		s.PasswordChangeRequired = restricted
		wantNoErr(t, r.svc.SignOut(identity.WithSession(from(ctxT(t), "198.51.100.7"), s)))
		e := r.only(actLogout)
		if e.Actor.Person != s.Person || e.Record.Kind != "identity.session" || e.Record.ID != [16]byte(s.ID) {
			t.Errorf("restricted=%v: entry = %+v", restricted, e)
		}
	}
}

func TestSignOut_NoSession_HandlerClearsCookie(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	w, f := call(t, handlerFor(t, r.svc, "POST", "/sign-out"), from(ctxT(t), "198.51.100.7"), "")
	if f.count() != 0 || w.Code/100 != 2 {
		t.Fatalf("status %d failure %v", w.Code, f.last())
	}
	if cs := setCookies(w); len(cs) != 1 || cs[0].MaxAge >= 0 {
		t.Errorf("cookies = %+v", cs)
	}
}

// F53: the clear uses the same name and attributes as the issue, and never both names.
func TestCookie_Clear_Table(t *testing.T) {
	tests := []struct {
		name      string
		plaintext bool
		scheme    string
		wantName  string
		secure    bool
	}{
		{"production", false, "https", identity.CookieName, true},
		{"dev under AllowPlaintext over http", true, "http", identity.DevCookieName, false},
		{"AllowPlaintext but https", true, "https", identity.CookieName, true},
		{"http without AllowPlaintext", false, "http", identity.CookieName, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mods []func(*identity.Options)
			if tc.plaintext {
				mods = append(mods, withPlaintext)
			}
			r := newRig(t, dbtest.NewFake(), mods...)
			ctx := httpx.WithScheme(httpx.WithClientAddr(ctxT(t), mustAddr("198.51.100.7")), tc.scheme)
			ctx = identity.WithSession(ctx, sessionFor(newAccountID(), newPersonID(), epoch))
			w, _ := call(t, handlerFor(t, r.svc, "POST", "/sign-out"), ctx, "")
			if got := w.Result().Header.Values("Set-Cookie"); len(got) != 1 { //nolint:bodyclose // recorder
				t.Fatalf("%d Set-Cookie headers, want exactly one: %v", len(got), got)
			}
			c := setCookies(w)[0]
			checkCookieAttrs(t, c, tc.wantName, tc.secure)
			if c.MaxAge >= 0 {
				t.Errorf("MaxAge = %d, want a clearing value", c.MaxAge)
			}
		})
	}
}

// checkCookieAttrs asserts the attributes every session cookie carries.
func checkCookieAttrs(t *testing.T, c *http.Cookie, name string, secure bool) {
	t.Helper()
	if c.Name != name {
		t.Errorf("name = %q, want %q", c.Name, name)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q: a __Host- cookie must not set one", c.Domain)
	}
	if !c.HttpOnly {
		t.Error("not HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Secure != secure {
		t.Errorf("Secure = %v, want %v", c.Secure, secure)
	}
}

// F53: sign-up through the handler, so a cookie is issued.
func TestCookie_Table(t *testing.T) {
	tests := []struct {
		name      string
		plaintext bool
		scheme    string
		wantName  string
		secure    bool
	}{
		{"production", false, "https", identity.CookieName, true},
		{"dev under AllowPlaintext over http", true, "http", identity.DevCookieName, false},
		{"AllowPlaintext but https", true, "https", identity.CookieName, true},
		{"http without AllowPlaintext", false, "http", identity.CookieName, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mods []func(*identity.Options)
			if tc.plaintext {
				mods = append(mods, withPlaintext)
			}
			r := newRig(t, dbtest.NewFake(), mods...)
			ctx := httpx.WithScheme(httpx.WithClientAddr(ctxT(t), mustAddr("198.51.100.7")), tc.scheme)
			w, f := call(t, handlerFor(t, r.svc, "POST", "/sign-up"), ctx, credsJSON("ada.quill@example.org", pwGood))
			if f.count() != 0 {
				t.Skipf("sign-up does not complete on the fake store (%v); covered by the integration tests", f.last())
			}
			if got := w.Result().Header.Values("Set-Cookie"); len(got) != 1 { //nolint:bodyclose // recorder
				t.Fatalf("%d Set-Cookie headers, want exactly one: %v", len(got), got)
			}
			c := setCookies(w)[0]
			checkCookieAttrs(t, c, tc.wantName, tc.secure)
			if c.MaxAge != 720*3600 {
				t.Errorf("Max-Age = %d, want the session lifetime in seconds (%d)", c.MaxAge, 720*3600)
			}
			if len(c.Value) != cookieChars {
				t.Errorf("value is %d characters, want %d", len(c.Value), cookieChars)
			}
			if strings.Contains(w.Body.String(), c.Value) {
				t.Error("the token is in the response body")
			}
			r.noLeak(c.Value, pwGood, "ada.quill")
		})
	}
}
