//go:build integration

package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ---- enqueue -----------------------------------------------------------------------------

func TestSend_Happy_QueuesRenderedRowPerRecipient(t *testing.T) {
	r := newIRig(t)
	p1, p2, p3 := r.dbPerson("one@example.org"), r.dbPerson("two@example.org"), r.dbPerson("three@example.org")
	r.langs.m[p1], r.langs.m[p3] = "es-MX", "fr"
	r.names.m[p1], r.names.m[p2] = "Zorro", "Pixel"
	wantNoErr(t, r.vol.Send(ctxT(t), greet(p1, p2, p3)))

	if n := r.count(); n != 3 {
		t.Fatalf("%d rows, want 3", n)
	}
	a, b, c := r.one(p1), r.one(p2), r.one(p3)
	for _, x := range []row{a, b, c} {
		if x.Status != "queued" || x.Attempts != 0 || x.Owner != "volunteers" || x.Template != "volunteers.greet" {
			t.Errorf("row = %+v", x)
		}
		if x.Address != nil {
			t.Error("a person's row copied their address")
		}
		if x.Subject == nil || x.Body == nil {
			t.Fatal("rendered text missing")
		}
		if x.Next.After(r.clk.Now()) {
			t.Errorf("next_attempt_at %v is in the future", x.Next)
		}
		if d := x.Expires.Sub(x.Created); d != notify.MaxAge {
			t.Errorf("expires_at - created_at = %v, want MaxAge", d)
		}
		for _, m := range []string{"Gate 06:00", siteURL + "/volunteers/shifts/1"} {
			if !strings.Contains(*x.Body, m) {
				t.Errorf("body lacks %q: %q", m, *x.Body)
			}
		}
	}
	if a.Language != "es" || b.Language != "en" || c.Language != "en" {
		t.Errorf("languages = %q %q %q, want es en en", a.Language, b.Language, c.Language)
	}
	if *a.Subject != "Turno para Zorro" {
		t.Errorf("es subject = %q", *a.Subject)
	}
	if !strings.HasPrefix(*b.Subject, "Shift for") || !strings.Contains(*b.Subject, "Pixel") {
		t.Errorf("en subject = %q", *b.Subject)
	}
	if !strings.Contains(*b.Body, "bring water") || !strings.Contains(*b.Body, "duty") || !strings.Contains(*b.Body, "Test Burn") {
		t.Errorf("term/site not rendered: %q", *b.Body)
	}
	if got := r.metric("bms.notify.enqueued", "owner", "volunteers"); got != 3 {
		t.Errorf("enqueued metric = %v, want 3", got)
	}
}

func TestSend_TTL_SetsExpiry(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = 90 * time.Minute
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	x := r.one(p)
	if d := x.Expires.Sub(x.Created); d != 90*time.Minute {
		t.Fatalf("expires_at - created_at = %v", d)
	}
}

func TestSend_OverrideBeatsDefault(t *testing.T) {
	r := newIRig(t)
	admin := r.admin(string(permTemplates))
	_, err := r.svc.PutTemplate(admin, "volunteers.greet", "en", notify.TemplateText{Subject: "Custom {{.Site.Name}}", Body: "Custom body {{.Vars.shift}}"})
	wantNoErr(t, err)
	p := r.dbPerson("a@example.org")
	wantNoErr(t, r.vol.Send(ctxT(t), greet(p)))
	x := r.one(p)
	if *x.Subject != "Custom Test Burn" || !strings.Contains(*x.Body, "Custom body Gate 06:00") {
		t.Fatalf("override not used: %q / %q", *x.Subject, *x.Body)
	}
}

func TestSend_DuplicateKey_Skipped(t *testing.T) {
	r := newIRig(t)
	p1, p2 := r.dbPerson("a@example.org"), r.dbPerson("b@example.org")
	m := greet(p1)
	m.Key = "shift:1"
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	before := r.metric("bms.notify.enqueued", "", "")
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	if r.count() != 1 {
		t.Fatalf("%d rows after a repeated key", r.count())
	}
	if after := r.metric("bms.notify.enqueued", "", ""); after != before {
		t.Errorf("a skipped duplicate was counted (%v -> %v)", before, after)
	}
	m.To = []model.PersonID{p1, p2}
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	if r.count() != 2 || len(r.rowsFor(p2)) != 1 {
		t.Fatalf("the new recipient did not get a row (rows=%d)", r.count())
	}
	if got := r.metric("bms.notify.enqueued", "", ""); got != before+1 {
		t.Errorf("enqueued = %v, want %v", got, before+1)
	}
	// Another owner may reuse the key.
	o := notify.Message{Template: "people.deleted", To: []model.PersonID{p1}, Key: "shift:1"}
	wantNoErr(t, r.ppl.Send(ctxT(t), o))
	if r.count() != 3 {
		t.Fatalf("another owner's identical key was skipped (rows=%d)", r.count())
	}
	// No key: repeats are separate notifications.
	m2 := greet(p1)
	wantNoErr(t, r.vol.Send(ctxT(t), m2))
	wantNoErr(t, r.vol.Send(ctxT(t), m2))
	if r.count() != 5 {
		t.Fatalf("keyless repeats collapsed (rows=%d)", r.count())
	}
}

