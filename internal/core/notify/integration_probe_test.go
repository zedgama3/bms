//go:build integration

package notify_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// F77: with nothing set, the state says not configured, the outbox is counted, no
// conversation is tried and the error is nil (check prints "not configured").
func TestProbe_NothingSet_IsNotConfigured(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	st, err := notify.Probe(ctxT(t), pool, settingstest.NewFake("notify", notify.Settings), "bms.example.org")
	wantNoErr(t, err)
	if !st.Known {
		t.Error("Known = false: the settings were readable")
	}
	if st.Configured {
		t.Error("Configured = true with nothing set")
	}
	if st.Pinged || st.PingErr != nil {
		t.Errorf("Pinged = %v, PingErr = %v: no conversation is tried when unconfigured", st.Pinged, st.PingErr)
	}
	if st.OutboxErr != nil {
		t.Errorf("OutboxErr = %v on a migrated database", st.OutboxErr)
	}
	for status, n := range st.Outbox {
		if n != 0 {
			t.Errorf("outbox %v = %d on an empty outbox", status, n)
		}
	}
}

// F78: whatever is configured and queued, the state and the error carry no address, user
// name or password: the sender, reply-to, alert address, the SMTP user and password, and a
// queued message's recipient.
func TestProbe_NeverReturnsAnAddress(t *testing.T) {
	r := newIRig(t)
	const (
		user     = "probe-user-7q3"
		pw       = "probe-pw-7q3"
		sender   = "probe-sender-7q3@example.org"
		replyTo  = "probe-reply-7q3@example.org"
		alert    = "probe-alert-7q3@example.org"
		receiver = "probe-recipient-7q3@example.org"
	)
	r.queue(receiver)
	f := settingstest.NewFake("notify", notify.Settings)
	f.Set("smtp_host", "127.0.0.1")
	f.Set("smtp_port", fmt.Sprint(closedPort(t)))
	f.Set("smtp_security", "none")
	f.Set("smtp_username", user)
	f.SetSecret("smtp_password", secret.New(pw))
	f.Set("sender_address", sender)
	f.Set("reply_to", replyTo)
	f.Set("alert_addresses", alert)

	st, err := notify.Probe(ctxT(t), r.pool, f, "bms.example.org")
	if !st.Known || !st.Configured || !st.Pinged {
		t.Fatalf("state = %+v, want known, configured and a conversation tried", st)
	}
	// Nobody listens on the port: the conversation fails, which is reported, not hidden.
	if st.PingErr == nil || !errs.Is(st.PingErr, errs.Unavailable) {
		t.Errorf("PingErr = %v, want Unavailable", st.PingErr)
	}
	if err == nil {
		t.Error("Probe returned a nil error although the conversation failed")
	}
	if st.Outbox == nil {
		t.Fatalf("no outbox counts (OutboxErr = %v)", st.OutboxErr)
	}
	total := 0
	for _, n := range st.Outbox {
		total += n
	}
	if total != 1 {
		t.Errorf("outbox counts total %d, want the one queued message", total)
	}
	everything := fmt.Sprintf("%v|%+v|%#v|%v", st, st, st, err)
	for _, secretText := range []string{user, pw, sender, replyTo, alert, receiver, "7q3"} {
		if strings.Contains(everything, secretText) {
			t.Errorf("the result carries %q", secretText)
		}
	}
	if r.cap.Contains(user) || r.cap.Contains(pw) || r.cap.Contains(receiver) {
		t.Error("a log, span or metric carries a credential or an address")
	}
}

// A reachable server is a clean conversation: nil error, Pinged, no PingErr (Mailpit through
// BMS_TEST_SMTP_ADDR).
func TestProbe_ReachableServer_PingsClean(t *testing.T) {
	addr := os.Getenv("BMS_TEST_SMTP_ADDR")
	if addr == "" {
		t.Skip("BMS_TEST_SMTP_ADDR is not set; skipping the Mailpit probe")
	}
	host, port, err := splitAddr(addr)
	wantNoErr(t, err)
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	f := settingstest.NewFake("notify", notify.Settings)
	f.Set("smtp_host", host)
	f.Set("smtp_port", fmt.Sprint(port))
	f.Set("smtp_security", "none")
	f.Set("sender_address", "probe@example.org")
	st, err := notify.Probe(ctxT(t), pool, f, "bms.example.org")
	wantNoErr(t, err)
	if !st.Configured || !st.Pinged || st.PingErr != nil {
		t.Errorf("state = %+v, want configured, pinged, no ping error", st)
	}
	if st.Host != host || st.Port != port || st.Security != notify.None {
		t.Errorf("connection = %s:%d (%v), want %s:%d (none)", st.Host, st.Port, st.Security, host, port)
	}
}

// F79: a schema behind (no outbox table) or a database that is down is Unavailable, and the
// state still says what the settings said.
func TestProbe_SchemaBehind_ReturnsUnavailable(t *testing.T) {
	pool := dbtest.TempDB(t) // migrated by nothing: core.notify_message does not exist
	st, err := notify.Probe(ctxT(t), pool, settingstest.NewFake("notify", notify.Settings), "bms.example.org")
	wantKind(t, err, errs.Unavailable)
	if !st.Known {
		t.Error("Known = false: the settings were readable")
	}
	if st.OutboxErr == nil || !errs.Is(st.OutboxErr, errs.Unavailable) {
		t.Errorf("OutboxErr = %v, want Unavailable", st.OutboxErr)
	}
	if st.Outbox != nil {
		t.Errorf("Outbox = %v with the table missing, want nil", st.Outbox)
	}
}

func TestProbe_DatabaseDown_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	r.flt.set(false, errs.E("test.down", errs.Unavailable, fmt.Errorf("database down")))
	_, err := notify.Probe(ctxT(t), r.flt, settingstest.NewFake("notify", notify.Settings), "bms.example.org")
	wantKind(t, err, errs.Unavailable)
}

// The probe reads; it never writes: it works on a read-only copy.
func TestProbe_ReadOnlyCopy_Works(t *testing.T) {
	r := newIRig(t)
	r.flt.set(true, nil)
	_, err := notify.Probe(ctxT(t), r.flt, settingstest.NewFake("notify", notify.Settings), "bms.example.org")
	wantNoErr(t, err)
}
