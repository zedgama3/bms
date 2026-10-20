//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
)

// Invented addresses for the e-mail test; nothing here is a real mailbox.
const (
	ntSender     = "bms.outbox@example.org"
	ntAlertDesk  = "alert.desk@example.org"
	ntMember     = "marlow.reset@example.org"
	ntHolder     = "hollis.admin@example.org"
	ntSMTPPass   = "smtp-secret-for-the-test" //nolint:gosec // an invented test value
	ntOldPass    = "correct horse battery staple 7413"
	ntNewPass    = "second horse battery staple 9021"
	ntMailBudget = 30 * time.Second
)

var ntTokenRE = regexp.MustCompile(`token=([A-Za-z0-9_-]{43})`)

// ntMailpit returns the Mailpit API base URL and the SMTP host and port the tests send
// through, or skips the test when the compose environment is not loaded.
func ntMailpit(t *testing.T) (apiBase, host, port string) {
	t.Helper()
	smtpAddr, base := os.Getenv("BMS_TEST_SMTP_ADDR"), os.Getenv("BMS_TEST_MAILPIT_URL")
	if smtpAddr == "" || base == "" {
		t.Skip("BMS_TEST_SMTP_ADDR and BMS_TEST_MAILPIT_URL are not set (load deploy/compose/.env)")
	}
	u, err := url.Parse("smtp://" + smtpAddr)
	if err != nil || u.Hostname() == "" || u.Port() == "" {
		t.Fatal("BMS_TEST_SMTP_ADDR is not host:port")
	}
	return strings.TrimRight(base, "/"), u.Hostname(), u.Port()
}

// ntMailGet reads one Mailpit API document into v.
func ntMailGet(ctx context.Context, t *testing.T, apiBase, path string, v any) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mailpit %s: %v", path, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("mailpit %s: status %d, %v", path, resp.StatusCode, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("mailpit %s: %v", path, err)
	}
}

// ntWaitMail waits for messages to the address and returns the text of each. The sender
// loop ticks every few seconds, so this polls until want have arrived.
func ntWaitMail(ctx context.Context, t *testing.T, apiBase, to string, want int) []string {
	t.Helper()
	wctx, cancel := context.WithTimeout(ctx, ntMailBudget)
	defer cancel()
	for {
		var list struct {
			Messages []struct {
				ID string `json:"ID"`
			} `json:"messages"`
		}
		ntMailGet(wctx, t, apiBase, "/api/v1/search?query="+url.QueryEscape("to:"+to), &list)
		if len(list.Messages) >= want {
			texts := make([]string, 0, len(list.Messages))
			for _, m := range list.Messages {
				var msg struct {
					Text string `json:"Text"`
				}
				ntMailGet(wctx, t, apiBase, "/api/v1/message/"+m.ID, &msg)
				texts = append(texts, msg.Text)
			}
			return texts
		}
		if err := clock.Wait(wctx, clock.Real(), 200*time.Millisecond); err != nil {
			t.Fatalf("%d message(s) to %s never arrived (got %d): %v", want, to, len(list.Messages), err)
		}
	}
}

// ntPurge removes the messages to an address, so a rerun starts clean.
func ntPurge(ctx context.Context, t *testing.T, apiBase, to string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/api/v1/search?query="+url.QueryEscape("to:"+to), bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mailpit purge: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mailpit purge of %s: status %d, want 200", to, resp.StatusCode)
	}
}

// personOf returns the person_id in a session body.
func ntPersonOf(t *testing.T, sessionBody string) string {
	t.Helper()
	var sess struct {
		PersonID string `json:"person_id"`
	}
	if err := json.Unmarshal([]byte(sessionBody), &sess); err != nil || sess.PersonID == "" {
		t.Fatalf("the session body has no person_id: %v", err)
	}
	return sess.PersonID
}

