//go:build integration

package identity_test

// The forgotten-password flow against a real, migrated database: plan/design/identity.md,
// "Amendment 2026-10-19 (D39)" (F69-F91) and the amendment "reset requests are uniform when
// e-mail is off". Every address, token and password is invented.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const (
	actResetRequested = "identity.password.reset-requested"
	actReset          = "identity.password.reset"
	tplPasswordReset  = "identity.password-reset"
)

// resetRig is a pool rig with a notify fake.
type resetRig struct {
	*prig
	n *notifytest.Fake
}

func newResetRig(t *testing.T, mods ...func(*identity.Options)) *resetRig {
	t.Helper()
	n := notifytest.NewFake("identity")
	p := newPoolRig(t, append([]func(*identity.Options){withNotify(n)}, mods...)...)
	return &resetRig{prig: p, n: n}
}

type resetRow struct {
	ID, Account [16]byte
	Hash        []byte
	Expires     time.Time
	Created     time.Time
	Used        *time.Time
}

// rows reads core.password_reset straight from the table.
func (r *resetRig) rows() []resetRow {
	r.t.Helper()
	rs, err := r.pool.Query(ctxT(r.t), "SELECT id, account_id, token_hash, expires_at, created_at, used_at FROM core.password_reset ORDER BY created_at, id")
	if err != nil {
		r.t.Fatalf("reading core.password_reset: %v", err)
	}
	defer rs.Close()
	var out []resetRow
	for rs.Next() {
		var x resetRow
		if err := rs.Scan(&x.ID, &x.Account, &x.Hash, &x.Expires, &x.Created, &x.Used); err != nil {
			r.t.Fatalf("scanning core.password_reset: %v", err)
		}
		out = append(out, x)
	}
	if err := rs.Err(); err != nil {
		r.t.Fatalf("core.password_reset rows: %v", err)
	}
	return out
}

// request asks for a reset from a fresh client address.
func (r *resetRig) request(email model.Email) error {
	r.t.Helper()
	return keep(r.svc.RequestPasswordReset(from(ctxT(r.t), nextIP()), email))
}

// complete redeems token.
func (r *resetRig) complete(token, pw string) error {
	r.t.Helper()
	return keep(r.svc.CompletePasswordReset(from(ctxT(r.t), nextIP()), token, pw))
}

// lastToken returns the token of the newest message the fake notifier recorded.
func (r *resetRig) lastToken() string {
	r.t.Helper()
	sent := r.n.Sent()
	if len(sent) == 0 {
		r.t.Fatal("no message was sent")
	}
	return tokenIn(r.t, sent[len(sent)-1])
}

// requestToken requests a reset for u and returns the emailed token.
func (r *resetRig) requestToken(u user) string {
	r.t.Helper()
	before := len(r.n.Sent())
	wantNoErr(r.t, r.request(u.email))
	if len(r.n.Sent()) != before+1 {
		r.t.Fatalf("%d messages after the request, want %d", len(r.n.Sent()), before+1)
	}
	return r.lastToken()
}

// superSession is a superuser principal for the admin routes.
func (r *resetRig) superSession() identity.Session {
	return identity.Session{ID: model.NewID[identity.SessionID](), Account: newAccountID(), Person: newPersonID(),
		Superuser: true, IssuedAt: r.clk.Now(), ExpiresAt: r.clk.Now().Add(time.Hour)}
}

func (r *resetRig) disable(u user) {
	r.t.Helper()
	ctx := identity.WithSession(from(ctxT(r.t), nextIP()), r.superSession())
	wantNoErr(r.t, r.admin().DisableAccount(ctx, u.sess.Account))
}

func (r *resetRig) hashMatches(token string, hash []byte) bool {
	sum := sha256.Sum256([]byte(token))
	if bytes.Equal(sum[:], hash) {
		return true
	}
	if raw, err := base64.RawURLEncoding.DecodeString(token); err == nil {
		sum = sha256.Sum256(raw)
		return bytes.Equal(sum[:], hash)
	}
	return false
}

// ---- F74 -------------------------------------------------------------------------------

