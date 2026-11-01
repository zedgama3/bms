package notify_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

func TestOptionsValidate_Table(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*notify.Options)
		field string // "" = valid
	}{
		{"valid as built", func(*notify.Options) {}, ""},
		{"optional fields nil", func(o *notify.Options) { o.Meter = nil; o.Channel = nil }, ""},
		{"zero durations take defaults", func(o *notify.Options) { o.PollInterval, o.Lease, o.SendTimeout = 0, 0, 0 }, ""},
		{"nil db", func(o *notify.Options) { o.DB = nil }, "notify.db"},
		{"nil clock", func(o *notify.Options) { o.Clock = nil }, "notify.clock"},
		{"nil logger", func(o *notify.Options) { o.Logger = nil }, "notify.logger"},
		{"nil tracer", func(o *notify.Options) { o.Tracer = nil }, "notify.tracer"},
		{"nil audit", func(o *notify.Options) { o.Audit = nil }, "notify.audit"},
		{"nil authz", func(o *notify.Options) { o.Authz = nil }, "notify.authz"},
		{"nil actor", func(o *notify.Options) { o.Actor = nil }, "notify.actor"},
		{"nil settings", func(o *notify.Options) { o.Settings = nil }, "notify.settings"},
		{"nil regional", func(o *notify.Options) { o.Regional = nil }, "notify.regional"},
		{"nil terms", func(o *notify.Options) { o.Terms = nil }, "notify.terms"},
		{"nil accounts", func(o *notify.Options) { o.Accounts = nil }, "notify.accounts"},
		{"nil profiles", func(o *notify.Options) { o.Profiles = nil }, "notify.profiles"},
		{"poll at minimum", func(o *notify.Options) { o.PollInterval = time.Second }, ""},
		{"poll at maximum", func(o *notify.Options) { o.PollInterval = 5 * time.Minute }, ""},
		{"poll below minimum", func(o *notify.Options) { o.PollInterval = 999 * time.Millisecond }, "notify.poll_interval"},
		{"poll above maximum", func(o *notify.Options) { o.PollInterval = 5*time.Minute + time.Second }, "notify.poll_interval"},
		{"poll negative", func(o *notify.Options) { o.PollInterval = -time.Second }, "notify.poll_interval"},
		{"lease at minimum", func(o *notify.Options) { o.Lease = 30 * time.Second }, ""},
		{"lease at maximum", func(o *notify.Options) { o.Lease = 30 * time.Minute }, ""},
		{"lease below minimum", func(o *notify.Options) { o.Lease = 29 * time.Second }, "notify.lease"},
		{"lease above maximum", func(o *notify.Options) { o.Lease = 30*time.Minute + time.Second }, "notify.lease"},
		{"send timeout at minimum", func(o *notify.Options) { o.SendTimeout = 5 * time.Second }, ""},
		{"send timeout at maximum", func(o *notify.Options) { o.SendTimeout = 5 * time.Minute }, ""},
		{"send timeout below minimum", func(o *notify.Options) { o.SendTimeout = 4 * time.Second }, "notify.send_timeout"},
		{"send timeout above maximum", func(o *notify.Options) { o.SendTimeout = 5*time.Minute + time.Second }, "notify.send_timeout"},
		{"hostname with underscore", func(o *notify.Options) { o.Hostname = "bms_test" }, "notify.hostname"},
		{"hostname upper case", func(o *notify.Options) { o.Hostname = "BMS.example" }, "notify.hostname"},
		{"hostname with space", func(o *notify.Options) { o.Hostname = "bms test" }, "notify.hostname"},
		{"hostname with CRLF", func(o *notify.Options) { o.Hostname = "bms\r\nBcc: x" }, "notify.hostname"},
		{"hostname 253 bytes", func(o *notify.Options) { o.Hostname = strings.Repeat("a", 253) }, ""},
		{"hostname 254 bytes", func(o *notify.Options) { o.Hostname = strings.Repeat("a", 254) }, "notify.hostname"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, o := newRigOptions(t, dbtest.NewFake())
			tc.mod(&o)
			verr := o.Validate()
			_, nerr := notify.New(o)
			if tc.field == "" {
				wantNoErr(t, verr)
				wantNoErr(t, nerr)
				return
			}
			wantField(t, verr, tc.field, "")
			wantField(t, nerr, tc.field, "")
		})
	}
}

