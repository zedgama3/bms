package identity_test

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// All people, addresses and passwords in this package are invented.
const (
	sessionKeyText = "identity-test-session-key-0123456789abcdef-xyz"

	pwGood  = "tangerine-orbit-quiet-77"
	pwOther = "violet-harbour-lantern-91"
	pwNew   = "marble-sunrise-kettle-58"

	testTimeout = 90 * time.Second
)

var epoch = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// ctxT returns a bounded context for one test.
func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// mail parses an invented address or stops the test.
func mail(t testing.TB, s string) model.Email {
	t.Helper()
	e, err := model.ParseEmail(s)
	if err != nil {
		t.Fatalf("ParseEmail(%q): %v", s, err)
	}
	return e
}

// from returns ctx as a request from client address ip over https.
func from(ctx context.Context, ip string) context.Context {
	ctx = httpx.WithClientAddr(ctx, netip.MustParseAddr(ip))
	return httpx.WithScheme(ctx, "https")
}

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

// wantReadOnly checks Unavailable wrapping db.ErrReadOnly.
func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error does not wrap db.ErrReadOnly: %v", err)
	}
}

// wantRateLimited checks Unavailable wrapping httpx.ErrRateLimited.
func wantRateLimited(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, httpx.ErrRateLimited) {
		t.Fatalf("error does not wrap httpx.ErrRateLimited: %v", err)
	}
}

// wantField checks that err carries a validate.FieldError for field with code.
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && (code == "" || fe.Code == code) {
			return
		}
	}
	t.Fatalf("no field error %q/%q in %v", field, code, validate.Fields(err))
}

// ---- seams -----------------------------------------------------------------------------

// policySource is a settings.Reader for the "accounts" area that a test can change as an
// identity.Policy (set) or make fail (err). Every read is answered from a settingstest.Fake
// loaded with the current policy, so identity's own key mapping is what the tests exercise.
type policySource struct {
	mu  sync.Mutex
	p   identity.Policy
	err error
}

var _ settings.Reader = (*policySource)(nil)

func newPolicySource() *policySource { return &policySource{p: identity.DefaultPolicy()} }

func (s *policySource) set(mod func(*identity.Policy)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mod(&s.p)
}

// fake returns a Fake carrying the current policy, or the injected error.
func (s *policySource) fake() (*settingstest.Fake, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return accountsFake(s.p), nil
}

// accountsFake is a settingstest.Fake for "accounts" holding p's eleven values.
func accountsFake(p identity.Policy) *settingstest.Fake {
	f := settingstest.NewFake("accounts", identity.Settings)
	f.Set("signup", string(p.SignupMode))
	f.Set("session_lifetime", p.SessionLifetime.String())
	f.Set("superuser_session_lifetime", p.SuperuserSessionLifetime.String())
	f.Set("password_min_length", strconv.Itoa(p.PasswordMinLength))
	f.Set("login_failure_limit", strconv.Itoa(p.LoginFailureLimit))
	f.Set("client_failure_limit", strconv.Itoa(p.ClientFailureLimit))
	f.Set("login_failure_window", p.LoginFailureWindow.String())
	f.Set("password_reset_lifetime", p.PasswordResetLifetime.String())
	f.Set("password_reset_client_limit", strconv.Itoa(p.PasswordResetClientLimit))
	f.Set("password_reset_address_limit", strconv.Itoa(p.PasswordResetAddressLimit))
	f.Set("password_reset_cooldown", p.PasswordResetCooldown.String())
	return f
}