// F74, and "the token is never stored in plaintext".
func TestRequestPasswordReset_Known_StoresHashAndSends(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	wantNoErr(t, r.request(u.email))

	sent := r.n.Sent()
	if len(sent) != 1 {
		t.Fatalf("%d messages, want 1", len(sent))
	}
	m := sent[0]
	if m.Template != tplPasswordReset {
		t.Errorf("template = %q", m.Template)
	}
	if len(m.To) != 1 || m.To[0] != u.sess.Person {
		t.Errorf("To = %v, want the account's person", m.To)
	}
	if got := m.Vars["lifetime_minutes"]; got != "60" {
		t.Errorf("lifetime_minutes = %q, want %q (a bare number, D40)", got, "60")
	}
	if _, old := m.Vars["lifetime"]; old {
		t.Error("the old lifetime var is still sent")
	}
	if !strings.HasPrefix(m.Link, "/accounts/password-reset?token=") {
		t.Errorf("link = %q", m.Link)
	}
	if strings.Contains(m.Link, "http") {
		t.Errorf("link is not a path: %q", m.Link)
	}
	token := tokenIn(t, m)
	if len(token) < 20 || strings.ContainsAny(token, "=+/ ") {
		t.Errorf("token %q is not unpadded base64url of enough bytes", token)
	}

	rows := r.rows()
	if len(rows) != 1 {
		t.Fatalf("%d token rows, want 1", len(rows))
	}
	row := rows[0]
	if row.Account != [16]byte(u.sess.Account) {
		t.Error("the row belongs to another account")
	}
	if len(row.Hash) != 32 {
		t.Errorf("stored hash is %d bytes, want 32", len(row.Hash))
	}
	if string(row.Hash) == token || bytes.Contains(row.Hash, []byte(token)) {
		t.Error("the token is stored in plaintext")
	}
	if !r.hashMatches(token, row.Hash) {
		t.Error("the stored value is not the SHA-256 of the token")
	}
	if want := r.clk.Now().Add(time.Hour); !row.Expires.Equal(want) {
		t.Errorf("expires_at = %v, want %v", row.Expires, want)
	}
	if row.Used != nil {
		t.Error("used_at set on a fresh token")
	}
	if want := "password-reset:" + uuidText(row.ID); m.Key != want {
		t.Errorf("Key = %q, want %q", m.Key, want)
	}

	e := r.only(actResetRequested)
	if e.Outcome != audit.Succeeded || e.Subject != u.sess.Person {
		t.Errorf("entry = %+v", e)
	}
	r.noLeak(token, string(u.email), m.Link, pwGood)

	// the whole row, rendered as text, never holds the token
	var dump string
	wantNoErr(t, keep(r.pool.QueryRow(ctxT(t), "SELECT coalesce(string_agg(t::text, '|'), '') FROM core.password_reset t").Scan(&dump)))
	if strings.Contains(dump, token) {
		t.Error("the token appears in the table")
	}
}

func uuidText(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// The lifetime follows the policy at mint time and is formatted in whole minutes.
func TestRequestPasswordReset_LifetimeFollowsPolicy(t *testing.T) {
	ps := newPolicySource()
	ps.set(func(p *identity.Policy) { p.PasswordResetLifetime = 90 * time.Minute })
	r := newResetRig(t, withPolicy(ps))
	u := r.signUp("ada.quill")
	wantNoErr(t, r.request(u.email))
	if got := r.n.Sent()[0].Vars["lifetime_minutes"]; got != "90" {
		t.Errorf("lifetime = %q", got)
	}
	if want := r.clk.Now().Add(90 * time.Minute); !r.rows()[0].Expires.Equal(want) {
		t.Errorf("expires_at = %v, want %v", r.rows()[0].Expires, want)
	}
	// a changed lifetime applies to tokens minted after the change
	ps.set(func(p *identity.Policy) { p.PasswordResetLifetime = 10 * time.Minute })
	wantNoErr(t, r.clk.Advance(defaultCooldown))
	wantNoErr(t, r.request(u.email))
	if got := r.n.Sent()[1].Vars["lifetime_minutes"]; got != "10" {
		t.Errorf("lifetime = %q", got)
	}
}

// ---- F73 and no enumeration ------------------------------------------------------------

// accountsForEnumeration returns a known, a disabled, a superuser and an unknown address.
func accountsForEnumeration(r *resetRig) (known, disabled, super, unknown model.Email) {
	k := r.signUp("known.person")
	d := r.signUp("disabled.person")
	r.disable(d)
	r.superuser("root.person@example.org")
	return k.email, d.email, "root.person@example.org", "nobody.here@example.org"
}

// F73
func TestRequestPasswordReset_UnknownDisabledSuperuser_SilentNil(t *testing.T) {
	r := newResetRig(t)
	_, disabled, super, unknown := accountsForEnumeration(r)
	for name, e := range map[string]model.Email{"unknown": unknown, "disabled": disabled, "superuser": super} {
		t.Run(name, func(t *testing.T) {
			sentBefore, deniedBefore := len(r.n.Sent()), deniedCount(r.rig)
			start := time.Now()
			wantNoErr(t, r.request(e))
			if time.Since(start) < 3*time.Millisecond {
				t.Error("answered without spending a password hash")
			}
			if len(r.n.Sent()) != sentBefore {
				t.Error("a message was sent")
			}
			if deniedCount(r.rig) != deniedBefore+1 {
				t.Error("no Denied entry was recorded")
			}
			if len(r.rows()) != 0 {
				t.Error("a token row exists")
			}
		})
	}
	r.noLeak(string(unknown), string(disabled), string(super))
}

func deniedCount(r *rig) int {
	n := 0
	for _, e := range r.rec.Recorded() {
		if e.Outcome == audit.Denied && string(e.Action) == actResetRequested {
			n++
		}
	}
	return n
}

// No account enumeration: with e-mail on, a known, an unknown, a disabled and a superuser
// address answer the same status, body and headers on the route, and nil from the service.
func TestRequestPasswordReset_NoEnumeration_EmailOn(t *testing.T) {
	r := newResetRig(t)
	known, disabled, super, unknown := accountsForEnumeration(r)
	rt := handlerFor(t, r.svc, "POST", "/password-reset")
	var first *httptest.ResponseRecorder
	for name, e := range map[string]model.Email{"known": known, "disabled": disabled, "superuser": super, "unknown": unknown} {
		w := callPlain(t, rt, from(ctxT(t), nextIP()), emailJSON(string(e)))
		if w.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d, want 202", name, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("%s: body = %q, want empty", name, w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 { //nolint:bodyclose // a recorder's body is a buffer
			t.Errorf("%s: a cookie was set", name)
		}
		if first == nil {
			first = w
			continue
		}
		sameResponse(t, name, first, w)
	}
	// only the known address produced a message
	if n := len(r.n.Sent()); n != 1 {
		t.Errorf("%d messages for four requests, want 1", n)
	}
}

