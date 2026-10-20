package identity_test

// Tests of the forgotten-password flow: plan/design/identity.md, "Amendment 2026-10-19
// (D39): the forgotten-password flow" (F69-F91) and the amendment "reset requests are
// uniform when e-mail is off". Black box. The unit tests here use a store with no accounts;
// the cases that need a known, a disabled or a superuser account are in
// integration_reset_test.go. Every address, token and password is invented.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

const (
	resetClient = "198.51.100.40"
	resetUnk    = "nobody.at.all@example.org"
	pwShort     = "short-1"
	pwCommon    = "password1234"
)

// ---- helpers ---------------------------------------------------------------------------

// withNotify installs n as the reset flow's Notifier.
func withNotify(n notify.Notifier) func(*identity.Options) {
	return func(o *identity.Options) { o.Notify = n }
}

// probeNotifier wraps a Notifier: Configured answers what the test says (and counts the
// asks), and Send fails with sendErr when it is set.
type probeNotifier struct {
	notify.Notifier

	mu         sync.Mutex
	configured bool
	probeErr   error
	sendErr    error
	probes     int
}

func newProbe(inner notify.Notifier) *probeNotifier {
	return &probeNotifier{Notifier: inner, configured: true}
}

func (p *probeNotifier) Configured(context.Context) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probes++
	return p.configured, p.probeErr
}

func (p *probeNotifier) Send(ctx context.Context, m notify.Message) error {
	p.mu.Lock()
	err := p.sendErr
	p.mu.Unlock()
	if err != nil {
		return err
	}
	return keep(p.Notifier.Send(ctx, m)) //nolint:wrapcheck // test double
}

func (p *probeNotifier) set(mod func(*probeNotifier)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	mod(p)
}

// requestReset calls RequestPasswordReset from a fresh client address.
func requestReset(t testing.TB, r *rig, email string) error {
	t.Helper()
	return keep(r.svc.RequestPasswordReset(from(ctxT(t), nextResetIP()), mail(t, email)))
}

var (
	resetIPMu sync.Mutex
	resetIPN  int
)

// nextResetIP returns a fresh documentation-range address so limiters never interfere.
func nextResetIP() string {
	resetIPMu.Lock()
	defer resetIPMu.Unlock()
	resetIPN++
	return fmt.Sprintf("10.77.%d.%d", (resetIPN>>8)&255, resetIPN&255)
}

// bareCall runs a handler with no Failer installed, so httpx.Fail writes its own fallback
// body and two responses can be compared byte for byte.
func bareCall(t testing.TB, rt identity.Route, ctx context.Context, body string) *httptest.ResponseRecorder { //nolint:revive // ctx is deliberately second: the route is the subject
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(ctx, rt.Method, "/api/accounts"+rt.Pattern, rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	rt.Handler.ServeHTTP(w, r)
	return w
}

func emailJSON(email string) string { return fmt.Sprintf(`{"email":%q}`, email) }

func completeJSON(token, pw string) string {
	return fmt.Sprintf(`{"token":%q,"new_password":%q}`, token, pw)
}

// sameResponse fails when two recorded responses differ in status, body or headers.
func sameResponse(t testing.TB, label string, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code {
		t.Errorf("%s: status %d vs %d", label, a.Code, b.Code)
	}
	if a.Body.String() != b.Body.String() {
		t.Errorf("%s: bodies differ: %q vs %q", label, a.Body.String(), b.Body.String())
	}
	ha, hb := a.Header().Clone(), b.Header().Clone()
	for _, h := range []string{"Date", "X-Request-Id"} {
		ha.Del(h)
		hb.Del(h)
	}
	if fmt.Sprint(ha) != fmt.Sprint(hb) {
		t.Errorf("%s: headers differ: %v vs %v", label, ha, hb)
	}
}

// ---- F69 -------------------------------------------------------------------------------

// F69
func TestRequestPasswordReset_BadEmail_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		email string
		code  string
	}{
		{"empty", "", "required"},
		{"malformed", "not-an-address", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := dbtest.NewFake()
			n := notifytest.NewFake("identity")
			r := newRig(t, fake, withNotify(n))
			err := r.svc.RequestPasswordReset(from(ctxT(t), resetClient), modelEmail(tc.email))
			wantField(t, err, "email", tc.code)
			if len(fake.Calls()) != 0 {
				t.Error("an invalid address reached the store")
			}
			if len(n.Sent()) != 0 {
				t.Error("an invalid address sent a message")
			}
		})
	}
}

