package smtp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/smtp"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

var epoch = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

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

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func configFor(s *server, mod ...func(*notify.SMTPConfig)) notify.SMTPConfig {
	host, port := s.hostPort()
	c := notify.SMTPConfig{
		Host: host, Port: port, Security: notify.None,
		Sender: notify.Address{Name: "Test Burn", Email: "burn@example.org"},
	}
	for _, m := range mod {
		m(&c)
	}
	return c
}

func opts() smtp.Options {
	return smtp.Options{SendTimeout: 10 * time.Second, Hostname: "bms-test.example", Clock: clock.NewFake(epoch)}
}

func newChannel(t *testing.T, cfg notify.SMTPConfig, o smtp.Options) notify.Channel {
	t.Helper()
	ch, err := smtp.New(cfg, o)
	if err != nil {
		t.Fatalf("smtp.New: %v", err)
	}
	return ch
}

func envelope() notify.Envelope {
	return notify.Envelope{
		From:      notify.Address{Name: "Test Burn", Email: "burn@example.org"},
		To:        "volunteer@example.org",
		Subject:   "Your shift",
		Body:      "You are on Gate at 06:00.\nSee you there.\n",
		MessageID: "0f3c9a52@bms-test.example",
	}
}

// parse splits a captured message into its header and decoded body.
func parse(t *testing.T, raw []byte) (mail.Header, string) {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("captured message does not parse: %v\n%s", err, raw)
	}
	body, err := io.ReadAll(m.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return m.Header, string(body)
}

// ---- F55 -------------------------------------------------------------------------------

func TestSMTPNew_Invalid_Table(t *testing.T) {
	good := func() notify.SMTPConfig {
		return notify.SMTPConfig{Host: "smtp.example.org", Port: 587, Security: notify.StartTLS,
			Sender: notify.Address{Email: "burn@example.org"}}
	}
	cases := []struct {
		name string
		mod  func(*notify.SMTPConfig)
	}{
		{"zero config", func(c *notify.SMTPConfig) { *c = notify.SMTPConfig{} }},
		{"no host (not configured)", func(c *notify.SMTPConfig) { c.Host = "" }},
		{"no sender (not configured)", func(c *notify.SMTPConfig) { c.Sender = notify.Address{} }},
		{"port zero", func(c *notify.SMTPConfig) { c.Port = 0 }},
		{"port too big", func(c *notify.SMTPConfig) { c.Port = 70000 }},
		{"security zero", func(c *notify.SMTPConfig) { c.Security = 0 }},
		{"host with space", func(c *notify.SMTPConfig) { c.Host = "a b" }},
		{"sender malformed", func(c *notify.SMTPConfig) { c.Sender.Email = "nope" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			tc.mod(&c)
			ch, err := smtp.New(c, opts())
			wantKind(t, err, errs.Invalid)
			if ch != nil {
				t.Fatal("a channel came back with the error")
			}
		})
	}
	t.Run("valid config builds without any I/O", func(t *testing.T) {
		s := newServer(t, nil)
		ignore(newChannel(t, configFor(s), opts()))
		if n := s.connections(); n != 0 {
			t.Fatalf("New connected %d times", n)
		}
	})
	t.Run("zero Options are usable", func(t *testing.T) {
		_, err := smtp.New(good(), smtp.Options{})
		wantNoErr(t, err)
	})
}

// ---- the message bytes -------------------------------------------------------------------

