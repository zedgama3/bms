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

// itWaitPerson waits for the core.person row people's account-created handler makes for the
// person in a session body (delivery happens after the sign-up commits).
func itWaitPerson(t *testing.T, pool *db.Pool, sessionBody string) {
	t.Helper()
	var sess struct {
		PersonID string `json:"person_id"`
	}
	if err := json.Unmarshal([]byte(sessionBody), &sess); err != nil || sess.PersonID == "" {
		t.Fatalf("the session body has no person_id: %v %s", err, sessionBody)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM core.person WHERE id = $1`, sess.PersonID).Scan(&n); err != nil {
			t.Fatalf("look for the person row: %v", err)
		}
		if n == 1 {
			return
		}
		if err := clock.Wait(ctx, clock.Real(), 20*time.Millisecond); err != nil {
			t.Fatalf("the person row for the new account never appeared: %v", err)
		}
	}
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
	// settings: the public document and the stylesheet need no session; the admin document does.
	if code, body := itGet(ctx, t, addr, "/api/regional/public"); code != 200 || !strings.Contains(body, `"name":"BMS"`) || !strings.Contains(body, `"term_department"`) {
		t.Errorf("anonymous GET /api/regional/public = %d %s, want the defaults and the terms", code, body)
	}
	if code, _ := itGet(ctx, t, addr, "/api/regional/theme.css"); code != 200 {
		t.Errorf("anonymous GET /api/regional/theme.css = %d, want 200", code)
	}
	if code, _ := itGet(ctx, t, addr, "/api/settings/"); code != 401 {
		t.Errorf("anonymous GET /api/settings/ = %d, want 401", code)
	}

	// A participant signs up (201, signed in) and is somebody. The base role, seeded by
	// migrate from hello's StarterRoles, lets them read and write hello (M2: 200, 201).
	status, body, member := itSend(t, addr, http.MethodPost, "/api/accounts/sign-up", "", credentials(itMemberMail, itMemberPass))
	if status != 201 || member == nil {
		t.Fatalf("sign-up = %d %s (cookie %v)", status, body, member)
	}
	cookie := member.Name + "=" + member.Value
	status, body, _ = itSend(t, addr, http.MethodGet, "/api/accounts/session", cookie, "")
	if status != 200 || !strings.Contains(body, `"signed_in":true`) || !strings.Contains(body, `"superuser":false`) {
		t.Errorf("session = %d %s", status, body)
	}
	itWaitPerson(t, pool, body)
	if status, body, _ = itSend(t, addr, http.MethodGet, "/api/hello/greeting", cookie, ""); status != 200 {
		t.Errorf("signed-in participant GET /api/hello/greeting = %d %s, want 200", status, body)
	}
	if status, body, _ = itSend(t, addr, http.MethodPost, "/api/hello/greetings", cookie, ""); status != 201 {
		t.Errorf("signed-in participant POST /api/hello/greetings = %d %s, want 201", status, body)
	}
	// The participant has no accounts.read: 403, and no audit.search either.
	for _, path := range []string{"/api/accounts/", "/api/audit/entries", "/api/settings/"} {
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
	// The superuser reads the admin document (every area, the accounts policy among them) and
	// renames the regional; the public document follows at once, without a restart.
	superCookie := super.Name + "=" + super.Value
	status, body, _ = itSend(t, addr, http.MethodGet, "/api/settings/", superCookie, "")
	if status != 200 || !strings.Contains(body, `"accounts.signup"`) || !strings.Contains(body, `"audit.retention"`) || !strings.Contains(body, `"hello.greeting"`) {
		t.Errorf("superuser GET /api/settings/ = %d %s, want the core and module areas", status, body)
	}
	status, body, _ = itSend(t, addr, http.MethodPut, "/api/settings/values", superCookie, `{"values":{"regional.name":"Quillfeather Burn","accounts.signup":"invitation"}}`)
	if status != 200 {
		t.Errorf("superuser PUT /api/settings/values = %d %s, want 200", status, body)
	}
	if code, body := itGet(ctx, t, addr, "/api/regional/public"); code != 200 || !strings.Contains(body, `"name":"Quillfeather Burn"`) {
		t.Errorf("GET /api/regional/public after the change = %d %s", code, body)
	}
	// identity reads its policy through settings: sign-up is now by invitation (501).
	if status, _, _ = itSend(t, addr, http.MethodPost, "/api/accounts/sign-up", "", credentials("late@example.org", itMemberPass)); status != 501 {
		t.Errorf("sign-up after accounts.signup=invitation = %d, want 501", status)
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

// jsonField returns the string at the dotted path of a JSON object, "" when absent.
func jsonField(t *testing.T, doc, path string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, doc)
	}
	for _, k := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = m[k]
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// The people story through the real assembly: a participant is somebody, reads hello, fills
// in a profile; the superuser reads the roles; `bms export` prints the document with hello
// inside; `bms delete` removes the person from every part and says so, and refuses the
// superuser's person.
func TestPeople_Integration_ProfileExportAndDeleteEndToEnd(t *testing.T) {
	u, pool, _ := itPool(t)
	env, addr := itEnv(t, u.String())
	env = append(env, "BMS_HTTP_ALLOW_PLAINTEXT=true")
	ctx := t.Context()
	migrate := itMain(ctx, env, "migrate")
	if migrate.code != ExitOK || !strings.Contains(migrate.out, "access: ") || !strings.Contains(migrate.out, " starter permissions seeded") {
		t.Fatalf("migrate = %d, want it to seed the starter roles\n%s\n%s", migrate.code, migrate.out, migrate.errOut)
	}
	if again := itMain(ctx, env, "migrate"); again.code != ExitOK || strings.Contains(again.out, "seeded") {
		t.Errorf("a second migrate seeded again:\n%s", again.out)
	}
	create := itMain(ctx, env, "create-superuser", itSuperEmail)
	pw := passwordLineRE.FindStringSubmatch(create.out)
	if create.code != ExitOK || pw == nil {
		t.Fatalf("create-superuser: %d\n%s\n%s", create.code, create.out, create.errOut)
	}
	s := itServe(t, env, addr)

	status, body, member := itSend(t, addr, http.MethodPost, "/api/accounts/sign-up", "", credentials(itMemberMail, itMemberPass))
	if status != 201 || member == nil {
		t.Fatalf("sign-up = %d %s", status, body)
	}
	cookie := member.Name + "=" + member.Value
	_, sessionBody, _ := itSend(t, addr, http.MethodGet, "/api/accounts/session", cookie, "")
	itWaitPerson(t, pool, sessionBody)
	personID := jsonField(t, sessionBody, "person_id")

	// Somebody, not yet complete; then complete, and the session carries the name.
	status, body, _ = itSend(t, addr, http.MethodGet, "/api/people/me", cookie, "")
	if status != 200 || !strings.Contains(body, `"complete":false`) {
		t.Errorf("GET /api/people/me = %d %s, want 200 and incomplete", status, body)
	}
	if status, body, _ = itSend(t, addr, http.MethodPut, "/api/people/me", cookie, `{"playa_name":"Dusty Fern"}`); status != 200 {
		t.Fatalf("PUT /api/people/me = %d %s", status, body)
	}
	_, sessionBody, _ = itSend(t, addr, http.MethodGet, "/api/accounts/session", cookie, "")
	if got := jsonField(t, sessionBody, "playa_name"); got != "Dusty Fern" {
		t.Errorf("session playa_name = %q, want Dusty Fern: %s", got, sessionBody)
	}
	if status, body, _ = itSend(t, addr, http.MethodPost, "/api/hello/greetings", cookie, ""); status != 201 {
		t.Fatalf("POST /api/hello/greetings = %d %s", status, body)
	}

	// The person's own data rights are served by datarights, and people's old paths are gone.
	if status, body, _ = itSend(t, addr, http.MethodGet, "/api/datarights/me/requests", cookie, ""); status != 200 {
		t.Errorf("GET /api/datarights/me/requests = %d %s, want 200", status, body)
	}
	if status, body, _ = itSend(t, addr, http.MethodGet, "/api/datarights/me/export", cookie, ""); status != 200 || jsonField(t, body, "person.playa_name") != "Dusty Fern" {
		t.Errorf("GET /api/datarights/me/export = %d %s, want 200 with the profile and hello's data", status, body)
	}
	for _, old := range []string{"/api/people/me/export", "/api/people/me/requests"} {
		if status, _, _ = itSend(t, addr, http.MethodGet, old, cookie, ""); status != 404 && status != 405 {
			t.Errorf("GET %s = %d, want 404: the data-rights routes moved to /api/datarights", old, status)
		}
	}

	// The participant holds no admin permission; the superuser reads the roles.
	if status, _, _ = itSend(t, addr, http.MethodGet, "/api/access/roles", cookie, ""); status != 403 {
		t.Errorf("participant GET /api/access/roles = %d, want 403", status)
	}
	status, body, super := itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(itSuperEmail, pw[1]))
	if status != 204 || super == nil {
		t.Fatalf("superuser sign-in = %d %s", status, body)
	}
	superCookie := super.Name + "=" + super.Value
	status, body, _ = itSend(t, addr, http.MethodGet, "/api/access/roles", superCookie, "")
	if status != 200 || !strings.Contains(body, `"key":"account-admin"`) || !strings.Contains(body, `"key":"participant"`) {
		t.Errorf("superuser GET /api/access/roles = %d %s, want the starter roles", status, body)
	}
	_, superSession, _ := itSend(t, addr, http.MethodGet, "/api/accounts/session", superCookie, "")
	superPerson := jsonField(t, superSession, "person_id")

	// bms export: the document, with hello inside, on stdout; the log elsewhere.
	exp := itMain(ctx, env, "export", personID)
	if exp.code != ExitOK {
		t.Fatalf("export = %d\n%s\n%s", exp.code, exp.out, exp.errOut)
	}
	if got := jsonField(t, exp.out, "person.playa_name"); got != "Dusty Fern" {
		t.Errorf("the export's person.playa_name = %q\n%s", got, exp.out)
	}
	var doc struct {
		Modules map[string]json.RawMessage `json:"modules"`
	}
	if err := json.Unmarshal([]byte(exp.out), &doc); err != nil || len(doc.Modules["hello"]) == 0 {
		t.Errorf("the export lacks hello's data (%v):\n%s", err, exp.out)
	}

	// bms delete: the report counts hello's greeting; the superuser's person and a malformed
	// or unknown id are refused with the documented statuses.
	if r := itMain(ctx, env, "delete", "not-a-uuid"); r.code != ExitUsage {
		t.Errorf("delete with a malformed id = %d, want ExitUsage", r.code)
	}
	if r := itMain(ctx, env, "delete", superPerson); r.code != ExitFailure {
		t.Errorf("delete of the superuser's person = %d, want ExitFailure\n%s", r.code, r.errOut)
	}
	del := itMain(ctx, env, "delete", personID)
	if del.code != ExitOK || !strings.Contains(del.out, "module hello deleted=1") || !strings.Contains(del.out, "person "+personID) {
		t.Fatalf("delete = %d\n%s\n%s", del.code, del.out, del.errOut)
	}
	if r := itMain(ctx, env, "delete", personID); r.code != ExitFailure {
		t.Errorf("a second delete = %d, want ExitFailure (not found)", r.code)
	}
	if status, _, _ = itSend(t, addr, http.MethodGet, "/api/people/me", cookie, ""); status != 401 {
		t.Errorf("the deleted person's session: GET /api/people/me = %d, want 401", status)
	}

	r := s.stop(t)
	if r.code != ExitOK {
		t.Fatalf("serve stopped with %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	for _, leak := range []string{itSuperEmail, itMemberMail, itMemberPass, "Dusty Fern", pw[1]} {
		if strings.Contains(r.out+r.errOut+exp.errOut+del.out+del.errOut, leak) {
			t.Errorf("a log or report carries %q", leak)
		}
	}

	// The commands wrote as the operator.
	rows, err := pool.Query(ctx, `SELECT action, actor_kind FROM core.audit_entry WHERE action IN ('datarights.export','datarights.delete','access.grants.deleted','identity.account.deleted') ORDER BY at`)
	if err != nil {
		t.Fatalf("read the audit table: %v", err)
	}
	defer rows.Close()
	kinds := map[string]string{}
	for rows.Next() {
		var action, kind string
		if err := rows.Scan(&action, &kind); err != nil {
			t.Fatal(err)
		}
		kinds[action] = kind
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"datarights.export", "datarights.delete", "identity.account.deleted"} {
		if kinds[action] != "operator" {
			t.Errorf("%s was recorded by %q, want operator (all: %v)", action, kinds[action], kinds)
		}
	}
}
