package notify_test

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

const (
	permTemplates = authz.Permission("notify.templates.manage")
	permOutbox    = authz.Permission("notify.outbox.manage")
	testHostname  = "bms-test.example"
	siteURL       = "https://burn.example.org"
)

func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

func mustEmail(t testing.TB, s string) model.Email {
	t.Helper()
	e, err := model.ParseEmail(s)
	if err != nil {
		t.Fatalf("ParseEmail(%q): %v", s, err)
	}
	return e
}

// ---- error assertions --------------------------------------------------------------------

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

func describe(err error) []string {
	var out []string
	for _, fe := range validate.Fields(err) {
		out = append(out, fe.Field+"/"+fe.Code)
	}
	return out
}

// wantField checks Invalid carrying a field error for field and code ("" matches any).
func wantField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if !hasField(err, field, code) {
		t.Fatalf("no field error %q/%q in %v", field, code, describe(err))
	}
}

func paramOf(err error, field, code, key string) string {
	for _, fe := range validate.Fields(err) {
		if fe.Field == field && fe.Code == code {
			return fe.Params[key]
		}
	}
	return ""
}

// ---- actors ------------------------------------------------------------------------------

type actorKey struct{}

func actorFn(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

func as(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p})
}

func asSuper(ctx context.Context, p model.PersonID) context.Context {
	return context.WithValue(ctx, actorKey{}, authz.Actor{Person: p, Superuser: true})
}

// ---- seam fakes --------------------------------------------------------------------------

type smtpSrc struct {
	mu    sync.Mutex
	cfg   notify.SMTPConfig
	err   error
	calls atomic.Int64
}

func (s *smtpSrc) SMTP(context.Context) (notify.SMTPConfig, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg, s.err
}

func (s *smtpSrc) set(mod func(*notify.SMTPConfig)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mod(&s.cfg)
}

func (s *smtpSrc) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

type siteSrc struct {
	mu    sync.Mutex
	site  notify.Site
	err   error
	calls atomic.Int64
	langs []notify.Language
}

func (s *siteSrc) Site(_ context.Context, l notify.Language) (notify.Site, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.langs = append(s.langs, l)
	return s.site, s.err
}

func (s *siteSrc) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

type retSrc struct {
	mu sync.Mutex
	d  time.Duration
}

func (s *retSrc) Retention(context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d, nil
}

func (s *retSrc) set(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = d
}

type addrSrc struct {
	mu    sync.Mutex
	m     map[model.PersonID]model.Email
	err   error
	calls atomic.Int64
}

func (s *addrSrc) AddressOf(_ context.Context, p model.PersonID) (model.Email, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	e, ok := s.m[p]
	if !ok {
		return "", errs.E("test.AddressOf", errs.NotFound, errors.New("no address"))
	}
	return e, nil
}

func (s *addrSrc) set(p model.PersonID, e model.Email) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e == "" {
		delete(s.m, p)
		return
	}
	s.m[p] = e
}

type langSrc struct {
	mu    sync.Mutex
	m     map[model.PersonID]notify.Language
	err   error
	calls atomic.Int64
}

func (s *langSrc) LanguageOf(_ context.Context, p model.PersonID) (notify.Language, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[p], s.err
}

func (s *langSrc) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

type nameSrc struct {
	mu    sync.Mutex
	m     map[model.PersonID]string
	err   error
	calls atomic.Int64
}

func (s *nameSrc) PlayaNameOf(_ context.Context, p model.PersonID) (string, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[p], s.err
}

func (s *nameSrc) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// chanFake is a scriptable notify.Channel. fn, when set, decides each Send.
type chanFake struct {
	mu        sync.Mutex
	envs      []notify.Envelope
	fn        func(notify.Envelope) error
	pingErr   error
	pings     int
	factories int
	cfgs      []notify.SMTPConfig
	factErr   error
}

func (c *chanFake) Send(ctx context.Context, e notify.Envelope) error {
	c.mu.Lock()
	fn := c.fn
	c.mu.Unlock()
	if fn != nil {
		if err := fn(e); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.envs = append(c.envs, e)
	c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return errs.E("test.chanFake.Send", errs.KindOf(err), err)
	}
	return nil
}

func (c *chanFake) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pings++
	return c.pingErr
}

func (c *chanFake) factory(cfg notify.SMTPConfig) (notify.Channel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.factories++
	c.cfgs = append(c.cfgs, cfg)
	if c.factErr != nil {
		return nil, c.factErr
	}
	return c, nil
}

func (c *chanFake) envelopes() []notify.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]notify.Envelope(nil), c.envs...)
}

func (c *chanFake) factoryCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.factories
}

// ---- templates ---------------------------------------------------------------------------

const (
	greetEN = "Subject: Shift for {{.Recipient.PlayaName}}\nVars: shift, note\n\n" +
		"You are on {{.Vars.shift}}. Link: {{.Link}} Word: {{term \"shift\"}} Site: {{.Site.Name}} {{.Site.URL}} Note: {{.Vars.note}}\n"
	greetES = "Subject: Turno para {{.Recipient.PlayaName}}\nVars: shift, note\n\n" +
		"Estas en {{.Vars.shift}}. Enlace: {{.Link}} Sitio: {{.Site.Name}}\n"
)

var longEN = "Subject: {{.Vars.note}}\nVars: note\n\n" + strings.Repeat("{{.Vars.note}}", 70) + "\n"

