//go:build integration

package app_test

// The demo command and a serving demo instance end to end, through app.Main against a
// throw-away PostgreSQL database. plan/design/demo.md (D53) Test plan, app.md F38 to F40.
// Every person, address and password is invented (.invalid addresses).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/app"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

const (
	demoPassword = "burn-demo-password" //nolint:gosec // G101: the demo account password is public by design (demo.md)
	testerPW     = "tangerine-orbit-quiet-77"
	testerNewPW  = "marble-sunrise-kettle-58"
)

var demoKeys = []string{"participant", "newcomer", "lead", "coordinator", "steward", "event-admin", "settings-admin", "account-admin", "access-admin", "audit-reviewer"}

type demoFx struct {
	t    *testing.T
	env  []string
	pool *db.Pool
	name string
}

// newDemoFx is a throw-away database, migrated unless unmigrated is set, and an environment
// with demo mode on (and plaintext HTTP on loopback) unless demoOff is set.
func newDemoFx(t *testing.T, migrated, demoOn bool, extra ...string) *demoFx {
	t.Helper()
	resetProbe(t)
	pool := dbtest.TempDB(t)
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("BMS_TEST_DB_DSN"))
	if err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	u.Path = "/" + name
	env := baseEnv(t, u.String(), "BMS_HTTP_ALLOW_PLAINTEXT=true")
	if demoOn {
		env = append(env, "BMS_DEMO_ENABLED=true")
	}
	env = append(env, extra...)
	fx := &demoFx{t: t, env: env, pool: pool, name: name}
	if migrated {
		wantCode(t, run(t.Context(), t, env, "migrate"), app.ExitOK)
	}
	return fx
}

func (f *demoFx) run(args ...string) result {
	f.t.Helper()
	return run(f.t.Context(), f.t, f.env, args...)
}

