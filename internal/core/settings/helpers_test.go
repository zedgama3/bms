package settings_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// testSealingKey is an invented key of the length config allows (32..1024 bytes).
var testSealingKey = secret.New("0123456789abcdef0123456789abcdef-invented")

// first returns the first of two results; for reads whose error a test does not need.
func first[T, U any](v T, _ U) T { return v }

// ignore swallows results a test does not need (a call made only to exercise a path).
func ignore(...any) {}

func newPerson() model.PersonID     { return model.NewID[model.PersonID]() }
func str(s string) *string          { return &s }
func repeat(s string, n int) string { return strings.Repeat(s, n) }

// ---- error assertions ------------------------------------------------------------------

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func hasField(err error, field, code string) bool {
	for _, fe := range validate.Fields(err) {
		if (field == "" || fe.Field == field) && (code == "" || fe.Code == code) {
			return true
		}
	}
	return false
}

// wantField checks Invalid carrying a field error for field and code ("" matches any).
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if !hasField(err, field, code) {
		t.Fatalf("no field error %q/%q in %v", field, code, describe(err))
	}
}

func describe(err error) []string {
	var out []string
	for _, fe := range validate.Fields(err) {
		out = append(out, fe.Field+"/"+fe.Code)
	}
	return out
}

func paramOf(err error, field, code, key string) string {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && fe.Code == code {
			return fe.Params[key]
		}
	}
	return ""
}

// ---- sessions --------------------------------------------------------------------------

func sessionOf(p model.PersonID, super, restricted bool) identity.Session {
	return identity.Session{
		ID:                     model.NewID[identity.SessionID](),
		Account:                model.NewID[model.AccountID](),
		Person:                 p,
		Superuser:              super,
		PasswordChangeRequired: restricted,
		IssuedAt:               epoch,
		ExpiresAt:              epoch.Add(time.Hour),
	}
}

func asPerson(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, sessionOf(p, false, false))
}

func asSuper(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, sessionOf(p, true, false))
}

func asRestricted(ctx context.Context, p model.PersonID) context.Context {
	return identity.WithSession(ctx, sessionOf(p, false, true))
}

// actorOf is what app builds: identity.PersonFrom plus the superuser flag.
func actorOf(ctx context.Context) (authz.Actor, bool) {
	p, ok := identity.PersonFrom(ctx)
	if !ok {
		return authz.Actor{}, false
	}
	s := first(identity.SessionFrom(ctx))
	return authz.Actor{Person: p, Superuser: s.Superuser}, true
}

// ---- the areas under test --------------------------------------------------------------

const (
	kHost     = "mail.host"
	kPort     = "mail.port"
	kSecurity = "mail.security"
	kEnabled  = "mail.enabled"
	kWait     = "mail.wait"
	kNote     = "mail.note"
	kPassword = "mail.password"
	kToken    = "mail.token"
	kGreeting = "hello.greeting"
	kLogin    = "accounts.login_failure_limit"
	kClient   = "accounts.client_failure_limit"
)

// checkPort is a Decl.Check returning a field error: Set must re-field it under the key
// and keep the code.
func checkPort(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return &validate.FieldError{Field: "port", Code: validate.CodeRange, Reason: "out of range"}
	}
	return nil
}

// checkHost is a Decl.Check returning a plain error: Set reports code "rejected".
func checkHost(v string) error {
	if strings.HasPrefix(v, "forbidden.") {
		return errors.New("that host is not allowed")
	}
	return nil
}

// checkLimits is an Area.Check: the client limit may not be below the login limit.
func checkLimits(values map[string]string) error {
	login := first(strconv.Atoi(values["login_failure_limit"]))
	client := first(strconv.Atoi(values["client_failure_limit"]))
	if client < login {
		return &validate.FieldError{Field: "client_failure_limit", Code: validate.CodeRange, Reason: "below the login limit"}
	}
	return nil
}

func testAreas() []settings.Area {
	return []settings.Area{
		{Name: "regional", Core: true, Decls: regional.Settings}, // app passes it, as it passes accounts
		{Name: "accounts", Core: true, Check: checkLimits, Decls: []settings.Decl{
			{Key: "login_failure_limit", Kind: settings.Int, Default: "5"},
			{Key: "client_failure_limit", Kind: settings.Int, Default: "20"},
		}},
		{Name: "mail", Core: true, Decls: []settings.Decl{
			{Key: "host", Kind: settings.Text, Default: "", Check: checkHost},
			{Key: "port", Kind: settings.Int, Default: "587", Check: checkPort},
			{Key: "security", Kind: settings.Enum, Default: "starttls", Options: []string{"starttls", "tls", "none"}},
			{Key: "enabled", Kind: settings.Bool, Default: "false"},
			{Key: "wait", Kind: settings.Duration, Default: "30s"},
			{Key: "note", Kind: settings.Text, Default: "hello"},
			{Key: "password", Kind: settings.Secret},
			{Key: "token", Kind: settings.Secret},
		}},
		{Name: "hello", Decls: []settings.Decl{
			{Key: "greeting", Kind: settings.Text, Default: "Hello, burner"},
		}},
	}
}

// ---- the rig ---------------------------------------------------------------------------

