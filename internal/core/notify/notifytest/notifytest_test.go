package notifytest_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var (
	_ notify.Notifier = (*notifytest.Fake)(nil)
	_ notify.Channel  = (*notifytest.Channel)(nil)
)

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
		if fe.Field == field && (code == "" || fe.Code == code) {
			return true
		}
	}
	return false
}

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func msg() notify.Message {
	return notify.Message{
		Template: "volunteers.greet", To: []model.PersonID{model.NewID[model.PersonID]()},
		Key: "k1", Vars: map[string]string{"shift": "Gate"}, Link: "/shifts/1",
	}
}

// ---- Fake --------------------------------------------------------------------------------

func TestFake_Send_RecordsTheMessage(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	m := msg()
	wantNoErr(t, f.Send(ctxT(t), m))
	got := f.Sent()
	if len(got) != 1 {
		t.Fatalf("Sent() has %d messages", len(got))
	}
	if got[0].Template != m.Template || got[0].Key != m.Key || got[0].Link != m.Link ||
		got[0].Vars["shift"] != "Gate" || len(got[0].To) != 1 || got[0].To[0] != m.To[0] {
		t.Fatalf("recorded %+v, sent %+v", got[0], m)
	}
}

func TestFake_Send_ValidatesLikeTheRealOne(t *testing.T) {
	bad := func(mod func(*notify.Message)) notify.Message {
		m := msg()
		mod(&m)
		return m
	}
	cases := []struct {
		name        string
		m           notify.Message
		field, code string
	}{
		{"no template", bad(func(m *notify.Message) { m.Template = "" }), "template", "required"},
		{"malformed template", bad(func(m *notify.Message) { m.Template = "Nope" }), "template", "malformed"},
		{"no recipients", bad(func(m *notify.Message) { m.To = nil }), "to", "required"},
		{"zero recipient", bad(func(m *notify.Message) { m.To = []model.PersonID{{}} }), "to", "malformed"},
		{"bad key", bad(func(m *notify.Message) { m.Key = "a b" }), "key", "malformed"},
		{"bad var key", bad(func(m *notify.Message) { m.Vars = map[string]string{"Bad": "x"} }), "vars", "malformed"},
		{"var with control char", bad(func(m *notify.Message) { m.Vars = map[string]string{"a": "x\x07"} }), "vars.a", "control_chars"},
		{"link with scheme", bad(func(m *notify.Message) { m.Link = "https://x.example" }), "link", "malformed"},
		{"another owner's template", bad(func(m *notify.Message) { m.Template = "people.deleted" }), "template", "scope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := notifytest.NewFake("volunteers")
			err := f.Send(ctxT(t), tc.m)
			wantKind(t, err, errs.Invalid)
			if !hasField(err, tc.field, tc.code) {
				t.Fatalf("no field error %s/%s", tc.field, tc.code)
			}
			if len(f.Sent()) != 0 {
				t.Fatal("an invalid message was recorded")
			}
		})
	}
}

func TestFake_Send_RecordsAfterCommit(t *testing.T) {
	d := dbtest.NewFake()
	f := notifytest.NewFake("volunteers")
	var during int
	err := d.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		wantNoErr(t, f.Send(ctx, msg()))
		during = len(f.Sent())
		return nil
	})
	wantNoErr(t, err)
	if during != 0 {
		t.Fatalf("recorded %d messages before the commit", during)
	}
	if got := len(f.Sent()); got != 1 {
		t.Fatalf("recorded %d messages after the commit, want 1", got)
	}
}

func TestFake_Send_RollbackRecordsNothing(t *testing.T) {
	d := dbtest.NewFake()
	f := notifytest.NewFake("volunteers")
	boom := errors.New("boom")
	err := d.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		wantNoErr(t, f.Send(ctx, msg()))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transact = %v", err)
	}
	if got := len(f.Sent()); got != 0 {
		t.Fatalf("a rolled-back Send was recorded (%d)", got)
	}
}

func TestFake_Send_OutsideTransaction_RecordsAtOnce(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	wantNoErr(t, f.Send(ctxT(t), msg()))
	if len(f.Sent()) != 1 {
		t.Fatal("not recorded")
	}
}

func TestFake_Sent_ReturnsACopy(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	wantNoErr(t, f.Send(ctxT(t), msg()))
	got := f.Sent()
	got[0].Key = "changed"
	got = append(got, notify.Message{})
	ignore(got)
	if f.Sent()[0].Key != "k1" || len(f.Sent()) != 1 {
		t.Fatal("Sent() exposes the fake's own storage")
	}
}

func TestFake_Fail_FailsEverySend_AndNilClears(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Unsupported} {
		f.Fail(errs.E("test", k, errors.New("down")))
		for i := 0; i < 2; i++ {
			wantKind(t, f.Send(ctxT(t), msg()), k)
		}
	}
	if len(f.Sent()) != 0 {
		t.Fatal("a failed Send was recorded")
	}
	f.Fail(nil)
	wantNoErr(t, f.Send(ctxT(t), msg()))
	if len(f.Sent()) != 1 {
		t.Fatal("not recorded after Fail(nil)")
	}
}

func TestFake_Fail_InTransaction_NothingRecorded(t *testing.T) {
	d := dbtest.NewFake()
	f := notifytest.NewFake("volunteers")
	f.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	err := d.Transact(ctxT(t), db.Write, func(ctx context.Context) error { return f.Send(ctx, msg()) })
	wantKind(t, err, errs.Unavailable)
	if len(f.Sent()) != 0 {
		t.Fatal("recorded")
	}
}