func (f *demoFx) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(f.t.Context(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (f *demoFx) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.t.Context(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *demoFx) dataDir() string { return envValue(f.env, "BMS_DATA_DIR") }

func (f *demoFx) accounts() int { return f.count("SELECT count(*) FROM core.account") }
func (f *demoFx) markers() int  { return f.count("SELECT count(*) FROM core.demo_state") }

// ---- exit statuses (F38, F39) ------------------------------------------------------------------

func TestDemoCommand_DemoOff_SeedAndReset_Exit2_NothingChanged(t *testing.T) {
	f := newDemoFx(t, true, false)
	for _, sub := range []string{"seed", "reset"} {
		r := f.run("demo", sub)
		wantCode(t, r, app.ExitUsage)
		if !strings.Contains(r.errOut, "BMS_DEMO_ENABLED") {
			t.Errorf("demo %s: stderr does not say how to turn demo mode on:\n%s", sub, r.errOut)
		}
		noMarker(t, r)
	}
	if f.accounts() != 0 || f.markers() != 0 {
		t.Errorf("demo off changed the database: %d accounts, %d markers", f.accounts(), f.markers())
	}
}

func TestDemoSeed_BuildsTenAccountsAndPrintsThem(t *testing.T) {
	f := newDemoFx(t, true, true)
	r := f.run("demo", "seed")
	wantCode(t, r, app.ExitOK)
	for _, k := range demoKeys {
		if !strings.Contains(r.out, k+"@demo.invalid") {
			t.Errorf("stdout lacks %s@demo.invalid:\n%s", k, r.out)
		}
	}
	if !strings.Contains(r.out, demoPassword) {
		t.Error("stdout lacks the shared password")
	}
	if f.count("SELECT count(*) FROM core.demo_account") != 10 {
		t.Error("want 10 demo accounts")
	}
	if f.count("SELECT count(*) FROM core.account WHERE superuser") != 0 {
		t.Error("the seed created a superuser")
	}
	if f.count("SELECT count(*) FROM core.account WHERE email !~ '@demo\\.invalid$'") != 0 {
		t.Error("an account outside demo.invalid exists")
	}
	if f.count("SELECT count(*) FROM core.demo_state WHERE seeded_at IS NOT NULL") != 1 {
		t.Error("the marker is not in the seeded state")
	}
	if f.count("SELECT count(*) FROM core.department") < 6 || f.count("SELECT count(*) FROM core.event") < 1 {
		t.Error("departments or the event are missing")
	}
	if f.count("SELECT count(*) FROM core.person") < 22 {
		t.Errorf("%d people, want at least 22 (ten accounts and twelve walk-ons)", f.count("SELECT count(*) FROM core.person"))
	}
	// The starter roles of the spec's table.
	for role, want := range map[string]int{"access-admin": 1, "department-steward": 1, "event-admin": 1, "settings-admin": 1, "account-admin": 1,
		"audit-reviewer": 1, "volunteer-coordinator": 1, "department-admin": 5} { // D1: the lead is department-admin at Rangers, Gate, Greeters, Medical and DPW
		got := f.count("SELECT count(*) FROM core.role_grant g JOIN core.role r ON r.id = g.role_id WHERE r.key = $1", role)
		if got != want {
			t.Errorf("%d grants of %s, want %d", got, role, want)
		}
	}
	// Every address and phone in the seeded people is fictional.
	phone := regexp.MustCompile(`^\+1 555 01\d\d$`)
	rows, err := f.pool.Query(t.Context(), "SELECT phone, coalesce(contact_email, '') FROM core.person")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ph, ce string
		if err := rows.Scan(&ph, &ce); err != nil {
			t.Fatal(err)
		}
		if ph != "" && !phone.MatchString(ph) {
			t.Errorf("a seeded person holds the number %q", ph)
		}
		if ce != "" && !strings.HasSuffix(ce, "@demo.invalid") {
			t.Errorf("a seeded person holds the contact address %q", ce)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestDemoSeed_Again_Exit2_NothingChanged(t *testing.T) {
	f := newDemoFx(t, true, true)
	wantCode(t, f.run("demo", "seed"), app.ExitOK)
	accounts, people := f.accounts(), f.count("SELECT count(*) FROM core.person")
	r := f.run("demo", "seed")
	wantCode(t, r, app.ExitUsage)
	if !strings.Contains(r.errOut, "reset") {
		t.Errorf("stderr does not suggest `bms demo reset`:\n%s", r.errOut)
	}
	if f.accounts() != accounts || f.count("SELECT count(*) FROM core.person") != people || f.markers() != 1 {
		t.Error("a refused second seed changed the database")
	}
}

func TestDemoSeed_NonEmptyDatabase_Exit2_NoMarker(t *testing.T) {
	f := newDemoFx(t, true, false)
	s := startServe(t, f.env)
	st, _ := testerRequest(t, nil, s.base, "POST", "/api/accounts/sign-up", `{"email":"tester.one@demo.invalid","password":"`+testerPW+`"}`)
	if st != 201 {
		t.Fatalf("sign-up = %d", st)
	}
	s.stop(t)

	env := append(append([]string(nil), f.env...), "BMS_DEMO_ENABLED=true")
	r := run(t.Context(), t, env, "demo", "seed")
	wantCode(t, r, app.ExitUsage)
	if f.markers() != 0 {
		t.Error("a refused seed left a marker")
	}
	if f.accounts() != 1 {
		t.Errorf("%d accounts, want only the tester's", f.accounts())
	}
}

func TestDemoReset_NoMarker_Exit2_NothingChanged(t *testing.T) {
	f := newDemoFx(t, true, false)
	s := startServe(t, f.env)
	testerRequest(t, nil, s.base, "POST", "/api/accounts/sign-up", `{"email":"tester.one@demo.invalid","password":"`+testerPW+`"}`)
	s.stop(t)
	spool := filepath.Join(f.dataDir(), "audit", "spool")
	if err := os.MkdirAll(spool, 0o750); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spool, "x.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(append([]string(nil), f.env...), "BMS_DEMO_ENABLED=true")
	r := run(t.Context(), t, env, "demo", "reset")
	wantCode(t, r, app.ExitUsage)
	if f.accounts() != 1 {
		t.Errorf("a reset without the marker changed the accounts (%d)", f.accounts())
	}
	if _, err := os.Stat(filepath.Join(spool, "x.json")); err != nil {
		t.Error("the spool was cleared by a refused reset")
	}
}

func TestDemoCommand_SchemaBehind_Exit3(t *testing.T) {
	f := newDemoFx(t, false, true) // not migrated
	for _, sub := range []string{"seed", "reset"} {
		r := f.run("demo", sub)
		if r.code != app.ExitSchema && r.code != app.ExitUsage {
			t.Errorf("demo %s on an unmigrated database = %d, want %d (schema) \n%s", sub, r.code, app.ExitSchema, r.errOut)
		}
		if sub == "seed" {
			wantCode(t, r, app.ExitSchema)
		}
	}
	if _, err := f.pool.Exec(t.Context(), "SELECT 1 FROM core.demo_state"); err == nil {
		t.Error("the seed created tables on an unmigrated database")
	}
}

func TestDemoSeed_ReadOnlyCopy_Exit1(t *testing.T) {
	f := newDemoFx(t, true, true)
	f.exec(`ALTER DATABASE "` + f.name + `" SET default_transaction_read_only = on`)
	r := f.run("demo", "seed")
	wantCode(t, r, app.ExitFailure)
	if f.accounts() != 0 {
		t.Error("accounts written on a read-only copy")
	}
}

func TestDemoReset_Success_SameCountsEverythingStrangersAddedGone(t *testing.T) {
	f := newDemoFx(t, true, true)
	wantCode(t, f.run("demo", "seed"), app.ExitOK)
	counts := func() [4]int {
		return [4]int{f.accounts(), f.count("SELECT count(*) FROM core.person"), f.count("SELECT count(*) FROM core.department"), f.count("SELECT count(*) FROM core.event")}
	}
	first := counts()

	s := startServe(t, f.env)
	jar := newJar()
	st, _ := testerRequest(t, jar, s.base, "POST", "/api/accounts/sign-up", `{"email":"stranger@demo.invalid","password":"`+testerPW+`"}`)
	if st != 201 {
		t.Fatalf("sign-up = %d", st)
	}
	if st, _ := testerRequest(t, jar, s.base, "GET", "/api/accounts/session", ""); st != 200 {
		t.Fatalf("session = %d", st)
	}
	s.stop(t)
	f.exec("INSERT INTO core.demo_mail (id, to_address, subject, body, message_id, captured_at) VALUES (gen_random_uuid(), 'stranger@demo.invalid', 's', 'b', 'm@x', now())")
	spool := filepath.Join(f.dataDir(), "audit", "spool")
	if err := os.MkdirAll(spool, 0o750); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spool, "x.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if counts() == first {
		t.Fatal("setup: the stranger's account did not change the counts")
	}

	r := f.run("demo", "reset")
	wantCode(t, r, app.ExitOK)
	if got := counts(); got != first {
		t.Errorf("counts after reset = %v, want %v", got, first)
	}
	if f.count("SELECT count(*) FROM core.demo_mail") != 0 {
		t.Error("captured mail survived the reset")
	}
	es, rerr := os.ReadDir(spool)
	if rerr != nil {
		t.Fatalf("read spool: %v", rerr)
	}
	if len(es) != 0 {
		t.Errorf("%d spool files survived the reset", len(es))
	}
	if f.count("SELECT count(*) FROM core.account WHERE email = 'stranger@demo.invalid'") != 0 {
		t.Error("the stranger's account survived")
	}

	// Every session is over: the old cookie names an unknown account.
	s2 := startServe(t, f.env)
	defer s2.stop(t)
	if st, body := testerRequest(t, jar, s2.base, "GET", "/api/accounts/session", ""); st != 200 || !strings.Contains(body, `"signed_in":false`) {
		t.Errorf("old cookie after reset: %d %s, want signed out", st, body)
	}
}

func TestDemoReset_DemoOff_Exit2_NothingChanged(t *testing.T) {
	f := newDemoFx(t, true, true)
	wantCode(t, f.run("demo", "seed"), app.ExitOK)
	accounts := f.accounts()
	env := envWithout(f.env, "BMS_DEMO_ENABLED")
	r := run(t.Context(), t, env, "demo", "reset")
	wantCode(t, r, app.ExitUsage)
	if f.accounts() != accounts || f.markers() != 1 {
		t.Error("a reset with demo off changed the database")
	}
}

// ---- a serving demo instance -----------------------------------------------------------------

func newJar() http.CookieJar {
	j, jerr := cookiejar.New(nil)
	if jerr != nil {
		panic(jerr) // cookiejar.New(nil) cannot fail
	}
	return j
}

// testerRequest sends one JSON request and returns the status and body. A nil jar sends no cookies.
func testerRequest(t *testing.T, jar http.CookieJar, base, method, path, body string) (int, string) {
	t.Helper()
	st, _, b := testerRequestH(t, jar, base, method, path, body)
	return st, b
}

func testerRequestH(t *testing.T, jar http.CookieJar, base, method, path, body string) (int, http.Header, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", base)
	}
	cl := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, string(b)
}

func signIn(t *testing.T, base, email, pw string) http.CookieJar {
	t.Helper()
	jar := newJar()
	st, body := testerRequest(t, jar, base, "POST", "/api/accounts/sign-in", `{"email":"`+email+`","password":"`+pw+`"}`)
	if st != 204 {
		t.Fatalf("sign-in %s = %d %s", email, st, body)
	}
	return jar
}

type servingDemo struct {
	*demoFx
	s *server
}

func startDemo(t *testing.T, demoOn bool) *servingDemo {
	t.Helper()
	f := newDemoFx(t, true, demoOn, "BMS_DEMO_PUBLIC_URL=")
	if demoOn {
		wantCode(t, f.run("demo", "seed"), app.ExitOK)
	}
	return &servingDemo{demoFx: f, s: startServe(t, f.env)}
}

func TestDemoInfo_PublicRoute_ListsTheAccountsAndThePassword(t *testing.T) {
	d := startDemo(t, true)
	st, body := testerRequest(t, nil, d.s.base, "GET", "/api/demo/", "") // no session needed
	if st != 200 {
		t.Fatalf("GET /api/demo/ = %d %s", st, body)
	}
	var info struct {
		Enabled  bool   `json:"enabled"`
		Regional string `json:"regional"`
		Password string `json:"password"`
		Accounts []struct {
			Key   string   `json:"key"`
			Email string   `json:"email"`
			Roles []string `json:"roles"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Enabled || info.Regional != "Ember Flats" || info.Password != demoPassword || len(info.Accounts) != 10 {
		t.Fatalf("info = %+v", info)
	}
	for i, k := range demoKeys {
		if info.Accounts[i].Key != k || info.Accounts[i].Email != k+"@demo.invalid" {
			t.Errorf("account %d = %+v", i, info.Accounts[i])
		}
	}
}

func TestDemoInfo_DemoOff_NotMounted_NoMailRoute(t *testing.T) {
	d := startDemo(t, false)
	st, body := testerRequest(t, nil, d.s.base, "GET", "/api/demo/", "")
	if st == 200 && strings.Contains(body, `"enabled":true`) {
		t.Fatalf("demo off but GET /api/demo/ says enabled: %s", body)
	}
	if st == 200 && strings.Contains(body, demoPassword) {
		t.Error("demo off but the shared password is served")
	}
	if st, _ := testerRequest(t, nil, d.s.base, "GET", "/api/demo/mail", ""); st != 404 && st != 501 && st != 401 {
		t.Errorf("GET /api/demo/mail with demo off = %d, want 404 or 501 (not a demo)", st)
	}
	// The sign-in with the demo password finds no such account.
	st, _ = testerRequest(t, newJar(), d.s.base, "POST", "/api/accounts/sign-in", `{"email":"participant@demo.invalid","password":"`+demoPassword+`"}`)
	if st != 401 {
		t.Errorf("demo-account sign-in with demo off = %d, want 401", st)
	}
}

func TestDemoMail_SignedOut_401_BadLimit_400(t *testing.T) {
	d := startDemo(t, true)
	if st, _ := testerRequest(t, nil, d.s.base, "GET", "/api/demo/mail", ""); st != 401 {
		t.Errorf("signed-out GET /api/demo/mail = %d, want 401", st)
	}
	jar := signIn(t, d.s.base, "participant@demo.invalid", demoPassword)
	for _, q := range []string{"?limit=0x", "?limit=-1", "?limit=51", "?limit=abc"} {
		st, _ := testerRequest(t, jar, d.s.base, "GET", "/api/demo/mail"+q, "")
		if st != 400 {
			t.Errorf("GET /api/demo/mail%s = %d, want 400", q, st)
		}
	}
	if st, body := testerRequest(t, jar, d.s.base, "GET", "/api/demo/mail?limit=50", ""); st != 200 {
		t.Errorf("limit=50 = %d %s", st, body)
	}
}

// Protected accounts: the shared accounts cannot be locked for everyone.
func TestDemoGuard_ProtectedAccounts_RefuseChangesAndSendNoMail(t *testing.T) {
	d := startDemo(t, true)
	jar := signIn(t, d.s.base, "participant@demo.invalid", demoPassword)

	st, body := testerRequest(t, jar, d.s.base, "POST", "/api/accounts/password", `{"current_password":"`+demoPassword+`","new_password":"`+testerNewPW+`"}`)
	if st != 403 || !strings.Contains(body, "protected") {
		t.Errorf("ChangePassword on a demo account = %d %s, want 403 protected", st, body)
	}
	// The password still works.
	signIn(t, d.s.base, "participant@demo.invalid", demoPassword)

	// Password reset answers as for any address and sends nothing.
	before := d.count("SELECT count(*) FROM core.demo_mail")
	st, _ = testerRequest(t, nil, d.s.base, "POST", "/api/accounts/password-reset", `{"email":"participant@demo.invalid"}`)
	if st != 202 {
		t.Errorf("password-reset for a demo account = %d, want 202 (as for any address)", st)
	}
	// Nothing was queued at all, so no asynchronous delivery can follow.
	if d.count("SELECT count(*) FROM core.demo_mail") != before || d.count("SELECT count(*) FROM core.password_reset") != 0 ||
		d.count("SELECT count(*) FROM core.notify_message WHERE template = 'identity.password-reset'") != 0 {
		t.Error("a reset was issued, queued or captured for a protected account")
	}

	// An account admin may not disable or issue a temporary password for a demo account.
	admin := signIn(t, d.s.base, "account-admin@demo.invalid", demoPassword)
	var id string
	if err := d.pool.QueryRow(t.Context(), "SELECT id::text FROM core.account WHERE email = 'lead@demo.invalid'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/disable", "/temporary-password"} {
		st, body := testerRequest(t, admin, d.s.base, "POST", "/api/accounts/"+id+path, "")
		if st != 403 || !strings.Contains(body, "protected") {
			t.Errorf("POST %s on a demo account = %d %s, want 403 protected", path, st, body)
		}
	}
	if d.count("SELECT count(*) FROM core.account WHERE email = 'lead@demo.invalid' AND disabled_at IS NULL") != 1 {
		t.Error("a demo account was disabled")
	}

	// A self-signed-up tester is not protected.
	tj := newJar()
	if st, _ := testerRequest(t, tj, d.s.base, "POST", "/api/accounts/sign-up", `{"email":"tester.one@demo.invalid","password":"`+testerPW+`"}`); st != 201 {
		t.Fatalf("sign-up = %d", st)
	}
	st, body = testerRequest(t, tj, d.s.base, "POST", "/api/accounts/password", `{"current_password":"`+testerPW+`","new_password":"`+testerNewPW+`"}`)
	if st >= 300 {
		t.Errorf("a tester's own ChangePassword = %d %s, want success", st, body)
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	for !cond() {
		if err := clock.Wait(ctx, clock.Real(), 50*time.Millisecond); err != nil {
			return false
		}
	}
	return true
}

// Captured mail is shown only to the account it was sent to, and never leaves the process.
func TestDemoMail_CapturedForTheRecipientOnly(t *testing.T) {
	d := startDemo(t, true)
	one, two := newJar(), newJar()
	for jar, email := range map[http.CookieJar]string{one: "tester.one@demo.invalid", two: "tester.two@demo.invalid"} {
		if st, _ := testerRequest(t, jar, d.s.base, "POST", "/api/accounts/sign-up", `{"email":"`+email+`","password":"`+testerPW+`"}`); st != 201 {
			t.Fatalf("sign-up %s = %d", email, st)
		}
	}
	// A forgotten-password request from a signed-out client: notify sends through the capture channel.
	if st, _ := testerRequest(t, nil, d.s.base, "POST", "/api/accounts/password-reset", `{"email":"tester.one@demo.invalid"}`); st != 202 {
		t.Fatalf("password-reset = %d, want 202 (the seed makes e-mail configured)", st)
	}
	if !waitFor(t, 30*time.Second, func() bool {
		return d.count("SELECT count(*) FROM core.demo_mail WHERE lower(to_address) = 'tester.one@demo.invalid'") == 1
	}) {
		t.Fatal("the reset mail was not captured")
	}

	mailTo := func(jar http.CookieJar, path string) []string {
		st, body := testerRequest(t, jar, d.s.base, "GET", path, "")
		if st != 200 {
			t.Fatalf("GET %s = %d %s", path, st, body)
		}
		var doc struct {
			Mail []struct {
				To string `json:"to"`
			} `json:"mail"`
		}
		var list []struct {
			To string `json:"to"`
		}
		var out []string
		if json.Unmarshal([]byte(body), &list) == nil {
			for _, m := range list {
				out = append(out, m.To)
			}
		} else if json.Unmarshal([]byte(body), &doc) == nil {
			for _, m := range doc.Mail {
				out = append(out, m.To)
			}
		} else {
			t.Fatalf("unreadable mail list: %s", body)
		}
		return out
	}
	if got := mailTo(one, "/api/demo/mail"); len(got) != 1 || got[0] != "tester.one@demo.invalid" {
		t.Errorf("recipient sees %v, want exactly their mail", got)
	}
	// A viewer sees only mail addressed to their own login address. The seed's sign-ups send
	// the participant their own confirmations, so a demo account may see some, but never
	// another address's.
	for _, v := range []struct {
		name string
		jar  http.CookieJar
		own  string
	}{
		{"another tester", two, "tester.two@demo.invalid"},
		{"a demo account", signIn(t, d.s.base, "participant@demo.invalid", demoPassword), "participant@demo.invalid"},
		{"the account-admin", signIn(t, d.s.base, "account-admin@demo.invalid", demoPassword), "account-admin@demo.invalid"},
	} {
		for _, to := range mailTo(v.jar, "/api/demo/mail") {
			if !strings.EqualFold(to, v.own) {
				t.Errorf("%s sees a message sent to %s, which is not their address", v.name, to)
			}
		}
	}
	// No query parameter widens the list.
	for _, q := range []string{"?to=tester.one@demo.invalid", "?all=true", "?address=tester.one@demo.invalid", "?email=tester.one@demo.invalid"} {
		if got := mailTo(two, "/api/demo/mail"+q); len(got) != 0 {
			t.Errorf("GET /api/demo/mail%s shows %d messages of someone else", q, len(got))
		}
	}
	// The capture is not audited (no demo.mail.read exists).
	if n := d.count("SELECT count(*) FROM core.audit_entry WHERE action LIKE 'demo.mail%'"); n != 0 {
		t.Errorf("%d audit entries for reading captured mail, want none", n)
	}
}

// X-Robots-Tag: noindex only in demo mode, only on UI responses.
func TestDemo_NoIndexHeader_OnlyInDemoMode(t *testing.T) {
	for _, on := range []bool{true, false} {
		d := startDemo(t, on)
		for _, p := range []string{"/", "/people", "/accounts/sign-in", "/robots.txt"} {
			st, h, _ := testerRequestH(t, nil, d.s.base, "GET", p, "")
			got := h.Get("X-Robots-Tag")
			if st != 200 {
				continue // a test binary may carry no UI build
			}
			if on && got != "noindex, nofollow" {
				t.Errorf("demo on, GET %s: X-Robots-Tag = %q", p, got)
			}
			if !on && got != "" {
				t.Errorf("demo off, GET %s: X-Robots-Tag = %q, want none", p, got)
			}
		}
		for _, p := range []string{"/api/regional/public", "/healthz"} {
			_, h, _ := testerRequestH(t, nil, d.s.base, "GET", p, "")
			if h.Get("X-Robots-Tag") != "" {
				t.Errorf("GET %s carries X-Robots-Tag", p)
			}
		}
		d.s.stop(t)
	}
}

// F40 / TestDemo_ChangesOnlyTheListed: the assembly differs only by the demo route set.
func TestDemo_ChangesOnlyTheListed_OpenAPI(t *testing.T) {
	env := baseEnv(t, closedDBURL(t))
	off := run(t.Context(), t, env, "openapi")
	on := run(t.Context(), t, append(append([]string(nil), env...), "BMS_DEMO_ENABLED=true"), "openapi")
	wantCode(t, off, app.ExitOK)
	wantCode(t, on, app.ExitOK)
	var a, b map[string]any
	if err := json.Unmarshal([]byte(off.out), &a); err != nil {
		t.Fatalf("openapi (off) is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(on.out), &b); err != nil {
		t.Fatalf("openapi (on) is not JSON: %v", err)
	}
	pa, okA := a["paths"].(map[string]any)
	pb, okB := b["paths"].(map[string]any)
	if !okA || !okB {
		t.Fatalf("openapi documents lack paths (off %v, on %v)", okA, okB)
	}
	for p := range pa {
		if strings.HasPrefix(p, "/api/demo") {
			t.Errorf("demo off documents %s", p)
		}
		if _, ok := pb[p]; !ok {
			t.Errorf("demo on lost the path %s", p)
		}
	}
	var added []string
	for p := range pb {
		if _, ok := pa[p]; !ok {
			added = append(added, p)
			if !strings.HasPrefix(p, "/api/demo") {
				t.Errorf("demo on added %s, which is not in the demo route set", p)
			}
		}
	}
	if len(added) != 2 {
		t.Errorf("demo adds %v, want exactly GET /api/demo and GET /api/demo/mail", added)
	}
	// Everything except paths is identical (components, tags, info).
	delete(a, "paths")
	delete(b, "paths")
	delete(a, "tags")
	delete(b, "tags")
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		t.Fatalf("marshal openapi: %v, %v", errA, errB)
	}
	if string(ja) != string(jb) {
		t.Error("the OpenAPI documents differ outside the demo paths")
	}
}

func TestDemo_ChangesOnlyTheListed_Catalogues(t *testing.T) {
	catalogues := func(demoOn bool) map[string]string {
		f := newDemoFx(t, true, demoOn)
		out, err := f.runSuperuserDocs(demoOn)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	off, on := catalogues(false), catalogues(true)
	for k, v := range off {
		if on[k] != v {
			t.Errorf("%s differs between demo off and demo on", k)
		}
	}
}

// runSuperuserDocs serves the instance, signs in as a superuser (every permission) and
// returns the permission catalogue and the settings document without values that vary.
func (f *demoFx) runSuperuserDocs(_ bool) (map[string]string, error) {
	t := f.t
	cr := f.run("create-superuser", "root.person@example.org")
	m := regexp.MustCompile(`(?m)^Password \(shown once, not stored anywhere else\): (\S+)$`).FindStringSubmatch(cr.out)
	if cr.code != app.ExitOK || m == nil {
		t.Fatalf("create-superuser: %d %s %s", cr.code, cr.out, cr.errOut)
	}
	s := startServe(t, f.env)
	defer s.stop(t)
	jar := signIn(t, s.base, "root.person@example.org", m[1])
	out := map[string]string{}
	for _, p := range []string{"/api/access/permissions", "/api/access/roles"} {
		st, body := testerRequest(t, jar, s.base, "GET", p, "")
		if st != 200 {
			t.Fatalf("GET %s = %d", p, st)
		}
		out[p] = stripVarying(body)
	}
	st, body := testerRequest(t, jar, s.base, "GET", "/api/settings/", "")
	if st != 200 {
		t.Fatalf("GET /api/settings/ = %d", st)
	}
	// Only the catalogue (keys), not the values.
	out["settings"] = strings.Join(regexp.MustCompile(`"key":"([a-z_.0-9]+)"`).FindAllString(body, -1), ",")
	return out, nil
}

var varying = regexp.MustCompile(`"(id|created_at|updated_at)":"[^"]*"`)

func stripVarying(s string) string { return varying.ReplaceAllString(s, `"$1":""`) }

// bms check reads the demo marker through the real pool, not through the services check
// builds on a database that refuses every statement (which printed "state unknown:
// unavailable" on a seeded instance).
func TestDemoCheck_ReadsTheMarkerFromTheRealDatabase(t *testing.T) {
	line := regexp.MustCompile(`(?m)^demo: .*$`)
	demoLine := func(r result) string { return line.FindString(r.out) }

	t.Run("not seeded", func(t *testing.T) {
		f := newDemoFx(t, true, true)
		r := f.run("check")
		if got := demoLine(r); got != "demo: on (not seeded)" {
			t.Errorf("demo line = %q, want %q:\n%s", got, "demo: on (not seeded)", r.out)
		}
	})
	t.Run("seeded", func(t *testing.T) {
		f := newDemoFx(t, true, true)
		wantCode(t, f.run("demo", "seed"), app.ExitOK)
		r := f.run("check")
		got := demoLine(r)
		if !regexp.MustCompile(`^demo: on \(seeded \d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ\)$`).MatchString(got) {
			t.Errorf("demo line = %q, want demo: on (seeded <RFC3339>):\n%s", got, r.out)
		}
		if strings.Contains(r.out, "state unknown") {
			t.Errorf("check could not read the marker:\n%s", r.out)
		}
	})
	t.Run("demo off", func(t *testing.T) {
		f := newDemoFx(t, true, false)
		if got := demoLine(f.run("check")); got != "demo: off" {
			t.Errorf("demo line = %q, want demo: off", got)
		}
	})
}
