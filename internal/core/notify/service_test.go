package notify_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func statements(d *dbtest.Fake, sub string) int {
	n := 0
	for _, c := range d.Calls() {
		if strings.Contains(strings.ToLower(c.SQL), strings.ToLower(sub)) {
			n++
		}
	}
	return n
}

func seamCalls(r *rig) int64 {
	return r.langs.calls.Load() + r.site.calls.Load() + r.names.calls.Load() + r.addrs.calls.Load()
}

// ---- Configured (amendment; F64, F65) ---------------------------------------------------

func TestConfigured_Table(t *testing.T) {
	cases := []struct {
		name         string
		host, sender string
		want         bool
	}{
		{"host and sender set", "smtp.example.org", "burn@example.org", true},
		{"host unset", "", "burn@example.org", false},
		{"sender unset", "smtp.example.org", "", false},
		{"both unset", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			r.smtp.set(func(c *notify.SMTPConfig) { c.Host = tc.host; c.Sender.Email = model.Email(tc.sender) })
			for name, n := range map[string]notify.Notifier{"scoped": r.vol, "other owner": r.ppl} {
				got, err := n.Configured(ctxT(t))
				wantNoErr(t, err)
				if got != tc.want {
					t.Fatalf("%s: Configured = %v, want %v", name, got, tc.want)
				}
			}
			if n := seamCalls(r); n != 0 {
				t.Fatalf("Configured looked at a recipient seam %d times", n)
			}
			if r.ch.factoryCalls() != 0 {
				t.Fatal("Configured built a channel (it must not connect)")
			}
			if len(d.Calls()) != 0 {
				t.Fatalf("Configured touched the database: %d statements", len(d.Calls()))
			}
		})
	}
}

func TestConfigured_FollowsSettingsWithoutCaching(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	got, err := r.vol.Configured(ctxT(t))
	wantNoErr(t, err)
	if !got {
		t.Fatal("want configured")
	}
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	got, err = r.vol.Configured(ctxT(t))
	wantNoErr(t, err)
	if got {
		t.Fatal("a cleared host must show at once")
	}
}

func TestConfigured_SourceFails_ReturnsKind(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		t.Run(k.String(), func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			r.smtp.fail(errs.E("test.smtp", k, errors.New("source down")))
			got, err := r.vol.Configured(ctxT(t))
			wantKind(t, err, k)
			if got {
				t.Fatal("Configured must be false when the source fails")
			}
		})
	}
}

// ---- Send before the store (F6..F12) ----------------------------------------------------

func TestSend_NotConfigured_Unsupported(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*notify.SMTPConfig)
	}{
		{"no host", func(c *notify.SMTPConfig) { c.Host = "" }},
		{"no sender", func(c *notify.SMTPConfig) { c.Sender.Email = "" }},
		{"neither", func(c *notify.SMTPConfig) { c.Host = ""; c.Sender = notify.Address{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			r.smtp.set(tc.mod)
			known := r.person("known@example.org")
			unknown := newPerson() // no address, no language, no name
			errKnown := r.vol.Send(ctxT(t), greet(known))
			errUnknown := r.vol.Send(ctxT(t), greet(unknown))
			for _, err := range []error{errKnown, errUnknown} {
				wantKind(t, err, errs.Unsupported)
				if !errors.Is(err, notify.ErrNotConfigured) {
					t.Fatalf("does not wrap ErrNotConfigured: %v", err)
				}
			}
			if n := seamCalls(r); n != 0 {
				t.Fatalf("a recipient was looked at %d times before the configured check", n)
			}
			if statements(d, "insert") != 0 {
				t.Fatal("a row was inserted")
			}
		})
	}
}

func TestSend_NotConfigured_WinsOverUnknownTemplate(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	m := greet(newPerson())
	m.Template = "volunteers.nosuch"
	err := r.vol.Send(ctxT(t), m)
	wantKind(t, err, errs.Unsupported)
}

func TestSend_SMTPSourceFails_Propagates(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.smtp.fail(errs.E("test", errs.Unavailable, errors.New("down")))
	wantKind(t, r.vol.Send(ctxT(t), greet(newPerson())), errs.Unavailable)
}

func TestSend_InvalidMessage_Invalid(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	m := greet()
	m.Link = "https://evil.example"
	err := r.vol.Send(ctxT(t), m)
	wantField(t, err, "to", "required")
	wantField(t, err, "link", "malformed")
	if statements(d, "insert") != 0 {
		t.Fatal("an invalid message was stored")
	}
}