func TestSend_Rollback_NoRow(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	boom := errors.New("boom")
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		wantNoErr(t, r.vol.Send(ctx, greet(p)))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact = %v", err)
	}
	if r.count() != 0 {
		t.Fatal("a row survived the rollback")
	}
	if got := r.metric("bms.notify.enqueued", "", ""); got != 0 {
		t.Fatalf("enqueued counted %v for a rolled-back send", got)
	}
}

func TestSend_Commit_RowAndMetricAfterCommit(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	var during float64
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		wantNoErr(t, r.vol.Send(ctx, greet(p)))
		during = r.metric("bms.notify.enqueued", "", "")
		return nil
	})
	wantNoErr(t, err)
	if during != 0 {
		t.Errorf("counted %v before the commit", during)
	}
	if r.count() != 1 || r.metric("bms.notify.enqueued", "", "") != 1 {
		t.Fatalf("rows=%d metric=%v", r.count(), r.metric("bms.notify.enqueued", "", ""))
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("Send talked to the channel inline")
	}
}

func TestSend_InReadTx_Internal_Real(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	var sendErr error
	wantNoErr(t, r.pool.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		sendErr = r.vol.Send(ctx, greet(p))
		return nil
	}))
	wantKind(t, sendErr, errs.Internal)
	if r.count() != 0 {
		t.Fatal("a row was written in a read transaction")
	}
}

func TestSend_ReadOnly_Unavailable_Real(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	r.flt.set(true, nil)
	err := r.vol.Send(ctxT(t), greet(p))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
	r.flt.set(false, nil)
	err = r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		r.flt.set(true, nil)
		return r.vol.Send(ctx, greet(p))
	})
	r.flt.set(false, nil)
	wantKind(t, err, errs.Unavailable)
	if r.count() != 0 {
		t.Fatal("rows written on a read-only copy")
	}
}

func TestSend_StoreFails_Unavailable(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		t.Run(k.String(), func(t *testing.T) {
			r := newIRig(t)
			p := r.dbPerson("a@example.org")
			r.flt.set(false, errs.E("test.store", k, errors.New("store down")))
			wantKind(t, r.vol.Send(ctxT(t), greet(p)), k)
		})
	}
}

func TestSend_CanceledAndExpiredContext(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	wantKind(t, r.vol.Send(ctx, greet(p)), errs.Canceled)
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	wantKind(t, r.vol.Send(dctx, greet(p)), errs.Timeout)
	if r.count() != 0 {
		t.Fatal("rows written under a dead context")
	}
}

func TestSend_FKToPerson_UnknownRecipientFails(t *testing.T) {
	r := newIRig(t)
	ghost := newPerson() // no core.person row
	err := r.vol.Send(ctxT(t), greet(ghost))
	if err == nil {
		t.Fatal("a row for a person that does not exist was accepted")
	}
	if r.count() != 0 {
		t.Fatal("row written")
	}
}

// ---- SendTo ------------------------------------------------------------------------------

func TestSendTo_Happy_AddressRowThenCleared(t *testing.T) {
	r := newIRig(t)
	wantNoErr(t, r.svc.SendTo(ctxT(t), "alerts@example.org", greet()))
	rs := r.rows("")
	if len(rs) != 1 {
		t.Fatalf("%d rows", len(rs))
	}
	x := rs[0]
	if x.Address == nil || *x.Address != "alerts@example.org" || x.Person != nil {
		t.Fatalf("row = %+v", x)
	}
	if x.Language != "en" {
		t.Errorf("language = %q, want the site default", x.Language)
	}
	r.sendOnce()
	envs := r.ch.envelopes()
	if len(envs) != 1 || envs[0].To != "alerts@example.org" {
		t.Fatalf("envelopes = %+v", envs)
	}
	x = r.byID(x.ID)
	if x.Status != "sent" || x.Address != nil || x.Subject != nil || x.Body != nil {
		t.Fatalf("after send: %+v", x)
	}
}