func fieldsOf(err error) []*validate.FieldError { return validate.Fields(err) }

func readTemplate(name string) (string, error) {
	b, err := fs.ReadFile(identity.Templates, name)
	return string(b), keep(err)
}

// ---- F70 -------------------------------------------------------------------------------

// F70: both buckets, counted once per request whatever the outcome.
func TestRequestPasswordReset_RateLimited_Table(t *testing.T) {
	t.Run("per submitted address, case-insensitive, from different clients", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)), withNotify(notifytest.NewFake("identity")))
		for i, e := range []string{"Target.One@example.org", "target.one@EXAMPLE.org", "TARGET.ONE@example.org"} {
			wantNoErr(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), fmt.Sprintf("203.0.113.%d", 30+i)), mail(t, e))))
		}
		err := keep(r.svc.RequestPasswordReset(from(ctxT(t), "203.0.113.40"), mail(t, "target.one@example.org")))
		wantRateLimited(t, err)
		// another address from the same fresh client is fine
		wantNoErr(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), "203.0.113.40"), mail(t, "target.two@example.org"))))
	})
	t.Run("per client address across different submitted addresses", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(clientPolicy(10)), withNotify(notifytest.NewFake("identity")))
		ctx := from(ctxT(t), "203.0.113.20")
		for i := range 10 {
			wantNoErr(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, fmt.Sprintf("p%d@example.org", i)))))
		}
		wantRateLimited(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, "fresh@example.org"))))
		// another client is not affected
		wantNoErr(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), "203.0.113.21"), mail(t, "fresh@example.org"))))
	})
	t.Run("a request is counted whatever the outcome: e-mail off still spends the bucket", func(t *testing.T) {
		n := notifytest.NewFake("identity")
		n.SetConfigured(false)
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)), withNotify(n))
		for range 3 {
			wantKind(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), nextResetIP()), mail(t, "same@example.org"))), errs.Unsupported)
		}
		wantRateLimited(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), nextResetIP()), mail(t, "same@example.org"))))
	})
	t.Run("the limit is reached by the Nth request, not the N+1th", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)), withNotify(notifytest.NewFake("identity")))
		for range 3 {
			wantNoErr(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), nextResetIP()), mail(t, "same@example.org"))))
		}
		wantRateLimited(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), nextResetIP()), mail(t, "same@example.org"))))
	})
	t.Run("resets after the window on the fake clock", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)), withNotify(notifytest.NewFake("identity")))
		for range 3 {
			wantNoErr(t, requestReset(t, r, "same@example.org"))
		}
		wantRateLimited(t, requestReset(t, r, "same@example.org"))
		wantNoErr(t, r.clk.Advance(14*time.Minute))
		wantRateLimited(t, requestReset(t, r, "same@example.org"))
		wantNoErr(t, r.clk.Advance(61*time.Second))
		wantNoErr(t, requestReset(t, r, "same@example.org"))
	})
	t.Run("the limit is checked before any lookup, hash or probe", func(t *testing.T) {
		fake := dbtest.NewFake()
		probe := newProbe(notifytest.NewFake("identity"))
		r := newRig(t, fake, withPolicy(limitPolicy(3)), withNotify(probe))
		for range 3 {
			wantNoErr(t, requestReset(t, r, "same@example.org"))
		}
		before, probes := len(fake.Calls()), probeCount(probe)
		start := time.Now()
		wantRateLimited(t, requestReset(t, r, "same@example.org"))
		if len(fake.Calls()) != before {
			t.Error("a rate-limited request reached the store")
		}
		if probeCount(probe) != probes {
			t.Error("a rate-limited request asked Configured")
		}
		if time.Since(start) > time.Second {
			t.Error("a rate-limited request was slow")
		}
	})
	t.Run("a limited request leaks neither address", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)), withNotify(notifytest.NewFake("identity")))
		for range 4 {
			_ = requestReset(t, r, "limited.person@example.org") //nolint:errcheck // outcome asserted elsewhere
		}
		r.noLeak("limited.person")
	})
}