func TestSMTPSend_Delivers_ExactHeadersAndBody(t *testing.T) {
	s := newServer(t, nil)
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
		c.ReplyTo = &notify.Address{Name: "Help Desk", Email: "help@example.org"}
	}), opts())
	e := envelope()
	wantNoErr(t, ch.Send(ctxT(t), e))

	msgs := s.messages()
	if len(msgs) != 1 {
		t.Fatalf("server got %d messages, want 1", len(msgs))
	}
	h, body := parse(t, msgs[0])
	from, err := mail.ParseAddress(h.Get("From"))
	wantNoErr(t, err)
	if from.Address != "burn@example.org" || from.Name != "Test Burn" {
		t.Errorf("From = %+v", from)
	}
	to, err := mail.ParseAddress(h.Get("To"))
	wantNoErr(t, err)
	if to.Address != "volunteer@example.org" {
		t.Errorf("To = %+v", to)
	}
	if got := h.Get("Subject"); got != "Your shift" {
		t.Errorf("Subject = %q", got)
	}
	date, err := h.Date()
	wantNoErr(t, err)
	if !date.Equal(epoch) {
		t.Errorf("Date = %v, want the injected clock's %v", date, epoch)
	}
	if got := h.Get("Message-ID"); got != "<0f3c9a52@bms-test.example>" {
		t.Errorf("Message-ID = %q", got)
	}
	if got := h.Get("MIME-Version"); got != "1.0" {
		t.Errorf("MIME-Version = %q", got)
	}
	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	wantNoErr(t, err)
	if mt != "text/plain" || !strings.EqualFold(params["charset"], "utf-8") {
		t.Errorf("Content-Type = %q %v", mt, params)
	}
	if got := strings.ToLower(h.Get("Content-Transfer-Encoding")); got != "quoted-printable" {
		t.Errorf("Content-Transfer-Encoding = %q", got)
	}
	if got := h.Get("Auto-Submitted"); got != "auto-generated" {
		t.Errorf("Auto-Submitted = %q", got)
	}
	rt, err := mail.ParseAddress(h.Get("Reply-To"))
	wantNoErr(t, err)
	if rt.Address != "help@example.org" {
		t.Errorf("Reply-To = %+v", rt)
	}
	if got := strings.ReplaceAll(decodeQP(t, body), "\r\n", "\n"); got != e.Body {
		t.Errorf("body = %q, want %q", got, e.Body)
	}

	cmds := strings.Join(s.commands(), "\n")
	for _, want := range []string{"EHLO bms-test.example", "MAIL FROM:<burn@example.org>", "RCPT TO:<volunteer@example.org>", "QUIT"} {
		if !strings.Contains(cmds, want) {
			t.Errorf("conversation lacks %q:\n%s", want, cmds)
		}
	}
}

func TestSMTPSend_NoReplyTo_NoHeader(t *testing.T) {
	s := newServer(t, nil)
	ch := newChannel(t, configFor(s), opts())
	wantNoErr(t, ch.Send(ctxT(t), envelope()))
	h, _ := parse(t, s.messages()[0])
	if got := h.Get("Reply-To"); got != "" {
		t.Fatalf("Reply-To = %q, want none", got)
	}
}

func TestSMTPSend_EnvelopeReplyTo_Header(t *testing.T) {
	s := newServer(t, nil)
	ch := newChannel(t, configFor(s), opts())
	e := envelope()
	e.ReplyTo = &notify.Address{Email: "boss@example.org"}
	wantNoErr(t, ch.Send(ctxT(t), e))
	h, _ := parse(t, s.messages()[0])
	rt, err := mail.ParseAddress(h.Get("Reply-To"))
	wantNoErr(t, err)
	if rt.Address != "boss@example.org" {
		t.Fatalf("Reply-To = %+v", rt)
	}
}

func decodeQP(t *testing.T, s string) string {
	t.Helper()
	b, err := io.ReadAll(newQP(strings.NewReader(s)))
	if err != nil {
		t.Fatalf("quoted-printable: %v", err)
	}
	return string(b)
}

func TestSMTPSend_BodyEdgeCases_Survive(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"lone dot line (dot stuffing)", "before\n.\nafter\n"},
		{"line starting with a dot", "before\n.hidden\nafter\n"},
		{"equals signs", "a=b and =3D literal\n"},
		{"very long line", strings.Repeat("word ", 400) + "\n"},
		{"non-ascii body", "Tu turno: señor Núñez – 06:00 ☀\n"},
		{"trailing spaces", "line with trailing space \nnext\n"},
		{"empty body", ""},
		{"no trailing newline", "last line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, nil)
			ch := newChannel(t, configFor(s), opts())
			e := envelope()
			e.Body = tc.body
			wantNoErr(t, ch.Send(ctxT(t), e))
			msgs := s.messages()
			if len(msgs) != 1 {
				t.Fatalf("server got %d messages", len(msgs))
			}
			_, body := parse(t, msgs[0])
			got := strings.ReplaceAll(decodeQP(t, body), "\r\n", "\n")
			if strings.TrimRight(got, "\n") != strings.TrimRight(tc.body, "\n") {
				t.Fatalf("body = %q, want %q", got, tc.body)
			}
			for _, line := range strings.Split(body, "\n") {
				if len(strings.TrimRight(line, "\r")) > 76 {
					t.Fatalf("encoded line over 76 characters: %q", line)
				}
			}
		})
	}
}