// With e-mail off, all four addresses answer Unsupported / 501 alike with no store work.
func TestRequestPasswordReset_NoEnumeration_EmailOff(t *testing.T) {
	r := newResetRig(t)
	known, disabled, super, unknown := accountsForEnumeration(r)
	r.n.SetConfigured(false)
	rt := handlerFor(t, r.svc, "POST", "/password-reset")
	r.fdb.reset()
	entries := len(r.rec.Recorded())
	var first *httptest.ResponseRecorder
	for name, e := range map[string]model.Email{"known": known, "disabled": disabled, "superuser": super, "unknown": unknown} {
		wantKind(t, r.request(e), errs.Unsupported)
		w := callPlain(t, rt, from(ctxT(t), nextIP()), emailJSON(string(e)))
		if w.Code != http.StatusNotImplemented {
			t.Fatalf("%s: status = %d, want 501", name, w.Code)
		}
		if first == nil {
			first = w
			continue
		}
		sameResponse(t, name, first, w)
	}
	if reads, writes, txs := r.fdb.counts(); reads+writes+txs != 0 {
		t.Errorf("the store was used while e-mail is off: reads %d writes %d txs %d", reads, writes, txs)
	}
	if len(r.rec.Recorded()) != entries {
		t.Error("an entry was recorded while e-mail is off")
	}
	if len(r.n.Sent()) != 0 || len(r.rows()) != 0 {
		t.Error("a message or token exists while e-mail is off")
	}
}

// Equal work, structurally: every request that passes the probe verifies the dummy hash
// exactly once, whatever the address and whatever the outcome (known, unknown, disabled,
// superuser, F72, read-only). The count is read through a hook, not the clock.
func TestRequestPasswordReset_DummyVerifiedOncePerRequest(t *testing.T) {
	n := notifytest.NewFake("identity")
	probe := newProbe(n)
	p := newPoolRig(t, withNotify(probe))
	r := &resetRig{prig: p, n: n}
	known, disabled, super, unknown := accountsForEnumeration(r)
	var calls atomic.Int64
	identity.SetDummyHook(r.svc, func() { calls.Add(1) })

	type tc struct {
		name  string
		email model.Email
		setup func()
		ro    bool // the request must fail Unavailable wrapping db.ErrReadOnly
	}
	cases := []tc{
		{"known", known, nil, false},
		{"unknown", unknown, nil, false},
		{"disabled", disabled, nil, false},
		{"superuser", super, nil, false},
		{"known, inside the cooldown", known, nil, false},
		{"F72 send unsupported after the probe", known, func() {
			wantNoErr(t, r.clk.Advance(defaultCooldown)) // past the cooldown, so the send is reached
			probe.set(func(p *probeNotifier) { p.sendErr = errs.E("test.send", errs.Unsupported, notify.ErrNotConfigured) })
		}, false},
		{"read-only, known", known, func() {
			probe.set(func(p *probeNotifier) { p.sendErr = nil })
			r.fdb.set(func(f *faultDB) { f.ro = true })
		}, true},
		{"read-only, unknown", unknown, nil, true},
	}
	for _, c := range cases {
		if c.setup != nil {
			c.setup()
		}
		before := calls.Load()
		err := r.request(c.email)
		if c.ro {
			wantReadOnly(t, err)
		} else {
			wantNoErr(t, err)
		}
		if got := calls.Load() - before; got != 1 {
			t.Errorf("%s: %d dummy verifications, want exactly 1", c.name, got)
		}
	}
	// e-mail off: refused before the probe's successor step, so no work at all.
	r.fdb.set(func(f *faultDB) { f.ro = false })
	probe.set(func(p *probeNotifier) { p.configured = false })
	before := calls.Load()
	wantKind(t, r.request(known), errs.Unsupported)
	if got := calls.Load() - before; got != 0 {
		t.Errorf("e-mail off: %d dummy verifications, want 0 (refused before any work)", got)
	}
}

// ---- F72 -------------------------------------------------------------------------------