func probeCount(p *probeNotifier) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probes
}

// ---- F71, F72, F91 ---------------------------------------------------------------------

// F71 (replaced): Notify nil, or Configured false: Unsupported before any lookup, for any
// address alike.
func TestRequestPasswordReset_NotConfigured_UnsupportedForAnyAddress(t *testing.T) {
	addrs := []string{"known.one@example.org", "unknown.two@example.org", "disabled.three@example.org", "root.four@example.org"}
	tests := []struct {
		name string
		mod  func() func(*identity.Options)
	}{
		{"Notify nil", func() func(*identity.Options) { return func(*identity.Options) {} }},
		{"Configured false", func() func(*identity.Options) {
			n := notifytest.NewFake("identity")
			n.SetConfigured(false)
			return withNotify(n)
		}},
		{"probe wrapper says false", func() func(*identity.Options) {
			p := newProbe(notifytest.NewFake("identity"))
			p.set(func(p *probeNotifier) { p.configured = false })
			return withNotify(p)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := dbtest.NewFake()
			r := newRig(t, fake, tc.mod())
			var firstMsg string
			for _, a := range addrs {
				err := requestReset(t, r, a)
				wantKind(t, err, errs.Unsupported)
				if firstMsg == "" {
					firstMsg = fmt.Sprint(err)
				}
			}
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("%d store calls: the lookup must come after the Configured probe", n)
			}
			if len(r.rec.Recorded()) != 0 {
				t.Errorf("entries recorded while e-mail is off: %v", actionsOf(r.rec.Recorded()))
			}
		})
	}
}

// F71: the answer for an address does not depend on the address (no kind, no field).
func TestRequestPasswordReset_NotConfigured_IdenticalErrors(t *testing.T) {
	n := notifytest.NewFake("identity")
	n.SetConfigured(false)
	r := newRig(t, dbtest.NewFake(), withNotify(n))
	var kinds []errs.Kind
	var fields []int
	for _, a := range []string{"a@example.org", "b@example.org", "c@example.org"} {
		err := requestReset(t, r, a)
		kinds = append(kinds, errs.KindOf(err))
		fields = append(fields, len(fieldsOf(err)))
	}
	for i := range kinds {
		if kinds[i] != kinds[0] || fields[i] != fields[0] {
			t.Errorf("answer %d differs: kind %v fields %d vs kind %v fields %d", i, kinds[i], fields[i], kinds[0], fields[0])
		}
	}
}

// F91: a failing probe answers its kind, before the lookup, for every address alike.
func TestRequestPasswordReset_ProbeFails_ReturnsKindBeforeLookup(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errDown, errs.Unavailable},
		{"timeout", errTimeout, errs.Timeout},
		{"canceled", errs.E("test.probe", errs.Canceled, errors.New("gave up")), errs.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := dbtest.NewFake()
			p := newProbe(notifytest.NewFake("identity"))
			p.set(func(p *probeNotifier) { p.probeErr = tc.err })
			r := newRig(t, fake, withNotify(p))
			for _, a := range []string{"known@example.org", "unknown@example.org"} {
				wantKind(t, requestReset(t, r, a), tc.kind)
			}
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("%d store calls before the probe answered", n)
			}
			if len(r.rec.Recorded()) != 0 {
				t.Error("an entry was recorded on a probe failure")
			}
		})
	}
}