func TestSendTo_SameKeyAddress_DedupedWhileLive(t *testing.T) {
	r := newIRig(t)
	m := greet()
	m.Key = "superuser-signed-in:abc"
	wantNoErr(t, r.svc.SendTo(ctxT(t), "a@example.org", m))
	wantNoErr(t, r.svc.SendTo(ctxT(t), "a@example.org", m))
	if r.count() != 1 {
		t.Fatalf("%d rows after a live duplicate, want 1", r.count())
	}
	wantNoErr(t, r.svc.SendTo(ctxT(t), "b@example.org", m)) // one key to several addresses is intended
	if r.count() != 2 {
		t.Fatalf("%d rows, want 2", r.count())
	}
	r.sendOnce() // both sent; addresses cleared
	wantNoErr(t, r.svc.SendTo(ctxT(t), "a@example.org", m))
	if r.count() != 3 {
		t.Fatalf("%d rows, a repeat after delivery inserts again (accepted)", r.count())
	}
}

// ---- sender ------------------------------------------------------------------------------

func TestSendOnce_Happy_DeliversAndClearsText(t *testing.T) {
	r := newIRig(t)
	p := r.queue("first@example.org")
	queued := r.one(p)
	r.advance(time.Minute)
	got := r.sendOnce()
	if got.Sent != 1 || got.Failed != 0 || got.Retried != 0 {
		t.Fatalf("Sent = %+v", got)
	}
	envs := r.ch.envelopes()
	if len(envs) != 1 {
		t.Fatalf("%d envelopes", len(envs))
	}
	e := envs[0]
	if e.To != "first@example.org" || e.From.Email != "burn@example.org" || e.From.Name != "Test Burn" {
		t.Errorf("envelope = %+v", e)
	}
	if e.Subject != *queued.Subject || e.Body != *queued.Body {
		t.Errorf("envelope text differs from the rendered row")
	}
	if !strings.HasSuffix(e.MessageID, "@"+testHostname) {
		t.Errorf("MessageID = %q", e.MessageID)
	}
	x := r.byID(queued.ID)
	if x.Status != "sent" || x.Attempts != 1 || x.SentAt == nil || !x.SentAt.Equal(r.clk.Now()) {
		t.Errorf("row = %+v", x)
	}
	if x.Subject != nil || x.Body != nil || x.Address != nil {
		t.Error("text survived delivery")
	}
	if g := r.metric("bms.notify.delivered", "outcome", "ok"); g != 1 {
		t.Errorf("delivered{ok} = %v", g)
	}
	// Nothing left: a second tick sends nothing.
	if got := r.sendOnce(); got != (notify.Sent{}) || len(r.ch.envelopes()) != 1 {
		t.Fatalf("second tick: %+v", got)
	}
}

func TestSendOnce_EmptyOutbox_Zero(t *testing.T) {
	r := newIRig(t)
	if got := r.sendOnce(); got != (notify.Sent{}) {
		t.Fatalf("Sent = %+v", got)
	}
}

func TestSendOnce_AddressResolvedAtSend(t *testing.T) {
	r := newIRig(t)
	p := r.queue("old@example.org")
	r.addrs.set(p, "new@example.org")
	r.sendOnce()
	envs := r.ch.envelopes()
	if len(envs) != 1 || envs[0].To != "new@example.org" {
		t.Fatalf("envelopes = %+v, want the address as it is at send time", envs)
	}
}

func TestSendOnce_UsesCurrentSettings(t *testing.T) {
	r := newIRig(t)
	r.queue("a@example.org")
	r.smtp.set(func(c *notify.SMTPConfig) {
		c.Host = "relay.invalid"
		c.Port = 2626
		c.Sender.Email = "other@example.org"
	})
	r.sendOnce()
	r.ch.mu.Lock()
	defer r.ch.mu.Unlock()
	if len(r.ch.envs) == 0 {
		t.Fatal("nothing was sent")
	}
	if r.ch.envs[0].From.Email != "other@example.org" {
		t.Errorf("From = %+v", r.ch.envs[0].From)
	}
}

func TestSendOnce_NotConfigured_SkipsWithoutAttempt(t *testing.T) {
	r := newIRig(t)
	p1, p2 := r.queue("a@example.org"), r.queue("b@example.org")
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	got := r.sendOnce()
	if got.Skipped != 2 || got.Sent != 0 {
		t.Fatalf("Sent = %+v, want Skipped 2", got)
	}
	logs := len(r.cap.Records())
	r.sendOnce()
	r.sendOnce()
	if more := len(r.cap.Records()) - logs; more > 0 {
		t.Errorf("repeated unconfigured ticks logged %d more records within the hour", more)
	}
	for _, p := range []model.PersonID{p1, p2} {
		x := r.one(p)
		if x.Status != "queued" || x.Attempts != 0 {
			t.Fatalf("row = %+v, want queued with no attempt counted", x)
		}
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("a channel was used while unconfigured")
	}
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "smtp.invalid" })
	if got := r.sendOnce(); got.Sent != 2 {
		t.Fatalf("after configuring: %+v", got)
	}
}