// F72 (replaced): Configured was true, Send answers Unsupported: nil, nothing committed, no
// address in any log.
func TestRequestPasswordReset_UnconfiguredAfterProbe_SilentNil(t *testing.T) {
	n := notifytest.NewFake("identity")
	probe := newProbe(n)
	probe.set(func(p *probeNotifier) { p.sendErr = errs.E("test.send", errs.Unsupported, notify.ErrNotConfigured) })
	r := newPoolRig(t, withNotify(probe))
	u := r.signUp("ada.quill")
	entries := len(r.rec.Recorded())
	wantNoErr(t, keep(r.svc.RequestPasswordReset(from(ctxT(t), nextIP()), u.email)))
	rr := &resetRig{prig: r, n: n}
	if len(rr.rows()) != 0 {
		t.Error("a token row was committed")
	}
	if len(n.Sent()) != 0 {
		t.Error("a message was recorded")
	}
	if len(r.rec.Recorded()) != entries {
		t.Errorf("an entry was recorded: %v", actionsOf(r.rec.Recorded()[entries:]))
	}
	r.noLeak(string(u.email))
}

// ---- F75 -------------------------------------------------------------------------------

// F75: only the newest token redeems; the older link answers invalid.
func TestRequestPasswordReset_Twice_OnlyNewestValid(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	first := r.requestToken(u)
	wantNoErr(t, r.clk.Advance(defaultCooldown))
	second := r.requestToken(u)
	if first == second {
		t.Fatal("two requests minted the same token")
	}
	if n := len(r.rows()); n != 1 {
		t.Fatalf("%d token rows after two requests, want 1", n)
	}
	wantField(t, r.complete(first, pwNew), "token", "invalid")
	wantNoErr(t, r.complete(second, pwNew))
}

// Used and expired rows of the account are purged by its next request.
func TestRequestPasswordReset_PurgesUsedAndExpired(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	wantNoErr(t, r.complete(r.requestToken(u), pwNew))
	if rows := r.rows(); len(rows) != 1 || rows[0].Used == nil {
		t.Fatalf("rows after a redemption = %+v", rows)
	}
	wantNoErr(t, r.clk.Advance(defaultCooldown))
	_ = r.requestToken(u)
	if rows := r.rows(); len(rows) != 1 || rows[0].Used != nil {
		t.Errorf("rows after the next request = %+v, want one unused", rows)
	}
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	_ = r.requestToken(u)
	if n := len(r.rows()); n != 1 {
		t.Errorf("%d rows after an expired one was replaced, want 1", n)
	}
}

// Another account's token survives this account's request.
func TestRequestPasswordReset_OtherAccountsTokenUntouched(t *testing.T) {
	r := newResetRig(t)
	a, b := r.signUp("ada.quill"), r.signUp("bram.stone")
	ta := r.requestToken(a)
	_ = r.requestToken(b)
	if n := len(r.rows()); n != 2 {
		t.Fatalf("%d rows, want 2", n)
	}
	wantNoErr(t, r.complete(ta, pwNew))
}

// ---- F76, F77, F78 ---------------------------------------------------------------------

// F76
func TestRequestPasswordReset_SendFails_NothingCommitted(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errDown, errs.Unavailable},
		{"timeout", errTimeout, errs.Timeout},
		{"canceled", errs.E("test.send", errs.Canceled, errors.New("gave up")), errs.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newResetRig(t)
			u := r.signUp("ada.quill")
			entries := len(r.rec.Recorded())
			r.n.Fail(tc.err)
			wantKind(t, r.request(u.email), tc.kind)
			if n := len(r.rows()); n != 0 {
				t.Errorf("%d token rows survive a failed send", n)
			}
			if len(r.n.Sent()) != 0 {
				t.Error("a message was recorded")
			}
			if len(r.rec.Recorded()) != entries {
				t.Error("an entry was recorded for a failed request")
			}
			// the failure leaves the account able to ask again
			r.n.Fail(nil)
			wantNoErr(t, r.request(u.email))
			if len(r.rows()) != 1 {
				t.Error("the retry did not mint a token")
			}
		})
	}
}

// A failed send does not delete the earlier token: the replacement is one transaction.
func TestRequestPasswordReset_SendFails_EarlierTokenStillValid(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	old := r.requestToken(u)
	wantNoErr(t, r.clk.Advance(defaultCooldown))
	r.n.Fail(errDown)
	wantKind(t, r.request(u.email), errs.Unavailable)
	r.n.Fail(nil)
	wantNoErr(t, r.complete(old, pwNew))
}

// F77
func TestRequestPasswordReset_AuditFails_RollsBack(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	r.rec.Fail(errDown)
	wantKind(t, r.request(u.email), errs.Unavailable)
	if n := len(r.rows()); n != 0 {
		t.Errorf("%d token rows survive an audit failure", n)
	}
	if len(r.n.Sent()) != 0 {
		t.Error("a message was recorded although the transaction rolled back")
	}
}

// F78: a known and an unknown address alike.
func TestRequestPasswordReset_ReadOnly_Unavailable(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	r.fdb.set(func(f *faultDB) { f.ro = true })
	wantReadOnly(t, r.request(u.email))
	wantReadOnly(t, r.request("nobody.at.all@example.org"))
	if len(r.rows()) != 0 || len(r.n.Sent()) != 0 {
		t.Error("something was written on a read-only copy")
	}
	// the route answers 503 for both
	for _, e := range []string{string(u.email), "nobody.at.all@example.org"} {
		w, _ := call(t, handlerFor(t, r.svc, "POST", "/password-reset"), from(ctxT(t), nextIP()), emailJSON(e))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	}
}