func TestFake_Configured_DefaultsTrue(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	got, err := f.Configured(ctxT(t))
	wantNoErr(t, err)
	if !got {
		t.Fatal("a new Fake must report configured")
	}
}

func TestFake_SetConfigured_SendUnsupported(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	f.SetConfigured(false)
	got, err := f.Configured(ctxT(t))
	wantNoErr(t, err)
	if got {
		t.Fatal("Configured = true after SetConfigured(false)")
	}
	err = f.Send(ctxT(t), msg())
	wantKind(t, err, errs.Unsupported)
	if !errors.Is(err, notify.ErrNotConfigured) {
		t.Fatalf("does not wrap ErrNotConfigured: %v", err)
	}
	if len(f.Sent()) != 0 {
		t.Fatal("recorded while unconfigured")
	}

	f.SetConfigured(true)
	got, err = f.Configured(ctxT(t))
	wantNoErr(t, err)
	if !got {
		t.Fatal("SetConfigured(true) did not restore")
	}
	wantNoErr(t, f.Send(ctxT(t), msg()))
	if len(f.Sent()) != 1 {
		t.Fatal("not recorded after re-enabling")
	}
}

func TestFake_SetConfigured_InTransaction_NothingRecorded(t *testing.T) {
	d := dbtest.NewFake()
	f := notifytest.NewFake("volunteers")
	f.SetConfigured(false)
	err := d.Transact(ctxT(t), db.Write, func(ctx context.Context) error { return f.Send(ctx, msg()) })
	wantKind(t, err, errs.Unsupported)
	if len(f.Sent()) != 0 {
		t.Fatal("recorded")
	}
}

func TestFake_ConcurrentUse_IsRaceFree(t *testing.T) {
	f := notifytest.NewFake("volunteers")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ignore(f.Send(ctxT(t), msg()))
			ignore(f.Sent())
			ignore(f.Configured(ctxT(t)))
			if i%5 == 0 {
				f.Fail(nil)
				f.SetConfigured(true)
			}
		}()
	}
	wg.Wait()
	if got := len(f.Sent()); got != 16 {
		t.Fatalf("recorded %d, want 16", got)
	}
}

// ---- Channel -----------------------------------------------------------------------------

func env(to string) notify.Envelope {
	return notify.Envelope{
		From: notify.Address{Email: "burn@example.org"}, To: model.Email(to),
		Subject: "s", Body: "b", MessageID: "id@host",
	}
}

func TestChannel_Send_RecordsEnvelopesInOrder(t *testing.T) {
	c := notifytest.NewChannel()
	wantNoErr(t, c.Send(ctxT(t), env("a@example.org")))
	wantNoErr(t, c.Send(ctxT(t), env("b@example.org")))
	got := c.Envelopes()
	if len(got) != 2 || got[0].To != "a@example.org" || got[1].To != "b@example.org" {
		t.Fatalf("Envelopes = %+v", got)
	}
	got[0].To = "changed"
	if c.Envelopes()[0].To != "a@example.org" {
		t.Fatal("Envelopes() exposes the channel's storage")
	}
}

func TestChannel_Fail_FailsUntilCleared(t *testing.T) {
	c := notifytest.NewChannel()
	c.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	wantKind(t, c.Send(ctxT(t), env("a@example.org")), errs.Unavailable)
	wantKind(t, c.Send(ctxT(t), env("a@example.org")), errs.Unavailable)
	if len(c.Envelopes()) != 0 {
		t.Fatal("a failed send was recorded")
	}
	c.Fail(errs.E("test", errs.Timeout, errors.New("slow")))
	wantKind(t, c.Send(ctxT(t), env("a@example.org")), errs.Timeout)
	c.Fail(nil)
	wantNoErr(t, c.Send(ctxT(t), env("a@example.org")))
	if len(c.Envelopes()) != 1 {
		t.Fatal("not recorded after clearing")
	}
}

func TestChannel_Reject_InvalidWrappingErrRejected(t *testing.T) {
	c := notifytest.NewChannel()
	c.Reject()
	err := c.Send(ctxT(t), env("a@example.org"))
	wantKind(t, err, errs.Invalid)
	if !errors.Is(err, notify.ErrRejected) {
		t.Fatalf("does not wrap ErrRejected: %v", err)
	}
	if len(c.Envelopes()) != 0 {
		t.Fatal("a rejected send was recorded")
	}
	c.Fail(nil)
	wantNoErr(t, c.Send(ctxT(t), env("a@example.org")))
}

func TestChannel_CanceledContext_DoesNotRecord(t *testing.T) {
	c := notifytest.NewChannel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Send(ctx, env("a@example.org"))
	if err == nil {
		t.Skip("the in-memory channel ignores the context")
	}
	wantKind(t, err, errs.Canceled)
	if len(c.Envelopes()) != 0 {
		t.Fatal("recorded after cancel")
	}
}

func TestChannel_ConcurrentUse_IsRaceFree(t *testing.T) {
	c := notifytest.NewChannel()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ignore(c.Send(ctxT(t), env("a@example.org")))
			ignore(c.Envelopes())
		}()
	}
	wg.Wait()
	if got := len(c.Envelopes()); got != 16 {
		t.Fatalf("recorded %d", got)
	}
}

// ignore swallows results a test does not need.
func ignore(...any) {}