// F22 (D57, D58)
func TestSendOnce_NoAddress_SkippedFinal(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.addrs.set(p, "")
	got := r.sendOnce()
	if got.Skipped != 1 || got.Failed != 0 || got.Sent != 0 || got.Retried != 0 {
		t.Fatalf("Sent = %+v, want Skipped 1 and nothing else", got)
	}
	x := r.one(p)
	if x.Status != "skipped" || x.LastErr == nil || *x.LastErr != "no_address" || x.FailedAt == nil {
		t.Fatalf("row = %+v, want skipped with last_error no_address and failed_at set", x)
	}
	if x.Subject != nil || x.Body != nil || x.Address != nil {
		t.Errorf("a skipped row kept its text or address: %+v", x)
	}
	if x.Leased != nil {
		t.Errorf("a skipped row still holds a lease: %v", x.Leased)
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("sent without an address")
	}
	if g := r.metric("bms.notify.delivered", "outcome", "skipped"); g != 1 {
		t.Errorf("delivered{skipped} = %v, want 1", g)
	}
	if g := r.metric("bms.notify.delivered", "outcome", "no_address"); g != 0 {
		t.Errorf("delivered{no_address} = %v, want 0 (the outcome is skipped)", g)
	}
	if g := r.metric("bms.notify.delivered", "outcome", "failed"); g != 0 {
		t.Errorf("delivered{failed} = %v, want 0", g)
	}
	reasoned := false
	for _, sp := range r.cap.Spans() {
		if sp.Attrs["outcome"] == "skipped" && sp.Attrs["reason"] == "no_address" {
			reasoned = true
		}
	}
	if !reasoned {
		t.Error("no notify.deliver span with outcome=skipped and reason=no_address")
	}
	if r.sendOnce() != (notify.Sent{}) {
		t.Fatal("a final row was picked up again")
	}
	if x := r.one(p); x.Attempts > 1 {
		t.Errorf("a skipped row was retried: attempts = %d", x.Attempts)
	}
}

// F22: a batch mixing an addressless row with a sendable one sends the other.
func TestSendOnce_NoAddress_DoesNotBlockTheBatch(t *testing.T) {
	r := newIRig(t)
	first, second, third := r.queue("a@example.org"), r.queue("b@example.org"), r.queue("c@example.org")
	r.addrs.set(second, "")
	got := r.sendOnce()
	if got.Sent != 2 || got.Skipped != 1 {
		t.Fatalf("Sent = %+v, want Sent 2 and Skipped 1", got)
	}
	for p, want := range map[model.PersonID]string{first: "sent", second: "skipped", third: "sent"} {
		if x := r.one(p); x.Status != want {
			t.Errorf("row for %v is %s, want %s", p, x.Status, want)
		}
	}
}

func TestSendOnce_Transient_RequeuedWithBackoff(t *testing.T) {
	for name, err := range map[string]error{
		"unavailable": errs.E("test.ch", errs.Unavailable, errors.New("4xx")),
		"auth":        errs.E("test.ch", errs.Unavailable, notify.ErrAuth),
		"smtp 4xx":    errs.E("test.ch", errs.Unavailable, notify.ErrTemporary),
	} {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t)
			p := r.queue("a@example.org")
			r.ch.failWith(err)
			now := r.clk.Now()
			got := r.sendOnce()
			if got.Retried != 1 || got.Sent != 0 || got.Failed != 0 {
				t.Fatalf("Sent = %+v", got)
			}
			x := r.one(p)
			if x.Status != "queued" || x.Attempts != 1 {
				t.Fatalf("row = %+v", x)
			}
			if x.LastErr == nil || *x.LastErr == "" || len(*x.LastErr) > 32 {
				t.Errorf("last_error = %v, want a short token", x.LastErr)
			}
			if x.Next.Before(now) || x.Next.After(now.Add(time.Hour)) {
				t.Errorf("next_attempt_at %v not within [now, now+1h]", x.Next)
			}
			if x.Subject == nil || x.Body == nil {
				t.Error("text must stay while the row can still be retried")
			}
			if g := r.metric("bms.notify.delivered", "outcome", "retry"); g != 1 {
				t.Errorf("delivered{retry} = %v", g)
			}
		})
	}
}