// A Send that cannot queue the e-mail for this account (Invalid, NotFound: the account has no
// person row, say) is silent: nil, nothing committed, one warn line without the address.
func TestRequestPasswordReset_SendRefused_SilentNil(t *testing.T) {
	for name, kind := range map[string]errs.Kind{"invalid": errs.Invalid, "not found": errs.NotFound} {
		t.Run(name, func(t *testing.T) {
			n := notifytest.NewFake("identity")
			probe := newProbe(n)
			probe.set(func(p *probeNotifier) { p.sendErr = errs.E("test.send", kind, errors.New("no such person")) })
			p := newPoolRig(t, withNotify(probe))
			r := &resetRig{prig: p, n: n}
			u := r.signUp("ada.quill")
			entries := len(r.rec.Recorded())
			wantNoErr(t, r.request(u.email))
			if len(r.rows()) != 0 || len(n.Sent()) != 0 {
				t.Error("a token or a message was committed")
			}
			if len(r.rec.Recorded()) != entries {
				t.Errorf("an entry was recorded: %v", actionsOf(r.rec.Recorded()[entries:]))
			}
			r.noLeak(string(u.email))
		})
	}
}

// Request and Complete take the account row before the reset rows, in that order, so racing
// them for one account never deadlocks (the server would abort one with 40P01, which would
// show up as Unavailable): every call ends nil or Invalid(token).
func TestResetRequestAndComplete_Racing_NoDeadlock(t *testing.T) {
	ps := newPolicySource() // room for many requests for one address
	ps.set(func(p *identity.Policy) { p.LoginFailureLimit, p.ClientFailureLimit = 100, 1000 })
	r := newResetRig(t, func(o *identity.Options) { o.Settings = ps })
	u := r.signUp("ada.quill")
	for round := range 8 {
		// each round spends 5 of the address's reset requests: move past the window (and so the cooldown)
		wantNoErr(t, r.clk.Advance(16*time.Minute))
		tok := r.requestToken(u)
		// past the cooldown, so the concurrent requests really race the redemption
		wantNoErr(t, r.clk.Advance(defaultCooldown))
		sentBefore := len(r.n.Sent())
		const n = 4
		var wg sync.WaitGroup
		results := make([]error, 2*n)
		start := make(chan struct{})
		for i := range n {
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				results[i] = r.svc.RequestPasswordReset(from(ctxT(t), nextIP()), u.email)
			}()
			go func() {
				defer wg.Done()
				<-start
				results[n+i] = r.svc.CompletePasswordReset(from(ctxT(t), nextIP()), tok, fmt.Sprintf("%s-%d-%d", pwNew, round, i))
			}()
		}
		close(start)
		wg.Wait()
		for i, err := range results {
			if err == nil {
				continue
			}
			if i < n {
				t.Fatalf("round %d: request %d failed: %v", round, i, err)
			}
			wantField(t, err, "token", "invalid")
		}
		// the four requests are one cooldown window: exactly one of them sent
		if got := len(r.n.Sent()) - sentBefore; got != 1 {
			t.Fatalf("round %d: %d new messages from %d concurrent requests, want exactly 1", round, got, n)
		}
	}
}

// An admin disables the account after the request looked it up and before its Write
// transaction: the same 202 (nil), no token, no e-mail.
func TestRequestPasswordReset_DisabledBetweenLookupAndTransaction_NoEmail(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	var hook *hookDB
	svc := r.other(func(o *identity.Options) {
		hook = &hookDB{DB: o.DB}
		o.DB = hook
	})
	hook.before = func() { r.disable(u) }
	wantNoErr(t, keep(svc.RequestPasswordReset(from(ctxT(t), nextIP()), u.email)))
	if hook.before != nil {
		t.Fatal("the disable step never ran: the test did not exercise the window")
	}
	if len(r.n.Sent()) != 0 {
		t.Errorf("%d messages sent to a disabled account", len(r.n.Sent()))
	}
	if len(r.rows()) != 0 {
		t.Error("a token was minted for a disabled account")
	}
	r.noLeak(string(u.email))
}

// ---- Complete: F79-F86 -----------------------------------------------------------------

// F79: a used token and a replay answer invalid.
func TestCompletePasswordReset_Used_Invalid(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	wantNoErr(t, r.complete(tok, pwNew))
	wantField(t, r.complete(tok, pwOther), "token", "invalid")
}

// F80
func TestCompletePasswordReset_Expired_Invalid(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	wantNoErr(t, r.clk.Advance(time.Hour-time.Second))
	// one second before expiry the token still works (checked on a second account so this
	// one stays unredeemed)
	v := r.signUp("bram.stone")
	vt := r.requestToken(v)
	wantNoErr(t, r.clk.Advance(time.Second))
	// expires_at <= now: exactly at expiry is expired
	wantField(t, r.complete(tok, pwNew), "token", "expired")
	if rows := r.rows(); len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, row := range r.rows() {
		if row.Used != nil {
			t.Error("an expired token was marked used")
		}
	}
	wantNoErr(t, r.complete(vt, pwNew)) // a token minted later is still live
	// the old password still signs in: an expired token changed nothing
	_, err := r.signIn(r.svc, u)
	wantNoErr(t, err)
}