// ---- F73 (unknown address, unit part) --------------------------------------------------

// F73: an unknown address answers nil, sends nothing, records a Denied entry, and spends a
// password hash so the timing matches a known address.
func TestRequestPasswordReset_Unknown_SilentNilDummyHash(t *testing.T) {
	n := notifytest.NewFake("identity")
	r := newRig(t, dbtest.NewFake(), withNotify(n))
	start := time.Now()
	err := r.svc.RequestPasswordReset(from(ctxT(t), resetClient), mail(t, resetUnk))
	elapsed := time.Since(start)
	wantNoErr(t, err)
	if elapsed < 3*time.Millisecond {
		t.Errorf("unknown address answered in %v: no dummy hash verified", elapsed)
	}
	if len(n.Sent()) != 0 {
		t.Error("a message was sent for an unknown address")
	}
	es := r.rec.Recorded()
	if len(es) != 1 {
		t.Fatalf("%d entries, want 1: %v", len(es), actionsOf(es))
	}
	if es[0].Outcome != audit.Denied {
		t.Errorf("outcome = %v, want denied", es[0].Outcome)
	}
	r.noLeak("nobody.at.all", resetClient)
}

func TestRequestPasswordReset_CancelledContext_Canceled(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")))
	ctx, cancel := context.WithCancel(from(ctxT(t), resetClient))
	cancel()
	wantKind(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, resetUnk))), errs.Canceled)
}

func TestRequestPasswordReset_DeadlinePassed_Timeout(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")))
	ctx, cancel := context.WithDeadline(from(ctxT(t), resetClient), time.Now().Add(-time.Second))
	defer cancel()
	wantKind(t, keep(r.svc.RequestPasswordReset(ctx, mail(t, resetUnk))), errs.Timeout)
}

func TestRequestPasswordReset_StoreDown_PassesKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{{"unavailable", errDown, errs.Unavailable}, {"timeout", errTimeout, errs.Timeout}} {
		t.Run(tc.name, func(t *testing.T) {
			fdb := newFaultDB(dbtest.NewFake())
			fdb.set(func(f *faultDB) { f.readErr = tc.err })
			n := notifytest.NewFake("identity")
			r := newRig(t, fdb, withNotify(n))
			wantKind(t, requestReset(t, r, "ada.quill@example.org"), tc.kind)
			if len(n.Sent()) != 0 {
				t.Error("a message was sent while the store was down")
			}
		})
	}
}

// ---- F79 and the unit part of Complete -------------------------------------------------

// F79: missing, malformed or unknown tokens all answer Invalid (token: invalid), the same
// for every kind of garbage.
func TestCompletePasswordReset_UnknownOrUsed_Invalid(t *testing.T) {
	for _, tc := range []struct{ name, token string }{
		{"empty", ""},
		{"not base64", "!!!not a token!!!"},
		{"unknown but well formed", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"very long", strings.Repeat("A", 4096)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")))
			err := r.svc.CompletePasswordReset(from(ctxT(t), resetClient), tc.token, pwNew)
			wantField(t, err, "token", "invalid")
			if len(r.bus.Published()) != 0 {
				t.Error("a signal was published")
			}
		})
	}
}

func TestCompletePasswordReset_StoreDown_PassesKind(t *testing.T) {
	fdb := newFaultDB(dbtest.NewFake())
	fdb.set(func(f *faultDB) { f.readErr = errDown })
	r := newRig(t, fdb, withNotify(notifytest.NewFake("identity")))
	wantKind(t, keep(r.svc.CompletePasswordReset(from(ctxT(t), resetClient), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", pwNew)), errs.Unavailable)
}

// ---- F87 -------------------------------------------------------------------------------

// F87
func TestPolicy_Validate_ResetLifetime_Table(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		ok   bool
	}{
		{"default", time.Hour, true},
		{"floor", 10 * time.Minute, true},
		{"ceiling", 24 * time.Hour, true},
		{"just under floor", 10*time.Minute - time.Nanosecond, false},
		{"just over ceiling", 24*time.Hour + time.Nanosecond, false},
		{"zero", 0, false},
		{"negative", -time.Hour, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := identity.DefaultPolicy()
			p.PasswordResetLifetime = tc.d
			err := p.Validate()
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "accounts.password_reset_lifetime", "range")
		})
	}
}