func TestSend_OtherOwner_InvalidScope(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	m := greet(newPerson())
	m.Template = "people.deleted"
	wantField(t, r.vol.Send(ctxT(t), m), "template", "scope")
	// Even a template that does not exist is another owner's, not "unknown".
	m.Template = "people.nosuch"
	wantField(t, r.vol.Send(ctxT(t), m), "template", "scope")
	if statements(d, "insert") != 0 {
		t.Fatal("a row was inserted")
	}
}

func TestSend_UnknownTemplate_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	m := greet(newPerson())
	m.Template = "volunteers.nosuch"
	wantField(t, r.vol.Send(ctxT(t), m), "template", "unknown")
}

func TestSend_ReadOnly_Unavailable(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	d.SetReadOnly(true)
	p := r.person("a@example.org")
	t.Run("outside a transaction", func(t *testing.T) {
		err := r.vol.Send(ctxT(t), greet(p))
		wantKind(t, err, errs.Unavailable)
		if !errors.Is(err, db.ErrReadOnly) {
			t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
		}
	})
	// Inside a write transaction on a copy that turns read-only is covered against a real
	// database (TestSend_ReadOnly_Unavailable_Real): dbtest.Fake refuses only Exec when read-only.
}

func TestSend_InReadTx_Internal(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	p := r.person("a@example.org")
	var sendErr error
	err := d.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		sendErr = r.vol.Send(ctx, greet(p))
		return nil
	})
	wantNoErr(t, err)
	wantKind(t, sendErr, errs.Internal)
}

func TestSend_SeamFails_Propagates(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		for _, seam := range []string{"languages", "site", "names"} {
			t.Run(seam+"/"+k.String(), func(t *testing.T) {
				d := dbtest.NewFake()
				r := newRig(t, d)
				p := r.person("a@example.org")
				e := errs.E("test."+seam, k, errors.New("seam down"))
				switch seam {
				case "languages":
					r.langs.fail(e)
				case "site":
					r.site.fail(e)
				case "names":
					r.names.fail(e)
				}
				wantKind(t, r.vol.Send(ctxT(t), greet(p)), k)
				if statements(d, "insert") != 0 {
					t.Fatal("a row was inserted although a seam failed")
				}
			})
		}
	}
}

func TestSend_RenderedTooLong_Invalid(t *testing.T) {
	cases := []struct {
		name, note string
	}{
		// long.en: the subject is the note (cap 256 bytes), the body is the note 70 times (cap 65536).
		{"subject over the cap", strings.Repeat("s", notify.MaxSubjectBytes+1)},
		{"body over the cap", strings.Repeat("b", 1000)}, // 70*1000 = 70000 > 65536 with a 1000-byte subject too
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			m := notify.Message{Template: "volunteers.long", To: []model.PersonID{r.person("a@example.org")},
				Vars: map[string]string{"note": tc.note}}
			wantField(t, r.vol.Send(ctxT(t), m), "template", "too_long")
			if statements(d, "insert") != 0 {
				t.Fatal("a row was inserted")
			}
		})
	}
	t.Run("subject at the cap is fine", func(t *testing.T) {
		d := dbtest.NewFake()
		r := newRig(t, d)
		m := notify.Message{Template: "volunteers.long", To: []model.PersonID{r.person("a@example.org")},
			Vars: map[string]string{"note": strings.Repeat("s", 100)}} // body 7000 bytes, subject 100
		wantNoErr(t, r.vol.Send(ctxT(t), m))
	})
}

func TestSend_FirstAndLastRecipientAreResolved(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	var ps []model.PersonID
	for i := 0; i < notify.MaxRecipients; i++ {
		ps = append(ps, r.person("p@example.org"))
	}
	wantNoErr(t, r.vol.Send(ctxT(t), greet(ps...)))
	if got := r.langs.calls.Load(); got != notify.MaxRecipients {
		t.Fatalf("LanguageOf called %d times, want %d (one per recipient, first to last)", got, notify.MaxRecipients)
	}
}

// ---- SendTo (F16) -----------------------------------------------------------------------