func TestSMTPSend_NonASCII_Encoded(t *testing.T) {
	s := newServer(t, nil)
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
		c.Sender = notify.Address{Name: "José Núñez", Email: "burn@example.org"}
	}), opts())
	e := envelope()
	e.From = notify.Address{Name: "José Núñez", Email: "burn@example.org"}
	e.Subject = "Turno de guardia – señor Núñez ☀"
	wantNoErr(t, ch.Send(ctxT(t), e))

	raw := s.messages()[0]
	head, _, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	for _, b := range head {
		if b > unicode.MaxASCII {
			t.Fatalf("header block holds raw non-ASCII bytes:\n%s", head)
		}
	}
	h, _ := parse(t, raw)
	dec := new(mime.WordDecoder)
	subj, err := dec.DecodeHeader(h.Get("Subject"))
	wantNoErr(t, err)
	if subj != e.Subject {
		t.Fatalf("Subject decodes to %q, want %q", subj, e.Subject)
	}
	from, err := mail.ParseAddress(h.Get("From"))
	wantNoErr(t, err)
	if from.Name != "José Núñez" {
		t.Fatalf("From name decodes to %q", from.Name)
	}
}

func TestSMTPSend_HeaderInjection_Refused(t *testing.T) {
	t.Run("subject", func(t *testing.T) {
		s := newServer(t, nil)
		ch := newChannel(t, configFor(s), opts())
		e := envelope()
		e.Subject = "Hello\r\nBcc: evil@example.org"
		err := ch.Send(ctxT(t), e)
		if err == nil {
			h, _ := parse(t, s.messages()[0])
			if h.Get("Bcc") != "" {
				t.Fatal("a Bcc header was injected through the subject")
			}
			return
		}
		wantKind(t, err, errs.Invalid)
	})
	t.Run("recipient", func(t *testing.T) {
		s := newServer(t, nil)
		ch := newChannel(t, configFor(s), opts())
		e := envelope()
		e.To = "a@example.org\r\nRCPT TO:<evil@example.org>"
		err := ch.Send(ctxT(t), e)
		wantKind(t, err, errs.Invalid)
		if s.saw("RCPT TO:<evil") {
			t.Fatal("the recipient smuggled a second RCPT command")
		}
		if len(s.messages()) != 0 {
			t.Fatal("something was delivered")
		}
	})
	t.Run("display name", func(t *testing.T) {
		s := newServer(t, nil)
		ch := newChannel(t, configFor(s), opts())
		e := envelope()
		e.From.Name = "Burn\r\nBcc: evil@example.org"
		if err := ch.Send(ctxT(t), e); err == nil {
			h, _ := parse(t, s.messages()[0])
			if h.Get("Bcc") != "" {
				t.Fatal("a Bcc header was injected through the sender name")
			}
		}
	})
}

// ---- server replies (F57..F59) -----------------------------------------------------------

func TestSMTPSend_5xx_Rejected(t *testing.T) {
	for _, stage := range []string{"MAIL", "RCPT", "DATA", "DATAEND"} {
		for _, code := range []string{"550 5.1.1 mailbox unavailable", "554 5.7.1 refused", "501 5.5.4 syntax"} {
			t.Run(stage+"/"+code[:3], func(t *testing.T) {
				s := newServer(t, func(s *server) { s.replies[stage] = code })
				ch := newChannel(t, configFor(s), opts())
				err := ch.Send(ctxT(t), envelope())
				wantKind(t, err, errs.Invalid)
				if !errors.Is(err, notify.ErrRejected) {
					t.Fatalf("does not wrap ErrRejected: %v", err)
				}
			})
		}
	}
}

