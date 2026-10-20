//go:build integration

package settings_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// ---- sealing (F11, F12) -------------------------------------------------------------------

func TestSecret_StoredSealed_NotPlaintext(t *testing.T) {
	r := newIRig(t)
	const plain = "invented-smtp-pass-7731"
	r.mustSet(kPassword, plain)

	var value *string
	var sealed []byte
	wantNoErr(t, r.pool.QueryRow(ctxT(t), "SELECT value, sealed FROM core.setting WHERE key = $1", kPassword).Scan(&value, &sealed))
	if value != nil {
		t.Error("a secret has a plaintext value column")
	}
	if len(sealed) < 12+16+len(plain) {
		t.Errorf("sealed is %d bytes; want nonce (12) + ciphertext + tag (16)", len(sealed))
	}
	if strings.Contains(string(sealed), plain) {
		t.Error("the sealed column holds the plaintext")
	}

	// A fresh random nonce per seal: the same plaintext seals differently.
	first := append([]byte(nil), sealed...)
	r.mustSet(kPassword, "invented-other-pass")
	r.mustSet(kPassword, plain)
	if again := r.sealed(kPassword); string(again) == string(first) {
		t.Error("the same plaintext sealed to the same bytes twice (nonce reuse)")
	}
	if strings.Contains(string(first[:12]), plain) {
		t.Error("nonce is the plaintext")
	}

	// Another process with the same key reads it.
	other := newIRigOn(t, r.pool, r.pool)
	if v, err := other.reader("mail").Secret(ctxT(t), "password"); err != nil || v.Value() != plain {
		t.Errorf("second service: %v", err)
	}
}

func TestReaderSecret_NoSealingKey_ReturnsUnsupported(t *testing.T) {
	base := newIRig(t)
	base.mustSet(kPassword, "invented-pass-1")
	r := newIRigOn(t, base.pool, base.pool, func(o *settings.Options) { o.SealingKey = secretNone() })
	_, err := r.reader("mail").Secret(ctxT(t), "password")
	wantKind(t, err, errs.Unsupported)
	// An unset secret needs no key.
	v, err := r.reader("mail").Secret(ctxT(t), "token")
	wantNoErr(t, err)
	if v.IsSet() {
		t.Error("an unset secret reads as set")
	}
	// The rest of the area still reads.
	if _, err := r.reader("mail").Int(ctxT(t), "port"); err != nil {
		t.Errorf("Int on a deployment without a key: %v", err)
	}
	// The document still lists the secret as set.
	if s := first(settingOf(r.doc(), kPassword)); !s.SecretSet {
		t.Error("secret_set = false without a key")
	}
}

func TestReaderSecret_WrongKey_ReturnsInternal(t *testing.T) {
	base := newIRig(t)
	const plain = "invented-pass-2"
	base.mustSet(kPassword, plain)
	r := newIRigOn(t, base.pool, base.pool, func(o *settings.Options) { o.SealingKey = secretNew("a-different-invented-sealing-key-0000") })
	_, err := r.reader("mail").Secret(ctxT(t), "password")
	wantKind(t, err, errs.Internal)
	if strings.Contains(err.Error(), plain) {
		t.Error("the error carries the plaintext")
	}
}