func TestSendTo_Invalid_Table(t *testing.T) {
	cases := []struct {
		name string
		to   model.Email
		m    func() notify.Message
	}{
		{"malformed address", "not-an-address", func() notify.Message { return greet() }},
		{"empty address", "", func() notify.Message { return greet() }},
		{"address with CRLF", "a@example.org\r\nBcc: x@example.org", func() notify.Message { return greet() }},
		{"non-empty To", "a@example.org", func() notify.Message { return greet(newPerson()) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			err := r.svc.SendTo(ctxT(t), tc.to, tc.m())
			wantField(t, err, "to", "")
			if statements(d, "insert") != 0 {
				t.Fatal("a row was inserted")
			}
		})
	}
}

func TestSendTo_NotConfigured_Unsupported(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	err := r.svc.SendTo(ctxT(t), "a@example.org", greet())
	wantKind(t, err, errs.Unsupported)
	if !errors.Is(err, notify.ErrNotConfigured) {
		t.Fatal("does not wrap ErrNotConfigured")
	}
}

func TestSendTo_ReadOnly_Unavailable(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	d.SetReadOnly(true)
	err := r.svc.SendTo(ctxT(t), "a@example.org", greet())
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatal("does not wrap db.ErrReadOnly")
	}
}

// ---- Register and Scoped (F17..F19) -----------------------------------------------------

func TestRegister_BadTemplates_Table(t *testing.T) {
	fileSet := func(name, content string) fstest.MapFS {
		return fstest.MapFS{name: {Data: []byte(content)}}
	}
	good := "Subject: Hi\n\nBody\n"
	cases := []struct {
		name string
		fsys fstest.MapFS
	}{
		{"file name without a language", fileSet("a.tmpl", good)},
		{"upper case ident", fileSet("A.en.tmpl", good)},
		{"malformed language", fileSet("a.e.tmpl", good)},
		{"no en file", fileSet("a.es.tmpl", good)},
		{"no Subject header", fileSet("a.en.tmpl", "Vars: x\n\nBody\n")},
		{"empty subject", fileSet("a.en.tmpl", "Subject:\n\nBody\n")},
		{"subject parse error", fileSet("a.en.tmpl", "Subject: {{.Vars.x\n\nBody\n")},
		{"body parse error", fileSet("a.en.tmpl", "Subject: Hi\n\n{{if}}\n")},
		{"unknown function", fileSet("a.en.tmpl", "Subject: Hi\n\n{{upper .Vars.x}}\n")},
		{"body over 16384 bytes", fileSet("a.en.tmpl", "Subject: Hi\n\n"+strings.Repeat("x", notify.MaxTemplateBytes))},
		{"subject over 256 bytes", fileSet("a.en.tmpl", "Subject: "+strings.Repeat("s", notify.MaxSubjectBytes+1)+"\n\nBody\n")},
		{"one good ident and one bad", fstest.MapFS{
			"a.en.tmpl": {Data: []byte(good)}, "b.en.tmpl": {Data: []byte("Subject: {{\n\nBody\n")}}},
		{"integer range", fileSet("a.en.tmpl", "Subject: Hi\n\n{{range 3}}x{{end}}\n")},
		{"range nested three deep", fileSet("a.en.tmpl", "Subject: Hi\n\n{{range .Vars}}{{range .Vars}}{{range .Vars}}x{{end}}{{end}}{{end}}\n")},
		{"second ident has no en", fstest.MapFS{
			"a.en.tmpl": {Data: []byte(good)}, "b.es.tmpl": {Data: []byte(good)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, o := newRigOptions(t, dbtest.NewFake())
			svc, err := notify.New(o)
			wantNoErr(t, err)
			wantKind(t, svc.Register("hello", tc.fsys), errs.Invalid)
		})
	}
	t.Run("range nested two deep is fine", func(t *testing.T) {
		_, o := newRigOptions(t, dbtest.NewFake())
		svc, err := notify.New(o)
		wantNoErr(t, err)
		wantNoErr(t, svc.Register("hello", fileSet("a.en.tmpl", "Subject: Hi\n\nBody {{range .Vars}}{{range .Vars}}x{{end}}{{end}}\n")))
	})
	t.Run("a valid set registers", func(t *testing.T) {
		_, o := newRigOptions(t, dbtest.NewFake())
		svc, err := notify.New(o)
		wantNoErr(t, err)
		wantNoErr(t, svc.Register("hello", fstest.MapFS{
			"a.en.tmpl": {Data: []byte(good)}, "a.es.tmpl": {Data: []byte(good)},
			"b.en.tmpl": {Data: []byte("Subject: {{term \"shift\"}}\nVars: x, y\n\n{{.Vars.x}}{{.Site.Name}}\n")}}))
	})
	t.Run("bad owner", func(t *testing.T) {
		_, o := newRigOptions(t, dbtest.NewFake())
		svc, err := notify.New(o)
		wantNoErr(t, err)
		wantKind(t, svc.Register("Bad Owner", fileSet("a.en.tmpl", good)), errs.Invalid)
	})
}

func TestRegister_Twice_Conflict(t *testing.T) {
	_, o := newRigOptions(t, dbtest.NewFake())
	svc, err := notify.New(o)
	wantNoErr(t, err)
	wantNoErr(t, svc.Register("hello", fstest.MapFS{"a.en.tmpl": {Data: []byte("Subject: Hi\n\nBody\n")}}))
	wantKind(t, svc.Register("hello", fstest.MapFS{"a.en.tmpl": {Data: []byte("Subject: Hi\n\nBody\n")}}), errs.Conflict)
	// notify's own owner is registered by New.
	wantKind(t, svc.Register("notify", fstest.MapFS{"a.en.tmpl": {Data: []byte("Subject: Hi\n\nBody\n")}}), errs.Conflict)
}

func TestScoped_BadOwner_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	for _, owner := range []string{"", "Bad Owner", "has.dot", "1abc", strings.Repeat("a", 33)} {
		_, err := r.svc.Scoped(owner)
		if owner == strings.Repeat("a", 33) {
			continue // the owner length cap is the schema's (32); not every implementation rejects here
		}
		wantKind(t, err, errs.Invalid)
	}
	n, err := r.svc.Scoped("volunteers")
	wantNoErr(t, err)
	if n == nil {
		t.Fatal("nil notifier")
	}
}

