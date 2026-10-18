//go:build integration

package identity_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const tempAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// adminFixture is a boss with accounts.* and an admin Service, plus a population.
type adminFixture struct {
	*prig
	adm  *identity.Service
	boss user
}

func newAdminFixture(t *testing.T, mods ...func(*identity.Options)) *adminFixture {
	t.Helper()
	p := newPoolRig(t, mods...)
	boss := p.signUp("boss.lady")
	return &adminFixture{prig: p, adm: p.other(append(mods, withPerms(allowing(identity.PermissionAccountsRead, identity.PermissionAccountsManage)))...), boss: boss}
}

func TestListAccounts_NewestFirstKeysetPaging(t *testing.T) {
	f := newAdminFixture(t)
	var want []model.AccountID
	want = append(want, f.boss.sess.Account)
	for _, n := range []string{"one", "two", "three", "four"} {
		wantNoErr(t, f.clk.Advance(time.Minute))
		want = append(want, f.signUp("person."+n).sess.Account)
	}
	// newest first
	for i, j := 0, len(want)-1; i < j; i, j = i+1, j-1 {
		want[i], want[j] = want[j], want[i]
	}
	var got []model.AccountID
	q := identity.AccountQuery{Limit: 2}
	pages := 0
	for {
		page, err := f.adm.ListAccounts(f.boss.ctx(t), q)
		wantNoErr(t, err)
		pages++
		for _, a := range page.Accounts {
			got = append(got, a.ID)
			if err := a.Validate(); err != nil {
				t.Errorf("listed account invalid: %v", err)
			}
		}
		if page.Next == "" {
			break
		}
		if len(page.Accounts) != 2 {
			t.Errorf("a non-final page holds %d accounts", len(page.Accounts))
		}
		q.After = page.Next
		if pages > 10 {
			t.Fatal("paging does not end")
		}
	}
	if pages != 3 {
		t.Errorf("%d pages, want 3", pages)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d accounts, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: wrong account (order must be newest first, no repeats)", i)
		}
	}
}

func TestListAccounts_SameInstantAccounts_KeysetStable(t *testing.T) {
	f := newAdminFixture(t) // no clock advance: every account has the same created_at
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		f.signUp("same." + n)
	}
	seen := map[model.AccountID]bool{}
	q := identity.AccountQuery{Limit: 2}
	for range 10 {
		page, err := f.adm.ListAccounts(f.boss.ctx(t), q)
		wantNoErr(t, err)
		for _, a := range page.Accounts {
			if seen[a.ID] {
				t.Fatal("an account appeared on two pages")
			}
			seen[a.ID] = true
		}
		if page.Next == "" {
			break
		}
		q.After = page.Next
	}
	if len(seen) != 6 {
		t.Errorf("saw %d accounts, want 6", len(seen))
	}
}

func TestListAccounts_ByEmail_ExactCanonicalMatch(t *testing.T) {
	f := newAdminFixture(t)
	target := f.signUpAs("Rae.Finch@Example.org", pwGood)
	f.signUp("someone.else")
	page, err := f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{Email: mail(t, "rae.finch@example.ORG"), Limit: 1, After: "ignored-because-email-is-set"})
	wantNoErr(t, err)
	if len(page.Accounts) != 1 || page.Accounts[0].ID != target.sess.Account || page.Next != "" {
		t.Fatalf("page = %+v", page)
	}
	page, err = f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{Email: mail(t, "rae.fin@example.org")})
	wantNoErr(t, err)
	if len(page.Accounts) != 0 {
		t.Error("a partial address matched")
	}
}

func TestListAccounts_DefaultLimit(t *testing.T) {
	f := newAdminFixture(t)
	f.signUp("p1")
	page, err := f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{})
	wantNoErr(t, err)
	if len(page.Accounts) != 2 {
		t.Errorf("%d accounts", len(page.Accounts))
	}
}

