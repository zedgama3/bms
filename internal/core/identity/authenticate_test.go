package identity_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

type authRig struct {
	*rig
	fake *dbtest.Fake
}

func newAuthRig(t *testing.T, mods ...func(*identityOptions)) *authRig {
	t.Helper()
	fake := dbtest.NewFake()
	return &authRig{rig: newRig(t, fake, mods...), fake: fake}
}

type identityOptions = identity.Options

func (a *authRig) authenticate(r *http.Request) (person model.PersonID, err error) {
	a.t.Helper()
	ctx, person, err := a.svc.Authenticate(from(ctxT(a.t), "198.51.100.7"), r)
	if ctx == nil {
		a.t.Fatal("Authenticate returned a nil ctx")
	}
	return person, keep(err)
}

func (a *authRig) validToken() tokenParts {
	return validParts(model.NewID[model.AccountID](), a.clk.Now())
}

// F2
func TestAuthenticate_NoCookie_Anonymous(t *testing.T) {
	a := newAuthRig(t)
	ctx, person, err := a.svc.Authenticate(from(ctxT(t), "198.51.100.7"), requestWithCookie(t, "", ""))
	wantNoErr(t, err)
	if !person.IsZero() {
		t.Error("person set without a cookie")
	}
	if _, ok := identity.SessionFrom(ctx); ok {
		t.Error("session in ctx without a cookie")
	}
	if n := len(a.fake.Calls()); n != 0 {
		t.Errorf("%d statements for an anonymous request", n)
	}
}

// F3: one case per byte region tampered, and every other malformed shape.
func TestAuthenticate_Malformed_Table(t *testing.T) {
	const ikm = sessionKeyText
	flip := func(idx int) func(tokenParts) string {
		return func(p tokenParts) string {
			b := p.raw(ikm)
			b[idx] ^= 0x01
			return base64.RawURLEncoding.EncodeToString(b)
		}
	}
	tests := []struct {
		name  string
		token func(p tokenParts, now time.Time) string
	}{
		{"version byte flipped", func(p tokenParts, _ time.Time) string { return flip(0)(p) }},
		{"account region flipped", func(p tokenParts, _ time.Time) string { return flip(1)(p) }},
		{"session region flipped", func(p tokenParts, _ time.Time) string { return flip(17)(p) }},
		{"generation region flipped", func(p tokenParts, _ time.Time) string { return flip(33)(p) }},
		{"issued region flipped", func(p tokenParts, _ time.Time) string { return flip(37)(p) }},
		{"expires region flipped", func(p tokenParts, _ time.Time) string { return flip(45)(p) }},
		{"first MAC byte flipped", func(p tokenParts, _ time.Time) string { return flip(tokenMACOff)(p) }},
		{"last MAC byte flipped", func(p tokenParts, _ time.Time) string { return flip(tokenLen - 1)(p) }},
		{"wrong version, correctly signed", func(p tokenParts, _ time.Time) string { p.Version = 2; return p.encode(ikm) }},
		{"version zero, correctly signed", func(p tokenParts, _ time.Time) string { p.Version = 0; return p.encode(ikm) }},
		{"signed with another key", func(p tokenParts, _ time.Time) string { return p.encode(ikm + "-other") }},
		{"one byte short", func(p tokenParts, _ time.Time) string {
			return base64.RawURLEncoding.EncodeToString(p.raw(ikm)[:tokenLen-1])
		}},
		{"one byte long", func(p tokenParts, _ time.Time) string {
			return base64.RawURLEncoding.EncodeToString(append(p.raw(ikm), 0))
		}},
		{"payload without MAC", func(p tokenParts, _ time.Time) string {
			return base64.RawURLEncoding.EncodeToString(p.raw(ikm)[:tokenMACOff])
		}},
		{"padded base64", func(p tokenParts, _ time.Time) string { return base64.URLEncoding.EncodeToString(p.raw(ikm)) + "=" }},
		{"not base64", func(tokenParts, time.Time) string { return "!!!not*base64!!!" }},
		{"standard alphabet", func(p tokenParts, _ time.Time) string {
			s := base64.StdEncoding.EncodeToString(p.raw(ikm))
			return s + "+/"
		}},
		{"issued beyond the skew", func(p tokenParts, now time.Time) string {
			p.Issued = now.Add(identity.ClockSkew + time.Second).Unix()
			p.Expires = now.Add(time.Hour).Unix()
			return p.encode(ikm)
		}},
		{"garbage of the right length", func(tokenParts, time.Time) string {
			return strings.Repeat("A", cookieChars)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newAuthRig(t)
			tok := tc.token(a.validToken(), a.clk.Now())
			person, err := a.authenticate(requestWithCookie(t, identity.CookieName, tok))
			wantKind(t, err, errs.Unauthenticated)
			if !person.IsZero() {
				t.Error("person set for a malformed token")
			}
			if n := len(a.fake.Calls()); n != 0 {
				t.Errorf("%d statements before the token was proven well formed", n)
			}
			if got := a.measured(metricRejected, map[string]string{"reason": "malformed"}); got != 1 {
				t.Errorf("sessions_rejected{malformed} = %v, want 1", got)
			}
			a.noLeak(tok)
		})
	}
}