func (s *policySource) Bool(ctx context.Context, k string) (bool, error) {
	f, err := s.fake()
	if err != nil {
		return false, err
	}
	return f.Bool(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (s *policySource) Int(ctx context.Context, k string) (int64, error) {
	f, err := s.fake()
	if err != nil {
		return 0, err
	}
	return f.Int(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (s *policySource) Duration(ctx context.Context, k string) (time.Duration, error) {
	f, err := s.fake()
	if err != nil {
		return 0, err
	}
	return f.Duration(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (s *policySource) Text(ctx context.Context, k string) (string, error) {
	f, err := s.fake()
	if err != nil {
		return "", err
	}
	return f.Text(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

func (s *policySource) Secret(ctx context.Context, k string) (secret.Secret, error) {
	f, err := s.fake()
	if err != nil {
		return secret.Secret{}, err
	}
	return f.Secret(ctx, k) //nolint:wrapcheck // test shim: the settingstest.Fake answer is the point
}

type permChecker struct {
	mu    sync.Mutex
	allow map[authz.Permission]bool
	asked []authz.Permission
}

func allowing(ps ...authz.Permission) *permChecker {
	c := &permChecker{allow: map[authz.Permission]bool{}}
	for _, p := range ps {
		c.allow[p] = true
	}
	return c
}

func (c *permChecker) Allow(_ context.Context, p authz.Permission) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, p)
	if c.allow[p] {
		return nil
	}
	return errs.E("test.permChecker.Allow", errs.Permission, errors.New("denied"))
}

func (c *permChecker) askedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.asked)
}

type lister struct {
	perms []string
	err   error
}

func (l *lister) PermissionsOf(context.Context, model.PersonID) ([]string, error) {
	return l.perms, l.err
}

type nameSource struct {
	name string
	err  error
}

func (n *nameSource) PlayaNameOf(context.Context, model.PersonID) (string, error) {
	return n.name, n.err
}

// ---- the rig ---------------------------------------------------------------------------

// rig is a Service over the given DB with a fake clock, recorder, bus and telemetry capture.
type rig struct {
	t    *testing.T
	svc  *identity.Service
	clk  *clock.Fake
	rec  *audittest.Fake
	bus  *signalstest.Fake
	cap  *telemetrytest.Capture
	opts identity.Options
}

func validOptions(t *testing.T, d db.DB) (identity.Options, *rig) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	rec := audittest.NewFake("identity")
	bus, err := signalstest.NewFake("identity", []signals.Name{identity.SignalAccountCreated}, nil)
	wantNoErr(t, err)
	o := identity.Options{
		DB:       d,
		Clock:    clk,
		Logger:   c.Logger(),
		Tracer:   c.Tracer("identity"),
		Meter:    c.Meter("identity"),
		Key:      secret.New(sessionKeyText),
		Audit:    rec,
		Signals:  bus,
		Settings: settingstest.NewFake("accounts", identity.Settings),
	}
	return o, &rig{t: t, clk: clk, rec: rec, bus: bus, cap: c}
}

// newRig builds a Service over d; mods edit the Options before identity.New.
func newRig(t *testing.T, d db.DB, mods ...func(*identity.Options)) *rig {
	t.Helper()
	o, r := validOptions(t, d)
	for _, m := range mods {
		m(&o)
	}
	svc, err := identity.New(o)
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	r.svc, r.opts = svc, o
	return r
}

func withPerms(p identity.PermissionChecker) func(*identity.Options) {
	return func(o *identity.Options) { o.Permissions = p }
}

func withPolicy(p settings.Reader) func(*identity.Options) {
	return func(o *identity.Options) { o.Settings = p }
}

func withPlaintext(o *identity.Options) { o.AllowPlaintext = true }

const (
	actLogin        = "identity.login"
	actLoginFailed  = "identity.login-failed"
	actLogout       = "identity.logout"
	actCreated      = "identity.account.created"
	actRead         = "identity.account.read"
	actDisabled     = "identity.account.disabled"
	actEnabled      = "identity.account.enabled"
	actDeleted      = "identity.account.deleted"
	actPwChanged    = "identity.password.changed"
	actTempSet      = "identity.password.temporary-set"
	actEnded        = "identity.sessions.ended"
	actSuCreated    = "identity.superuser.created"
	actSuReset      = "identity.superuser.reset"
	actSuLogin      = "identity.superuser.login"
	metricLogins    = "bms.identity.logins"
	metricRejected  = "bms.identity.sessions_rejected"
	metricRateLimit = "bms.identity.rate_limited"
)

// count returns how many entries with action were recorded.
func (r *rig) count(action string) int {
	r.t.Helper()
	n := 0
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == action {
			n++
		}
	}
	return n
}

// only returns the single entry with action or stops the test.
func (r *rig) only(action string) audit.Entry {
	r.t.Helper()
	var got []audit.Entry
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == action {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		r.t.Fatalf("%d entries for %s, want 1 (all: %v)", len(got), action, actionsOf(r.rec.Recorded()))
	}
	return got[0]
}

func actionsOf(es []audit.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Action)
	}
	return out
}

// measured returns the sum of the counter increments of instrument whose attributes
// include every key/value in want.
func (r *rig) measured(instrument string, want map[string]string) float64 {
	r.t.Helper()
	var sum float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument != instrument {
			continue
		}
		ok := true
		for k, v := range want {
			if m.Attrs[k] != v {
				ok = false
			}
		}
		if ok {
			sum += m.Value
		}
	}
	return sum
}

// noLeak fails when any needle is in the logs, spans, metrics or the recorded entries.
func (r *rig) noLeak(needles ...string) {
	r.t.Helper()
	dump := fmt.Sprintf("%+v", r.rec.Recorded())
	for _, n := range needles {
		if n == "" {
			continue
		}
		if r.cap.Contains(n) {
			r.t.Errorf("a log, span or metric contains %q", n)
		}
		if strings.Contains(dump, n) {
			r.t.Errorf("an audit entry contains %q", n)
		}
	}
}

// ---- tokens, forged from the spec ------------------------------------------------------

const (
	tokenLen    = 85
	tokenMACOff = 53
	cookieChars = 114
)

// tokenParts is the token layout of the spec ("The token").
type tokenParts struct {
	Version byte
	Account model.AccountID
	Session identity.SessionID
	Gen     uint32
	Issued  int64
	Expires int64
}

func signingKey(ikm string) []byte {
	k, err := hkdf.Key(sha256.New, []byte(ikm), nil, "bms identity session v1", 32)
	if err != nil {
		panic(err)
	}
	return k
}