// F35
func TestListAccounts_RecordsOneEntryPerOtherAccount(t *testing.T) {
	f := newAdminFixture(t)
	a := f.signUp("person.a")
	b := f.signUp("person.b")
	base := f.count(actRead)
	page, err := f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{})
	wantNoErr(t, err)
	if len(page.Accounts) != 3 {
		t.Fatalf("%d accounts", len(page.Accounts))
	}
	reads := f.entries2(actRead)[base:]
	if len(reads) != 2 {
		t.Fatalf("%d read entries, want 2 (own account excluded)", len(reads))
	}
	subjects := map[model.PersonID]bool{}
	for _, e := range reads {
		subjects[e.Subject] = true
		if e.Outcome != audit.Succeeded || e.Level != model.SensitivityPersonal || e.Record.Kind != "identity.account" ||
			len(e.Fields) != 3 || e.Fields[0] != "email" || e.Fields[1] != "last_login_at" || e.Fields[2] != "disabled_at" {
			t.Errorf("entry = %+v", e)
		}
	}
	if !subjects[a.sess.Person] || !subjects[b.sess.Person] || subjects[f.boss.sess.Person] {
		t.Errorf("subjects = %v", subjects)
	}
	f.noLeak("person.a@", "person.b@")
}

// F35
func TestListAccounts_AuditFails_ReturnsNothing(t *testing.T) {
	f := newAdminFixture(t)
	f.signUp("person.a")
	other := f.signUp("person.c")
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	page, err := f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{})
	wantKind(t, err, errs.Unavailable)
	if len(page.Accounts) != 0 || page.Next != "" {
		t.Errorf("a page was returned although the read could not be audited: %+v", page)
	}
	_, err = f.adm.GetAccount(f.boss.ctx(t), other.sess.Account)
	wantKind(t, err, errs.Unavailable)
}

func TestListAccounts_OnlyOwnAccount_RecordsNothing(t *testing.T) {
	f := newAdminFixture(t)
	page, err := f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{})
	wantNoErr(t, err)
	if len(page.Accounts) != 1 || f.count(actRead) != 0 {
		t.Errorf("accounts %d, read entries %d", len(page.Accounts), f.count(actRead))
	}
}

func TestListAccounts_WorksOnReadOnlyCopy(t *testing.T) {
	f := newAdminFixture(t)
	f.signUp("person.a")
	f.fdb.set(func(d *faultDB) { d.ro = true })
	page, err := f.adm.ListAccounts(f.boss.ctx(t), identity.AccountQuery{})
	wantNoErr(t, err)
	if len(page.Accounts) != 2 || f.count(actRead) != 1 {
		t.Errorf("accounts %d read entries %d", len(page.Accounts), f.count(actRead))
	}
}