func TestSMTPSend_4xx_Unavailable(t *testing.T) {
	for _, stage := range []string{"GREETING", "MAIL", "RCPT", "DATA", "DATAEND"} {
		for _, code := range []string{"421 4.3.2 shutting down", "450 4.2.1 mailbox busy", "451 4.3.0 local error", "452 4.5.3 too many"} {
			t.Run(stage+"/"+code[:3], func(t *testing.T) {
				s := newServer(t, func(s *server) { s.replies[stage] = code })
				ch := newChannel(t, configFor(s), opts())
				err := ch.Send(ctxT(t), envelope())
				wantKind(t, err, errs.Unavailable)
				if errors.Is(err, notify.ErrRejected) {
					t.Fatal("a 4xx reply must be retried, not rejected")
				}
			})
		}
	}
}

func TestSMTPSend_AuthRefused_Unavailable(t *testing.T) {
	s := newServer(t, func(s *server) { s.authAd = true; s.replies["AUTH"] = "535 5.7.8 bad credentials" })
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
		c.Username = "mailer"
		c.Password = secret.New("invented-pass-1")
	}), opts())
	err := ch.Send(ctxT(t), envelope())
	wantKind(t, err, errs.Unavailable)
	if errors.Is(err, notify.ErrRejected) {
		t.Fatal("a refused login must not reject the message for good")
	}
	if len(s.messages()) != 0 {
		t.Fatal("a message went out after the login was refused")
	}
	if s.saw("MAIL") {
		t.Fatal("the conversation went on after the login was refused")
	}
}

func TestSMTPSend_Auth_SendsCredentialsOnce(t *testing.T) {
	s := newServer(t, func(s *server) { s.authAd = true })
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
		c.Username = "mailer"
		c.Password = secret.New("invented-pass-1")
	}), opts())
	wantNoErr(t, ch.Send(ctxT(t), envelope()))
	var auth string
	for _, c := range s.commands() {
		if strings.HasPrefix(strings.ToUpper(c), "AUTH PLAIN ") {
			auth = strings.Fields(c)[2]
		}
	}
	if auth == "" {
		t.Skipf("the channel did not log in with PLAIN (commands: %v)", s.commands())
	}
	raw, err := base64.StdEncoding.DecodeString(auth)
	wantNoErr(t, err)
	parts := bytes.Split(raw, []byte{0})
	if len(parts) != 3 || string(parts[1]) != "mailer" || string(parts[2]) != "invented-pass-1" {
		t.Fatalf("PLAIN credentials = %q", raw)
	}
	if len(s.messages()) != 1 {
		t.Fatal("message not delivered after login")
	}
}

func TestSMTPSend_NoCredentials_NoAuthAttempt(t *testing.T) {
	s := newServer(t, func(s *server) { s.authAd = true })
	ch := newChannel(t, configFor(s), opts())
	wantNoErr(t, ch.Send(ctxT(t), envelope()))
	if s.saw("AUTH") {
		t.Fatal("the channel logged in without a username or password")
	}
}

func TestSMTPSend_ServerReplyText_NeverInError(t *testing.T) {
	const marker = "leak-addr-9d41@example.org"
	for _, stage := range []string{"MAIL", "RCPT", "DATA", "AUTH"} {
		for _, code := range []string{"550", "450"} {
			t.Run(stage+"/"+code, func(t *testing.T) {
				s := newServer(t, func(s *server) {
					s.authAd = true
					s.replies[stage] = code + " 5.1.1 <" + marker + "> no such user here"
				})
				ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
					c.Username, c.Password = "mailer", secret.New("pw-invented")
				}), opts())
				err := ch.Send(ctxT(t), envelope())
				if err == nil {
					t.Fatal("want an error")
				}
				for _, text := range []string{err.Error(), strings.ToLower(err.Error())} {
					if strings.Contains(text, marker) || strings.Contains(text, "no such user") {
						t.Fatalf("error carries the server's reply text: %v", err)
					}
				}
			})
		}
	}
}

