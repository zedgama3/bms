//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// Invented people for the identity and audit tests; nothing here is a real address.
const (
	itSuperEmail = "quill.admin@example.org"
	itMemberMail = "river.test@example.org"
	itMemberPass = "correct horse battery staple 7413"
)

// itPool is itDB with the pool and the database name, for tests that read tables or freeze
// the database.
func itPool(t *testing.T) (u *url.URL, pool *db.Pool, name string) {
	t.Helper()
	pool = dbtest.TempDB(t)
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	var err error
	if u, err = url.Parse(os.Getenv("BMS_TEST_DB_DSN")); err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	u.Path = "/" + name
	return u, pool, name
}

var passwordLineRE = regexp.MustCompile(`(?m)^Password \(shown once, not stored anywhere else\): (\S+)$`)

// itSend sends one request with the given cookie and JSON body and returns the status, the
// body and the session cookie the response set, if any.
func itSend(t *testing.T, addr, method, path, cookie, body string) (status int, resBody string, set *http.Cookie) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, "http://"+addr+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://"+addr)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
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
	for _, c := range resp.Cookies() {
		if strings.Contains(c.Name, "bms_session") && c.Value != "" {
			set = c
		}
	}
	return resp.StatusCode, string(b), set
}

func credentials(email, password string) string {
	b, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestCreateSuperuser_Integration_NewThenResetPrintsPasswordOnceNeverTheAddress(t *testing.T) {
	u, _, _ := itPool(t)
	env, _ := itEnv(t, u.String())
	ctx := t.Context()

	if r := itMain(ctx, env, "create-superuser", itSuperEmail); r.code != ExitSchema {
		t.Fatalf("before migrate: status = %d, want ExitSchema\n%s", r.code, r.errOut)
	} else if strings.Contains(r.out+r.errOut, itSuperEmail) || !strings.Contains(r.errOut, "migrate") {
		t.Errorf("before migrate: stderr must say to run migrate and never echo the address:\n%s", r.errOut)
	}
	if r := itMain(ctx, env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}

	first := itMain(ctx, env, "create-superuser", itSuperEmail)
	if first.code != ExitOK {
		t.Fatalf("create: status = %d\n%s\n%s", first.code, first.out, first.errOut)
	}
	m := passwordLineRE.FindStringSubmatch(first.out)
	if m == nil || !strings.HasPrefix(first.out, "Superuser account created.\n") || !strings.HasSuffix(first.out, "Sign in at /sign-in and keep this password in your password manager.\n") {
		t.Fatalf("stdout = %q, want the three lines of identity.md \"The command\"", first.out)
	}
	second := itMain(ctx, env, "create-superuser", itSuperEmail)
	m2 := passwordLineRE.FindStringSubmatch(second.out)
	if second.code != ExitOK || m2 == nil || !strings.HasPrefix(second.out, "Superuser account reset; its sessions have ended.\n") {
		t.Fatalf("reset: status = %d, stdout %q", second.code, second.out)
	}
	if m[1] == m2[1] {
		t.Error("a reset must generate a new password")
	}
	for _, r := range []itResult{first, second} {
		if strings.Contains(r.out+r.errOut, itSuperEmail) {
			t.Errorf("the address was printed or logged:\n%s\n%s", r.out, r.errOut)
		}
		if strings.Contains(r.errOut, m[1]) || strings.Contains(r.errOut, m2[1]) {
			t.Errorf("a password reached stderr:\n%s", r.errOut)
		}
	}

	bad := itMain(ctx, env, "create-superuser", "not-an-address")
	if bad.code != ExitUsage || bad.out != "" {
		t.Errorf("an invalid address: status = %d, stdout %q; want ExitUsage and nothing printed", bad.code, bad.out)
	}
}

func TestCreateSuperuser_Integration_ReadOnlyCopy_Exit1(t *testing.T) {
	u, pool, name := itPool(t)
	env, _ := itEnv(t, u.String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}
	if _, err := pool.Exec(t.Context(), `ALTER DATABASE "`+name+`" SET default_transaction_read_only = on`); err != nil {
		t.Fatalf("freeze the database: %v", err)
	}
	r := itMain(t.Context(), env, "create-superuser", itSuperEmail)
	if r.code != ExitFailure || r.out != "" || !strings.Contains(r.errOut, "read-only") {
		t.Errorf("status = %d, stdout %q, stderr %q; want ExitFailure, no output, a read-only message", r.code, r.out, r.errOut)
	}
}

// The whole accounts story through the real assembly: accounts, the actor in hello, the
// audit table, and the spool of a read-only copy.
func TestServe_Integration_AccountsAuditAndHelloEndToEnd(t *testing.T) {
	u, pool, _ := itPool(t)
	env, addr := itEnv(t, u.String())
	env = append(env, "BMS_HTTP_ALLOW_PLAINTEXT=true")
	ctx := t.Context()
	if r := itMain(ctx, env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}
	create := itMain(ctx, env, "create-superuser", itSuperEmail)
	pw := passwordLineRE.FindStringSubmatch(create.out)
	if create.code != ExitOK || pw == nil {
		t.Fatalf("create-superuser: %d\n%s\n%s", create.code, create.out, create.errOut)
	}

	s := itServe(t, env, addr)

	// Anonymous: the session says so, hello is 401, audit is not public.
	if code, body := itGet(ctx, t, addr, "/api/accounts/session"); code != 200 || !strings.Contains(body, `"signed_in":false`) {
		t.Errorf("anonymous session = %d %s", code, body)
	}
	if code, _ := itGet(ctx, t, addr, "/api/hello/greeting"); code != 401 {
		t.Errorf("anonymous hello greeting = %d, want 401", code)
	}
	if code, _ := itGet(ctx, t, addr, "/api/audit/entries"); code != 401 {
		t.Errorf("anonymous audit search = %d, want 401", code)
	}

	// A participant signs up (201, signed in), is somebody, and is denied hello (M1: 403).
	status, body, member := itSend(t, addr, http.MethodPost, "/api/accounts/sign-up", "", credentials(itMemberMail, itMemberPass))
	if status != 201 || member == nil {
		t.Fatalf("sign-up = %d %s (cookie %v)", status, body, member)
	}
	cookie := member.Name + "=" + member.Value
	status, body, _ = itSend(t, addr, http.MethodGet, "/api/accounts/session", cookie, "")
	if status != 200 || !strings.Contains(body, `"signed_in":true`) || !strings.Contains(body, `"superuser":false`) {
		t.Errorf("session = %d %s", status, body)
	}
	for _, rt := range []struct{ method, path string }{{http.MethodGet, "/api/hello/greeting"}, {http.MethodPost, "/api/hello/greetings"}} {
		if status, body, _ = itSend(t, addr, rt.method, rt.path, cookie, ""); status != 403 {
			t.Errorf("signed-in %s %s = %d %s, want 403 (actor present, permission denied until authz)", rt.method, rt.path, status, body)
		}
	}
	// The participant has no accounts.read: 403, and no audit.search either.
	for _, path := range []string{"/api/accounts/", "/api/audit/entries"} {
		if status, _, _ = itSend(t, addr, http.MethodGet, path, cookie, ""); status != 403 {
			t.Errorf("participant GET %s = %d, want 403", path, status)
		}
	}
	if status, _, _ = itSend(t, addr, http.MethodPost, "/api/accounts/sign-out", cookie, ""); status != 204 {
		t.Errorf("sign-out = %d, want 204", status)
	}

	// The superuser signs in with the printed password; the session says superuser.
	status, body, super := itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(itSuperEmail, pw[1]))
	if status != 204 || super == nil {
		t.Fatalf("superuser sign-in = %d %s", status, body)
	}
	status, body, _ = itSend(t, addr, http.MethodGet, "/api/accounts/session", super.Name+"="+super.Value, "")
	if status != 200 || !strings.Contains(body, `"superuser":true`) {
		t.Errorf("superuser session = %d %s", status, body)
	}
	// A participant address is refused by create-superuser (Conflict -> 2) while serving.
	if r := itMain(ctx, env, "create-superuser", itMemberMail); r.code != ExitUsage || !strings.Contains(r.errOut, "use another address") {
		t.Errorf("create-superuser on a participant address: status = %d\n%s", r.code, r.errOut)
	} else if strings.Contains(r.out+r.errOut, itMemberMail) {
		t.Error("the participant's address was echoed")
	}

	r := s.stop(t)
	if r.code != ExitOK {
		t.Fatalf("serve stopped with %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	for _, leak := range []string{itSuperEmail, itMemberMail, itMemberPass, pw[1]} {
		if strings.Contains(r.out+r.errOut, leak) {
			t.Errorf("serve's output carries %q", leak)
		}
	}
	recs := itRecords(t, r.out)
	var steps []string
	for _, rec := range recs {
		if rec["msg"] == "step stopped" {
			if n, ok := rec["name"].(string); ok {
				steps = append(steps, n)
			}
		}
	}
	if got := strings.Join(steps, ","); got != "http,runnables,signals,modules,audit,db,telemetry" {
		t.Errorf("shutdown steps = %s", got)
	}

	// The audit table holds the story, the superuser marked, and no personal value.
	rows, err := pool.Query(ctx, `SELECT action, outcome, actor_superuser FROM core.audit_entry ORDER BY at, id`)
	if err != nil {
		t.Fatalf("read the audit table: %v", err)
	}
	defer rows.Close()
	seen := map[string]int{}
	for rows.Next() {
		var action, outcome string
		var super bool
		if err := rows.Scan(&action, &outcome, &super); err != nil {
			t.Fatal(err)
		}
		seen[action]++
		if action == "identity.superuser.login" && !super {
			t.Error("the superuser's login is not marked")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"identity.superuser.created", "identity.account.created", "identity.logout", "identity.superuser.login"} {
		if seen[action] == 0 {
			t.Errorf("no %s entry; table holds %v", action, seen)
		}
	}
}

// On a read-only copy audited actions still work: entries go to the spool, and `check`
// reports the waiting file; nothing is imported until the copy can write.
func TestServe_Integration_ReadOnlyCopy_SpoolsAuditAndCheckReportsIt(t *testing.T) {
	u, pool, name := itPool(t)
	env, addr := itEnv(t, u.String())
	env = append(env, "BMS_HTTP_ALLOW_PLAINTEXT=true")
	ctx := t.Context()
	if r := itMain(ctx, env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}
	create := itMain(ctx, env, "create-superuser", itSuperEmail)
	pw := passwordLineRE.FindStringSubmatch(create.out)
	if create.code != ExitOK || pw == nil {
		t.Fatalf("create-superuser: %d\n%s", create.code, create.errOut)
	}
	if _, err := pool.Exec(ctx, `ALTER DATABASE "`+name+`" SET default_transaction_read_only = on`); err != nil {
		t.Fatalf("freeze the database: %v", err)
	}
	s := itServeMigrated(t, env, addr)
	status, body, c := itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(itSuperEmail, pw[1]))
	if status != 204 || c == nil {
		t.Fatalf("sign-in on a read-only copy = %d %s, want 204 (works read-only; the audit entry is spooled)", status, body)
	}
	if r := s.stop(t); r.code != ExitOK {
		t.Fatalf("serve stopped with %d\n%s", r.code, r.errOut)
	}
	dir := filepath.Join(env2(env, "BMS_DATA_DIR"), filepath.FromSlash(audit.SpoolDirName))
	files, err := os.ReadDir(dir)
	if err != nil || len(files) == 0 {
		t.Fatalf("spool %s: %v, %d files; want at least one", dir, err, len(files))
	}
	var out, errOut bytes.Buffer
	Main(context.Background(), []string{"check"}, env, &out, &errOut)
	if !strings.Contains(out.String(), "audit spool: 1 waiting, 0 failed") {
		t.Errorf("check does not report the waiting spool file:\n%s", &out)
	}
}

// itServeMigrated is itServe for a database that is already migrated (and may be read-only,
// where migrate refuses).
func itServeMigrated(t *testing.T, env []string, addr string) *itServing {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := &itServing{cancel: cancel, done: make(chan itResult, 1), out: &lockedBuf{}}
	go func() {
		var errOut lockedBuf
		code := Main(ctx, []string{"serve"}, env, s.out, &errOut)
		s.done <- itResult{code, s.out.String(), errOut.String()}
	}()
	t.Cleanup(cancel)
	wctx, wcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer wcancel()
	for {
		if code, _ := itGet(wctx, t, addr, "/readyz"); code == http.StatusOK {
			return s
		}
		select {
		case r := <-s.done:
			t.Fatalf("serve exited with %d before it was ready\n%s\n%s", r.code, r.out, r.errOut)
		default:
		}
		if err := clock.Wait(wctx, clock.Real(), 25*time.Millisecond); err != nil {
			t.Fatalf("serve never became ready: %v", err)
		}
	}
}

// env2 returns the value of name in env.
func env2(env []string, name string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			return v
		}
	}
	return ""
}

// A data directory that cannot hold the audit spool (it sits under a regular file) is the
// operator's input: serve stops at the importer's start with exit 2 (audit F33), having
// stopped what it had started, and says which variable.
func TestServe_Integration_DataDirUnderARegularFile_Exit2(t *testing.T) {
	u, _, _ := itPool(t)
	env, _ := itEnv(t, u.String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Later entries win. The secrets file moves out of the data directory, or config.Load
	// would refuse the unreadable path before serve gets as far as the importer.
	env = append(env,
		"BMS_DATA_DIR="+filepath.Join(blocker, "data"),
		"BMS_SECRETS_FILE="+filepath.Join(t.TempDir(), "secrets.json"))
	r := itMain(t.Context(), env, "serve")
	if r.code != ExitUsage {
		t.Fatalf("serve = %d, want ExitUsage\nstdout:\n%s\nstderr:\n%s", r.code, r.out, r.errOut)
	}
	if !strings.Contains(r.errOut, "BMS_DATA_DIR") {
		t.Errorf("stderr does not name BMS_DATA_DIR:\n%s", r.errOut)
	}
}
