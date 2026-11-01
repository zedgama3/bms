package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity/account"
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

// bareService builds a Service over no store, for the pieces that never touch one.
func bareService(t *testing.T) (*Service, *clock.Fake) {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	bus, err := signalstest.NewFake("identity", []signals.Name{SignalAccountCreated}, nil)
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	s, err := newService(Options{
		DB: dbtest.NewFake(), Clock: clk, Logger: c.Logger(), Tracer: c.Tracer("identity"),
		Key: secret.New("internal-test-session-key-0123456789abcdef"), Audit: audittest.NewFake("identity"), Signals: bus,
		Settings: settingstest.NewFake("accounts", Settings),
	}, struct{ store }{})
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return s, clk
}

func TestTokenRoundTrip_SignThenVerify(t *testing.T) {
	s, clk := bareService(t)
	now := clk.Now()
	want := claims{
		account: model.NewID[model.AccountID](), session: model.NewID[SessionID](), generation: 7,
		issued: now, expires: now.Add(time.Hour),
	}
	tok := s.sign(want)
	if len(tok) != 114 {
		t.Fatalf("token is %d characters, want 114", len(tok))
	}
	got, err := s.verifyToken(tok, now)
	if err != nil {
		t.Fatalf("verifyToken: %v", err)
	}
	if got != want {
		t.Errorf("claims = %+v, want %+v", got, want)
	}
	if _, err := s.verifyToken(tok, now.Add(2*time.Hour)); !errs.Is(err, errs.Unauthenticated) || reasonOf(err) != "expired" {
		t.Errorf("expired token: kind %v reason %q", errs.KindOf(err), reasonOf(err))
	}
}

func TestPHC_RehashWhenParametersAreWeaker(t *testing.T) {
	s, _ := bareService(t)
	ctx := context.Background()
	salt := []byte("0123456789abcdef")
	current := secret.New(encodePHC("a long enough password", salt))
	ok, weak, err := s.verify(ctx, "a long enough password", current)
	if err != nil || !ok || weak {
		t.Fatalf("current hash: ok=%v weak=%v err=%v", ok, weak, err)
	}
	if ok, _, err := s.verify(ctx, "another password entirely", current); err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
	// The same password hashed with t=1 verifies and is reported as weaker.
	p, err := parsePHC(current.Value())
	if err != nil {
		t.Fatalf("parsePHC: %v", err)
	}
	p.time = argonTime - 1
	if !p.weaker() {
		t.Error("t below the current value is not weaker")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=0,t=2,p=1$AAAA$AAAA", "$bcrypt$x", "$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHQ$" + strings.Repeat("A", 22)} {
		if _, err := parsePHC(bad); err == nil {
			t.Errorf("parsePHC accepted %q", bad)
		}
	}
}

func TestGeneratePassword_AlphabetAndLength(t *testing.T) {
	s, _ := bareService(t)
	seen := map[string]bool{}
	for range 50 {
		pw, err := s.generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		if len(pw) != GeneratedPasswordLength || strings.Trim(pw, generatedAlphabet) != "" {
			t.Fatalf("password has the wrong shape: %d characters", len(pw))
		}
		seen[pw] = true
	}
	if len(seen) != 50 {
		t.Errorf("%d distinct passwords in 50 draws", len(seen))
	}
	s.rand = iotest.ErrReader(errors.New("entropy exhausted"))
	if _, err := s.generatePassword(); !errs.Is(err, errs.Internal) {
		t.Errorf("failing random source: kind %v", errs.KindOf(err))
	}
}