func TestRegister_AfterRun_Internal(t *testing.T) {
	d := dbtest.NewFake()
	d.SetReadOnly(true) // the loop idles; nothing is written
	r := newRig(t, d)
	ctx, cancel := context.WithCancel(ctxT(t))
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	var gotRegister, gotScoped error
	i := 0
	eventually(t, "Run to start", func() bool {
		i++
		owner := "probe" + strings.Repeat("a", i%20) + string(rune('a'+i%26))
		gotRegister = r.svc.Register(owner, fstest.MapFS{"a.en.tmpl": {Data: []byte("Subject: Hi\n\nBody\n")}})
		return gotRegister != nil
	})
	wantKind(t, gotRegister, errs.Internal)
	_, gotScoped = r.svc.Scoped("volunteers")
	wantKind(t, gotScoped, errs.Internal)
	cancel()
	wantNoErr(t, <-done)
}

// ---- Run, SendOnce, Purge, Ping on the read-only fake ------------------------------------

func TestRun_Stops_OnCancel(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, cancel := context.WithCancel(ctxT(t))
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		wantNoErr(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_AlreadyCanceled_ReturnsNil(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	wantNoErr(t, r.svc.Run(ctx))
}

func TestClose_Idempotent_StopsRun(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctxT(t)) }()
	eventually(t, "Run to start", func() bool {
		return r.svc.Register("zz"+strings.Repeat("a", 3), fstest.MapFS{"a.en.tmpl": {Data: []byte("Subject: Hi\n\nBody\n")}}) != nil
	})
	wantNoErr(t, r.svc.Close(ctxT(t)))
	select {
	case err := <-done:
		wantNoErr(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after Close")
	}
	wantNoErr(t, r.svc.Close(ctxT(t)))
	wantNoErr(t, r.svc.Close(ctxT(t)))
}

func TestClose_WithoutRun_Nil(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.svc.Close(ctxT(t)))
	wantNoErr(t, r.svc.Close(ctxT(t)))
}

