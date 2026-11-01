package notify_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Probe validates its dependencies before any I/O.
func TestProbe_NilDependency_ReturnsInvalid(t *testing.T) {
	good := settingstest.NewFake("notify", notify.Settings)
	t.Run("no database", func(t *testing.T) {
		_, err := notify.Probe(ctxT(t), nil, good, "bms.example.org")
		wantKind(t, err, errs.Invalid)
	})
	t.Run("no reader", func(t *testing.T) {
		_, err := notify.Probe(ctxT(t), dbtest.NewFake(), nil, "bms.example.org")
		wantKind(t, err, errs.Invalid)
	})
}

// A reader of another settings area declares no SMTP key: the answer is Invalid with nothing
// known, never "not configured".
func TestProbe_WrongArea_ReturnsInvalid(t *testing.T) {
	st, err := notify.Probe(ctxT(t), dbtest.NewFake(), settingstest.NewFake("people", nil), "bms.example.org")
	wantKind(t, err, errs.Invalid)
	if st.Known || st.Configured {
		t.Errorf("state = %+v for an unreadable configuration, want nothing known", st)
	}
}

// Options.Channel replaces SMTP: Ping goes over the given channel even though the settings
// name a port nobody listens on, so the SMTP channel is not built (boundaries step 10).
func TestNew_ChannelOption_ReplacesSMTP(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "127.0.0.1"; c.Port = closedPort(t); c.Security = notify.None })
	wantNoErr(t, r.svc.Ping(ctxT(t)))
	if r.ch.pingCount() != 1 {
		t.Fatalf("pings over the given channel = %d, want 1", r.ch.pingCount())
	}
	r.ch.pingErr = errs.E("test.ping", errs.Unavailable, errors.New("ping failed"))
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
}

// With Channel nil the default SMTP channel is used: the same settings now fail the ping.
func TestNew_NilChannel_UsesSMTP(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Channel = nil })
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "127.0.0.1"; c.Port = closedPort(t); c.Security = notify.None })
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
}

// say writes one SMTP reply; a write error ends the conversation on the next read.
func say(c net.Conn, reply string) {
	if _, err := c.Write([]byte(reply)); err != nil {
		return
	}
}

// fakeSMTP is a loopback listener that speaks just enough SMTP for a connection check:
// the 220 greeting, EHLO and QUIT. It is closed with the test; the Ping that dials it is the
// synchronisation, so there are no sleeps.
func fakeSMTP(t *testing.T) int {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("not a TCP address")
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return // closed by the cleanup
			}
			go func() {
				defer func() {
					if err := c.Close(); err != nil {
						return // the peer already went away
					}
				}()
				say(c, "220 fake ESMTP\r\n")
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					cmd := strings.ToUpper(sc.Text())
					switch {
					case strings.HasPrefix(cmd, "QUIT"):
						say(c, "221 bye\r\n")
						return
					case strings.HasPrefix(cmd, "EHLO"):
						say(c, "250 fake\r\n")
					default:
						say(c, "250 ok\r\n")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Logf("close listener: %v", err)
		}
	})
	return addr.Port
}

// With Channel nil the SMTP channel is built from the CURRENT settings on every use: the
// same service fails Ping against a closed port, then succeeds once the settings name a
// listening server, and fails again when they are changed back.
func TestNew_NilChannel_BuildsFromTheCurrentSettings(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Channel = nil })
	closed, open := closedPort(t), fakeSMTP(t)
	point := func(port int) {
		r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "127.0.0.1"; c.Port = port; c.Security = notify.None })
	}
	point(closed)
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
	point(open)
	wantNoErr(t, r.svc.Ping(ctxT(t)))
	point(closed)
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
}