// nowFn is a fixed clock for the limiter.
func nowFn(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestLimiter_ReserveSweepsAndEvictsOldest(t *testing.T) {
	l := newLimiter()
	l.maxKeys = 3
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	window := 15 * time.Minute
	a := netip.MustParseAddr("192.0.2.1")
	for range 3 {
		if _, _, ok := l.reserve(a, "who@example.org", nowFn(now), 3, 3, window); !ok {
			t.Fatal("refused before the limit")
		}
	}
	if _, b, ok := l.reserve(a, "", nowFn(now), 3, 3, window); ok || b != bucketAddress {
		t.Fatalf("reserve = %v %q, want the address bucket full", ok, b)
	}
	if _, b, ok := l.reserve(netip.MustParseAddr("192.0.2.2"), "who@example.org", nowFn(now), 3, 3, window); ok || b != bucketEmail {
		t.Fatalf("reserve = %v %q, want the email bucket full", ok, b)
	}
	later := now.Add(window)
	at, _, ok := l.reserve(a, "who@example.org", nowFn(later), 3, 3, window)
	if !ok || !at.Equal(later) {
		t.Fatalf("after the window: ok=%v at=%v", ok, at)
	}
	// Forgiving takes back exactly one attempt.
	l.forgive(a, "who@example.org", at)
	if len(l.addr.keys) != 0 || len(l.email.keys) != 0 {
		t.Errorf("forgiven attempt left keys behind: %d, %d", len(l.addr.keys), len(l.email.keys))
	}
	for _, k := range []string{"a", "b", "c", "d"} {
		if _, _, ok := l.reserve(netip.Addr{}, k, nowFn(now), 3, 3, window); !ok {
			t.Fatal("refused")
		}
	}
	if len(l.email.keys) != 3 {
		t.Errorf("%d tracked keys, want 3", len(l.email.keys))
	}
	if _, ok := l.email.keys["a"]; ok {
		t.Error("the oldest key was not evicted")
	}
}

// The client address and the submitted address have their own limits (decision 12): a
// venue shares one address, so its bucket holds more.
func TestLimiter_ReserveUsesEachBucketsOwnLimit(t *testing.T) {
	l := newLimiter()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a := netip.MustParseAddr("192.0.2.1")
	const addrLimit, emailLimit = 5, 2
	for i := range emailLimit {
		if _, _, ok := l.reserve(a, "one@example.org", nowFn(now), addrLimit, emailLimit, time.Hour); !ok {
			t.Fatalf("attempt %d refused before the submitted-address limit", i)
		}
	}
	if _, b, ok := l.reserve(a, "one@example.org", nowFn(now), addrLimit, emailLimit, time.Hour); ok || b != bucketEmail {
		t.Fatalf("reserve = %v %q, want the submitted-address bucket full at %d", ok, b, emailLimit)
	}
	// Other submitted addresses from the same client keep going until the client limit.
	for i := emailLimit; i < addrLimit; i++ {
		if _, _, ok := l.reserve(a, "other"+string(rune('a'+i))+"@example.org", nowFn(now), addrLimit, emailLimit, time.Hour); !ok {
			t.Fatalf("attempt %d refused before the client-address limit", i)
		}
	}
	if _, b, ok := l.reserve(a, "last@example.org", nowFn(now), addrLimit, emailLimit, time.Hour); ok || b != bucketAddress {
		t.Fatalf("reserve = %v %q, want the client-address bucket full at %d", ok, b, addrLimit)
	}
}

// After a forgive lowers a key's newest time, the list must still run oldest to newest, or
// sweep stops early and eviction removes the wrong key.
func TestLimiter_ForgiveKeepsTheListOrdered(t *testing.T) {
	l := newLimiter()
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	window := time.Hour
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	reserve := func(key string, m int) time.Time {
		t.Helper()
		got, _, ok := l.reserve(netip.Addr{}, key, nowFn(at(m)), 5, 5, window)
		if !ok {
			t.Fatalf("refused %s", key)
		}
		return got
	}
	reserve("x", 1)
	late := reserve("x", 30) // x: 1, 30
	reserve("y", 10)         // list: y(10), x(30)
	l.forgive(netip.Addr{}, "x", late)
	// x's newest is now 1: it must sit before y.
	var order []string
	for e := l.email.head; e != nil; e = e.next {
		order = append(order, e.key)
	}
	if len(order) != 2 || order[0] != "x" || order[1] != "y" {
		t.Fatalf("order = %v, want [x y]", order)
	}
	// At minute 65 the window (60) has passed x's newest failure (1) but not y's (10)... y expires at 70.
	if _, _, ok := l.reserve(netip.Addr{}, "z", nowFn(at(65)), 5, 5, window); !ok {
		t.Fatal("refused z")
	}
	if _, ok := l.email.keys["x"]; ok {
		t.Error("x survived the sweep")
	}
	if _, ok := l.email.keys["y"]; !ok {
		t.Error("y was swept early")
	}
}

func TestLimiter_ConcurrentReservationsNeverExceedTheLimit(t *testing.T) {
	l := newLimiter()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a := netip.MustParseAddr("192.0.2.9")
	const tries, limit = 300, 10
	var wg sync.WaitGroup
	var granted atomic.Int64
	for range tries {
		wg.Go(func() {
			if _, _, ok := l.reserve(a, "target@example.org", nowFn(now), limit, limit, time.Hour); ok {
				granted.Add(1)
			}
		})
	}
	wg.Wait()
	if got := granted.Load(); got != limit {
		t.Errorf("%d of %d concurrent attempts passed, want %d", got, tries, limit)
	}
}

func TestAddrKey_GroupsIPv6By64(t *testing.T) {
	k1, _ := addrKey(netip.MustParseAddr("2001:db8:1:2:aaaa::1"))
	k2, _ := addrKey(netip.MustParseAddr("2001:db8:1:2:bbbb::2"))
	k3, _ := addrKey(netip.MustParseAddr("2001:db8:1:3::1"))
	if k1 != k2 || k1 == k3 {
		t.Errorf("keys %q %q %q: want the first two equal and the third different", k1, k2, k3)
	}
	v4, _ := addrKey(netip.MustParseAddr("::ffff:192.0.2.7"))
	if v4 != "192.0.2.7" {
		t.Errorf("mapped IPv4 key = %q", v4)
	}
	if _, ok := addrKey(netip.Addr{}); ok {
		t.Error("the zero address has a key")
	}
}

func TestReadToken_DevCookieNeedsPlaintextAndHTTP(t *testing.T) {
	s, _ := bareService(t)
	read := func(scheme string) string {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: DevCookieName, Value: "dev-token"}) //nolint:gosec // a request cookie, read back by the code under test
		return s.readToken(httpx.WithScheme(t.Context(), scheme), r)
	}
	if got := read("http"); got != "" {
		t.Errorf("dev cookie read without AllowPlaintext: %q", got)
	}
	s.allowPlaintext = true
	if got := read("http"); got != "dev-token" {
		t.Errorf("dev cookie over http with AllowPlaintext: %q", got)
	}
	if got := read("https"); got != "" {
		t.Errorf("dev cookie over https: %q", got)
	}
}