func TestOptionsValidate_ZeroOptions_ReportsEveryRequiredField(t *testing.T) {
	err := (notify.Options{}).Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{
		"notify.db", "notify.clock", "notify.logger", "notify.tracer", "notify.audit", "notify.authz",
		"notify.actor", "notify.settings", "notify.regional", "notify.terms", "notify.accounts", "notify.profiles",
	} {
		if !hasField(err, f, "") {
			t.Errorf("missing field error %q in %v", f, describe(err))
		}
	}
}

func TestNew_DoesNoIO(t *testing.T) {
	d := dbtest.NewFake()
	r, o := newRigOptions(t, d)
	_, err := notify.New(o)
	wantNoErr(t, err)
	if n := len(d.Calls()); n != 0 {
		t.Fatalf("New ran %d statements", n)
	}
	if n := r.smtp.calls.Load() + seamCalls(r); n != 0 {
		t.Fatalf("New called a seam %d times", n)
	}
}

func TestNew_WorksAgainstDBNone(t *testing.T) {
	_, o := newRigOptions(t, dbtest.NewFake())
	o.DB = db.None()
	svc, err := notify.New(o)
	wantNoErr(t, err)
	wantNoErr(t, svc.Register("volunteers", volunteerTemplates()))
	_, err = svc.Scoped("volunteers")
	wantNoErr(t, err)
}

func TestNew_RegistersItsOwnTemplate(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	admin := r.holder(permTemplates)
	list, err := r.svc.ListTemplates(as(ctxT(t), admin))
	wantNoErr(t, err)
	found := false
	for _, ti := range list {
		if ti.Name == "notify.test" {
			found = true
			have := map[notify.Language]bool{}
			for _, l := range ti.Languages {
				have[l.Language] = true
			}
			if !have["en"] || !have["es"] {
				t.Fatalf("notify.test languages = %v, want en and es", ti.Languages)
			}
		}
	}
	if !found {
		t.Fatal("notify.test is not listed")
	}
}

func TestSMTPConfigValidate_Table(t *testing.T) {
	valid := func() notify.SMTPConfig {
		return notify.SMTPConfig{
			Host: "smtp.example.org", Port: 587, Security: notify.StartTLS,
			Username: "mailer", Password: secret.New("invented-password"),
			Sender:         notify.Address{Name: "Burn", Email: "burn@example.org"},
			ReplyTo:        &notify.Address{Email: "help@example.org"},
			AlertAddresses: []model.Email{"a@example.org", "b@example.org"},
		}
	}
	tenAlerts := func(n int) []model.Email {
		out := make([]model.Email, n)
		for i := range out {
			out[i] = model.Email("alert" + string(rune('a'+i)) + "@example.org")
		}
		return out
	}
	cases := []struct {
		name  string
		mod   func(*notify.SMTPConfig)
		field string
	}{
		{"valid", func(*notify.SMTPConfig) {}, ""},
		{"ip host", func(c *notify.SMTPConfig) { c.Host = "127.0.0.1" }, ""},
		{"port minimum", func(c *notify.SMTPConfig) { c.Port = 1 }, ""},
		{"port maximum", func(c *notify.SMTPConfig) { c.Port = 65535 }, ""},
		{"ten alert addresses", func(c *notify.SMTPConfig) { c.AlertAddresses = tenAlerts(10) }, ""},
		{"host with space", func(c *notify.SMTPConfig) { c.Host = "smtp example.org" }, "notify.smtp_host"},
		{"host with scheme", func(c *notify.SMTPConfig) { c.Host = "smtp://example.org" }, "notify.smtp_host"},
		{"host with port", func(c *notify.SMTPConfig) { c.Host = "example.org:25" }, "notify.smtp_host"},
		{"host over 253 bytes", func(c *notify.SMTPConfig) { c.Host = strings.Repeat("a", 254) }, "notify.smtp_host"},
		{"port zero", func(c *notify.SMTPConfig) { c.Port = 0 }, "notify.smtp_port"},
		{"port negative", func(c *notify.SMTPConfig) { c.Port = -1 }, "notify.smtp_port"},
		{"port too big", func(c *notify.SMTPConfig) { c.Port = 65536 }, "notify.smtp_port"},
		{"security zero", func(c *notify.SMTPConfig) { c.Security = 0 }, "notify.smtp_security"},
		{"security out of range", func(c *notify.SMTPConfig) { c.Security = 99 }, "notify.smtp_security"},
		{"username 255 bytes", func(c *notify.SMTPConfig) { c.Username = strings.Repeat("u", 255) }, ""},
		{"username 256 bytes", func(c *notify.SMTPConfig) { c.Username = strings.Repeat("u", 256) }, "notify.smtp_username"},
		{"sender malformed", func(c *notify.SMTPConfig) { c.Sender.Email = "not-an-address" }, "notify.sender_address"},
		{"reply-to malformed", func(c *notify.SMTPConfig) { c.ReplyTo = &notify.Address{Email: "nope"} }, "notify.reply_to"},
		{"eleven alert addresses", func(c *notify.SMTPConfig) { c.AlertAddresses = tenAlerts(11) }, "notify.alert_addresses"},
		{"alert address malformed", func(c *notify.SMTPConfig) { c.AlertAddresses = []model.Email{"a@example.org", "bad"} }, "notify.alert_addresses"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mod(&c)
			err := c.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, "")
		})
	}
	t.Run("several problems reported together", func(t *testing.T) {
		c := valid()
		c.Port, c.Security, c.Sender.Email = 0, 0, "x"
		err := c.Validate()
		for _, f := range []string{"notify.smtp_port", "notify.smtp_security", "notify.sender_address"} {
			if !hasField(err, f, "") {
				t.Errorf("missing %q in %v", f, describe(err))
			}
		}
	})
}