// raw returns the 85 token bytes signed with the key derived from ikm.
func (p tokenParts) raw(ikm string) []byte {
	b := make([]byte, 0, tokenLen)
	b = append(b, p.Version)
	b = append(b, p.Account[:]...)
	b = append(b, p.Session[:]...)
	b = binary.BigEndian.AppendUint32(b, p.Gen)
	b = binary.BigEndian.AppendUint64(b, uint64(p.Issued))  //nolint:gosec // test fixture, small positive values
	b = binary.BigEndian.AppendUint64(b, uint64(p.Expires)) //nolint:gosec // test fixture, small positive values
	m := hmac.New(sha256.New, signingKey(ikm))
	m.Write(b)
	return m.Sum(b)
}

func (p tokenParts) encode(ikm string) string {
	return base64.RawURLEncoding.EncodeToString(p.raw(ikm))
}

// validParts returns parts that are well formed at the epoch for account.
func validParts(account model.AccountID, now time.Time) tokenParts {
	return tokenParts{
		Version: identity.TokenVersion,
		Account: account,
		Session: model.NewID[identity.SessionID](),
		Issued:  now.Unix(),
		Expires: now.Add(time.Hour).Unix(),
	}
}

// requestWithCookie builds a GET with one cookie.
func requestWithCookie(t testing.TB, name, value string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/hello/greeting", nil)
	if name != "" {
		r.AddCookie(reqCookie(name, value))
	}
	return r
}

// ---- handler plumbing ------------------------------------------------------------------

// failRec is the Failer a handler test installs: it records the error handed to
// httpx.Fail and answers with the status the shell's mapper would.
type failRec struct {
	mu   sync.Mutex
	errs []error
}

func (f *failRec) Fail(w http.ResponseWriter, _ *http.Request, err error) {
	f.mu.Lock()
	f.errs = append(f.errs, err)
	f.mu.Unlock()
	status := httpx.StatusOf(errs.KindOf(err))
	if errors.Is(err, httpx.ErrRateLimited) {
		status = http.StatusTooManyRequests
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		status = http.StatusRequestEntityTooLarge
	}
	if errors.Is(err, httpx.ErrUnsupportedMediaType) {
		status = http.StatusUnsupportedMediaType
	}
	w.WriteHeader(status)
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

// handlerFor returns the route's handler or stops the test.
func handlerFor(t testing.TB, svc *identity.Service, method, pattern string) httpx.Route {
	t.Helper()
	for _, rt := range svc.Routes() {
		if rt.Method == method && rt.Pattern == pattern {
			return rt
		}
	}
	t.Fatalf("no route %s %s", method, pattern)
	return httpx.Route{}
}

// call runs one route handler. ctx is the request context (a session, a client address, a
// scheme); a failRec is installed and returned.
func call(t testing.TB, rt httpx.Route, ctx context.Context, body string, mods ...func(*http.Request)) (*httptest.ResponseRecorder, *failRec) { //nolint:revive // ctx is deliberately second: the route is the subject
	t.Helper()
	f := &failRec{}
	ctx = httpx.WithFailer(ctx, f)
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(ctx, rt.Method, "/api/accounts"+strings.TrimSuffix(rt.Pattern, "/"), rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mods {
		m(r)
	}
	w := httptest.NewRecorder()
	rt.Handler.ServeHTTP(w, r)
	return w, f
}

// setCookies returns the Set-Cookie headers of w parsed.
func setCookies(w *httptest.ResponseRecorder) []*http.Cookie {
	return w.Result().Cookies() //nolint:bodyclose // a recorder's body is a buffer
}

func credsJSON(email, password string) string {
	return fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
}

// session returns a restricted or full session for tests that only need a principal.
func sessionFor(acct model.AccountID, person model.PersonID, now time.Time) identity.Session {
	return identity.Session{
		ID:        model.NewID[identity.SessionID](),
		Account:   acct,
		Person:    person,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
}

// httpxHTTP returns ctx as a plaintext http request from a documentation address.
func httpxHTTP(ctx context.Context) context.Context {
	ctx = httpx.WithClientAddr(ctx, netip.MustParseAddr("198.51.100.7"))
	return httpx.WithScheme(ctx, "http")
}

func newAccountID() model.AccountID { return model.NewID[model.AccountID]() }
func newPersonID() model.PersonID   { return model.NewID[model.PersonID]() }

func modelEmail(s string) model.Email { return model.Email(s) }

// entries2 returns the recorded entries with action.
func (r *rig) entries2(action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

// reqCookie builds a cookie for a request; the attributes only matter on responses, but
// carrying them keeps scanners quiet and states the intent.
func reqCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

// keep returns err wrapped with context so the kind and sentinels survive (errors.As and
// errors.Is see through it) while the linter sees a wrapped error; nil stays nil.
func keep(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("test: %w", err)
}

// isRateLimited reports whether err wraps httpx.ErrRateLimited.
func isRateLimited(err error) bool { return errors.Is(err, httpx.ErrRateLimited) }