func TestSMTPSend_ErrorsNeverCarryAddressOrBody(t *testing.T) {
	const addr, subj, body = "secret-rcpt-31@example.org", "SUBJECTMARK-71", "BODYMARK-55"
	s := newServer(t, func(s *server) { s.replies["RCPT"] = "450 try later" })
	ch := newChannel(t, configFor(s), opts())
	e := envelope()
	e.To, e.Subject, e.Body = addr, subj, body
	err := ch.Send(ctxT(t), e)
	wantKind(t, err, errs.Unavailable)
	for _, m := range []string{addr, subj, body} {
		if strings.Contains(err.Error(), m) {
			t.Fatalf("error carries %q: %v", m, err)
		}
	}
}

// ---- connect, timeout, cancel (F56, F60) -------------------------------------------------

func closedConfig(t *testing.T) notify.SMTPConfig {
	t.Helper()
	s := newServer(t, nil)
	cfg := configFor(s)
	if err := s.ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return cfg
}

func TestSMTPSend_Connect_Unavailable(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		ch := newChannel(t, closedConfig(t), opts())
		wantKind(t, ch.Send(ctxT(t), envelope()), errs.Unavailable)
	})
	t.Run("DNS failure", func(t *testing.T) {
		ch := newChannel(t, notify.SMTPConfig{Host: "no-such-host.invalid", Port: 25, Security: notify.None,
			Sender: notify.Address{Email: "burn@example.org"}}, opts())
		wantKind(t, ch.Send(ctxT(t), envelope()), errs.Unavailable)
	})
}

func TestSMTPSend_Deadline_Timeout(t *testing.T) {
	for _, stage := range []string{"GREETING", "EHLO", "MAIL", "RCPT", "DATA", "DATAEND", "QUIT"} {
		t.Run("ctx deadline/"+stage, func(t *testing.T) {
			s := newServer(t, func(s *server) { s.stall = stage })
			ch := newChannel(t, configFor(s), opts())
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			err := ch.Send(ctx, envelope())
			if stage == "QUIT" && err == nil {
				return // the message was accepted; a stuck QUIT may be forgiven
			}
			wantKind(t, err, errs.Timeout)
		})
	}
	t.Run("already expired", func(t *testing.T) {
		s := newServer(t, nil)
		ch := newChannel(t, configFor(s), opts())
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		err := ch.Send(ctx, envelope())
		wantKind(t, err, errs.Timeout)
		if len(s.messages()) != 0 {
			t.Fatal("delivered with an expired deadline")
		}
	})
	t.Run("SendTimeout bounds the conversation", func(t *testing.T) {
		s := newServer(t, func(s *server) { s.stall = "RCPT" })
		o := opts()
		o.SendTimeout = 150 * time.Millisecond
		ch := newChannel(t, configFor(s), o)
		wantKind(t, ch.Send(ctxT(t), envelope()), errs.Timeout)
	})
}

func TestSMTPSend_Canceled_Canceled(t *testing.T) {
	t.Run("before", func(t *testing.T) {
		s := newServer(t, nil)
		ch := newChannel(t, configFor(s), opts())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		wantKind(t, ch.Send(ctx, envelope()), errs.Canceled)
		if len(s.messages()) != 0 {
			t.Fatal("delivered after cancel")
		}
	})
	t.Run("mid-conversation", func(t *testing.T) {
		s := newServer(t, func(s *server) { s.stall = "RCPT" })
		ch := newChannel(t, configFor(s), opts())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-s.stalled
			cancel()
		}()
		wantKind(t, ch.Send(ctx, envelope()), errs.Canceled)
	})
}

// ---- TLS ---------------------------------------------------------------------------------

func TestSMTPSend_StartTLSRequired_NotOffered_Unavailable(t *testing.T) {
	s := newServer(t, nil) // does not advertise STARTTLS
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) { c.Security = notify.StartTLS }), opts())
	wantKind(t, ch.Send(ctxT(t), envelope()), errs.Unavailable)
	if s.saw("MAIL") || len(s.messages()) != 0 {
		t.Fatal("the message was sent in plaintext although STARTTLS was required")
	}
}