func TestSendOnce_ReadOnly_NothingLeased(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	d.SetReadOnly(true)
	got, err := r.svc.SendOnce(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
	if got != (notify.Sent{}) {
		t.Fatalf("Sent = %+v, want zero", got)
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("something was sent")
	}
	if statements(d, "update") != 0 {
		t.Fatal("a lease was attempted")
	}
}

func TestPurge_Forever_DeletesNothing(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	r.ret.set(0)
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 0 {
		t.Fatalf("Purge = %d", n)
	}
	if statements(d, "delete") != 0 {
		t.Fatal("Purge deleted with retention 0")
	}
}

func TestPurge_ReadOnly_Unavailable(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	r.ret.set(30 * 24 * time.Hour)
	d.SetReadOnly(true)
	_, err := r.svc.Purge(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
}

func TestPing_NotConfigured_Unsupported(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	err := r.svc.Ping(ctxT(t))
	wantKind(t, err, errs.Unsupported)
	if !errors.Is(err, notify.ErrNotConfigured) {
		t.Fatal("does not wrap ErrNotConfigured")
	}
	if r.ch.factoryCalls() != 0 {
		t.Fatal("a channel was built for an unconfigured service")
	}
}

func TestPing_UsesCurrentSettings(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.svc.Ping(ctxT(t)))
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "other.invalid"; c.Port = 2626 })
	wantNoErr(t, r.svc.Ping(ctxT(t)))
	r.ch.mu.Lock()
	defer r.ch.mu.Unlock()
	if r.ch.pings != 2 || len(r.ch.cfgs) != 2 {
		t.Fatalf("pings=%d factories=%d", r.ch.pings, len(r.ch.cfgs))
	}
	if r.ch.cfgs[1].Host != "other.invalid" || r.ch.cfgs[1].Port != 2626 {
		t.Fatalf("second ping used stale settings: %+v", r.ch.cfgs[1])
	}
}

func TestPing_ChannelFails_ReturnsItsKind(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		t.Run(k.String(), func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			r.ch.pingErr = errs.E("test.ping", k, errors.New("nope"))
			wantKind(t, r.svc.Ping(ctxT(t)), k)
		})
	}
}

func TestPing_FactoryInvalid_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.ch.factErr = errs.E("test.factory", errs.Invalid, errors.New("bad config"))
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Invalid)
}

func TestPing_Unreachable_Unavailable(t *testing.T) {
	// The default channel (smtp.New) against a loopback port nobody listens on.
	port := closedPort(t)
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Channel = nil })
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "127.0.0.1"; c.Port = port; c.Security = notify.None })
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
}

func TestPing_CanceledContext_Canceled(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Channel = nil })
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "127.0.0.1"; c.Port = closedPort(t); c.Security = notify.None })
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	err := r.svc.Ping(ctx)
	if err == nil || (!errs.Is(err, errs.Canceled) && !errs.Is(err, errs.Unavailable)) {
		t.Fatalf("Ping(canceled) = %v, want Canceled", err)
	}
	wantKind(t, err, errs.Canceled)
}

// ---- Admin permissions (F37, F38, F39) --------------------------------------------------

type adminCall struct {
	name string
	perm authz.Permission
	call func(r *rig, ctx context.Context) error
}

func adminCalls() []adminCall {
	text := notify.TemplateText{Subject: "Hi {{.Site.Name}}", Body: "Body {{.Link}}"}
	return []adminCall{
		{"ListTemplates", permTemplates, func(r *rig, c context.Context) error { return errOf(r.svc.ListTemplates(c)) }},
		{"GetTemplate", permTemplates, func(r *rig, c context.Context) error {
			return errOf(r.svc.GetTemplate(c, "volunteers.greet", "en"))
		}},
		{"PutTemplate", permTemplates, func(r *rig, c context.Context) error {
			return errOf(r.svc.PutTemplate(c, "volunteers.greet", "en", text))
		}},
		{"ResetTemplate", permTemplates, func(r *rig, c context.Context) error { return r.svc.ResetTemplate(c, "volunteers.greet", "en") }},
		{"Preview", permTemplates, func(r *rig, c context.Context) error {
			return errOf(r.svc.Preview(c, "volunteers.greet", "en", text))
		}},
		{"SendTest", permTemplates, func(r *rig, c context.Context) error { return errOf(r.svc.SendTest(c)) }},
		{"Outbox", permOutbox, func(r *rig, c context.Context) error { return errOf(r.svc.Outbox(c)) }},
		{"Retry", permOutbox, func(r *rig, c context.Context) error { return r.svc.Retry(c, model.NewID[notify.MessageID]()) }},
		{"Cancel", permOutbox, func(r *rig, c context.Context) error { return r.svc.Cancel(c, model.NewID[notify.MessageID]()) }},
	}
}

