//go:build integration

package smtp_test

// SMTP AUTH against the dev Mailpit (notify.md Proposed change 14, D40): the compose Mailpit
// accepts any credentials on a plaintext connection (MP_SMTP_AUTH_ACCEPT_ANY=1,
// MP_SMTP_AUTH_ALLOW_INSECURE=1), so the AUTH path of the channel runs for real. The fake
// server (F57) proves a refusal; Mailpit proves an accepted login delivers. Names and
// credentials are invented.

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/secret"
)

func TestMailpit_SendWithAuth_Delivered(t *testing.T) {
	m := newMailpit(t)
	to := uniqueAddr(t)
	m.cleanup(to)
	cfg := m.config()
	cfg.Username = "invented-user"
	cfg.Password = secret.New("invented-password-0001")
	ch := newChannel(t, cfg, opts())
	e := envelope()
	e.To = notifyEmail(to)
	e.Subject = "Mailpit AUTH delivery check"
	wantNoErr(t, ch.Send(ctxT(t), e))

	msgs := m.find(to)
	if len(msgs) != 1 {
		t.Fatalf("Mailpit holds %d messages for the recipient, want exactly 1", len(msgs))
	}
	if msgs[0].Subject != e.Subject {
		t.Errorf("Subject = %q", msgs[0].Subject)
	}
	var detail struct{ Text string }
	m.get("/api/v1/message/"+msgs[0].ID, &detail)
	if !strings.Contains(detail.Text, "You are on Gate at 06:00.") {
		t.Errorf("Text = %q", detail.Text)
	}
}

func TestMailpit_PingWithAuth_OK(t *testing.T) {
	m := newMailpit(t)
	cfg := m.config()
	cfg.Username = "invented-user"
	cfg.Password = secret.New("invented-password-0002")
	wantNoErr(t, newChannel(t, cfg, opts()).Ping(ctxT(t)))
}

// A username with no password (or the reverse) does not attempt AUTH, and the message still
// goes through the open relay.
func TestMailpit_HalfCredentials_SkipAuth_Delivered(t *testing.T) {
	m := newMailpit(t)
	for name, mod := range map[string]func(c *notify.SMTPConfig){
		"username only": func(c *notify.SMTPConfig) { c.Username = "invented-user" },
		"password only": func(c *notify.SMTPConfig) { c.Password = secret.New("invented-password-0003") },
	} {
		t.Run(name, func(t *testing.T) {
			to := uniqueAddr(t)
			m.cleanup(to)
			cfg := m.config()
			mod(&cfg)
			e := envelope()
			e.To = notifyEmail(to)
			wantNoErr(t, newChannel(t, cfg, opts()).Send(ctxT(t), e))
			if got := len(m.find(to)); got != 1 {
				t.Errorf("%d messages, want 1", got)
			}
		})
	}
}