// F80: the last second of the lifetime still redeems.
func TestCompletePasswordReset_JustBeforeExpiry_Succeeds(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	wantNoErr(t, r.clk.Advance(time.Hour-time.Second))
	wantNoErr(t, r.complete(tok, pwNew))
}

// F81
func TestCompletePasswordReset_Disabled_Invalid(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	r.disable(u)
	wantField(t, r.complete(tok, pwNew), "token", "invalid")
}

// F82: the token stays valid after a refused password.
func TestCompletePasswordReset_WeakPassword_Invalid(t *testing.T) {
	tests := []struct{ name, pw, code string }{
		{"too short", pwShort, "too_short"},
		{"too long", strings.Repeat("a", identity.MaxPasswordBytes+1), "too_long"},
		{"common", pwCommon, "common"},
	}
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantField(t, r.complete(tok, tc.pw), "new_password", tc.code)
			if rows := r.rows(); len(rows) != 1 || rows[0].Used != nil {
				t.Errorf("the token was consumed by a refused password: %+v", rows)
			}
		})
	}
	wantNoErr(t, r.complete(tok, pwNew))
}

// F83, session generation bump, no automatic sign-in.
func TestCompletePasswordReset_Success_EndsSessions(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	other := r.signUp("bram.stone")
	oldCookie := r.cookie(u.email, u.pw)
	otherCookie := r.cookie(other.email, other.pw)
	r.wantAuthenticated(oldCookie, u.sess.Person)
	before := r.rowOf(u.sess.Person)
	entries := len(r.rec.Recorded())

	tok := r.requestToken(u)
	wantNoErr(t, r.complete(tok, pwNew))

	after := r.rowOf(u.sess.Person)
	if after.Generation != before.Generation+1 {
		t.Errorf("session_generation = %d, want %d", after.Generation, before.Generation+1)
	}
	if after.PasswordHash == before.PasswordHash || strings.Contains(after.PasswordHash, pwNew) {
		t.Error("the password hash did not change, or holds the password")
	}
	if after.PasswordChangeRequired {
		t.Error("password_change_required not cleared")
	}
	if rows := r.rows(); len(rows) != 1 || rows[0].Used == nil {
		t.Errorf("rows = %+v, want one used", rows)
	}
	// every session of the account ended; another account's did not
	r.wantRejected(oldCookie)
	r.wantAuthenticated(otherCookie, other.sess.Person)
	// the old password no longer signs in; the new one does
	_, err := r.signIn(r.svc, u)
	wantKind(t, err, errs.Unauthenticated)
	u2 := u
	u2.pw = pwNew
	_, err = r.signIn(r.svc, u2)
	wantNoErr(t, err)
	// exactly one entry for the redemption
	if got := len(r.entries2(actReset)); got != 1 {
		t.Errorf("%d %s entries, want 1", got, actReset)
	}
	if r.count(actResetRequested) != 1 {
		t.Error("the request entry is missing")
	}
	_ = entries
	r.noLeak(tok, pwNew, string(u.email))
}

// A reset ends a restricted (temporary-password) session as well, and clears the flag.
func TestCompletePasswordReset_RestrictedSession_EndsAndClearsFlag(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	admin := identity.WithSession(from(ctxT(t), nextIP()), r.superSession())
	temp, err := r.admin().SetTemporaryPassword(admin, u.sess.Account)
	wantNoErr(t, err)
	restricted := u
	restricted.pw = temp.Value()
	iss, err := r.signIn(r.svc, restricted)
	wantNoErr(t, err)
	if !iss.Session.PasswordChangeRequired {
		t.Fatal("a temporary password did not give a restricted session")
	}
	if !r.rowOf(u.sess.Person).PasswordChangeRequired {
		t.Fatal("flag not set")
	}
	tok := r.requestToken(u)
	wantNoErr(t, r.complete(tok, pwNew))
	if r.rowOf(u.sess.Person).PasswordChangeRequired {
		t.Error("password_change_required not cleared")
	}
	u.pw = pwNew
	wantNoErr(t, keep2(r.signIn(r.svc, u)))
}

func keep2(_ identity.Issued, err error) error { return err }