func TestGetAccount_Fields_AndAudit(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUpAs("Mo.Reed@example.org", pwGood)
	wantNoErr(t, f.clk.Advance(time.Hour))
	_, err := f.signIn(f.svc, u)
	wantNoErr(t, err)
	got, err := f.adm.GetAccount(f.boss.ctx(t), u.sess.Account)
	wantNoErr(t, err)
	if got.ID != u.sess.Account || got.Person != u.sess.Person || got.Email != "Mo.Reed@example.org" || got.Superuser ||
		got.PasswordChangeRequired || !got.DisabledAt.IsZero() || !got.LastLoginAt.Equal(f.clk.Now()) {
		t.Errorf("account = %+v", got)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if f.count(actRead) != 1 {
		t.Errorf("%d read entries, want 1", f.count(actRead))
	}
	// own account: not audited
	_, err = f.adm.GetAccount(f.boss.ctx(t), f.boss.sess.Account)
	wantNoErr(t, err)
	if f.count(actRead) != 1 {
		t.Error("a read of the actor's own account was audited")
	}
}

// F39
func TestAdminChanges_SuperuserTarget_Permission(t *testing.T) {
	f := newAdminFixture(t)
	res, _ := f.superuser("root.operator@example.org")
	ctx := f.boss.ctx(t)
	wantKind(t, f.adm.DisableAccount(ctx, res.Account), errs.Permission)
	wantKind(t, f.adm.EnableAccount(ctx, res.Account), errs.Permission)
	_, err := f.adm.SetTemporaryPassword(ctx, res.Account)
	wantKind(t, err, errs.Permission)
	row := f.rowOf(res.Person)
	if row.DisabledAt != nil || row.PasswordChangeRequired {
		t.Error("the superuser account was changed through the API")
	}
	// the superuser cannot change another superuser either
	res2, iss2 := f.superuser("second.operator@example.org")
	suCtx := identity.WithSession(from(ctxT(t), nextIP()), iss2.Session)
	wantKind(t, f.adm.DisableAccount(suCtx, res.Account), errs.Permission)
	_ = res2
}

// F40
func TestAdminChanges_Self_Invalid(t *testing.T) {
	f := newAdminFixture(t)
	ctx := f.boss.ctx(t)
	wantField(t, f.adm.DisableAccount(ctx, f.boss.sess.Account), "account", "self")
	wantField(t, f.adm.EnableAccount(ctx, f.boss.sess.Account), "account", "self")
	_, err := f.adm.SetTemporaryPassword(ctx, f.boss.sess.Account)
	wantField(t, err, "account", "self")
	if row := f.rowOf(f.boss.sess.Person); row.DisabledAt != nil || row.PasswordChangeRequired {
		t.Error("own account changed")
	}
}

// The superuser check comes before the self check, so a superuser acting on their own
// (superuser) account is refused with Permission.
func TestAdminChanges_SuperuserOnOwnSuperuserAccount_Permission(t *testing.T) {
	f := newAdminFixture(t)
	res, iss := f.superuser("root.operator@example.org")
	suCtx := identity.WithSession(from(ctxT(t), nextIP()), iss.Session)
	wantKind(t, f.adm.DisableAccount(suCtx, res.Account), errs.Permission)
	wantKind(t, f.adm.EnableAccount(suCtx, res.Account), errs.Permission)
	_, err := f.adm.SetTemporaryPassword(suCtx, res.Account)
	wantKind(t, err, errs.Permission)
}

func TestDisableEnable_Success_RevokesSessions(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	c := f.cookie(u.email, u.pw)
	ctx := f.boss.ctx(t)
	wantNoErr(t, f.adm.DisableAccount(ctx, u.sess.Account))
	e := f.only(actDisabled)
	if e.Outcome != audit.Succeeded || e.Subject != u.sess.Person || e.Record.Kind != "identity.account" || e.Record.ID != [16]byte(u.sess.Account) {
		t.Errorf("entry = %+v", e)
	}
	row := f.rowOf(u.sess.Person)
	if row.DisabledAt == nil || !row.DisabledAt.Equal(f.clk.Now()) || row.Generation != 1 {
		t.Errorf("row = %+v, want disabled now at generation 1", row)
	}
	f.wantRejected(c)
	wantNoErr(t, f.adm.EnableAccount(ctx, u.sess.Account))
	e = f.only(actEnabled)
	if e.Outcome != audit.Succeeded || e.Subject != u.sess.Person {
		t.Errorf("entry = %+v", e)
	}
	if row := f.rowOf(u.sess.Person); row.DisabledAt != nil {
		t.Error("still disabled")
	}
	if _, err := f.signIn(f.svc, u); err != nil {
		t.Errorf("cannot sign in after enable: %v", err)
	}
}

// F49
func TestDisableAccount_Idempotent(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	ctx := f.boss.ctx(t)
	wantNoErr(t, f.adm.EnableAccount(ctx, u.sess.Account)) // already enabled
	if f.count(actEnabled) != 0 {
		t.Error("enable of an enabled account was recorded")
	}
	wantNoErr(t, f.adm.DisableAccount(ctx, u.sess.Account))
	gen := f.rowOf(u.sess.Person).Generation
	wantNoErr(t, f.adm.DisableAccount(ctx, u.sess.Account))
	if f.count(actDisabled) != 1 {
		t.Errorf("%d disabled entries, want 1", f.count(actDisabled))
	}
	if f.rowOf(u.sess.Person).Generation != gen {
		t.Error("the second disable bumped the generation")
	}
}

func TestDisableAccount_Concurrent_BothSucceedOneEntry(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := f.adm.DisableAccount(f.boss.ctx(t), u.sess.Account); err != nil {
				t.Errorf("DisableAccount: %v", err)
			}
		})
	}
	wg.Wait()
	if f.count(actDisabled) != 1 {
		t.Errorf("%d disabled entries, want 1", f.count(actDisabled))
	}
}

// F42
func TestAdminChanges_ReadOnly_Integration(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	f.fdb.set(func(d *faultDB) { d.ro = true })
	ctx := f.boss.ctx(t)
	wantReadOnly(t, f.adm.DisableAccount(ctx, u.sess.Account))
	wantReadOnly(t, f.adm.EnableAccount(ctx, u.sess.Account))
	_, err := f.adm.SetTemporaryPassword(ctx, u.sess.Account)
	wantReadOnly(t, err)
	if f.count(actDisabled)+f.count(actEnabled)+f.count(actTempSet) != 0 {
		t.Error("entries recorded for refused changes")
	}
}