func TestReaderSecret_TamperedOrMovedCiphertext_ReturnsInternal(t *testing.T) {
	base := newIRig(t)
	base.mustSet(kPassword, "invented-pass-3", kToken, "invented-token-3")

	t.Run("a flipped byte", func(t *testing.T) {
		base.exec("UPDATE core.setting SET sealed = set_byte(sealed, 20, get_byte(sealed, 20) # 255) WHERE key = $1", kPassword)
		r := newIRigOn(t, base.pool, base.pool)
		_, err := r.reader("mail").Secret(ctxT(t), "password")
		wantKind(t, err, errs.Internal)
		// the other secret is unaffected
		if v, err := r.reader("mail").Secret(ctxT(t), "token"); err != nil || v.Value() != "invented-token-3" {
			t.Errorf("token: %v", err)
		}
	})
	t.Run("ciphertext moved to another key (AAD is the key)", func(t *testing.T) {
		base.exec("UPDATE core.setting SET sealed = (SELECT sealed FROM core.setting WHERE key = $1) WHERE key = $2", kToken, kPassword)
		r := newIRigOn(t, base.pool, base.pool)
		_, err := r.reader("mail").Secret(ctxT(t), "password")
		wantKind(t, err, errs.Internal)
	})
	t.Run("a truncated row", func(t *testing.T) {
		base.exec("UPDATE core.setting SET sealed = decode(repeat('00', 40), 'hex') WHERE key = $1", kPassword)
		r := newIRigOn(t, base.pool, base.pool)
		_, err := r.reader("mail").Secret(ctxT(t), "password")
		wantKind(t, err, errs.Internal)
	})
}

// ---- Regional appearance -----------------------------------------------------------------

func hexChannels(t testing.TB, h string) [3]int {
	t.Helper()
	if len(h) != 7 || h[0] != '#' {
		t.Fatalf("not a #rrggbb colour: %q", h)
	}
	var out [3]int
	for i := range 3 {
		v, err := strconv.ParseInt(h[1+2*i:3+2*i], 16, 32)
		if err != nil {
			t.Fatalf("bad colour %q: %v", h, err)
		}
		out[i] = int(v)
	}
	return out
}

// cssValue returns the value of the first "--name: value;" in css after offset from.
func cssValues(css, name string) []string {
	var out []string
	rest := css
	for {
		i := strings.Index(rest, name+":")
		if i < 0 {
			return out
		}
		rest = rest[i+len(name)+1:]
		j := strings.IndexAny(rest, ";}")
		if j < 0 {
			return out
		}
		out = append(out, strings.TrimSpace(rest[:j]))
	}
}

func near(a, b int) bool { return a-b <= 1 && b-a <= 1 }

func TestThemeCSS_Rendering(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	css, err := r.svc.ThemeCSS(ctx)
	wantNoErr(t, err)
	if css != "" {
		t.Errorf("default theme = %q, want empty", css)
	}
	if pub := first(r.svc.Public(ctx)); pub.Theme {
		t.Error("Public.Theme true with no colours")
	}

	t.Run("light only", func(t *testing.T) {
		r.mustSet("regional.color_primary", "#9a3412") //nolint:contextcheck // the helper builds its own ctx
		css := first(r.svc.ThemeCSS(ctx))
		if !strings.Contains(css, ":root") || strings.Contains(css, "prefers-color-scheme") {
			t.Fatalf("css = %q", css)
		}
		if got := cssValues(css, "--primary"); len(got) != 1 || got[0] != "#9a3412" {
			t.Errorf("--primary = %v", got)
		}
		if got := cssValues(css, "--primary-fg"); len(got) != 1 || got[0] != "#ffffff" {
			t.Errorf("--primary-fg = %v, want white (it contrasts more)", got)
		}
		hov := cssValues(css, "--primary-hover")
		if len(hov) != 1 {
			t.Fatalf("--primary-hover = %v", hov)
		}
		base, h := hexChannels(t, "#9a3412"), hexChannels(t, hov[0])
		for i := range 3 {
			if !near(h[i], int(float64(base[i])*0.88)) {
				t.Errorf("channel %d: hover %d, want about %d (12%% darker)", i, h[i], int(float64(base[i])*0.88))
			}
		}
		if pub := first(r.svc.Public(ctx)); !pub.Theme {
			t.Error("Public.Theme false with a colour set")
		}
	})
	t.Run("both schemes", func(t *testing.T) {
		r.mustSet("regional.color_primary_dark", "#f97316") //nolint:contextcheck // the helper builds its own ctx
		css := first(r.svc.ThemeCSS(ctx))
		if strings.Count(css, ":root") != 2 || strings.Count(css, "@media (prefers-color-scheme: dark)") != 1 {
			t.Fatalf("css = %q", css)
		}
		if got := cssValues(css, "--primary-fg"); len(got) != 2 || got[0] != "#ffffff" || got[1] != "#1a1a1a" {
			t.Errorf("--primary-fg = %v, want white then #1a1a1a", got)
		}
		hov := cssValues(css, "--primary-hover")
		if len(hov) != 2 {
			t.Fatalf("--primary-hover = %v", hov)
		}
		base, h := hexChannels(t, "#f97316"), hexChannels(t, hov[1])
		for i := range 3 {
			want := base[i] + int(float64(255-base[i])*0.12)
			if !near(h[i], want) {
				t.Errorf("channel %d: dark hover %d, want about %d (12%% lighter)", i, h[i], want)
			}
		}
		if i, j := strings.Index(css, "@media"), strings.Index(css, "#f97316"); i < 0 || j < i {
			t.Error("the dark colour is not inside the media block")
		}
	})
	t.Run("dark only", func(t *testing.T) {
		r.mustReset("regional.color_primary") //nolint:contextcheck // the helper builds its own ctx
		css := first(r.svc.ThemeCSS(ctx))
		if strings.Count(css, "--primary:") != 1 || !strings.Contains(css, "prefers-color-scheme: dark") || strings.Contains(css, "#9a3412") {
			t.Errorf("css = %q", css)
		}
	})
	t.Run("reset to empty", func(t *testing.T) {
		r.mustReset("regional.color_primary_dark") //nolint:contextcheck // the helper builds its own ctx
		if css := first(r.svc.ThemeCSS(ctx)); css != "" {
			t.Errorf("css = %q", css)
		}
	})
}

