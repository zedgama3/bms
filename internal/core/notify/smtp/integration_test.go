//go:build integration

package smtp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// mailpit is the compose Mailpit: SMTP on BMS_TEST_SMTP_ADDR, HTTP API on
// BMS_TEST_MAILPIT_URL. Tests skip, with a message, when either is missing.
type mailpit struct {
	t    *testing.T
	host string
	port int
	api  string
}

func newMailpit(t *testing.T) *mailpit {
	t.Helper()
	addr := os.Getenv("BMS_TEST_SMTP_ADDR")
	api := strings.TrimRight(os.Getenv("BMS_TEST_MAILPIT_URL"), "/")
	if addr == "" {
		t.Skip("BMS_TEST_SMTP_ADDR is not set (compose: localhost:1025); skipping the Mailpit tests")
	}
	if api == "" {
		t.Skip("BMS_TEST_MAILPIT_URL is not set (add BMS_TEST_MAILPIT_URL=http://localhost:8025 to deploy/compose/.env); skipping the Mailpit tests")
	}
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("BMS_TEST_SMTP_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("BMS_TEST_SMTP_ADDR port: %v", err)
	}
	return &mailpit{t: t, host: host, port: port, api: api}
}

func (m *mailpit) config() notify.SMTPConfig {
	return notify.SMTPConfig{
		Host: m.host, Port: m.port, Security: notify.None,
		Sender: notify.Address{Name: "Test Burn", Email: "burn@example.org"},
	}
}

func (m *mailpit) get(path string, v any) []byte {
	m.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.api+path, nil)
	if err != nil {
		m.t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.t.Fatalf("Mailpit API unreachable: %v", err)
	}
	defer func() { ignore(resp.Body.Close()) }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		m.t.Fatalf("reading Mailpit reply: %v", err)
	}
	if resp.StatusCode != 200 {
		m.t.Fatalf("Mailpit %s: status %d", path, resp.StatusCode)
	}
	if v != nil {
		if err := json.Unmarshal(b, v); err != nil {
			m.t.Fatalf("Mailpit reply: %v", err)
		}
	}
	return b
}

type found struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
}

// find returns the messages whose recipient is addr.
func (m *mailpit) find(addr string) []found {
	m.t.Helper()
	var out struct {
		Messages []found `json:"messages"`
	}
	m.get("/api/v1/search?query="+url.QueryEscape("to:"+addr), &out)
	return out.Messages
}

// cleanup deletes what this test created.
func (m *mailpit) cleanup(addr string) {
	m.t.Cleanup(func() {
		ids := []string{}
		for _, f := range m.find(addr) {
			ids = append(ids, f.ID)
		}
		if len(ids) == 0 {
			return
		}
		body, err := json.Marshal(map[string][]string{"IDs": ids})
		if err != nil {
			m.t.Errorf("cleanup: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, m.api+"/api/v1/messages", bytes.NewReader(body))
		if err != nil {
			m.t.Errorf("cleanup: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			m.t.Errorf("cleanup: %v", err)
			return
		}
		ignore(resp.Body.Close())
	})
}

func uniqueAddr(_ *testing.T) string {
	return fmt.Sprintf("t%d-%d@example.org", time.Now().UnixNano(), os.Getpid())
}

func TestMailpit_Delivered_OnceWithExpectedHeadersAndBody(t *testing.T) {
	m := newMailpit(t)
	to := uniqueAddr(t)
	m.cleanup(to)
	ch := newChannel(t, m.config(), opts())
	e := envelope()
	e.To = notifyEmail(to)
	e.Subject = "Mailpit delivery check"
	wantNoErr(t, ch.Send(ctxT(t), e))

	msgs := m.find(to)
	if len(msgs) != 1 {
		t.Fatalf("Mailpit holds %d messages for the recipient, want exactly 1", len(msgs))
	}
	var detail struct {
		Subject string
		Text    string
		From    struct{ Name, Address string }
		To      []struct{ Address string }
	}
	m.get("/api/v1/message/"+msgs[0].ID, &detail)
	if detail.Subject != e.Subject {
		t.Errorf("Subject = %q", detail.Subject)
	}
	if detail.From.Address != "burn@example.org" {
		t.Errorf("From = %+v", detail.From)
	}
	if len(detail.To) != 1 || detail.To[0].Address != to {
		t.Errorf("To = %+v", detail.To)
	}
	if !strings.Contains(detail.Text, "You are on Gate at 06:00.") {
		t.Errorf("Text = %q", detail.Text)
	}
	var hdr map[string][]string
	m.get("/api/v1/message/"+msgs[0].ID+"/headers", &hdr)
	if got := hdr["Auto-Submitted"]; len(got) != 1 || got[0] != "auto-generated" {
		t.Errorf("Auto-Submitted = %v", got)
	}
	if got := hdr["Message-Id"]; len(got) != 1 || !strings.Contains(got[0], "0f3c9a52@bms-test.example") {
		t.Errorf("Message-ID = %v", got)
	}
}

func TestMailpit_NonASCII_Encoded(t *testing.T) {
	m := newMailpit(t)
	to := uniqueAddr(t)
	m.cleanup(to)
	ch := newChannel(t, m.config(), opts())
	e := envelope()
	e.To = notifyEmail(to)
	e.Subject = "Turno de guardia – señor Núñez ☀"
	e.Body = "Tu turno: señor Núñez – 06:00 ☀\n"
	wantNoErr(t, ch.Send(ctxT(t), e))
	msgs := m.find(to)
	if len(msgs) != 1 {
		t.Fatalf("found %d messages", len(msgs))
	}
	if msgs[0].Subject != e.Subject {
		t.Errorf("Subject = %q, want %q", msgs[0].Subject, e.Subject)
	}
	raw := m.get("/api/v1/message/"+msgs[0].ID+"/raw", nil)
	head, _, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !bytes.Contains(head, []byte("=?")) {
		t.Errorf("subject is not RFC 2047 encoded:\n%s", head)
	}
	for _, b := range head {
		if b > 127 {
			t.Fatalf("raw non-ASCII byte in the header block:\n%s", head)
		}
	}
	var detail struct{ Text string }
	m.get("/api/v1/message/"+msgs[0].ID, &detail)
	if !strings.Contains(detail.Text, "señor Núñez – 06:00 ☀") {
		t.Errorf("Text = %q", detail.Text)
	}
}

func TestMailpit_Ping_OK(t *testing.T) {
	m := newMailpit(t)
	wantNoErr(t, newChannel(t, m.config(), opts()).Ping(ctxT(t)))
}

func TestSMTPSend_Connect_Unavailable_ClosedPort_Integration(t *testing.T) {
	cfg := closedConfig(t)
	wantKind(t, newChannel(t, cfg, opts()).Send(ctxT(t), envelope()), errs.Unavailable)
}

func notifyEmail(s string) model.Email { return model.Email(s) }