func TestCursor_RoundTripAndRejectsGarbage(t *testing.T) {
	k := account.Key{CreatedAt: time.Date(2026, 10, 17, 12, 0, 0, 123456000, time.UTC), ID: model.NewID[model.AccountID]()}
	got, err := cursorOf(k).key()
	if err != nil || !got.CreatedAt.Equal(k.CreatedAt) || got.ID != k.ID {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for _, bad := range []Cursor{"!", "bm9wZQ", Cursor(strings.Repeat("A", 40))} {
		if _, err := bad.key(); !errs.Is(err, errs.Invalid) {
			t.Errorf("cursor %q: kind %v", bad, errs.KindOf(err))
		}
	}
	q := AccountQuery{Email: "someone@example.org", After: "ignored", Limit: -4}
	if err := q.Validate(); err != nil {
		t.Errorf("an address query ignores After and Limit: %v", err)
	}
	q = AccountQuery{After: "ignored", Limit: 1000}
	if got := len(validate.Fields(q.Validate())); got != 2 {
		t.Errorf("%d field errors, want 2", got)
	}
}

func TestIssued_FormattingNeverPrintsTheToken(t *testing.T) {
	iss := Issued{Session: Session{ID: model.NewID[SessionID]()}, token: "tok-should-never-print"}
	for _, s := range []string{iss.String(), iss.GoString(), iss.LogValue().String()} {
		if strings.Contains(s, "tok-should-never-print") {
			t.Errorf("%q contains the token", s)
		}
	}
}

// superuserStore answers only the superuser upsert.
type superuserStore struct {
	store
	created bool
}

func (s superuserStore) Superuser(_ context.Context, id model.AccountID, p model.PersonID, _ model.Email, _ secret.Secret, _ time.Time) (account.Superuser, error) {
	return account.Superuser{ID: id, Person: p, Created: s.created}, nil
}

func TestCreateSuperuser_PrintsTheThreeLinesAndNeverTheAddress(t *testing.T) {
	const addr = "hollis.marsh@example.org"
	for _, created := range []bool{true, false} {
		s, _ := bareService(t)
		s.store = superuserStore{created: created}
		var out strings.Builder
		if err := s.CreateSuperuser(context.Background(), &out, addr); err != nil {
			t.Fatalf("CreateSuperuser: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("%d lines: %q", len(lines), out.String())
		}
		want := lineReset
		if created {
			want = lineCreated
		}
		pw, ok := strings.CutPrefix(lines[1], linePasswordPrefix)
		if lines[0] != want || !ok || len(pw) != GeneratedPasswordLength || lines[2] != lineSuperuserHint {
			t.Errorf("created=%v: unexpected output %q", created, out.String())
		}
		if strings.Contains(out.String(), "hollis") {
			t.Error("the address was printed")
		}
	}
	s, _ := bareService(t)
	var out strings.Builder
	if err := s.CreateSuperuser(context.Background(), &out, "not an address"); !errs.Is(err, errs.Invalid) || out.Len() != 0 {
		t.Errorf("bad address: kind %v, printed %q", errs.KindOf(err), out.String())
	}
}