// ---- Run (F32-F34) ---------------------------------------------------------------------------

type running struct {
	ended chan struct{}
	err   error
	stop  context.CancelFunc
}

func startRun(t *testing.T, r *irig) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	run := &running{ended: make(chan struct{}), stop: cancel}
	go func() {
		run.err = r.svc.Run(ctx)
		close(run.ended)
	}()
	bctx, bcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer bcancel()
	wantNoErr(t, r.clk.BlockUntil(bctx, 1))
	t.Cleanup(func() {
		cancel()
		<-run.ended
	})
	return run
}

func (run *running) shutdown(t *testing.T) {
	t.Helper()
	run.stop()
	select {
	case <-run.ended:
		wantNoErr(t, run.err)
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

const poll = 5 * time.Second

func withPoll(o *settings.Options) { o.RefreshInterval = poll }

func TestRun_ReplicaFollowsTheWriterAfterOnePoll(t *testing.T) {
	writer := newIRig(t, withPoll)
	reader := newIRigOn(t, writer.pool, writer.pool, withPoll)
	ctx := ctxT(t)
	if v := first(reader.reader("mail").Int(ctx, "port")); v != 587 {
		t.Fatalf("port = %d", v)
	}
	run := startRun(t, reader)

	writer.mustSet(kPort, "2525")
	if v := first(reader.reader("mail").Int(ctx, "port")); v != 587 {
		t.Errorf("the reader saw the change before a poll: %d", v)
	}
	wantNoErr(t, reader.clk.Advance(poll))
	eventually(t, "the reader to see the writer's change", func() bool {
		v, err := reader.reader("mail").Int(ctx, "port")
		return err == nil && v == 2525
	})
	if countMeasures(reader.cap.Measurements(), "bms.settings.refresh", "outcome", "ok") == 0 {
		t.Error("no bms.settings.refresh{outcome=ok}")
	}
	run.shutdown(t)
}

func TestRun_UnchangedRevision_KeepsTheSnapshot(t *testing.T) {
	writer := newIRig(t, withPoll)
	reader := newIRigOn(t, writer.pool, writer.pool, withPoll)
	ctx := ctxT(t)
	ignore(reader.reader("mail").Int(ctx, "port"))
	f := reader.cap
	run := startRun(t, reader)
	wantNoErr(t, reader.clk.Advance(poll))
	eventually(t, "an unchanged poll", func() bool {
		return countMeasures(f.Measurements(), "bms.settings.refresh", "outcome", "unchanged") >= 1
	})
	if countMeasures(f.Measurements(), "bms.settings.refresh", "outcome", "ok") != 0 {
		t.Error("an unchanged revision was counted as a reload")
	}
	run.shutdown(t)
}

func TestRun_PollFails_KeepsSnapshotAndWarnsOnce(t *testing.T) {
	writer := newIRig(t, withPoll)
	writer.mustSet(kPort, "2525")
	f := &faultDB{DB: writer.pool}
	reader := newIRigOn(t, f, writer.pool, withPoll)
	ctx := ctxT(t)
	if v := first(reader.reader("mail").Int(ctx, "port")); v != 2525 {
		t.Fatalf("port = %d", v)
	}
	run := startRun(t, reader)
	f.set(unavailable(), nil, false)

	failed := func() int {
		return countMeasures(reader.cap.Measurements(), "bms.settings.refresh", "outcome", "failed")
	}
	for i := 1; i <= 3; i++ {
		wantNoErr(t, reader.clk.Advance(poll))
		eventually(t, "a failed poll", func() bool { return failed() >= i })
	}
	if n := len(warnings(reader.rig, "")); n != 1 {
		t.Errorf("%d warnings for 3 failed polls inside a minute, want 1", n)
	}
	if v, err := reader.reader("mail").Int(ctx, "port"); err != nil || v != 2525 {
		t.Errorf("the old snapshot was lost: %v, %v", v, err)
	}
	// Past the minute the failure is logged again.
	wantNoErr(t, reader.clk.Advance(2*time.Minute))
	eventually(t, "the next failed poll", func() bool { return failed() >= 4 })
	eventually(t, "a second warning", func() bool { return len(warnings(reader.rig, "")) >= 2 })

	// Recovery: the next poll reloads what the writer changed meanwhile.
	writer.mustSet(kPort, "3000")
	f.set(nil, nil, false)
	wantNoErr(t, reader.clk.Advance(poll))
	eventually(t, "recovery", func() bool {
		v, err := reader.reader("mail").Int(ctx, "port")
		return err == nil && v == 3000
	})
	run.shutdown(t)
}

func TestRun_OnReadOnlyDatabase_StillRefreshes(t *testing.T) {
	writer := newIRig(t, withPoll)
	f := &faultDB{DB: writer.pool}
	reader := newIRigOn(t, f, writer.pool, withPoll)
	ctx := ctxT(t)
	ignore(reader.reader("mail").Int(ctx, "port"))
	f.set(nil, nil, true) // this copy is a standby
	run := startRun(t, reader)
	writer.mustSet(kPort, "2525")
	wantNoErr(t, reader.clk.Advance(poll))
	eventually(t, "a standby to follow the writer", func() bool {
		v, err := reader.reader("mail").Int(ctx, "port")
		return err == nil && v == 2525
	})
	run.shutdown(t)
}

// ---- HTTP -----------------------------------------------------------------------------------

func (r *irig) get(ctx context.Context, path string, hdr ...string) *recorded {
	r.t.Helper()
	w, f := r.do(ctx, "GET", path, "", hdr...)
	return &recorded{status: w.Code, body: w.Body.String(), header: w.Header(), fail: f}
}

type recorded struct {
	status int
	body   string
	header http.Header
	fail   *failRec
}

func TestGetPublic_ServesWithoutASession(t *testing.T) {
	r := newIRig(t)
	r.mustSet("regional.name", "Camp Quill", kPassword, "invented-pass-5")
	res := r.get(t.Context(), "/public")
	if res.status != 200 {
		t.Fatalf("status = %d, failures %v", res.status, res.fail.last())
	}
	var pub settings.Public
	wantNoErr(t, json.Unmarshal([]byte(res.body), &pub))
	if pub.Name != "Camp Quill" || pub.Language != "en" || pub.Theme || len(pub.Terms["en"]) == 0 {
		t.Errorf("public = %+v", pub)
	}
	var raw map[string]any
	wantNoErr(t, json.Unmarshal([]byte(res.body), &raw))
	for _, key := range []string{"theme", "terms"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("public document lacks %q: %v", key, res.body)
		}
	}
	if strings.Contains(res.body, "invented-pass-5") || strings.Contains(res.body, "mail.") {
		t.Error("the public document leaks a non-regional setting")
	}
	if res.header.Get("ETag") == "" {
		t.Error("no ETag")
	}
}

func TestGetPublic_IfNoneMatch_Returns304(t *testing.T) {
	r := newIRig(t)
	for _, path := range []string{"/public", "/theme.css"} {
		t.Run(path, func(t *testing.T) {
			first := r.get(t.Context(), path)
			etag := first.header.Get("ETag")
			if first.status != 200 || etag == "" {
				t.Fatalf("status %d, ETag %q", first.status, etag)
			}
			second := r.get(t.Context(), path, "If-None-Match", etag)
			if second.status != http.StatusNotModified || second.body != "" {
				t.Errorf("status = %d, body %q; want 304 and no body", second.status, second.body)
			}
			if second.fail.count() != 0 {
				t.Errorf("a 304 went through the failer: %v", second.fail.last())
			}
			stale := r.get(t.Context(), path, "If-None-Match", `"not-the-etag"`)
			if stale.status != 200 {
				t.Errorf("a different ETag answered %d", stale.status)
			}
		})
	}
	// A write changes the ETag of /public.
	old := r.get(t.Context(), "/public").header.Get("ETag")
	r.mustSet("regional.name", "Camp Quill")
	after := r.get(t.Context(), "/public", "If-None-Match", old)
	if after.status != 200 || after.header.Get("ETag") == old {
		t.Errorf("after a write: status %d, ETag %q (old %q)", after.status, after.header.Get("ETag"), old)
	}
}

func TestGetThemeCSS_ContentTypeAndBody(t *testing.T) {
	r := newIRig(t)
	res := r.get(t.Context(), "/theme.css")
	if res.status != 200 || res.body != "" {
		t.Errorf("default: status %d body %q", res.status, res.body)
	}
	if ct := res.header.Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	r.mustSet("regional.color_primary", "#9a3412") //nolint:contextcheck // the helper builds its own ctx
	res = r.get(t.Context(), "/theme.css")
	if !strings.Contains(res.body, "--primary: #9a3412") || res.header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("themed: %q (%q)", res.body, res.header.Get("Content-Type"))
	}
}