func TestSendOnce_NotDueYet_Untouched(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.exec("UPDATE core.notify_message SET next_attempt_at = $1", r.clk.Now().Add(10*time.Minute))
	if got := r.sendOnce(); got != (notify.Sent{}) {
		t.Fatalf("Sent = %+v", got)
	}
	if x := r.one(p); x.Attempts != 0 || x.Status != "queued" {
		t.Fatalf("row = %+v", x)
	}
	r.advance(10 * time.Minute) // exactly due
	if got := r.sendOnce(); got.Sent != 1 {
		t.Fatalf("a row due exactly now was not sent: %+v", got)
	}
}

func TestSendOnce_Timeout_Requeued(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.ch.failWith(errs.E("test.ch", errs.Timeout, errors.New("slow")))
	if got := r.sendOnce(); got.Retried != 1 {
		t.Fatalf("Sent = %+v", got)
	}
	x := r.one(p)
	if x.Status != "queued" || x.LastErr == nil || *x.LastErr != "timeout" {
		t.Fatalf("row = %+v", x)
	}
}

func TestSendOnce_Rejected_FailedFinal(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.ch.failWith(errs.E("test.ch", errs.Invalid, notify.ErrRejected))
	got := r.sendOnce()
	if got.Failed != 1 {
		t.Fatalf("Sent = %+v", got)
	}
	x := r.one(p)
	if x.Status != "failed" || x.LastErr == nil || *x.LastErr != "rejected" || x.FailedAt == nil {
		t.Fatalf("row = %+v", x)
	}
	if x.Subject == nil || x.Body == nil {
		t.Error("a failed row keeps its text until expiry so Retry can resend it (amendment)")
	}
	if r.metric("bms.notify.delivered", "outcome", "rejected") != 1 {
		t.Error("delivered{rejected} not counted")
	}
	r.advance(24 * time.Hour)
	if r.sendOnce() != (notify.Sent{}) {
		t.Fatal("a failed row was sent again")
	}
}

func TestSendOnce_Rejected_WithNotifytestChannel(t *testing.T) {
	// Same path through the in-memory channel notify ships for tests.
	nc := newNotifytestChannel()
	r := newIRig(t, func(o *notify.Options) { o.Channel = nc })
	p := r.queue("a@example.org")
	nc.Reject()
	if got := r.sendOnce(); got.Failed != 1 {
		t.Fatalf("Sent = %+v", got)
	}
	if x := r.one(p); x.Status != "failed" || *x.LastErr != "rejected" {
		t.Fatalf("row = %+v", x)
	}
}

func TestSendOnce_MaxAttempts_Failed(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.ch.failWith(errs.E("test.ch", errs.Unavailable, errors.New("down")))
	for i := 1; i <= notify.MaxAttempts; i++ {
		got := r.sendOnce()
		x := r.one(p)
		if x.Attempts != i {
			t.Fatalf("after attempt %d: attempts = %d", i, x.Attempts)
		}
		if i < notify.MaxAttempts {
			if got.Retried != 1 || x.Status != "queued" {
				t.Fatalf("attempt %d: %+v %+v", i, got, x)
			}
			if x.Next.After(r.clk.Now().Add(time.Hour)) {
				t.Fatalf("attempt %d: backoff %v exceeds the 1h cap", i, x.Next.Sub(r.clk.Now()))
			}
			r.advance(time.Hour + time.Second)
		} else if got.Failed != 1 || x.Status != "failed" || x.LastErr == nil || *x.LastErr == "" {
			t.Fatalf("attempt %d should fail the row: %+v %+v", i, got, x)
		}
	}
	if n := len(r.ch.envelopes()); n != 0 {
		t.Fatalf("%d envelopes recorded for failing sends", n)
	}
	r.advance(24 * time.Hour)
	if r.sendOnce() != (notify.Sent{}) {
		t.Fatal("a failed row was attempted again")
	}
}

func TestSendOnce_OneFailureDoesNotStopTheBatch(t *testing.T) {
	r := newIRig(t)
	for i := 0; i < 3; i++ {
		r.queue("p" + string(rune('a'+i)) + "@example.org")
	}
	calls := 0
	r.ch.script(func(notify.Envelope) error {
		calls++
		if calls == 1 {
			return errs.E("test.ch", errs.Unavailable, errors.New("blip"))
		}
		return nil
	})
	got := r.sendOnce()
	if got.Sent != 2 || got.Retried != 1 {
		t.Fatalf("Sent = %+v", got)
	}
}