func TestPolicy_Default_ResetLifetimeIsOneHour(t *testing.T) {
	if got := identity.DefaultPolicy().PasswordResetLifetime; got != time.Hour {
		t.Errorf("default = %v, want 1h", got)
	}
}

// The setting declares the key, with the same range as the policy.
func TestSettings_DeclaresPasswordResetLifetime(t *testing.T) {
	var d *settings.Decl
	for i := range identity.Settings {
		if identity.Settings[i].Key == "password_reset_lifetime" {
			d = &identity.Settings[i]
		}
	}
	if d == nil {
		t.Fatal("identity.Settings does not declare password_reset_lifetime")
	}
	if d.Kind != settings.Duration {
		t.Errorf("kind = %v, want Duration", d.Kind)
	}
	if got, err := time.ParseDuration(d.Default); err != nil || got != time.Hour {
		t.Errorf("default = %q (%v), want 1h", d.Default, err)
	}
	if d.Check == nil {
		t.Fatal("no range check")
	}
	for v, ok := range map[string]bool{"10m": true, "1h": true, "24h": true, "9m59s": false, "24h1s": false, "0s": false} {
		err := d.Check(v)
		if ok && err != nil {
			t.Errorf("Check(%s) = %v, want nil", v, err)
		}
		if !ok && err == nil {
			t.Errorf("Check(%s) = nil, want a refusal", v)
		}
	}
	if len(identity.Settings) != 8 {
		t.Errorf("declares %d keys, want 8", len(identity.Settings))
	}
}

// ---- F88 -------------------------------------------------------------------------------

