package notify_test

import (
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

// boundaries step 5: notify maps its "notify.*" keys onto SMTPConfig and the retention
// itself, from Options.Settings. The SMTP mapping is reached through the exported
// notify.SMTPFromSettings (assumed to stay exported for bms check until notify.Probe,
// step 10); the retention mapping is unexported and is seen through Service.Purge.

// Moved from app: TestSMTPSource_NothingSet_IsNotConfigured.
func TestSMTPSource_NothingSet_IsNotConfigured(t *testing.T) {
	cfg, err := notify.SMTPFromSettings(t.Context(), settingstest.NewFake("notify", notify.Settings))
	if err != nil {
		t.Fatalf("SMTPFromSettings: %v", err)
	}
	if cfg.Configured() || cfg.Port != 587 || cfg.Security != notify.StartTLS {
		t.Errorf("cfg = %+v, want unconfigured with the default port and starttls", cfg)
	}
}

// Moved from app: TestSMTPSource_ReadsEveryKey.
func TestSMTPSource_ReadsEveryKey(t *testing.T) {
	r := settingstest.NewFake("notify", notify.Settings)
	r.Set("smtp_host", "mail.example.org")
	r.Set("smtp_port", "2525")
	r.Set("smtp_security", "tls")
	r.Set("smtp_username", "outbox")
	r.SetSecret("smtp_password", secret.New("pw-for-the-test"))
	r.Set("sender_address", "bms@example.org")
	r.Set("sender_name", "Quill Burn")
	r.Set("reply_to", "help@example.org")
	r.Set("alert_addresses", "a@example.org, b@example.org")
	cfg, err := notify.SMTPFromSettings(t.Context(), r)
	if err != nil {
		t.Fatalf("SMTPFromSettings: %v", err)
	}
	if !cfg.Configured() || cfg.Host != "mail.example.org" || cfg.Port != 2525 || cfg.Security != notify.TLS ||
		cfg.Username != "outbox" || cfg.Password.Value() != "pw-for-the-test" ||
		cfg.Sender.Email != "bms@example.org" || cfg.Sender.Name != "Quill Burn" ||
		cfg.ReplyTo == nil || *cfg.ReplyTo != (notify.Address{Email: "help@example.org"}) ||
		!slices.Equal(cfg.AlertAddresses, []model.Email{"a@example.org", "b@example.org"}) {
		t.Errorf("cfg = %+v", cfg)
	}
}

// Moved from app: TestSMTPSource_WrongArea_ReturnsInvalid.
func TestSMTPSource_WrongArea_ReturnsInvalid(t *testing.T) {
	if _, err := notify.SMTPFromSettings(t.Context(), settingstest.NewFake("people", nil)); !errs.Is(err, errs.Invalid) {
		t.Errorf("err = %v, want Invalid", err)
	}
}

// The service reads the SMTP keys per use case through Options.Settings: Configured
// follows the settings as they change, and a Reader of another area fails Invalid.
func TestConfigured_FollowsTheSettingsReader(t *testing.T) {
	f := settingstest.NewFake("notify", notify.Settings)
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Settings = f })
	ctx := t.Context()
	got, err := r.vol.Configured(ctx)
	wantNoErr(t, err)
	if got {
		t.Fatal("Configured with nothing set")
	}
	f.Set("smtp_host", "mail.example.org")
	f.Set("sender_address", "bms@example.org")
	got, err = r.vol.Configured(ctx)
	wantNoErr(t, err)
	if !got {
		t.Error("Configured = false after the SMTP keys were set: the settings were cached")
	}
}

func TestConfigured_WrongArea_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Settings = settingstest.NewFake("people", nil) })
	_, err := r.vol.Configured(t.Context())
	wantKind(t, err, errs.Invalid)
}

// Moved from app: TestRetention_Positive_PurgeReachesTheStore. Retention 0 keeps everything (no
// statement); a positive one makes Purge reach the store; a failing Reader fails Purge with
// its kind.
func TestRetention_Positive_PurgeReachesTheStore(t *testing.T) {
	f := settingstest.NewFake("notify", notify.Settings)
	f.Set("retention", "240h")
	d := dbtest.NewFake()
	r := newRig(t, d, func(o *notify.Options) { o.Settings = f })
	before := len(d.Calls())
	_, err := r.svc.Purge(t.Context())
	wantNoErr(t, err)
	if len(d.Calls()) == before {
		t.Error("Purge ran no statement with notify.retention = 240h")
	}

	f.Set("retention", "0s")
	d2 := dbtest.NewFake()
	r2 := newRig(t, d2, func(o *notify.Options) { o.Settings = f })
	n, err := r2.svc.Purge(t.Context())
	wantNoErr(t, err)
	if n != 0 || len(d2.Calls()) != 0 {
		t.Errorf("Purge with retention 0 = %d rows, %d statements, want none", n, len(d2.Calls()))
	}
}

func TestRetention_WrongArea_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake(), func(o *notify.Options) { o.Settings = settingstest.NewFake("people", nil) })
	_, err := r.svc.Purge(t.Context())
	wantKind(t, err, errs.Invalid)
}

// An unset key is settings' declared default, not a constant in notify: the declarations
// here carry other defaults (port 2626, retention 240h) and notify must follow them.
func TestPolicy_UnsetKey_IsTheDeclaredDefault(t *testing.T) {
	decls := append([]settings.Decl(nil), notify.Settings...)
	for i := range decls {
		switch decls[i].Key {
		case "smtp_port":
			decls[i].Default = "2626"
		case "smtp_security":
			decls[i].Default = "tls"
		}
	}
	cfg, err := notify.SMTPFromSettings(t.Context(), settingstest.NewFake("notify", decls))
	wantNoErr(t, err)
	if cfg.Port != 2626 || cfg.Security != notify.TLS {
		t.Errorf("cfg = port %d, security %v; want the declared defaults 2626 and tls", cfg.Port, cfg.Security)
	}

	for i := range decls {
		if decls[i].Key == "retention" {
			decls[i].Default = "240h"
		}
	}
	d := dbtest.NewFake()
	r := newRig(t, d, func(o *notify.Options) { o.Settings = settingstest.NewFake("notify", decls) })
	before := len(d.Calls())
	_, err = r.svc.Purge(t.Context())
	wantNoErr(t, err)
	if len(d.Calls()) == before {
		t.Error("Purge ran no statement: the declared retention default (240h) was not used")
	}
}