func TestAdminRoutes_RoundTrip(t *testing.T) {
	r := newIRig(t)
	ctx := r.actx()
	w, f := r.do(ctx, "PUT", "/values", `{"values":{"regional.name":"Camp Quill","mail.port":"2525","mail.password":"invented-pass-6"}}`)
	if w.Code/100 != 2 {
		t.Fatalf("PUT /values: %d (%v)", w.Code, f.last())
	}
	if strings.Contains(w.Body.String(), "invented-pass-6") {
		t.Error("PUT /values echoes a secret")
	}

	res := r.get(ctx, "/")
	if res.status != 200 {
		t.Fatalf("GET /: %d (%v)", res.status, res.fail.last())
	}
	var doc settings.Document
	wantNoErr(t, json.Unmarshal([]byte(res.body), &doc))
	if s := first(settingOf(doc, kPort)); s.Value == nil || *s.Value != "2525" {
		t.Errorf("port in the document = %v", s.Value)
	}
	if s := first(settingOf(doc, kPassword)); !s.SecretSet || s.Value != nil {
		t.Errorf("password in the document = %+v", s)
	}
	if strings.Contains(res.body, "invented-pass-6") {
		t.Error("GET / returns a secret")
	}
	if !strings.Contains(res.body, `"secret_set"`) {
		t.Error("the document has no secret_set field")
	}

	// null resets.
	w, f = r.do(ctx, "PUT", "/values", `{"values":{"mail.port":null}}`)
	if w.Code/100 != 2 {
		t.Fatalf("PUT reset: %d (%v)", w.Code, f.last())
	}
	if v := first(r.reader("mail").Int(ctx, "port")); v != 587 {
		t.Errorf("port = %d after a JSON null", v)
	}

	// Validation errors reach the failer as Invalid with the setting key as the field.
	w, f = r.do(ctx, "PUT", "/values", `{"values":{"mail.port":"abc"}}`)
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
	wantField(t, f.last(), kPort, "malformed")

	// Terms.
	w, f = r.do(ctx, "PUT", "/terms/en", `{"terms":{"department":{"singular":"team"}}}`)
	if w.Code/100 != 2 {
		t.Fatalf("PUT /terms/en: %d (%v)", w.Code, f.last())
	}
	got := first(r.svc.TermsFor(ctx, "en"))
	if got["department"].Singular != "team" {
		t.Errorf("department = %+v", got["department"])
	}
	res = r.get(ctx, "/terms")
	if res.status != 200 || !strings.Contains(res.body, "team") {
		t.Errorf("GET /terms: %d %q", res.status, res.body)
	}
	w, f = r.do(ctx, "PUT", "/terms/xx", `{"terms":{"department":{"singular":"team"}}}`)
	if w.Code != 400 {
		t.Errorf("unknown language: status %d", w.Code)
	}
	wantField(t, f.last(), "lang", "one_of")
}