// F88
func TestTemplates_Register_Parses(t *testing.T) {
	wantNoErr(t, notify.CheckTemplates("identity", identity.Templates))
	for _, name := range []string{
		"password-reset.en.tmpl", "password-reset.es.tmpl",
		"superuser-signed-in.en.tmpl", "superuser-signed-in.es.tmpl",
	} {
		b, err := readTemplate(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.Contains(b, "Subject:") {
			t.Errorf("%s has no Subject header", name)
		}
	}
	// The reset e-mail names the link's lifetime and the link, and never shows a token
	// in a subject.
	b, err := readTemplate("password-reset.en.tmpl")
	wantNoErr(t, err)
	subject, _, _ := strings.Cut(b, "\n")
	if strings.Contains(subject, "Link") || strings.Contains(subject, "token") {
		t.Errorf("the subject carries the link: %q", subject)
	}
	if !strings.Contains(b, "lifetime") {
		t.Error("the reset template does not use the lifetime variable")
	}
}

func TestTemplates_MissingEn_Invalid(t *testing.T) {
	fsys := fstest.MapFS{}
	for _, n := range []string{"password-reset.es.tmpl"} {
		b, err := readTemplate(n)
		wantNoErr(t, err)
		fsys[n] = &fstest.MapFile{Data: []byte(b)}
	}
	wantKind(t, notify.CheckTemplates("identity", fsys), errs.Invalid)
}

// ---- F89 and the handler contract ------------------------------------------------------

// F89
func TestResetHandlers_BadBody_Table(t *testing.T) {
	small := func(r *http.Request) { r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 16) }
	contentType := func(ct string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Content-Type", ct) } }
	inv := func(err error) bool { return errs.Is(err, errs.Invalid) }
	tests := []struct {
		name       string
		req, compl string
		mod        func(*http.Request)
		is         func(error) bool
	}{
		{"not json", `{nope`, `{nope`, nil, inv},
		{"array instead of object", `[]`, `[]`, nil, inv},
		{"wrong type", `{"email":7}`, `{"token":7,"new_password":"x"}`, nil, inv},
		{"wrong media type", `{"email":"ada.quill@example.org"}`, `{"token":"x","new_password":"y"}`, contentType("text/plain"),
			func(err error) bool { return errors.Is(err, httpx.ErrUnsupportedMediaType) }},
		{"too large", `{"email":"` + strings.Repeat("a", 64) + `@example.org"}`, `{"token":"` + strings.Repeat("a", 64) + `","new_password":"y"}`, small,
			func(err error) bool { var m *http.MaxBytesError; return errors.As(err, &m) }},
	}
	routes := []struct{ pattern string }{{"/password-reset"}, {"/password-reset/complete"}}
	for _, rt := range routes {
		for _, tc := range tests {
			t.Run(rt.pattern+"/"+tc.name, func(t *testing.T) {
				fake := dbtest.NewFake()
				n := notifytest.NewFake("identity")
				r := newRig(t, fake, withNotify(n))
				body := tc.req
				if strings.HasSuffix(rt.pattern, "/complete") {
					body = tc.compl
				}
				var mods []func(*http.Request)
				if tc.mod != nil {
					mods = append(mods, tc.mod)
				}
				w, f := call(t, handlerFor(t, r.svc, "POST", rt.pattern), from(ctxT(t), resetClient), body, mods...)
				if f.count() != 1 {
					t.Fatalf("httpx.Fail called %d times (status %d)", f.count(), w.Code)
				}
				if !tc.is(f.last()) {
					t.Errorf("failure %v does not match the case", f.last())
				}
				if len(setCookies(w)) != 0 {
					t.Error("a cookie was set on a bad body")
				}
				if len(n.Sent()) != 0 {
					t.Error("a bad body sent a message")
				}
			})
		}
	}
}

// The routes are public and not authenticated by the shell's gate: Public is set, and they
// take POST only.
func TestResetRoutes_ArePublicPosts(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")))
	for _, p := range []string{"/password-reset", "/password-reset/complete"} {
		rt := handlerFor(t, r.svc, "POST", p)
		if !rt.Public {
			t.Errorf("%s is not public", p)
		}
	}
}

// Handler: 202 with no body and no cookie for an unknown address when e-mail is on; 501
// for every address alike when it is off; the two answers are identical across addresses
// byte for byte (status, body, headers).
func TestResetHandler_UniformAnswers_OnAndOff(t *testing.T) {
	addrs := []string{"one@example.org", "two@example.org", "three@example.org"}
	for _, on := range []bool{true, false} {
		t.Run(fmt.Sprintf("configured=%v", on), func(t *testing.T) {
			n := notifytest.NewFake("identity")
			n.SetConfigured(on)
			r := newRig(t, dbtest.NewFake(), withNotify(n))
			rt := handlerFor(t, r.svc, "POST", "/password-reset")
			var first *httptest.ResponseRecorder
			for _, a := range addrs {
				w := bareCall(t, rt, from(ctxT(t), nextResetIP()), emailJSON(a))
				if on {
					if w.Code != http.StatusAccepted {
						t.Fatalf("status = %d, want 202", w.Code)
					}
					if w.Body.Len() != 0 {
						t.Errorf("a 202 carries a body: %q", w.Body.String())
					}
				} else if w.Code != http.StatusNotImplemented {
					t.Fatalf("status = %d, want 501", w.Code)
				}
				if len(w.Result().Cookies()) != 0 { //nolint:bodyclose // a recorder's body is a buffer
					t.Error("a cookie was set")
				}
				if first == nil {
					first = w
					continue
				}
				sameResponse(t, a, first, w)
			}
			if len(n.Sent()) != 0 {
				t.Error("a message was sent for an unknown address")
			}
		})
	}
}

// Handler: statuses of the other failure kinds.
func TestResetHandler_Statuses(t *testing.T) {
	t.Run("bad address is 400", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")))
		w, _ := call(t, handlerFor(t, r.svc, "POST", "/password-reset"), from(ctxT(t), resetClient), emailJSON("nope"))
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
	t.Run("rate limited is 429", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withPolicy(limitPolicy(3)), withNotify(notifytest.NewFake("identity")))
		rt := handlerFor(t, r.svc, "POST", "/password-reset")
		for range 3 {
			w, _ := call(t, rt, from(ctxT(t), nextResetIP()), emailJSON("same@example.org"))
			if w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", w.Code)
			}
		}
		w, _ := call(t, rt, from(ctxT(t), nextResetIP()), emailJSON("same@example.org"))
		if w.Code != http.StatusTooManyRequests {
			t.Errorf("status = %d, want 429", w.Code)
		}
	})
	t.Run("no Notify is 501", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake())
		w, _ := call(t, handlerFor(t, r.svc, "POST", "/password-reset"), from(ctxT(t), resetClient), emailJSON(resetUnk))
		if w.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", w.Code)
		}
	})
	t.Run("complete with an unknown token is 400", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake(), withNotify(notifytest.NewFake("identity")))
		w, f := call(t, handlerFor(t, r.svc, "POST", "/password-reset/complete"), from(ctxT(t), resetClient),
			completeJSON("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", pwNew))
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (%v)", w.Code, f.last())
		}
		if len(setCookies(w)) != 0 {
			t.Error("a cookie was set on a failed reset")
		}
	})
}

