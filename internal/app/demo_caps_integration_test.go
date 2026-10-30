//go:build integration

package app_test

// End-to-end checks of the demo's limits (demo.md, D53): the upload cap the settings cannot
// lift, the object store after a reset, and that demo mode never sends through SMTP.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/app"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// pdfOf returns a document that starts like a PDF and is n bytes long.
func pdfOf(n int) []byte {
	b := bytes.Repeat([]byte("0"), n)
	copy(b, "%PDF-1.4\n")
	return b
}

// uploadCertificate posts a multipart certificate upload as the jar's account.
func uploadCertificate(t *testing.T, jar http.CookieJar, base, qualification string, data []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{"qualification": qualification, "acquired_on": "2026-01-01"} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="certificate.pdf"`)
	h.Set("Content-Type", "application/pdf")
	pw, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/volunteers/me/qualifications", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", base)
	resp, err := (&http.Client{Jar: jar, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// qualificationID finds a qualification by name through the module's list route.
func qualificationID(t *testing.T, jar http.CookieJar, base, name string) string {
	t.Helper()
	st, body := testerRequest(t, jar, base, "GET", "/api/volunteers/qualifications", "")
	if st != 200 {
		t.Fatalf("list qualifications = %d %s", st, body)
	}
	var doc struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("qualifications: %v: %s", err, body)
	}
	for _, q := range doc.Items {
		if q.Name == name {
			return q.ID
		}
	}
	t.Fatalf("no qualification %q in %s", name, body)
	return ""
}

// objectFiles counts the objects in the disk object store (data files, not their .meta).
func objectFiles(t *testing.T, f *demoFx) int {
	t.Helper()
	root := filepath.Join(f.dataDir(), "files", "store")
	n := 0
	err := filepath.WalkDir(root, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.Type().IsRegular() && !strings.HasSuffix(e.Name(), ".meta") { // each object has a .meta beside it
			n++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return n
}

// The cap is a constant: a settings-admin who sets files.max_bytes far above it still cannot
// upload more than demo.MaxUploadBytes, and an upload just under it works.
func TestDemoUpload_OverTheCap_FailsTooLarge_WhileMaxBytesIsHigher(t *testing.T) {
	d := startDemo(t, true)
	admin := signIn(t, d.s.base, "settings-admin@demo.invalid", demoPassword)
	st, body := testerRequest(t, admin, d.s.base, "PUT", "/api/settings/values", `{"values":{"files.max_bytes":"`+strconv.Itoa(25<<20)+`"}}`)
	if st != 200 {
		t.Fatalf("raising files.max_bytes = %d %s", st, body)
	}
	jar := signIn(t, d.s.base, "participant@demo.invalid", demoPassword)
	q := qualificationID(t, jar, d.s.base, "First aid")

	if st, body := uploadCertificate(t, jar, d.s.base, q, pdfOf(demo.MaxUploadBytes+1)); st != http.StatusBadRequest || !strings.Contains(body, "too_large") {
		t.Errorf("an upload over the cap = %d %s, want 400 too_large", st, body)
	}
	if st, body := uploadCertificate(t, jar, d.s.base, q, pdfOf(demo.MaxUploadBytes-1024)); st != http.StatusCreated {
		t.Errorf("an upload under the cap = %d %s, want 201", st, body)
	}
}

// A reset leaves exactly one seed's worth of objects (the participant's certificate); the
// strangers' uploads are gone.
func TestDemoReset_Objects_BackToOneSeedsWorth(t *testing.T) {
	f := newDemoFx(t, true, true)
	wantCode(t, f.run("demo", "seed"), app.ExitOK)
	seeded := objectFiles(t, f)
	if seeded != 1 {
		t.Fatalf("a seed leaves %d object files, want 1 (the participant's certificate)", seeded)
	}

	s := startServe(t, f.env)
	jar := signIn(t, s.base, "participant@demo.invalid", demoPassword)
	q := qualificationID(t, jar, s.base, "First aid")
	if st, body := uploadCertificate(t, jar, s.base, q, pdfOf(4096)); st != http.StatusCreated {
		t.Fatalf("upload = %d %s", st, body)
	}
	s.stop(t)
	if got := objectFiles(t, f); got != seeded+1 {
		t.Fatalf("setup: %d object files after the upload, want %d", got, seeded+1)
	}

	wantCode(t, f.run("demo", "reset"), app.ExitOK)
	if got := objectFiles(t, f); got != seeded {
		t.Errorf("%d object files after the reset, want %d (one seed's worth)", got, seeded)
	}
}

// With SMTP pointed at a listening server, demo mode still sends nothing to it: the mail is
// captured instead.
func TestDemoMail_SMTPConfigured_NothingSent_MailCaptured(t *testing.T) {
	addr, api := os.Getenv("BMS_TEST_SMTP_ADDR"), os.Getenv("BMS_TEST_MAILPIT_URL")
	if addr == "" || api == "" {
		t.Skip("BMS_TEST_SMTP_ADDR and BMS_TEST_MAILPIT_URL are not set; skipping the SMTP test")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("BMS_TEST_SMTP_ADDR %q: %v", addr, err)
	}
	d := startDemo(t, true)
	admin := signIn(t, d.s.base, "settings-admin@demo.invalid", demoPassword)
	vals := map[string]string{"notify.smtp_host": host, "notify.smtp_port": port, "notify.smtp_security": "none"}
	b, err := json.Marshal(map[string]any{"values": vals})
	if err != nil {
		t.Fatal(err)
	}
	if st, body := testerRequest(t, admin, d.s.base, "PUT", "/api/settings/values", string(b)); st != 200 {
		t.Fatalf("pointing SMTP at the fake = %d %s", st, body)
	}

	const to = "smtp.probe@demo.invalid"
	tj := newJar()
	if st, _ := testerRequest(t, tj, d.s.base, "POST", "/api/accounts/sign-up", `{"email":"`+to+`","password":"`+testerPW+`"}`); st != 201 {
		t.Fatalf("sign-up = %d", st)
	}
	if st, _ := testerRequest(t, nil, d.s.base, "POST", "/api/accounts/password-reset", `{"email":"`+to+`"}`); st != 202 {
		t.Fatalf("password-reset = %d, want 202", st)
	}
	if !waitFor(t, 30*time.Second, func() bool {
		return d.count("SELECT count(*) FROM core.demo_mail WHERE lower(to_address) = $1", to) >= 1
	}) {
		t.Fatal("the reset mail was not captured")
	}
	// Give an asynchronous send every chance to show, then ask the SMTP server.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := clock.Wait(ctx, clock.Real(), 2*time.Second); err != nil { // nothing to wait on for something that must not happen
		t.Fatalf("wait: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(api, "/")+"/api/v1/search?query="+url.QueryEscape("to:"+to), nil) //nolint:gosec // G704: the URL is the test environment's BMS_TEST_MAILPIT_URL, never input
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: the test environment's own URL
	if err != nil {
		t.Fatalf("the SMTP test server's API is unreachable: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close body: %v", cerr)
		}
	}()
	var out struct {
		Messages []struct{ ID string } `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Messages) != 0 {
		t.Errorf("%d messages reached the SMTP server in demo mode, want none", len(out.Messages))
	}
}