func TestPutValues_ReadOnly_Returns503(t *testing.T) {
	base := newIRig(t)
	f := &faultDB{DB: base.pool}
	r := newIRigOn(t, f, base.pool)
	ignore(r.reader("mail").Int(ctxT(t), "port"))
	f.set(nil, nil, true)
	w, fr := r.do(r.actx(), "PUT", "/values", `{"values":{"mail.port":"2525"}}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	wantReadOnly(t, fr.last())
}

// ---- nothing sensitive leaks (decision 13) --------------------------------------------------------

func TestLeak_NoSecretNoEmailInLogsSpansMetricsAuditOrErrors(t *testing.T) {
	r := newIRig(t)
	const (
		secretMarker = "Zq-secret-marker-5521"
		nameMarker   = "Zq-name-marker-5521"
		hostMarker   = "mx-5521.example.invalid"
		email        = "wren.5521@example.org"
		badEmail     = "bad.5521@@example.org"
		badSecret    = "Zq-badsecret-5521\x00"                    //nolint:gosec // invented marker
		badURL       = "https://wren5521:pw5521@burn.example.org" //nolint:gosec // invented marker
	)
	var responses, errorTexts []string
	note := func(err error) {
		if err != nil {
			errorTexts = append(errorTexts, err.Error(), fmt.Sprintf("%+v", validateReasons(err)))
		}
	}

	ctx := r.actx()
	_, err := r.svc.Set(ctx, changes("regional.name", nameMarker, kHost, hostMarker, "regional.contact_email", email, kPassword, secretMarker))
	wantNoErr(t, err)
	// Bad inputs, each carrying a marker.
	_, err = r.svc.Set(ctx, changes("regional.contact_email", badEmail))
	note(err)
	_, err = r.svc.Set(ctx, changes(kPassword, badSecret))
	note(err)
	_, err = r.svc.Set(ctx, changes("regional.public_url", badURL))
	note(err)
	_, err = r.svc.Set(ctx, changes(kPort, "Zq-bad-port-5521"))
	note(err)
	_, err = r.svc.Set(ctx, changes("Zq-unknown-5521.key", "x"))
	note(err)
	_, err = r.svc.Set(ctx, changes(kHost, "forbidden."+hostMarker))
	note(err)
	_, err = r.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{"department": {Singular: str("Zq-term-5521\x00")}}))
	note(err)

	// Reads of every kind.
	mail := r.reader("mail")
	ignore(mail.Text(ctx, "host"))
	if v, err := mail.Secret(ctx, "password"); err != nil || v.Value() != secretMarker {
		t.Fatalf("secret round trip: %v", err)
	}
	ignore(r.svc.Regional(ctx))
	ignore(r.svc.Public(ctx))
	ignore(r.svc.ThemeCSS(ctx))
	ignore(r.svc.Document(ctx))
	ignore(r.svc.Terms(ctx))

	// A stale value and a sealed value under the wrong key, which log and fail.
	r.exec("INSERT INTO core.setting (key, value, updated_at) VALUES ('mail.wait', $1, now())", "Zq-stale-5521")
	other := newIRigOn(t, r.pool, r.pool, func(o *settings.Options) { o.SealingKey = secretNew("another-invented-key-for-the-leak-test") })
	_, err = other.reader("mail").Secret(ctx, "password")
	note(err)
	ignore(other.reader("mail").Duration(ctx, "wait"))

	// HTTP: every route, successes and failures.
	for _, path := range []string{"/", "/public", "/theme.css", "/terms"} {
		responses = append(responses, r.get(ctx, path).body)
	}
	for _, body := range []string{
		`{"values":{"regional.contact_email":"` + badEmail + `"}}`,
		`{"values":{"mail.password":"Zq-badsecret-5521\u0000"}}`,
		`{"values":{"mail.port":"Zq-bad-port-5521"}}`,
		`{"values":{"mail.password":"` + secretMarker + `-again"}}`,
	} {
		w, f := r.do(ctx, "PUT", "/values", body)
		responses = append(responses, w.Body.String())
		note(f.last())
	}
	w, f := r.do(ctx, "PUT", "/values", `{"values": nope`+badEmail)
	responses = append(responses, w.Body.String())
	note(f.last())

	// Logs, spans, metrics.
	for _, m := range []string{secretMarker, nameMarker, hostMarker, email, badEmail, "Zq-bad-port-5521", "Zq-stale-5521", "wren5521", "pw5521", "Zq-term-5521"} {
		if r.cap.Contains(m) || other.cap.Contains(m) {
			t.Errorf("%q reached a log, span or metric", m)
		}
	}
	// Audit entries never hold a value.
	for _, e := range r.rec.Recorded() {
		text := fmt.Sprintf("%+v", e)
		for _, m := range []string{secretMarker, nameMarker, hostMarker, email} {
			if strings.Contains(text, m) {
				t.Errorf("audit entry %s carries %q", e.Action, m)
			}
		}
	}
	// The secret is in no response at all; the e-mail and bad input are in no error text.
	for i, body := range responses {
		if strings.Contains(body, secretMarker) {
			t.Errorf("response %d carries the secret", i)
		}
	}
	for _, text := range errorTexts {
		for _, m := range []string{secretMarker, email, badEmail, "Zq-badsecret-5521", "wren5521", "pw5521", "Zq-bad-port-5521", "Zq-stale-5521", "Zq-term-5521"} {
			if strings.Contains(text, m) {
				t.Errorf("an error carries %q: %.120s", m, text)
			}
		}
	}
	// Error responses (the ones that went through the failer are statuses only here; the
	// bodies the handlers wrote must not echo the input either).
	for i, body := range responses[4:] {
		for _, m := range []string{badEmail, "Zq-badsecret-5521", "Zq-bad-port-5521"} {
			if strings.Contains(body, m) {
				t.Errorf("error response %d echoes %q", i, m)
			}
		}
	}

	// Spans carry the batch size and area, never a value.
	found := false
	for _, s := range r.cap.Spans() {
		if strings.HasPrefix(s.Name, "settings.") && s.Attrs["settings.keys"] != "" {
			found = true
		}
	}
	if !found {
		t.Error("no settings.<use-case> span with a settings.keys attribute")
	}
	// Warnings, where any, name the setting key only.
	for _, rec := range warnings(other.rig, "") {
		for k, v := range rec.Attrs {
			if strings.Contains(v, "Zq-") {
				t.Errorf("warning attribute %s=%q carries a value", k, v)
			}
		}
	}
	_ = slog.LevelWarn
}