func volunteerTemplates() fs.FS {
	return fstest.MapFS{
		"greet.en.tmpl": {Data: []byte(greetEN)},
		"greet.es.tmpl": {Data: []byte(greetES)},
		"long.en.tmpl":  {Data: []byte(longEN)},
	}
}

func peopleTemplates() fs.FS {
	return fstest.MapFS{
		"deleted.en.tmpl": {Data: []byte("Subject: Deletion\n\nA request is waiting. {{.Link}}\n")},
	}
}

// ---- the rig -----------------------------------------------------------------------------

type rig struct {
	t     *testing.T
	svc   *notify.Service
	opts  notify.Options
	clk   *clock.Fake
	cap   *telemetrytest.Capture
	rec   *audittest.Fake
	az    *authztest.Fake
	smtp  *smtpSrc
	site  *siteSrc
	ret   *retSrc
	addrs *addrSrc
	langs *langSrc
	names *nameSrc
	ch    *chanFake
	vol   notify.Notifier // scoped "volunteers"
	ppl   notify.Notifier // scoped "people"
}

func defaultConfig() notify.SMTPConfig {
	return notify.SMTPConfig{
		Host: "smtp.invalid", Port: 2525, Security: notify.None,
		Sender: notify.Address{Name: "Test Burn", Email: "burn@example.org"},
	}
}

// newRigOptions builds seams and Options but does not call New or Register.
func newRigOptions(t *testing.T, d db.DB) (*rig, notify.Options) {
	t.Helper()
	c := telemetrytest.New(t)
	r := &rig{
		t: t, clk: clock.NewFake(epoch), cap: c,
		rec: audittest.NewFake("notify"), az: authztest.NewFake(),
		smtp:  &smtpSrc{cfg: defaultConfig()},
		site:  &siteSrc{site: notify.Site{Name: "Test Burn", URL: siteURL, DefaultLanguage: "en", Terms: map[string]string{"shift": "duty"}}},
		ret:   &retSrc{},
		addrs: &addrSrc{m: map[model.PersonID]model.Email{}},
		langs: &langSrc{m: map[model.PersonID]notify.Language{}},
		names: &nameSrc{m: map[model.PersonID]string{}},
		ch:    &chanFake{},
	}
	r.az.SetActor(actorFn)
	o := notify.Options{
		DB: d, Clock: r.clk, Logger: c.Logger(), Tracer: c.Tracer("notify"), Meter: c.Meter("notify"),
		Audit: r.rec, Authz: r.az, Actor: actorFn,
		SMTP: r.smtp, Site: r.site, Retention: r.ret,
		Addresses: r.addrs, Languages: r.langs, Names: r.names,
		Channel: r.ch.factory, Hostname: testHostname,
	}
	return r, o
}

func newRig(t *testing.T, d db.DB, mods ...func(*notify.Options)) *rig {
	t.Helper()
	r, o := newRigOptions(t, d)
	for _, m := range mods {
		m(&o)
	}
	svc, err := notify.New(o)
	if err != nil {
		t.Fatalf("notify.New: %v", err)
	}
	r.svc, r.opts = svc, o
	wantNoErr(t, svc.Register("volunteers", volunteerTemplates()))
	wantNoErr(t, svc.Register("people", peopleTemplates()))
	r.vol = r.scoped("volunteers")
	r.ppl = r.scoped("people")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := svc.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return r
}

func (r *rig) scoped(owner string) notify.Notifier {
	r.t.Helper()
	n, err := r.svc.Scoped(owner)
	if err != nil {
		r.t.Fatalf("Scoped(%q): %v", owner, err)
	}
	return n
}

// person returns a new person with an address (and optionally a name and language).
func (r *rig) person(addr string) model.PersonID {
	p := newPerson()
	if addr != "" {
		r.addrs.set(p, mustEmail(r.t, addr))
	}
	return p
}

func (r *rig) holder(perms ...authz.Permission) model.PersonID {
	p := newPerson()
	for _, perm := range perms {
		r.az.Grant(p, perm)
	}
	return p
}

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func greet(to ...model.PersonID) notify.Message {
	return notify.Message{
		Template: "volunteers.greet", To: to,
		Vars: map[string]string{"shift": "Gate 06:00", "note": "bring water"},
		Link: "/volunteers/shifts/1",
	}
}

// ---- waiting without sleeping ------------------------------------------------------------

// eventually spins (yielding) until cond holds or a real deadline passes.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// ---- HTTP --------------------------------------------------------------------------------

func (r *rig) mux() *http.ServeMux {
	r.t.Helper()
	m := http.NewServeMux()
	for _, rt := range r.svc.Routes() {
		m.Handle(rt.Method+" /api/notify"+rt.Pattern, rt.Handler)
	}
	return m
}

func (r *rig) do(ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	r.t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(ctx, method, "/api/notify"+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.mux().ServeHTTP(w, req)
	return w
}

func statusFor(k errs.Kind) int { return httpx.StatusOf(k) }

var _ = secret.New

// closedPort returns a loopback TCP port nothing listens on.
func closedPort(t testing.TB) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("not a TCP address")
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr.Port
}

// ignore swallows results a test does not need (a call made only to exercise a path).
func ignore(...any) {}

func errOf[T any](_ T, err error) error { return err }
