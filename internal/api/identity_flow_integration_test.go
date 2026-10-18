//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/lib/secret"
)

const (
	flowKey  = "api-flow-session-key-0123456789abcdef-0123456789"
	flowPass = "tangerine-orbit-quiet-77"
	flowNew  = "marble-sunrise-kettle-58"
)

type allowAll struct{}

func (allowAll) Allow(_ context.Context, _ authz.Permission) error { return nil }

// flowRig is the real api.Server with the real identity service on a throw-away database,
// reached over HTTP the way a browser would (plain http, so the dev cookie).
type flowRig struct {
	*rig
	svc *identity.Service
}

func newFlowRig(t *testing.T) *flowRig {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(t.Context(), []db.Set{identity.Set}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	r := newRig(t)
	bus, err := signalstest.NewFake("identity", []signals.Name{identity.SignalAccountCreated}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := identity.New(identity.Options{
		DB: pool, Clock: r.clk, Logger: r.cap.Logger(), Tracer: r.cap.Tracer("identity"), Meter: r.cap.Meter("identity"),
		Key: secret.New(flowKey), AllowPlaintext: true, Audit: audittest.NewFake("identity"), Signals: bus,
		Permissions: allowAll{},
	})
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	var routes []module.Route
	for _, rt := range svc.Routes() {
		access := module.AccessSignedIn
		if rt.Public {
			access = module.AccessPublic
		}
		routes = append(routes, module.Route{Method: rt.Method, Pattern: rt.Pattern, Access: access, Handler: rt.Handler})
	}
	other := route("GET", "/private", module.AccessSignedIn, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.opts.Routes = append(r.opts.Routes,
		module.ModuleRoutes{Module: "accounts", Routes: routes, OpenAPI: svc.OpenAPI()},
		setOf(t, "alpha", other))
	r.opts.Authenticator = svc
	r.opts.AllowPlaintext = true
	return &flowRig{rig: r, svc: svc}
}

// send does one same-origin request, optionally with a session cookie, and returns the
// response and the session cookie it set (nil when none).
func (f *flowRig) send(method, path, body string, cookie *http.Cookie) (*httptest.ResponseRecorder, *http.Cookie) {
	f.t.Helper()
	opts := []reqOpt{hdr("Origin", "http://bms.test")}
	if body != "" {
		opts = append(opts, jsonBody(body))
	}
	req := newRequest(f.t, method, path, opts...)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.server().Handler().ServeHTTP(rec, req)
	var set *http.Cookie
	for _, c := range rec.Result().Cookies() { //nolint:bodyclose // recorder
		if c.Name == identity.DevCookieName && c.MaxAge >= 0 && c.Value != "" {
			set = c
		}
	}
	return rec, set
}

func (f *flowRig) session(cookie *http.Cookie) identity.SessionInfo {
	f.t.Helper()
	rec, _ := f.send("GET", "/api/accounts/session", "", cookie)
	if rec.Code != 200 {
		f.t.Fatalf("GET /session = %d: %s", rec.Code, rec.Body.String())
	}
	var info identity.SessionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		f.t.Fatalf("session body: %v", err)
	}
	return info
}

func jsonOf(t *testing.T, m map[string]string) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func creds(t *testing.T, email, pw string) string {
	t.Helper()
	return jsonOf(t, map[string]string{"email": email, "password": pw})
}

// The temporary-password flow end to end through the real shell: the shell lets the
// restricted session reach identity's own routes and nothing else.
func TestFlow_TemporaryPassword_RestrictedThenNormal(t *testing.T) {
	f := newFlowRig(t)

	// two people sign up over HTTP
	rec, bossCookie := f.send("POST", "/api/accounts/sign-up", creds(t, "boss.lady@example.org", flowPass), nil)
	if rec.Code/100 != 2 || bossCookie == nil {
		t.Fatalf("boss sign-up = %d: %s", rec.Code, rec.Body.String())
	}
	rec, userCookie := f.send("POST", "/api/accounts/sign-up", creds(t, "sam.gray@example.org", flowPass), nil)
	if rec.Code/100 != 2 || userCookie == nil {
		t.Fatalf("user sign-up = %d: %s", rec.Code, rec.Body.String())
	}
	user := f.session(userCookie)
	if !user.SignedIn || user.PasswordChangeRequired {
		t.Fatalf("session = %+v", user)
	}
	if rec, _ := f.send("GET", "/api/alpha/private", "", userCookie); rec.Code != 204 {
		t.Fatalf("signed-in route = %d", rec.Code)
	}

	// the admin sets a temporary password
	rec, _ = f.send("POST", "/api/accounts/"+user.AccountID.String()+"/temporary-password", "", bossCookie)
	if rec.Code != 200 {
		t.Fatalf("temporary-password = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	temp := ""
	for _, v := range body {
		if s, ok := v.(string); ok && len(s) == identity.GeneratedPasswordLength {
			temp = s
		}
	}
	if temp == "" {
		t.Fatalf("no generated password in the response: %v", body)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Error("temporary-password response is cacheable")
	}
	// the user's old session ended
	if info := f.session(userCookie); info.SignedIn {
		t.Error("the old session survived the temporary password")
	}

	// sign in with it: a restricted session
	rec, restricted := f.send("POST", "/api/accounts/sign-in", creds(t, "sam.gray@example.org", temp), nil)
	if rec.Code/100 != 2 || restricted == nil {
		t.Fatalf("sign-in with the temporary password = %d: %s", rec.Code, rec.Body.String())
	}
	info := f.session(restricted)
	if !info.SignedIn || !info.PasswordChangeRequired {
		t.Fatalf("session = %+v, want signed in and restricted", info)
	}
	// ... which opens no other route
	if rec, _ := f.send("GET", "/api/alpha/private", "", restricted); rec.Code != 401 {
		t.Errorf("signed-in route for a restricted session = %d, want 401", rec.Code)
	}
	// ... nor the list of accounts, though identity's route set is the same
	if rec, _ := f.send("GET", "/api/accounts/", "", restricted); rec.Code != 401 {
		t.Errorf("accounts list for a restricted session = %d, want 401", rec.Code)
	}

	// change the password with the restricted session
	rec, normal := f.send("POST", "/api/accounts/password",
		jsonOf(t, map[string]string{"current_password": temp, "new_password": flowNew}), restricted)
	if rec.Code/100 != 2 || normal == nil {
		t.Fatalf("POST /password = %d: %s", rec.Code, rec.Body.String())
	}
	info = f.session(normal)
	if !info.SignedIn || info.PasswordChangeRequired || info.PersonID != user.PersonID {
		t.Fatalf("session after the change = %+v", info)
	}
	if rec, _ := f.send("GET", "/api/alpha/private", "", normal); rec.Code != 204 {
		t.Errorf("signed-in route after the change = %d, want 204", rec.Code)
	}
	// the restricted session ended with the change
	if info := f.session(restricted); info.SignedIn {
		t.Error("the restricted session survived the password change")
	}
}