// rig is a settings.Service over a DB with every seam faked.
type rig struct {
	t    *testing.T
	svc  *settings.Service
	opts settings.Options
	clk  *clock.Fake
	rec  *audittest.Fake
	az   *authztest.Fake
	cap  *telemetrytest.Capture
}

func validOptions(t *testing.T, d db.DB) (settings.Options, *rig) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	rec := audittest.NewFake("settings")
	az := authztest.NewFake()
	az.SetActor(actorOf)
	r := &rig{t: t, clk: clk, rec: rec, az: az, cap: c}
	o := settings.Options{
		DB:         d,
		Clock:      clk,
		Logger:     c.Logger(),
		Tracer:     c.Tracer("settings"),
		Meter:      c.Meter("settings"),
		Audit:      rec,
		Authz:      az,
		Actor:      actorOf,
		Areas:      testAreas(),
		SealingKey: testSealingKey,
	}
	return o, r
}

func newRig(t *testing.T, d db.DB, mods ...func(*settings.Options)) *rig {
	t.Helper()
	o, r := validOptions(t, d)
	for _, m := range mods {
		m(&o)
	}
	svc, err := settings.New(o)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
}

func (r *rig) reader(area string) settings.Reader {
	r.t.Helper()
	rd, err := r.svc.Scoped(area)
	if err != nil {
		r.t.Fatalf("Scoped(%q): %v", area, err)
	}
	return rd
}

// person returns a new person holding perms site-wide.
func (r *rig) person(perms ...authz.Permission) model.PersonID {
	p := newPerson()
	for _, perm := range perms {
		r.az.Grant(p, perm)
	}
	return p
}

func changes(kv ...string) settings.Changes {
	c := settings.Changes{Values: map[string]*string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		c.Values[kv[i]] = str(kv[i+1])
	}
	return c
}

func resetting(keys ...string) settings.Changes {
	c := settings.Changes{Values: map[string]*string{}}
	for _, key := range keys {
		c.Values[key] = nil
	}
	return c
}

// ---- store wrappers that inject failures ------------------------------------------------

// faultDB wraps a db.DB and fails transactions on demand. Reads and writes are failed
// independently; readOnly makes Write transactions fail like a replica and ReadOnly()
// report true.
type faultDB struct {
	db.DB
	mu       sync.Mutex
	readErr  error
	writeErr error
	readOnly bool
	txCount  int
}

func (f *faultDB) set(read, write error, ro bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readErr, f.writeErr, f.readOnly = read, write, ro
}

func (f *faultDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	f.mu.Lock()
	f.txCount++
	rerr, werr, ro := f.readErr, f.writeErr, f.readOnly
	f.mu.Unlock()
	if mode == db.Write && ro {
		return errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	if mode == db.Write && werr != nil {
		return werr
	}
	if mode == db.Read && rerr != nil {
		return rerr
	}
	return f.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

func (f *faultDB) ReadOnly() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readOnly || f.DB.ReadOnly()
}

type failRow struct{ err error }

func (r failRow) Scan(...any) error { return r.err }

// Statements outside a transaction fail with the read error (the refresher's poll uses
// one); inside a transaction the Transact gate already decided.
func (f *faultDB) readFault() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readErr
}

func (f *faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := f.readFault(); err != nil {
		return nil, err
	}
	return f.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (f *faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := f.readFault(); err != nil {
		return failRow{err}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	werr, ro := f.writeErr, f.readOnly
	f.mu.Unlock()
	if ro {
		return pgconn.CommandTag{}, errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	if werr != nil {
		return pgconn.CommandTag{}, werr
	}
	return f.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

// ---- HTTP ------------------------------------------------------------------------------

type failRec struct {
	mu   sync.Mutex
	errs []error
}

func (f *failRec) Fail(w http.ResponseWriter, _ *http.Request, err error) {
	f.mu.Lock()
	f.errs = append(f.errs, err)
	f.mu.Unlock()
	w.WriteHeader(httpx.StatusOf(errs.KindOf(err)))
}

func (f *failRec) last() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errs) == 0 {
		return nil
	}
	return f.errs[len(f.errs)-1]
}

func (f *failRec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.errs)
}

// normPattern treats "/{$}" and "/" as the same root pattern.
func normPattern(p string) string {
	if p == "/{$}" {
		return "/"
	}
	return p
}

// mux mounts every route under /api/settings, as app does.
func (r *rig) mux() *http.ServeMux {
	r.t.Helper()
	m := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		pattern := normPattern(rt.Pattern)
		if pattern == "/" {
			pattern = "/{$}"
		}
		m.Handle(rt.Method+" /api/settings"+pattern, rt.Handler)
	}
	return m
}

func (r *rig) do(ctx context.Context, method, path, body string, hdr ...string) (*httptest.ResponseRecorder, *failRec) {
	r.t.Helper()
	f := &failRec{}
	ctx = httpx.WithFailer(ctx, f)
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, "/api/settings"+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	r.mux().ServeHTTP(w, req)
	return w, f
}

func actionsOf(es []audit.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Action)
	}
	return out
}

// errOf returns only the error of a call's results, for table rows that test one error.
func errOf[T any](_ T, err error) error { return err }