func TestSendOnce_BatchLimit_FirstAndLast(t *testing.T) {
	r := newIRig(t)
	n := notify.MaxBatch + 10
	for i := 0; i < n; i++ {
		r.queue("bulk@example.org")
	}
	if got := r.sendOnce(); got.Sent != notify.MaxBatch {
		t.Fatalf("first tick sent %d, want %d", got.Sent, notify.MaxBatch)
	}
	if got := r.sendOnce(); got.Sent != 10 {
		t.Fatalf("second tick sent %d, want 10", got.Sent)
	}
	if got := r.sendOnce(); got.Sent != 0 {
		t.Fatalf("third tick sent %d", got.Sent)
	}
	if len(r.ch.envelopes()) != n {
		t.Fatalf("%d envelopes, want %d", len(r.ch.envelopes()), n)
	}
}

func TestSendOnce_ReadOnly_NothingLeased_Real(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.flt.set(true, nil)
	_, err := r.svc.SendOnce(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
	r.flt.set(false, nil)
	if x := r.one(p); x.Status != "queued" || x.Attempts != 0 || x.Leased != nil {
		t.Fatalf("row touched: %+v", x)
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("sent on a read-only copy")
	}
}

func TestSendOnce_Canceled_RowsReleased(t *testing.T) {
	r := newIRig(t)
	var ps []model.PersonID
	for i := 0; i < 3; i++ {
		ps = append(ps, r.queue("p"+string(rune('a'+i))+"@example.org"))
	}
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	r.ch.script(func(notify.Envelope) error {
		cancel()
		return errs.E("test.ch", errs.Canceled, context.Canceled)
	})
	_, err := r.svc.SendOnce(ctx)
	wantKind(t, err, errs.Canceled)
	for _, p := range ps {
		x := r.one(p)
		if x.Status != "queued" || x.Attempts != 0 {
			t.Fatalf("row = %+v, want queued with the attempt not counted", x)
		}
		if x.Next.After(r.clk.Now()) {
			t.Fatalf("row not due now: %v", x.Next)
		}
	}
	r.ch.script(nil)
	if got := r.sendOnce(); got.Sent != 3 {
		t.Fatalf("after release: %+v", got)
	}
}

func TestSendOnce_UpdateFailsAfterSend_ResentAfterLease(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.ch.script(func(notify.Envelope) error {
		r.flt.set(true, nil) // the copy turns read-only after the server accepted
		return nil
	})
	ignore(r.svc.SendOnce(ctxT(t)))
	r.flt.set(false, nil)
	r.ch.script(nil)
	if x := r.one(p); x.Status != "sending" {
		t.Fatalf("row = %+v, want the lease left in place", x)
	}
	if got := r.sendOnce(); got.Sent != 0 {
		t.Fatalf("resent before the lease ended: %+v", got)
	}
	r.advance(notify.DefaultLease + time.Second)
	if got := r.sendOnce(); got.Sent != 1 {
		t.Fatalf("after the lease: %+v", got)
	}
	if n := len(r.ch.envelopes()); n != 2 {
		t.Fatalf("%d deliveries, want exactly one resend (2 total)", n)
	}
	if x := r.one(p); x.Status != "sent" {
		t.Fatalf("row = %+v", x)
	}
}

func TestSendOnce_ExpiredLease_Reclaimed(t *testing.T) {
	r := newIRig(t)
	live, stale := r.queue("a@example.org"), r.queue("b@example.org")
	r.exec("UPDATE core.notify_message SET status='sending', attempts=1, leased_until=$1 WHERE person_id=$2",
		r.clk.Now().Add(time.Minute), [16]byte(live))
	r.exec("UPDATE core.notify_message SET status='sending', attempts=1, leased_until=$1 WHERE person_id=$2",
		r.clk.Now().Add(-time.Minute), [16]byte(stale))
	got := r.sendOnce()
	if got.Sent != 1 {
		t.Fatalf("Sent = %+v, want only the expired lease reclaimed", got)
	}
	if envs := r.ch.envelopes(); len(envs) != 1 || envs[0].To != "b@example.org" {
		t.Fatalf("envelopes = %+v", envs)
	}
	if x := r.one(live); x.Status != "sending" {
		t.Fatalf("a live lease was taken: %+v", x)
	}
	if x := r.one(stale); x.Status != "sent" || x.Attempts != 2 {
		t.Fatalf("reclaimed row = %+v", x)
	}
}

func TestSendOnce_TwoWorkers_EachRowOnce(t *testing.T) {
	r := newIRig(t)
	other := newIRigOn(t, r.pool, func(o *notify.Options) { o.Channel = r.ch })
	const n = 40
	for i := 0; i < n; i++ {
		p := r.dbPerson("")
		addr := "w" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + "@example.org"
		r.addrs.set(p, mustEmail(t, addr))
		other.addrs.set(p, mustEmail(t, addr))
		wantNoErr(t, r.vol.Send(ctxT(t), greet(p)))
	}
	errc := make(chan error, 2)
	run := func(s *notify.Service) {
		for {
			got, err := s.SendOnce(ctxT(t))
			if err != nil {
				errc <- err
				return
			}
			if got.Sent == 0 {
				errc <- nil
				return
			}
		}
	}
	go run(r.svc)
	go run(other.svc)
	wantNoErr(t, <-errc)
	wantNoErr(t, <-errc)
	seen := map[string]int{}
	for _, e := range r.ch.envelopes() {
		seen[e.MessageID]++
	}
	if len(r.ch.envelopes()) != n || len(seen) != n {
		t.Fatalf("%d envelopes, %d distinct, want %d each", len(r.ch.envelopes()), len(seen), n)
	}
}

// Options.Channel replaces SMTP: the sender carries the envelope over it even though the
// settings name a host nobody listens on (boundaries step 10; the SMTP-per-batch default is
// covered by the Mailpit tests).
func TestSendOnce_ChannelOption_ReplacesSMTP(t *testing.T) {
	r := newIRig(t)
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "127.0.0.1"; c.Port = closedPort(t); c.Security = notify.None })
	r.queue("a@example.org")
	got := r.sendOnce()
	if got.Sent != 1 {
		t.Fatalf("Sent = %+v, want 1 over the given channel", got)
	}
	if envs := r.ch.envelopes(); len(envs) != 1 || envs[0].To != "a@example.org" {
		t.Fatalf("envelopes = %+v", envs)
	}
}