func TestAdmin_NoActor_Unauthenticated(t *testing.T) {
	for _, ac := range adminCalls() {
		t.Run(ac.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			wantKind(t, ac.call(r, ctxT(t)), errs.Unauthenticated)
			if len(r.rec.Recorded()) != 0 {
				t.Fatal("audited an unauthenticated call")
			}
			if statements(d, "insert")+statements(d, "update")+statements(d, "delete") != 0 {
				t.Fatal("wrote on behalf of nobody")
			}
		})
	}
}

func TestAdmin_PermissionMatrix(t *testing.T) {
	for _, ac := range adminCalls() {
		other := permOutbox
		if ac.perm == permOutbox {
			other = permTemplates
		}
		t.Run(ac.name+"/no grants", func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			wantKind(t, ac.call(r, as(ctxT(t), r.holder())), errs.Permission)
		})
		t.Run(ac.name+"/the other admin permission", func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			wantKind(t, ac.call(r, as(ctxT(t), r.holder(other))), errs.Permission)
		})
		t.Run(ac.name+"/an unrelated permission", func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			wantKind(t, ac.call(r, as(ctxT(t), r.holder("settings.manage"))), errs.Permission)
		})
		t.Run(ac.name+"/the right permission", func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			err := ac.call(r, as(ctxT(t), r.holder(ac.perm)))
			if errs.Is(err, errs.Permission) || errs.Is(err, errs.Unauthenticated) {
				t.Fatalf("holder of %s refused: %v", ac.perm, err)
			}
		})
		t.Run(ac.name+"/superuser", func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			p := newPerson()
			r.az.Superuser(p)
			err := ac.call(r, asSuper(ctxT(t), p))
			if errs.Is(err, errs.Permission) || errs.Is(err, errs.Unauthenticated) {
				t.Fatalf("superuser refused: %v", err)
			}
		})
	}
}

func TestAdmin_PermissionCheckedBeforeArguments(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx := as(ctxT(t), r.holder())
	_, err := r.svc.GetTemplate(ctx, "nosuch.template", "EN_us")
	wantKind(t, err, errs.Permission)
	_, err = r.svc.PutTemplate(ctx, "nosuch.template", "EN_us", notify.TemplateText{})
	wantKind(t, err, errs.Permission)
	wantKind(t, r.svc.ResetTemplate(ctx, "nosuch.template", "EN_us"), errs.Permission)
	_, err = r.svc.Preview(ctx, "nosuch.template", "EN_us", notify.TemplateText{})
	wantKind(t, err, errs.Permission)
	wantKind(t, r.svc.Retry(ctx, model.NewID[notify.MessageID]()), errs.Permission)
}

// ---- Template admin that fails before the store (F40, F41) -------------------------------

func TestGetTemplate_Unknown_NotFound(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx := as(ctxT(t), r.holder(permTemplates))
	_, err := r.svc.GetTemplate(ctx, "volunteers.nosuch", "en")
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.GetTemplate(ctx, "nosuch.greet", "en")
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.GetTemplate(ctx, "volunteers.greet", "fr")
	wantKind(t, err, errs.NotFound)
	wantKind(t, r.svc.ResetTemplate(ctx, "volunteers.nosuch", "en"), errs.NotFound)
	_, err = r.svc.PutTemplate(ctx, "volunteers.nosuch", "en", notify.TemplateText{Subject: "x", Body: "y"})
	wantKind(t, err, errs.NotFound)
}

func TestPutTemplate_BadLanguage_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx := as(ctxT(t), r.holder(permTemplates))
	ok := notify.TemplateText{Subject: "x", Body: "y"}
	for _, lang := range []notify.Language{"EN_us", "e", "en us", "en-" + notify.Language(strings.Repeat("a", 40))} {
		_, err := r.svc.PutTemplate(ctx, "volunteers.greet", lang, ok)
		wantField(t, err, "language", "malformed")
		wantField(t, r.svc.ResetTemplate(ctx, "volunteers.greet", lang), "language", "malformed")
		_, err = r.svc.Preview(ctx, "volunteers.greet", lang, ok)
		wantField(t, err, "language", "malformed")
	}
}