// A password reset request delivers one message to Mailpit, completing it changes the
// password, Greet sends hello.greeted, and a superuser sign-in alerts the holders of
// accounts.manage and the alert addresses, once per sign-in, whatever cookie it carries.
func TestServe_Integration_NotifyEndToEnd_ResetGreetAndAlert(t *testing.T) {
	apiBase, smtpHost, smtpPort := ntMailpit(t)
	u, pool, _ := itPool(t)
	env, addr := itEnv(t, u.String())
	env = append(env, "BMS_HTTP_ALLOW_PLAINTEXT=true")
	ctx := t.Context()
	if r := itMain(ctx, env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s", r.code, r.errOut)
	}
	create := itMain(ctx, env, "create-superuser", itSuperEmail)
	pw := passwordLineRE.FindStringSubmatch(create.out)
	if create.code != ExitOK || pw == nil {
		t.Fatalf("create-superuser: %d\n%s\n%s", create.code, create.out, create.errOut)
	}
	for _, to := range []string{ntMember, ntHolder, ntAlertDesk} {
		ntPurge(ctx, t, apiBase, to)
	}
	s := itServe(t, env, addr)

	// Without SMTP configured, a reset request is 501 and nothing is queued.
	if status, body, _ := itSend(t, addr, http.MethodPost, "/api/accounts/password-reset", "", `{"email":"`+ntMember+`"}`); status != 501 {
		t.Errorf("reset request before SMTP is configured = %d %s, want 501", status, body)
	}
	// The first superuser sign-in happens before SMTP is set up: it queues nothing.
	status, body, super := itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(itSuperEmail, pw[1]))
	if status != 204 || super == nil {
		t.Fatalf("first superuser sign-in = %d %s", status, body)
	}
	superCookie := super.Name + "=" + super.Value
	if n := pcCount(t, pool, `SELECT count(*) FROM core.notify_message`); n != 0 {
		t.Errorf("the outbox holds %d message(s) after a sign-in with e-mail unset, want 0", n)
	}

	// A member, and a holder of accounts.manage (the account-admin starter role).
	signUp := func(email string) (cookie, person string) {
		t.Helper()
		status, body, c := itSend(t, addr, http.MethodPost, "/api/accounts/sign-up", "", credentials(email, ntOldPass))
		if status != 201 || c == nil {
			t.Fatalf("sign-up = %d %s", status, body)
		}
		cookie = c.Name + "=" + c.Value
		_, body, _ = itSend(t, addr, http.MethodGet, "/api/accounts/session", cookie, "")
		itWaitPerson(t, pool, body)
		return cookie, ntPersonOf(t, body)
	}
	_, memberPerson := signUp(ntMember)
	_, holderPerson := signUp(ntHolder)
	pcGrant(t, pool, holderPerson, "account-admin")

	// The superuser points notify at Mailpit and names an alert address. A password is
	// stored (sealed) so that the leak checks below cover it, but no user name: the compose
	// Mailpit does not offer AUTH (it needs MP_SMTP_AUTH_ACCEPT_ANY and
	// MP_SMTP_AUTH_ALLOW_INSECURE), and the channel authenticates only with both set.
	values, err := json.Marshal(map[string]map[string]string{"values": {
		"regional.public_url":    "http://" + addr,
		"notify.smtp_host":       smtpHost,
		"notify.smtp_port":       smtpPort,
		"notify.smtp_security":   "none",
		"notify.smtp_password":   ntSMTPPass,
		"notify.sender_address":  ntSender,
		"notify.alert_addresses": ntAlertDesk,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if status, body, _ = itSend(t, addr, http.MethodPut, "/api/settings/values", superCookie, string(values)); status != 200 {
		t.Fatalf("PUT /api/settings/values = %d %s", status, body)
	}

	// The reset request delivers one message; the token in its link completes the reset.
	if status, body, _ = itSend(t, addr, http.MethodPost, "/api/accounts/password-reset", "", `{"email":"`+ntMember+`"}`); status != 202 {
		t.Fatalf("reset request = %d %s, want 202", status, body)
	}
	mails := ntWaitMail(ctx, t, apiBase, ntMember, 1)
	if len(mails) != 1 {
		t.Fatalf("messages to the member = %d, want 1", len(mails))
	}
	tok := ntTokenRE.FindStringSubmatch(mails[0])
	if tok == nil {
		t.Fatal("the reset message carries no token link")
	}
	complete := fmt.Sprintf(`{"token":%q,"new_password":%q}`, tok[1], ntNewPass)
	if status, body, _ = itSend(t, addr, http.MethodPost, "/api/accounts/password-reset/complete", "", complete); status != 204 {
		t.Fatalf("reset complete = %d %s, want 204", status, body)
	}
	if status, _, _ = itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(ntMember, ntOldPass)); status == 204 {
		t.Error("the old password still signs in")
	}
	status, body, member := itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(ntMember, ntNewPass))
	if status != 204 || member == nil {
		t.Fatalf("sign-in with the new password = %d %s", status, body)
	}

	// Greet queues hello.greeted for the greeter.
	if status, body, _ = itSend(t, addr, http.MethodPost, "/api/hello/greetings", member.Name+"="+member.Value, ""); status != 201 {
		t.Fatalf("POST /api/hello/greetings = %d %s, want 201", status, body)
	}
	mails = ntWaitMail(ctx, t, apiBase, ntMember, 2)
	if !hasText(mails, "you said") {
		t.Errorf("no greeting message among %d messages to the member", len(mails))
	}

	// Each superuser sign-in alerts the alert address and the holder once, including two that
	// carry the cookie of the first session.
	if status, body, _ = itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", "", credentials(itSuperEmail, pw[1])); status != 204 {
		t.Fatalf("second superuser sign-in = %d %s", status, body)
	}
	for range 2 {
		if status, body, _ = itSend(t, addr, http.MethodPost, "/api/accounts/sign-in", superCookie, credentials(itSuperEmail, pw[1])); status != 204 {
			t.Fatalf("superuser sign-in with an old cookie = %d %s", status, body)
		}
	}
	for _, to := range []string{ntAlertDesk, ntHolder} {
		alerts := ntWaitMail(ctx, t, apiBase, to, 3)
		if len(alerts) != 3 {
			t.Errorf("alerts to %s = %d, want exactly 3 (one per sign-in after SMTP was set up)", to, len(alerts))
		}
		for _, a := range alerts {
			if !strings.Contains(a, "/audit?superuser=1") {
				t.Errorf("an alert to %s does not link the audit log:\n%s", to, a)
			}
		}
	}

	// check reports e-mail configured, with the warning for security none, and no secret.
	chk := itMain(ctx, env, "check")
	if !strings.Contains(chk.out, "e-mail: configured "+smtpHost+":"+smtpPort+" (none)") ||
		!strings.Contains(chk.out, "smtp: ok") || !strings.Contains(chk.out, "warning: security is none") {
		t.Errorf("check output lacks the e-mail lines:\n%s", chk.out)
	}

	// The outbox is part of a person's export.
	if r := itMain(ctx, env, "export", memberPerson); r.code != ExitOK || !strings.Contains(r.out, `"notify"`) {
		t.Errorf("export = %d, want OK with a notify section\n%s", r.code, r.errOut)
	}

	r := s.stop(t)
	if r.code != ExitOK {
		t.Fatalf("serve stopped with %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	for _, leak := range []string{ntMember, ntHolder, ntAlertDesk, ntSender, ntSMTPPass, ntNewPass, tok[1]} {
		for name, out := range map[string]string{"serve": r.out + r.errOut, "check": chk.out + chk.errOut} {
			if strings.Contains(out, leak) {
				t.Errorf("%s's output carries %q", name, leak)
			}
		}
	}
}

func hasText(texts []string, sub string) bool {
	for _, s := range texts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