// ---- expiry (amendment: F67..F72) ---------------------------------------------------------

func TestSendOnce_ExpiredBeforeLease_NotSent(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = time.Hour
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	r.advance(2 * time.Hour)
	if got := r.sendOnce(); got.Sent != 0 {
		t.Fatalf("Sent = %+v", got)
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("a dead message went out")
	}
	x := r.one(p)
	if x.Status != "queued" || x.Attempts != 0 || x.Subject == nil || x.Body == nil {
		t.Fatalf("SendOnce must leave the expired row queued with its text: %+v", x)
	}
	n, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n != 1 {
		t.Fatalf("ExpireStale = %d", n)
	}
	x = r.one(p)
	if x.Status != "failed" || x.LastErr == nil || *x.LastErr != "expired" || x.Subject != nil || x.Body != nil || x.Address != nil {
		t.Fatalf("after ExpireStale: %+v", x)
	}
}

func TestExpireStale_ClearsStaleRows_KeepsLiveLease(t *testing.T) {
	r := newIRig(t)
	short := func(addr string) model.PersonID {
		p := r.dbPerson(addr)
		m := greet(p)
		m.TTL = time.Hour
		wantNoErr(t, r.vol.Send(ctxT(t), m))
		return p
	}
	queued := short("q@example.org")
	failed := short("f@example.org")
	stale := short("s@example.org")
	live := short("l@example.org")
	fresh := r.queue("z@example.org") // 7 days
	r.ch.script(func(e notify.Envelope) error {
		if e.To == "f@example.org" {
			return errs.E("test.ch", errs.Invalid, notify.ErrRejected)
		}
		return errs.E("test.ch", errs.Unavailable, errors.New("park"))
	})
	ignore(queued)
	// Fail only the rejected row; leave others queued by not delivering them.
	r.exec("UPDATE core.notify_message SET next_attempt_at = $1 WHERE person_id <> $2", r.clk.Now().Add(100*time.Hour), [16]byte(failed))
	r.sendOnce()
	r.ch.script(nil)
	if x := r.one(failed); x.Status != "failed" {
		t.Fatalf("setup: %+v", x)
	}
	r.advance(2 * time.Hour)
	r.exec("UPDATE core.notify_message SET status='sending', leased_until=$1 WHERE person_id=$2", r.clk.Now().Add(-time.Minute), [16]byte(stale))
	r.exec("UPDATE core.notify_message SET status='sending', leased_until=$1 WHERE person_id=$2", r.clk.Now().Add(time.Minute), [16]byte(live))
	var sendToID [16]byte
	{
		m := greet()
		m.TTL = time.Hour
		// created two hours ago: back-date so it is stale too
		wantNoErr(t, r.svc.SendTo(ctxT(t), "direct@example.org", m))
		rs := r.rows("address IS NOT NULL")
		sendToID = rs[0].ID
		r.exec("UPDATE core.notify_message SET created_at = $1, expires_at = $2 WHERE id = $3", r.clk.Now().Add(-2*time.Hour), r.clk.Now().Add(-time.Second), sendToID)
	}

	n, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n < 3 {
		t.Fatalf("ExpireStale = %d", n)
	}
	for name, p := range map[string]model.PersonID{"queued": queued, "stale lease": stale} {
		x := r.one(p)
		if x.Status != "failed" || x.LastErr == nil || *x.LastErr != "expired" || x.FailedAt == nil ||
			x.Subject != nil || x.Body != nil || x.Address != nil {
			t.Errorf("%s: %+v", name, x)
		}
	}
	if x := r.one(failed); x.Status != "failed" || *x.LastErr != "rejected" || x.Subject != nil || x.Body != nil {
		t.Errorf("failed row: want only cleared, token kept: %+v", x)
	}
	if x := r.byID(sendToID); x.Address != nil || x.Status != "failed" {
		t.Errorf("SendTo row: %+v", x)
	}
	if x := r.one(live); x.Status != "sending" || x.Subject == nil {
		t.Errorf("a live lease was touched: %+v", x)
	}
	if x := r.one(fresh); x.Status != "queued" || x.Subject == nil {
		t.Errorf("an unexpired row was touched: %+v", x)
	}
	again, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if again != 0 {
		t.Errorf("second ExpireStale = %d, want 0", again)
	}
}