func templateValidationCases() []struct {
	name        string
	text        notify.TemplateText
	field, code string
} {
	return []struct {
		name        string
		text        notify.TemplateText
		field, code string
	}{
		{"empty subject", notify.TemplateText{Subject: "", Body: "b"}, "subject", "required"},
		{"empty body", notify.TemplateText{Subject: "s", Body: ""}, "body", "required"},
		{"both empty", notify.TemplateText{}, "subject", "required"},
		{"body over the cap", notify.TemplateText{Subject: "s", Body: strings.Repeat("b", notify.MaxTemplateBytes+1)}, "body", "too_long"},
		{"subject over 1024 bytes", notify.TemplateText{Subject: strings.Repeat("s", 1025), Body: "b"}, "subject", "too_long"},
		{"subject bell", notify.TemplateText{Subject: "s\x07", Body: "b"}, "subject", "control_chars"},
		{"subject newline", notify.TemplateText{Subject: "s\nBcc: x", Body: "b"}, "subject", "control_chars"},
		{"body NUL", notify.TemplateText{Subject: "s", Body: "b\x00"}, "body", "control_chars"},
		{"subject parse error", notify.TemplateText{Subject: "{{", Body: "b"}, "subject", "parse"},
		{"body parse error", notify.TemplateText{Subject: "s", Body: "{{.Vars.x"}, "body", "parse"},
		{"body unknown function", notify.TemplateText{Subject: "s", Body: "{{upper .Vars.x}}"}, "body", "parse"},
		{"subject unknown function", notify.TemplateText{Subject: "{{upper .Link}}", Body: "b"}, "subject", "parse"},
	}
}

func TestPutTemplate_Validation_Table(t *testing.T) {
	for _, tc := range templateValidationCases() {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			ctx := as(ctxT(t), r.holder(permTemplates))
			_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", tc.text)
			wantField(t, err, tc.field, tc.code)
			if statements(d, "insert") != 0 || len(r.rec.Recorded()) != 0 {
				t.Fatal("an invalid override was saved or audited")
			}
		})
	}
}

func TestPreview_Validation_Table(t *testing.T) {
	for _, tc := range templateValidationCases() {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			ctx := as(ctxT(t), r.holder(permTemplates))
			_, err := r.svc.Preview(ctx, "volunteers.greet", "en", tc.text)
			wantField(t, err, tc.field, tc.code)
		})
	}
}

// ---- Export / Delete argument checks (F50) -----------------------------------------------

func TestExportPerson_ZeroID_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	_, err := r.svc.ExportPerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

// ---- concurrency -------------------------------------------------------------------------

func TestService_ConcurrentUse_IsRaceFree(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	admin := r.holder(permTemplates, permOutbox)
	people := make([]model.PersonID, 8)
	for i := range people {
		people[i] = r.person("p@example.org")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := as(ctxT(t), admin)
			ignore(r.vol.Send(ctx, greet(people[i])))
			ignore(r.vol.Configured(ctx))
			ignore(r.svc.ListTemplates(ctx))
			ignore(r.svc.GetTemplate(ctx, "volunteers.greet", "en"))
			ignore(r.svc.Preview(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "b"}))
			ignore(r.svc.Ping(ctx))
			ignore(r.svc.Scoped("people"))
		}()
	}
	wg.Wait()
}

// ---- routes (F63) ------------------------------------------------------------------------

func TestRoutes_ArePresent(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	want := map[string]bool{
		"GET /templates": false, "GET /templates/{name}/{lang}": false, "PUT /templates/{name}/{lang}": false,
		"DELETE /templates/{name}/{lang}": false, "POST /templates/{name}/{lang}/preview": false,
		"POST /test": false, "GET /outbox": false, "POST /outbox/check": false, "POST /outbox/{message}/retry": false,
		"POST /outbox/{message}/cancel": false,
	}
	for _, rt := range r.svc.Routes() {
		if rt.Handler == nil {
			t.Errorf("%s %s has no handler", rt.Method, rt.Pattern)
		}
		key := rt.Method + " " + strings.TrimPrefix(rt.Pattern, "/api/notify")
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected route %s", key)
			continue
		}
		want[key] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("route %s is missing", k)
		}
	}
	if len(r.svc.OpenAPI()) == 0 {
		t.Error("OpenAPI is empty")
	}
}