func TestAuthenticate_TokenShape_Is85BytesAs114Characters(t *testing.T) {
	if got := len(tokenParts{Version: 1}.encode(sessionKeyText)); got != cookieChars {
		t.Fatalf("test helper encodes %d characters", got)
	}
}

// F4
func TestAuthenticate_Expired_Unauthenticated(t *testing.T) {
	for _, tc := range []struct {
		name string
		exp  func(now time.Time) time.Time
	}{
		{"expires exactly now", func(now time.Time) time.Time { return now }},
		{"expired a second ago", func(now time.Time) time.Time { return now.Add(-time.Second) }},
		{"expired a day ago", func(now time.Time) time.Time { return now.Add(-24 * time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAuthRig(t)
			p := a.validToken()
			p.Issued = a.clk.Now().Add(-48 * time.Hour).Unix()
			p.Expires = tc.exp(a.clk.Now()).Unix()
			_, err := a.authenticate(requestWithCookie(t, identity.CookieName, p.encode(sessionKeyText)))
			wantKind(t, err, errs.Unauthenticated)
			if n := len(a.fake.Calls()); n != 0 {
				t.Errorf("%d statements for an expired token", n)
			}
			if got := a.measured(metricRejected, map[string]string{"reason": "expired"}); got != 1 {
				t.Errorf("sessions_rejected{expired} = %v, want 1", got)
			}
		})
	}
}

func TestAuthenticate_ExpiryFollowsTheClock(t *testing.T) {
	a := newAuthRig(t)
	p := a.validToken() // expires in an hour
	req := func() *http.Request { return requestWithCookie(t, identity.CookieName, p.encode(sessionKeyText)) }
	_, err := a.authenticate(req())
	wantKind(t, err, errs.Unauthenticated) // unknown account, but the state was read
	if n := len(a.fake.Calls()); n != 1 {
		t.Fatalf("a live token made %d statements, want 1", n)
	}
	wantNoErr(t, a.clk.Advance(time.Hour))
	_, err = a.authenticate(req())
	wantKind(t, err, errs.Unauthenticated)
	if n := len(a.fake.Calls()); n != 1 {
		t.Errorf("an expired token read the account (%d statements)", n)
	}
}

func TestAuthenticate_SkewBoundary(t *testing.T) {
	a := newAuthRig(t)
	p := a.validToken()
	p.Issued = a.clk.Now().Add(identity.ClockSkew).Unix() // exactly at the limit: accepted
	p.Expires = a.clk.Now().Add(time.Hour).Unix()
	_, err := a.authenticate(requestWithCookie(t, identity.CookieName, p.encode(sessionKeyText)))
	wantKind(t, err, errs.Unauthenticated) // unknown account on the fake
	if n := len(a.fake.Calls()); n != 1 {
		t.Fatalf("token issued at now+skew was rejected before the state read (%d statements)", n)
	}
}

// F6
func TestAuthenticate_UnknownAccount_Unauthenticated(t *testing.T) {
	a := newAuthRig(t)
	person, err := a.authenticate(requestWithCookie(t, identity.CookieName, a.validToken().encode(sessionKeyText)))
	wantKind(t, err, errs.Unauthenticated)
	if !person.IsZero() {
		t.Error("person set for an unknown account")
	}
	if got := a.measured(metricRejected, map[string]string{"reason": "unknown"}); got != 1 {
		t.Errorf("sessions_rejected{unknown} = %v, want 1", got)
	}
	calls := a.fake.Calls()
	if len(calls) != 1 || calls[0].InTx {
		t.Errorf("want exactly one statement outside a transaction, got %+v", calls)
	}
}

// F7
func TestAuthenticate_StoreUnavailable_PassesKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errDown, errs.Unavailable},
		{"timeout", errTimeout, errs.Timeout},
		{"canceled", errs.E("test.canceled", errs.Canceled, errDown), errs.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fdb := newFaultDB(dbtest.NewFake())
			fdb.set(func(f *faultDB) { f.readErr = tc.err })
			r := newRig(t, fdb)
			_, person, err := r.svc.Authenticate(from(ctxT(t), "198.51.100.7"),
				requestWithCookie(t, identity.CookieName, validParts(model.NewID[model.AccountID](), r.clk.Now()).encode(sessionKeyText)))
			wantKind(t, err, tc.kind)
			if !person.IsZero() {
				t.Error("person set on a store failure")
			}
		})
	}
}