// Permission rules: both routes are public; a signed-in person and a restricted session may
// use them, and neither route returns a session.
func TestResetRoutes_PublicToSignedInAndRestricted(t *testing.T) {
	n := notifytest.NewFake("identity")
	r := newRig(t, dbtest.NewFake(), withNotify(n), withPerms(allowing()))
	full := identity.WithSession(from(ctxT(t), nextResetIP()), sessionFor(newAccountID(), newPersonID(), epoch))
	restricted := sessionFor(newAccountID(), newPersonID(), epoch)
	restricted.PasswordChangeRequired = true
	rctx := identity.WithSession(from(ctxT(t), nextResetIP()), restricted)
	for name, ctx := range map[string]context.Context{"signed in": full, "restricted": rctx, "anonymous": from(ctxT(t), nextResetIP())} {
		t.Run(name, func(t *testing.T) {
			w, f := call(t, handlerFor(t, r.svc, "POST", "/password-reset"), ctx, emailJSON(resetUnk))
			if w.Code != http.StatusAccepted || f.count() != 0 {
				t.Errorf("request: status %d, failures %d", w.Code, f.count())
			}
			if len(setCookies(w)) != 0 {
				t.Error("a cookie was set")
			}
			w, _ = call(t, handlerFor(t, r.svc, "POST", "/password-reset/complete"), ctx,
				completeJSON("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", pwNew))
			if w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
				t.Errorf("complete refused a %s caller with %d", name, w.Code)
			}
			if len(setCookies(w)) != 0 {
				t.Error("a cookie was set")
			}
		})
	}
}

// F90 (unit part): an unknown address, a token and a password never reach telemetry.
func TestPasswordReset_NoSecretsInTelemetry_Unknown(t *testing.T) {
	const tok = "TOKENMARKERzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	n := notifytest.NewFake("identity")
	r := newRig(t, dbtest.NewFake(), withNotify(n))
	wantNoErr(t, requestReset(t, r, "marker.person@example.org"))
	err := r.svc.CompletePasswordReset(from(ctxT(t), resetClient), tok, pwNew)
	wantKind(t, err, errs.Invalid)
	r.noLeak("marker.person", tok, pwNew)
	if strings.Contains(fmt.Sprint(err), tok) || strings.Contains(fmt.Sprint(err), pwNew) {
		t.Error("an error string carries the token or the password")
	}
}