// No automatic sign-in: Complete issues nothing, the route answers 204 with no body and no
// cookie, and an anonymous request is still unauthenticated afterwards.
func TestCompletePasswordReset_NoAutomaticSignIn(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	w, f := call(t, handlerFor(t, r.svc, "POST", "/password-reset/complete"), from(ctxT(t), nextIP()), completeJSON(tok, pwNew))
	if f.count() != 0 {
		t.Fatalf("handler failed: %v", f.last())
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
	if h := w.Header().Get("Set-Cookie"); h != "" {
		t.Error("a cookie was set")
	}
	if len(setCookies(w)) != 0 {
		t.Error("a cookie was set")
	}
	ctx, person, err := r.auth(nil)
	if err == nil {
		if _, ok := identity.SessionFrom(ctx); ok || !person.IsZero() {
			t.Error("an anonymous request carries a session after a reset")
		}
	}
	// and no session row appeared as a side effect of the reset itself: the next sign-in is
	// the first (the audit log has no login before it)
	if r.count(actLogin) != 0 {
		t.Error("a login was recorded by the reset")
	}
}

// F84: the same token twice, and concurrently: exactly one wins.
func TestCompletePasswordReset_Replay_Invalid(t *testing.T) {
	t.Run("sequential", func(t *testing.T) {
		r := newResetRig(t)
		u := r.signUp("ada.quill")
		tok := r.requestToken(u)
		wantNoErr(t, r.complete(tok, pwNew))
		wantField(t, r.complete(tok, pwOther), "token", "invalid")
		// the second attempt changed nothing
		u.pw = pwNew
		wantNoErr(t, keep2(r.signIn(r.svc, u)))
	})
	t.Run("concurrent", func(t *testing.T) {
		r := newResetRig(t)
		u := r.signUp("ada.quill")
		tok := r.requestToken(u)
		const n = 6
		var wg sync.WaitGroup
		results := make([]error, n)
		start := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results[i] = r.svc.CompletePasswordReset(from(ctxT(t), nextIP()), tok, fmt.Sprintf("%s-%d", pwNew, i))
			}()
		}
		close(start)
		wg.Wait()
		wins := 0
		for _, err := range results {
			if err == nil {
				wins++
			} else {
				wantField(t, err, "token", "invalid")
			}
		}
		if wins != 1 {
			t.Errorf("%d redemptions succeeded, want exactly 1", wins)
		}
		if got := len(r.entries2(actReset)); got != 1 {
			t.Errorf("%d %s entries, want 1", got, actReset)
		}
		if g := r.rowOf(u.sess.Person).Generation; g != 1 {
			t.Errorf("generation = %d, want 1 (bumped once)", g)
		}
	})
}

// hookDB runs before once, just before the first Write transaction, then delegates.
type hookDB struct {
	db.DB
	mu     sync.Mutex
	before func()
}