func TestSMTPSend_StartTLS_UntrustedCertificate_Unavailable(t *testing.T) {
	cfg, _ := selfSigned(t)
	s := newServer(t, func(s *server) { s.starttls = true; s.tlsCfg = cfg })
	o := opts()
	o.TLSConfig = nil // the system roots do not trust the test certificate
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) { c.Security = notify.StartTLS }), o)
	wantKind(t, ch.Send(ctxT(t), envelope()), errs.Unavailable)
	if s.saw("MAIL") || len(s.messages()) != 0 {
		t.Fatal("the message was sent although the handshake failed")
	}
}

func TestSMTPSend_ImplicitTLS_UntrustedCertificate_Unavailable(t *testing.T) {
	cfg, _ := selfSigned(t)
	s := newServer(t, func(s *server) { s.implicit = true; s.tlsCfg = cfg })
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) { c.Security = notify.TLS }), opts())
	wantKind(t, ch.Send(ctxT(t), envelope()), errs.Unavailable)
	if len(s.messages()) != 0 {
		t.Fatal("delivered although the handshake failed")
	}
}

func TestSMTPSend_NoneSecurity_NeverStartsTLS(t *testing.T) {
	cfg, _ := selfSigned(t)
	s := newServer(t, func(s *server) { s.starttls = true; s.tlsCfg = cfg })
	ch := newChannel(t, configFor(s), opts()) // Security None
	wantNoErr(t, ch.Send(ctxT(t), envelope()))
	if s.saw("STARTTLS") {
		t.Fatal("Security none issued STARTTLS")
	}
}

// ---- Ping --------------------------------------------------------------------------------

func TestSMTPPing_Success_NoMessageSent(t *testing.T) {
	s := newServer(t, func(s *server) { s.authAd = true })
	ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
		c.Username, c.Password = "mailer", secret.New("pw-invented")
	}), opts())
	wantNoErr(t, ch.Ping(ctxT(t)))
	if s.saw("MAIL") || s.saw("RCPT") || s.saw("DATA") {
		t.Fatalf("Ping started a mail transaction: %v", s.commands())
	}
	if !s.saw("EHLO") || !s.saw("QUIT") {
		t.Fatalf("Ping did not greet and quit: %v", s.commands())
	}
}

func TestSMTPPing_Failures_Table(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*server)
		kind errs.Kind
	}{
		{"greeting 421", func(s *server) { s.replies["GREETING"] = "421 busy" }, errs.Unavailable},
		{"auth refused", func(s *server) { s.authAd = true; s.replies["AUTH"] = "535 no" }, errs.Unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, tc.mod)
			ch := newChannel(t, configFor(s, func(c *notify.SMTPConfig) {
				c.Username, c.Password = "mailer", secret.New("pw-invented")
			}), opts())
			wantKind(t, ch.Ping(ctxT(t)), tc.kind)
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		ch := newChannel(t, closedConfig(t), opts())
		wantKind(t, ch.Ping(ctxT(t)), errs.Unavailable)
	})
	t.Run("stalled", func(t *testing.T) {
		s := newServer(t, func(s *server) { s.stall = "EHLO" })
		ch := newChannel(t, configFor(s), opts())
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		wantKind(t, ch.Ping(ctx), errs.Timeout)
	})
	t.Run("canceled", func(t *testing.T) {
		s := newServer(t, nil)
		ch := newChannel(t, configFor(s), opts())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		wantKind(t, ch.Ping(ctx), errs.Canceled)
	})
}

// ---- concurrency -------------------------------------------------------------------------

func TestSMTPSend_Concurrent_AllDelivered(t *testing.T) {
	s := newServer(t, nil)
	ch := newChannel(t, configFor(s), opts())
	const n = 8
	var wg sync.WaitGroup
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := envelope()
			e.To = model.Email("p" + fmt.Sprintf("%d", i) + "@example.org")
			errc <- ch.Send(ctxT(t), e)
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		wantNoErr(t, err)
	}
	if got := len(s.messages()); got != n {
		t.Fatalf("delivered %d of %d", got, n)
	}
}

var _ = textproto.CanonicalMIMEHeaderKey

// ignore swallows results a test does not need.
func ignore(...any) {}