// F7b
func TestAuthenticate_DevCookieIgnoredWithoutAllowPlaintext(t *testing.T) {
	a := newAuthRig(t)
	ctx := httpxHTTP(ctxT(t))
	req := requestWithCookie(t, identity.DevCookieName, a.validToken().encode(sessionKeyText))
	_, person, err := a.svc.Authenticate(ctx, req)
	wantNoErr(t, err)
	if !person.IsZero() {
		t.Error("dev cookie honoured without AllowPlaintext")
	}
	if n := len(a.fake.Calls()); n != 0 {
		t.Errorf("%d statements for an ignored cookie", n)
	}
}

func TestAuthenticate_DevCookieReadUnderAllowPlaintextOverHTTP(t *testing.T) {
	a := newAuthRig(t, withPlaintext)
	req := requestWithCookie(t, identity.DevCookieName, a.validToken().encode(sessionKeyText))
	_, _, err := a.svc.Authenticate(httpxHTTP(ctxT(t)), req)
	wantKind(t, err, errs.Unauthenticated) // unknown account on the fake: the cookie was read
	if n := len(a.fake.Calls()); n != 1 {
		t.Errorf("dev cookie not read under AllowPlaintext (%d statements)", n)
	}
}

func TestAuthenticate_DevCookieMalformedUnderAllowPlaintext_Unauthenticated(t *testing.T) {
	a := newAuthRig(t, withPlaintext)
	_, _, err := a.svc.Authenticate(httpxHTTP(ctxT(t)), requestWithCookie(t, identity.DevCookieName, "garbage"))
	wantKind(t, err, errs.Unauthenticated)
}

func TestAuthenticate_ProductionCookiePreferredOverDev(t *testing.T) {
	a := newAuthRig(t, withPlaintext)
	req := requestWithCookie(t, identity.CookieName, "garbage")
	req.AddCookie(reqCookie(identity.DevCookieName, a.validToken().encode(sessionKeyText)))
	_, _, err := a.svc.Authenticate(httpxHTTP(ctxT(t)), req)
	wantKind(t, err, errs.Unauthenticated)
	if n := len(a.fake.Calls()); n != 0 {
		t.Errorf("the dev cookie was consulted although the production cookie was present (%d statements)", n)
	}
}

func TestAuthenticate_ProductionCookieReadOverHTTPWithoutAllowPlaintext(t *testing.T) {
	a := newAuthRig(t)
	req := requestWithCookie(t, identity.CookieName, a.validToken().encode(sessionKeyText))
	_, _, err := a.svc.Authenticate(httpxHTTP(ctxT(t)), req)
	wantKind(t, err, errs.Unauthenticated)
	if n := len(a.fake.Calls()); n != 1 {
		t.Errorf("production cookie not read (%d statements)", n)
	}
}

// F61
func TestAuthenticate_IgnoresAuthorizationHeader(t *testing.T) {
	a := newAuthRig(t)
	req := requestWithCookie(t, "", "")
	req.Header.Set("Authorization", "Bearer "+a.validToken().encode(sessionKeyText))
	ctx, person, err := a.svc.Authenticate(from(ctxT(t), "198.51.100.7"), req)
	wantNoErr(t, err)
	if !person.IsZero() {
		t.Error("bearer token honoured")
	}
	if _, ok := identity.SessionFrom(ctx); ok {
		t.Error("session from an Authorization header")
	}
	if n := len(a.fake.Calls()); n != 0 {
		t.Errorf("%d statements", n)
	}
}

func TestAuthenticate_SafeForConcurrentUse(t *testing.T) {
	a := newAuthRig(t)
	tok := a.validToken().encode(sessionKeyText)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := a.svc.Authenticate(from(ctxT(t), "198.51.100.7"), requestWithCookie(t, identity.CookieName, tok))
			if !errs.Is(err, errs.Unauthenticated) {
				t.Errorf("err = %v", err)
			}
		}()
	}
	wg.Wait()
}