func (h *hookDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	h.mu.Lock()
	f := h.before
	if mode == db.Write {
		h.before = nil
	}
	h.mu.Unlock()
	if f != nil && mode == db.Write {
		f()
	}
	return h.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

// An admin disables the account after Complete looked the token up and before its
// transaction took the account lock: the reset is refused (token: invalid), the password
// stays, the token stays unspent and no reset entry exists.
func TestCompletePasswordReset_DisabledBetweenLookupAndCommit_Invalid(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	before := r.rowOf(u.sess.Person)
	var hook *hookDB
	svc := r.other(func(o *identity.Options) {
		hook = &hookDB{DB: o.DB}
		o.DB = hook
	})
	hook.before = func() { r.disable(u) }
	wantField(t, keep(svc.CompletePasswordReset(from(ctxT(t), nextIP()), tok, pwNew)), "token", "invalid")
	if hook.before != nil {
		t.Fatal("the disable step never ran: the test did not exercise the window")
	}
	after := r.rowOf(u.sess.Person)
	// disabling ends the sessions (one bump); the refused reset adds none and writes no hash
	if after.PasswordHash != before.PasswordHash || after.Generation != before.Generation+1 {
		t.Errorf("password changed %v, generation %d -> %d (want one bump, from the disable)",
			after.PasswordHash != before.PasswordHash, before.Generation, after.Generation)
	}
	if after.DisabledAt == nil {
		t.Error("the account is not disabled")
	}
	if rows := r.rows(); len(rows) != 1 || rows[0].Used != nil {
		t.Errorf("the token was spent: %+v", rows)
	}
	if got := len(r.entries2(actReset)); got != 0 {
		t.Errorf("%d %s entries, want 0", got, actReset)
	}
}

// F85
func TestCompletePasswordReset_ReadOnly_Unavailable(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	r.fdb.set(func(f *faultDB) { f.ro = true })
	wantReadOnly(t, r.complete(tok, pwNew))
	w, _ := call(t, handlerFor(t, r.svc, "POST", "/password-reset/complete"), from(ctxT(t), nextIP()), completeJSON(tok, pwNew))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	r.fdb.set(func(f *faultDB) { f.ro = false })
	wantNoErr(t, r.complete(tok, pwNew)) // the token is still valid
}

// F86
func TestCompletePasswordReset_AuditFails_RollsBack(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	cookie := r.cookie(u.email, u.pw)
	tok := r.requestToken(u)
	before := r.rowOf(u.sess.Person)
	r.rec.Fail(errDown)
	wantKind(t, r.complete(tok, pwNew), errs.Unavailable)
	r.rec.Fail(nil)
	after := r.rowOf(u.sess.Person)
	if after.PasswordHash != before.PasswordHash || after.Generation != before.Generation {
		t.Error("the password or the generation changed although the audit failed")
	}
	r.wantAuthenticated(cookie, u.sess.Person)
	if rows := r.rows(); len(rows) != 1 || rows[0].Used != nil {
		t.Errorf("the token was consumed: %+v", rows)
	}
	wantNoErr(t, r.complete(tok, pwNew)) // the token stays valid
}

// ---- F90 -------------------------------------------------------------------------------

// F90: the token, the address, the link and the password never reach a log, span, metric,
// audit entry or error string, along the happy path and the failure paths.
func TestPasswordReset_NoSecretsInTelemetry(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("marker.person")
	addr := string(u.email)
	tok := r.requestToken(u)
	link := r.n.Sent()[0].Link
	var errStrings []string
	note := func(err error) {
		if err != nil {
			errStrings = append(errStrings, fmt.Sprint(err))
		}
	}
	note(r.complete(tok, pwShort))
	note(r.complete(tok+"x", pwNew))
	note(r.complete(tok, pwNew))
	note(r.complete(tok, pwNew)) // replay
	r.n.Fail(errDown)
	note(r.request(u.email))
	r.n.Fail(nil)
	r.n.SetConfigured(false)
	note(r.request(u.email))
	// the HTTP layer too: a request and a completion through the handlers
	r.n.SetConfigured(true)
	callPlain(t, handlerFor(t, r.svc, "POST", "/password-reset"), from(ctxT(t), nextIP()), emailJSON(addr))
	callPlain(t, handlerFor(t, r.svc, "POST", "/password-reset/complete"), from(ctxT(t), nextIP()), completeJSON(tok, pwNew))
	r.noLeak(tok, addr, link, pwNew, pwShort, "marker.person")
	for _, s := range errStrings {
		for _, secret := range []string{tok, addr, link, pwNew, pwShort} {
			if strings.Contains(s, secret) {
				t.Errorf("an error string carries a secret: %q", s)
			}
		}
	}
}

// ---- Data: the migration ---------------------------------------------------------------

// token_hash is unique and exactly 32 bytes; a cascade removes the rows with the account.
func TestPasswordReset_Table_ConstraintsAndCascade(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	_ = r.requestToken(u)
	row := r.rows()[0]
	ctx := ctxT(t)
	ins := "INSERT INTO core.password_reset (id, account_id, token_hash, expires_at, created_at) VALUES ($1, $2, $3, $4, $5)"
	now := r.clk.Now()
	if _, err := r.pool.Exec(ctx, ins, [16]byte(model.NewID[model.AccountID]()), row.Account, row.Hash, now.Add(time.Hour), now); err == nil {
		t.Error("a duplicate token_hash was accepted")
	}
	if _, err := r.pool.Exec(ctx, ins, [16]byte(model.NewID[model.AccountID]()), row.Account, []byte("short"), now.Add(time.Hour), now); err == nil {
		t.Error("a token_hash of the wrong length was accepted")
	}
	if _, err := r.pool.Exec(ctx, ins, [16]byte(model.NewID[model.AccountID]()), [16]byte(model.NewID[model.AccountID]()), bytes.Repeat([]byte{7}, 32), now.Add(time.Hour), now); err == nil {
		t.Error("a row for a missing account was accepted")
	}
	err := r.pool.Transact(ctx, db.Write, func(ctx2 context.Context) error {
		_, e := r.svc.DeletePerson(ctx2, u.sess.Person)
		return keep(e)
	})
	wantNoErr(t, err)
	if n := len(r.rows()); n != 0 {
		t.Errorf("%d token rows survive the account", n)
	}
}

// ExportPerson never includes the credential.
func TestPasswordReset_ExportPerson_OmitsTheToken(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	var out []byte
	err := r.pool.Transact(ctxT(t), db.Read, func(ctx2 context.Context) error {
		b, e := r.svc.ExportPerson(ctx2, u.sess.Person)
		out = b
		return keep(e)
	})
	wantNoErr(t, err)
	if bytesContains(out, tok) || bytesContains(out, "token_hash") {
		t.Error("the export includes reset-token material")
	}
}

func TestCompletePasswordReset_CancelledContext_Canceled(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	tok := r.requestToken(u)
	ctx, cancel := context.WithCancel(from(ctxT(t), nextIP()))
	cancel()
	wantKind(t, keep(r.svc.CompletePasswordReset(ctx, tok, pwNew)), errs.Canceled)
	wantNoErr(t, r.complete(tok, pwNew)) // the token is untouched
}

func TestRequestPasswordReset_CancelledContext_KnownAccount_Canceled(t *testing.T) {
	r := newResetRig(t)
	u := r.signUp("ada.quill")
	ctx, cancel := context.WithCancel(from(ctxT(t), nextIP()))
	cancel()
	wantKind(t, keep(r.svc.RequestPasswordReset(ctx, u.email)), errs.Canceled)
	if len(r.rows()) != 0 || len(r.n.Sent()) != 0 {
		t.Error("a cancelled request left a token or a message")
	}
}

// tokenIn returns the token carried by the Link of m ("" when absent).
func tokenIn(t testing.TB, m notify.Message) string {
	t.Helper()
	_, q, ok := strings.Cut(m.Link, "?")
	if !ok {
		t.Fatalf("link %q has no query", m.Link)
	}
	vals, err := url.ParseQuery(q)
	if err != nil {
		t.Fatalf("link query: %v", err)
	}
	return vals.Get("token")
}