func TestSMTPConfigConfigured_Table(t *testing.T) {
	cases := []struct {
		name         string
		host, sender string
		want         bool
	}{
		{"both", "smtp.example.org", "burn@example.org", true},
		{"no host", "", "burn@example.org", false},
		{"no sender", "smtp.example.org", "", false},
		{"neither", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := notify.SMTPConfig{Host: tc.host, Port: 587, Security: notify.StartTLS, Sender: notify.Address{Email: model.Email(tc.sender)}}
			if got := c.Configured(); got != tc.want {
				t.Fatalf("Configured = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConstants_MatchTheSpec(t *testing.T) {
	checks := []struct {
		name      string
		got, want any
	}{
		{"MaxRecipients", notify.MaxRecipients, 100}, {"MaxVars", notify.MaxVars, 32},
		{"MaxVarKeyBytes", notify.MaxVarKeyBytes, 32}, {"MaxVarBytes", notify.MaxVarBytes, 1024},
		{"MaxLinkBytes", notify.MaxLinkBytes, 1024}, {"MaxKeyBytes", notify.MaxKeyBytes, 128},
		{"MaxTemplateBytes", notify.MaxTemplateBytes, 16384}, {"MaxSubjectBytes", notify.MaxSubjectBytes, 256},
		{"MaxAttempts", notify.MaxAttempts, 8}, {"MaxBatch", notify.MaxBatch, 50}, {"MaxOutboxList", notify.MaxOutboxList, 50},
		{"DefaultPollInterval", notify.DefaultPollInterval, 5 * time.Second},
		{"DefaultLease", notify.DefaultLease, 2 * time.Minute},
		{"DefaultSendTimeout", notify.DefaultSendTimeout, 30 * time.Second},
		{"PurgeInterval", notify.PurgeInterval, time.Hour},
		{"MinRetention", notify.MinRetention, 7 * 24 * time.Hour},
		{"TemplateFileExt", notify.TemplateFileExt, ".tmpl"},
		{"DefaultLanguage", notify.DefaultLanguage, "en"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestPermissions_DeclaredAsSpecified(t *testing.T) {
	want := map[authz.Permission]bool{permTemplates: false, permOutbox: false}
	for _, p := range notify.Permissions {
		if _, ok := want[p.Name]; !ok {
			t.Errorf("unexpected permission %q", p.Name)
			continue
		}
		want[p.Name] = true
		wantNoErr(t, p.Validate())
		if p.Scope != authz.ScopeSite {
			t.Errorf("%s scope = %v, want site", p.Name, p.Scope)
		}
		if p.Reaches != model.SensitivityInternal {
			t.Errorf("%s reaches %v, want internal", p.Name, p.Reaches)
		}
		if len(p.StarterRoles) != 1 || p.StarterRoles[0] != "settings-admin" {
			t.Errorf("%s starter roles = %v, want [settings-admin]", p.Name, p.StarterRoles)
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("permission %q is not declared", p)
		}
	}
}

func TestSettings_DeclaredAsSpecified(t *testing.T) {
	kinds := map[string]settings.Kind{
		"smtp_host": settings.Text, "smtp_port": settings.Int, "smtp_security": settings.Enum,
		"smtp_username": settings.Text, "smtp_password": settings.Secret, "sender_address": settings.Text,
		"sender_name": settings.Text, "reply_to": settings.Text, "alert_addresses": settings.Text,
		"retention": settings.Duration,
	}
	seen := map[string]settings.Decl{}
	for _, d := range notify.Settings {
		key := strings.TrimPrefix(d.Key, "notify.")
		seen[key] = d
		wantNoErr(t, d.Validate())
	}
	if len(seen) != len(kinds) {
		t.Fatalf("declared %d settings, want %d", len(seen), len(kinds))
	}
	for key, kind := range kinds {
		d, ok := seen[key]
		if !ok {
			t.Errorf("setting %q is not declared", key)
			continue
		}
		if d.Kind != kind {
			t.Errorf("%s kind = %v, want %v", key, d.Kind, kind)
		}
	}
	if d := seen["smtp_port"]; d.Default != "587" {
		t.Errorf("smtp_port default = %q", d.Default)
	}
	if d := seen["smtp_security"]; d.Default != "starttls" {
		t.Errorf("smtp_security default = %q", d.Default)
	}
	if d := seen["smtp_host"]; d.Default != "" {
		t.Errorf("smtp_host default = %q, e-mail must be off by default", d.Default)
	}
}

func TestSettings_Checks_Table(t *testing.T) {
	check := func(key, value string) error {
		for _, d := range notify.Settings {
			if strings.TrimPrefix(d.Key, "notify.") == key {
				if d.Check == nil {
					return nil
				}
				return d.Check(value)
			}
		}
		t.Fatalf("no setting %q", key)
		return nil
	}
	cases := []struct {
		key, value string
		okay       bool
	}{
		{"smtp_port", "587", true}, {"smtp_port", "1", true}, {"smtp_port", "65535", true},
		{"smtp_port", "0", false}, {"smtp_port", "65536", false}, {"smtp_port", "-1", false},
		{"smtp_host", "", true}, {"smtp_host", "mail.example.org", true}, {"smtp_host", "bad host", false},
		{"smtp_host", strings.Repeat("a", 254), false},
		{"sender_address", "", true}, {"sender_address", "burn@example.org", true}, {"sender_address", "nope", false},
		{"reply_to", "", true}, {"reply_to", "nope", false},
		{"sender_name", strings.Repeat("n", 100), true}, {"sender_name", strings.Repeat("n", 101), false},
		{"sender_name", "Burn\x07", false},
		{"smtp_username", strings.Repeat("u", 256), false},
		{"alert_addresses", "", true}, {"alert_addresses", "a@example.org, b@example.org", true},
		{"alert_addresses", "a@example.org,bad", false},
		{"alert_addresses", strings.TrimSuffix(strings.Repeat("a@example.org,", 11), ","), false},
		{"retention", "0", true}, {"retention", "168h", true}, {"retention", "876000h", true},
		{"retention", "167h", false}, {"retention", "876001h", false}, {"retention", "1h", false},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			err := check(tc.key, tc.value)
			if tc.okay {
				wantNoErr(t, err)
			} else if err == nil {
				t.Fatalf("accepted %q for %s", tc.value, tc.key)
			}
		})
	}
}