func TestHandlers_NoSession_401_NoPermission_403(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{"GET", "/templates", ""},
		{"GET", "/templates/volunteers.greet/en", ""},
		{"PUT", "/templates/volunteers.greet/en", `{"subject":"s","body":"b"}`},
		{"DELETE", "/templates/volunteers.greet/en", ""},
		{"POST", "/templates/volunteers.greet/en/preview", `{"subject":"s","body":"b"}`},
		{"POST", "/test", ""},
		{"GET", "/outbox", ""},
		{"POST", "/outbox/check", ""},
		{"POST", "/outbox/00000000-0000-4000-8000-000000000001/retry", ""},
		{"POST", "/outbox/00000000-0000-4000-8000-000000000001/cancel", ""},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			w := r.do(ctxT(t), rt.method, rt.path, rt.body)
			if w.Code != statusFor(errs.Unauthenticated) {
				t.Fatalf("no session: status %d, want 401", w.Code)
			}
			w = r.do(as(ctxT(t), r.holder()), rt.method, rt.path, rt.body)
			if w.Code != statusFor(errs.Permission) {
				t.Fatalf("no permission: status %d, want 403", w.Code)
			}
		})
	}
}

func TestHandlers_BadBody_Invalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx := as(ctxT(t), r.holder(permTemplates))
	for _, path := range []string{"/templates/volunteers.greet/en", "/templates/volunteers.greet/en/preview"} {
		method := "PUT"
		if strings.HasSuffix(path, "preview") {
			method = "POST"
		}
		t.Run(method+" malformed JSON "+path, func(t *testing.T) {
			if w := r.do(ctx, method, path, `{"subject":`); w.Code != 400 {
				t.Fatalf("status %d, want 400", w.Code)
			}
		})
		t.Run(method+" wrong type "+path, func(t *testing.T) {
			if w := r.do(ctx, method, path, `{"subject":123,"body":[]}`); w.Code != 400 {
				t.Fatalf("status %d, want 400", w.Code)
			}
		})
		t.Run(method+" body over the cap "+path, func(t *testing.T) {
			big := `{"subject":"s","body":"` + strings.Repeat("x", 4<<20) + `"}`
			w := r.do(ctx, method, path, big)
			if w.Code != 400 && w.Code != 413 {
				t.Fatalf("status %d, want 400 or 413", w.Code)
			}
		})
	}
}

func TestHandlers_UnknownTemplate_404_BadLanguage_400(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx := as(ctxT(t), r.holder(permTemplates))
	if w := r.do(ctx, "GET", "/templates/volunteers.nosuch/en", ""); w.Code != 404 {
		t.Fatalf("unknown template: %d", w.Code)
	}
	if w := r.do(ctx, "PUT", "/templates/volunteers.greet/EN_us", `{"subject":"s","body":"b"}`); w.Code != 400 {
		t.Fatalf("bad language: %d", w.Code)
	}
}

func TestSend_TTLOverMaxAge_Invalid(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	m := greet(r.person("a@example.org"))
	m.TTL = notify.MaxAge + time.Second
	wantField(t, r.vol.Send(ctxT(t), m), "ttl", "range")
	if statements(d, "insert") != 0 {
		t.Fatal("a row was inserted")
	}
}

func TestExpireStale_ReadOnly_Unavailable(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	d.SetReadOnly(true)
	_, err := r.svc.ExpireStale(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
}

func permissionRefused(t *testing.T, perm authz.Permission) {
	t.Helper()
	for _, ac := range adminCalls() {
		if ac.perm != perm {
			continue
		}
		other := permOutbox
		if perm == permOutbox {
			other = permTemplates
		}
		for _, grants := range map[string][]authz.Permission{"no grants": nil, "the other admin permission": {other}} {
			r := newRig(t, dbtest.NewFake())
			wantKind(t, ac.call(r, as(ctxT(t), r.holder(grants...))), errs.Permission)
		}
	}
}

func TestTemplates_NoPermission_Permission(t *testing.T) { permissionRefused(t, permTemplates) }

func TestOutbox_NoPermission_Permission(t *testing.T) { permissionRefused(t, permOutbox) }

func TestPutTemplate_ParseError_HasLine(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx := as(ctxT(t), r.holder(permTemplates))
	_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "fine\nstill fine\n{{.Vars.x"})
	wantField(t, err, "body", "parse")
	if got := paramOf(err, "body", "parse", "line"); got != "3" {
		t.Fatalf("line = %q, want 3 (%v)", got, describe(err))
	}
}