func TestAdminChanges_AuditFails_NothingChanged(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, errDown))
	wantKind(t, f.adm.DisableAccount(f.boss.ctx(t), u.sess.Account), errs.Unavailable)
	if row := f.rowOf(u.sess.Person); row.DisabledAt != nil || row.Generation != 0 {
		t.Errorf("row changed although the entry could not be written: %+v", row)
	}
	_, err := f.adm.SetTemporaryPassword(f.boss.ctx(t), u.sess.Account)
	wantKind(t, err, errs.Unavailable)
	f.rec.Fail(nil)
	if _, err := f.signIn(f.svc, u); err != nil {
		t.Errorf("old password stopped working: %v", err)
	}
}

func TestSetTemporaryPassword_Success(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	c := f.cookie(u.email, u.pw)
	ctx := f.boss.ctx(t)
	tmp, err := f.adm.SetTemporaryPassword(ctx, u.sess.Account)
	wantNoErr(t, err)
	pw := tmp.Value()
	if len(pw) != identity.GeneratedPasswordLength {
		t.Errorf("password length %d, want %d", len(pw), identity.GeneratedPasswordLength)
	}
	for _, r := range pw {
		if !strings.ContainsRune(tempAlphabet, r) {
			t.Errorf("character %q outside the alphabet", r)
			break
		}
	}
	if strings.Contains(tmp.String(), pw) {
		t.Error("the Secret prints its value")
	}
	e := f.only(actTempSet)
	if e.Outcome != audit.Succeeded || e.Subject != u.sess.Person || e.Record.ID != [16]byte(u.sess.Account) {
		t.Errorf("entry = %+v", e)
	}
	row := f.rowOf(u.sess.Person)
	if !row.PasswordChangeRequired || row.Generation != 1 {
		t.Errorf("row = %+v", row)
	}
	f.wantRejected(c) // old sessions end
	if _, err := f.signIn(f.svc, u); err == nil {
		t.Error("old password still works")
	}
	if _, err := f.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: u.email, Password: pw}); err != nil {
		t.Errorf("temporary password does not sign in: %v", err)
	}
	// not idempotent: a second call is a new password and a new entry
	tmp2, err := f.adm.SetTemporaryPassword(ctx, u.sess.Account)
	wantNoErr(t, err)
	if tmp2.Value() == pw || f.count(actTempSet) != 2 {
		t.Error("second call reused the password or was not recorded")
	}
	f.noLeak(pw, tmp2.Value(), u.pw)
}

func TestSetTemporaryPassword_ThroughHandler_NoStore(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	svc := f.adm
	rt := handlerFor(t, svc, "POST", "/{account}/temporary-password")
	w, fl := call(t, rt, f.boss.ctx(t), "", func(r *http.Request) { r.SetPathValue("account", u.sess.Account.String()) })
	if fl.count() != 0 || w.Code != 200 {
		t.Fatalf("status %d failure %v", w.Code, fl.last())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q", cc)
	}
	if w.Body.Len() == 0 {
		t.Error("empty body")
	}
}

func TestHandlers_AdminRoundTrip(t *testing.T) {
	f := newAdminFixture(t)
	u := f.signUp("sam.gray")
	set := func(r *http.Request) { r.SetPathValue("account", u.sess.Account.String()) }
	for _, tc := range []struct{ method, pattern string }{
		{"POST", "/{account}/disable"}, {"POST", "/{account}/enable"}, {"GET", "/{account}"}, {"GET", "/"},
	} {
		w, fl := call(t, handlerFor(t, f.adm, tc.method, tc.pattern), f.boss.ctx(t), "", set)
		if fl.count() != 0 || w.Code/100 != 2 {
			t.Errorf("%s %s: status %d failure %v", tc.method, tc.pattern, w.Code, fl.last())
		}
	}
	// the denied path: the plain service answers 403
	w, _ := call(t, handlerFor(t, f.svc, "GET", "/"), f.boss.ctx(t), "")
	if w.Code != 403 {
		t.Errorf("status = %d, want 403 without a checker", w.Code)
	}
}