func TestExpireStale_Boundary_JustBeforeAndJustAfterExpiry(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = time.Hour
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	r.advance(time.Hour - time.Millisecond)
	n, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n != 0 || r.one(p).Status != "queued" {
		t.Fatalf("expired before expires_at: n=%d row=%+v", n, r.one(p))
	}
	r.advance(2 * time.Millisecond)
	n, err = r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n != 1 {
		t.Fatalf("ExpireStale = %d after the expiry", n)
	}
}

func TestExpireStale_ExactlyAtExpiry_IsEnded(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = time.Hour
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	r.advance(time.Hour)
	n, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n != 1 || r.one(p).Status != "failed" {
		t.Fatalf("a row at exactly expires_at must be ended (expires_at <= now): n=%d", n)
	}
}

func TestExpireStale_ManyRows_BatchedToCompletion(t *testing.T) {
	r := newIRig(t)
	r.exec(`INSERT INTO core.notify_message (id, owner, template, language, status, attempts, next_attempt_at, created_at, expires_at, subject, body, address)
		SELECT gen_random_uuid(), 'volunteers', 'volunteers.greet', 'en', 'queued', 0, $1, $3, $2, 's', 'b', 'x@example.org' FROM generate_series(1, 1005)`,
		r.clk.Now(), r.clk.Now().Add(-time.Hour), r.clk.Now().Add(-2*time.Hour))
	n, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n != 1005 {
		t.Fatalf("ExpireStale = %d, want 1005 (batches of 1000 until none)", n)
	}
	if got := r.rows("status = 'failed' AND subject IS NULL"); len(got) != 1005 {
		t.Fatalf("%d rows cleared", len(got))
	}
}

func TestExpireStale_ReadOnly_Real(t *testing.T) {
	r := newIRig(t)
	r.flt.set(true, nil)
	_, err := r.svc.ExpireStale(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
}

func TestExpireStale_Independent_OfRetention(t *testing.T) {
	r := newIRig(t)
	r.ret.set(0)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = time.Hour
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	r.advance(2 * time.Hour)
	n, err := r.svc.ExpireStale(ctxT(t))
	wantNoErr(t, err)
	if n != 1 {
		t.Fatalf("ExpireStale = %d with retention 0", n)
	}
}

func TestSendOnce_MalformedAddress_RejectedFinal(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.addrs.mu.Lock()
	r.addrs.m[p] = "not an address" // a bad address from the seam is final, never retried
	r.addrs.mu.Unlock()
	got := r.sendOnce()
	if got.Failed != 1 || got.Sent != 0 || got.Retried != 0 {
		t.Fatalf("Sent = %+v", got)
	}
	x := r.one(p)
	if x.Status != "failed" || x.LastErr == nil || (*x.LastErr != "address" && *x.LastErr != "rejected") {
		t.Fatalf("row = %+v", x)
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("sent to a malformed address")
	}
}

func TestSendOnce_AddressLookupFails_RequeuedAsLookup(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.addrs.mu.Lock()
	r.addrs.err = errs.E("test.addr", errs.Unavailable, errors.New("directory down"))
	r.addrs.mu.Unlock()
	got := r.sendOnce()
	if got.Retried != 1 || got.Failed != 0 {
		t.Fatalf("Sent = %+v", got)
	}
	x := r.one(p)
	if x.Status != "queued" || x.LastErr == nil || *x.LastErr != "lookup" {
		t.Fatalf("row = %+v", x)
	}
}
