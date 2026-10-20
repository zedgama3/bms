//go:build integration

package settings_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// ---- migrations and defaults --------------------------------------------------------------

func TestMigrations_ApplyOnEmptyDatabase_AndAgain(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateSettings(t, pool)
	migrateSettings(t, pool) // idempotent
	r := newIRigOn(t, pool, pool)
	if got := r.dbRevision(); got != 1 {
		t.Errorf("a fresh database has revision %d, want 1", got)
	}
	if n := r.rowCount(); n != 0 {
		t.Errorf("a fresh database holds %d settings", n)
	}
	var n int
	wantNoErr(t, pool.QueryRow(ctxT(t), "SELECT count(*) FROM core.terminology").Scan(&n))
	if n != 0 {
		t.Errorf("a fresh database holds %d term overrides", n)
	}
	if got := r.doc().Revision; got != 1 {
		t.Errorf("Document revision = %d, want 1", got)
	}
}

func TestSettingTable_RejectsBothOrNeitherValueAndSealed(t *testing.T) {
	r := newIRig(t)
	_, err := r.pool.Exec(ctxT(t), "INSERT INTO core.setting (key, value, sealed, updated_at) VALUES ('x.a', 'v', '\\x00', now())")
	if err == nil {
		t.Error("a row with value and sealed was accepted")
	}
	_, err = r.pool.Exec(ctxT(t), "INSERT INTO core.setting (key, updated_at) VALUES ('x.b', now())")
	if err == nil {
		t.Error("a row with neither value nor sealed was accepted")
	}
	_, err = r.pool.Exec(ctxT(t), "INSERT INTO core.setting_revision (id, revision) VALUES (2, 1)")
	if err == nil {
		t.Error("a second revision row was accepted")
	}
}

func TestDocument_Fresh_ListsAreasInOrderWithDefaults(t *testing.T) {
	r := newIRig(t)
	doc := r.doc()
	var names []string
	for _, a := range doc.Areas {
		names = append(names, a.Name)
	}
	if want := []string{"regional", "accounts", "mail", "hello"}; !slices.Equal(names, want) {
		t.Fatalf("areas = %v, want %v (regional, core areas, then modules in build order)", names, want)
	}
	for _, a := range doc.Areas {
		wantCore := a.Name != "hello"
		if a.Core != wantCore {
			t.Errorf("area %s: Core = %v", a.Name, a.Core)
		}
		for _, s := range a.Settings {
			if !strings.HasPrefix(s.Key, a.Name+".") {
				t.Errorf("setting %s is not under its area %s", s.Key, a.Name)
			}
			if s.Value != nil || s.Stale || s.SecretSet {
				t.Errorf("fresh %s: value=%v stale=%v secret_set=%v", s.Key, s.Value, s.Stale, s.SecretSet)
			}
		}
	}
	for key, want := range map[string]struct{ kind, def string }{
		kPort: {"int", "587"}, kEnabled: {"bool", "false"}, kWait: {"duration", "30s"},
		kHost: {"text", ""}, kSecurity: {"enum", "starttls"}, kPassword: {"secret", ""},
		"regional.name": {"text", "BMS"}, "regional.language": {"enum", "en"},
	} {
		s, ok := settingOf(doc, key)
		if !ok {
			t.Errorf("Document lacks %s", key)
			continue
		}
		if s.Kind != want.kind || s.Default != want.def {
			t.Errorf("%s = kind %q default %q, want %q %q", key, s.Kind, s.Default, want.kind, want.def)
		}
	}
	if s := first(settingOf(doc, kSecurity)); !slices.Equal(s.Options, []string{"starttls", "tls", "none"}) {
		t.Errorf("security options = %v", s.Options)
	}
	regional := doc.Areas[0]
	for _, key := range []string{"name", "contact_email", "public_url", "language", "timezone", "color_primary", "color_primary_dark"} {
		if _, ok := settingOf(doc, "regional."+key); !ok {
			t.Errorf("regional.%s is not declared", key)
		}
	}
	if len(regional.Settings) != 7 {
		t.Errorf("regional declares %d settings, want 7", len(regional.Settings))
	}
}

func TestReader_Defaults(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	mail := r.reader("mail")
	if v, err := mail.Int(ctx, "port"); err != nil || v != 587 {
		t.Errorf("port = %v, %v", v, err)
	}
	if v, err := mail.Bool(ctx, "enabled"); err != nil || v {
		t.Errorf("enabled = %v, %v", v, err)
	}
	if v, err := mail.Duration(ctx, "wait"); err != nil || v != 30*time.Second {
		t.Errorf("wait = %v, %v", v, err)
	}
	if v, err := mail.Text(ctx, "security"); err != nil || v != "starttls" {
		t.Errorf("security = %q, %v (Text reads an Enum)", v, err)
	}
	if v, err := mail.Text(ctx, "note"); err != nil || v != "hello" {
		t.Errorf("note = %q, %v", v, err)
	}
	if v, err := mail.Secret(ctx, "password"); err != nil || v.IsSet() {
		t.Errorf("unset secret = %v, %v", v.IsSet(), err)
	}
	if v, err := r.reader("hello").Text(ctx, "greeting"); err != nil || v != "Hello, burner" {
		t.Errorf("greeting = %q, %v", v, err)
	}
}

// ---- Set: the round trip -------------------------------------------------------------------

func TestSet_RoundTrip_AllKinds(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	before := r.dbRevision()
	rev, err := r.svc.Set(r.actx(), changes(
		kPort, "2525", kEnabled, "true", kWait, "90m", kSecurity, "tls", kHost, "mx.example.org",
		kGreeting, "Welcome home", kPassword, "invented-pass-1",
	))
	wantNoErr(t, err)
	if int64(rev) != before+1 {
		t.Errorf("revision = %d, want %d (one bump per Set, not per key)", rev, before+1)
	}
	if got := r.dbRevision(); got != int64(rev) {
		t.Errorf("stored revision = %d, returned %d", got, rev)
	}

	// Visible to the next read in this process before Set returned.
	mail := r.reader("mail")
	if v := first(mail.Int(ctx, "port")); v != 2525 {
		t.Errorf("port = %d", v)
	}
	if v := first(mail.Bool(ctx, "enabled")); !v {
		t.Error("enabled not true")
	}
	if v := first(mail.Duration(ctx, "wait")); v != 90*time.Minute {
		t.Errorf("wait = %v", v)
	}
	if v := first(mail.Text(ctx, "security")); v != "tls" {
		t.Errorf("security = %q", v)
	}
	if v := first(mail.Text(ctx, "host")); v != "mx.example.org" {
		t.Errorf("host = %q", v)
	}
	if v := first(r.reader("hello").Text(ctx, "greeting")); v != "Welcome home" {
		t.Errorf("greeting = %q", v)
	}
	if v, err := mail.Secret(ctx, "password"); err != nil || v.Value() != "invented-pass-1" {
		t.Errorf("password round trip failed: %v", err)
	}

	doc := r.doc()
	if doc.Revision != rev {
		t.Errorf("Document revision = %d, want %d", doc.Revision, rev)
	}
	if s := first(settingOf(doc, kPort)); s.Value == nil || *s.Value != "2525" {
		t.Errorf("Document port value = %v", s.Value)
	}
	if v, ok := r.stored(kPort); !ok || v != "2525" {
		t.Errorf("stored port = %q, %v", v, ok)
	}

	// A different process (a new Service on the same database) loads the same values.
	other := newIRigOn(t, r.pool, r.pool)
	if v, err := other.reader("mail").Int(ctx, "port"); err != nil || v != 2525 {
		t.Errorf("second service port = %v, %v", v, err)
	}
}

func TestSet_Revision_StrictlyIncreasesOnEveryWrite(t *testing.T) {
	r := newIRig(t)
	a := r.mustSet(kPort, "1000")
	b := r.mustSet(kPort, "1001")
	c := r.mustReset(kPort)
	if a >= b || b >= c {
		t.Errorf("revisions %d, %d, %d are not increasing", a, b, c)
	}
}

func TestSet_Reset_DeletesRowAndRestoresDefault(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPort, "2525")
	if _, ok := r.stored(kPort); !ok {
		t.Fatal("no row after Set")
	}
	before := r.rowCount()
	r.mustReset(kPort)
	if _, ok := r.stored(kPort); ok {
		t.Error("the row survived a reset")
	}
	if got := r.rowCount(); got != before-1 {
		t.Errorf("rows = %d, want %d", got, before-1)
	}
	if v := first(r.reader("mail").Int(ctxT(t), "port")); v != 587 {
		t.Errorf("port = %d after reset, want the default", v)
	}
	if s := first(settingOf(r.doc(), kPort)); s.Value != nil {
		t.Errorf("Document still shows a value: %v", *s.Value)
	}
}

func TestSet_ValueEqualToDefault_IsStoredAsReset(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPort, "2525")
	r.mustSet(kPort, "587") // the default
	if _, ok := r.stored(kPort); ok {
		t.Error("a value equal to the default is stored")
	}
	if s := first(settingOf(r.doc(), kPort)); s.Value != nil {
		t.Errorf("Document value = %v, want nil (default applies)", *s.Value)
	}
	// A fresh key set to its default writes nothing at all.
	n := r.rowCount()
	r.mustSet(kWait, "30s")
	if got := r.rowCount(); got != n {
		t.Errorf("rows %d -> %d for a default value", n, got)
	}
}

func TestSet_Unchanged_IsNoOp(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPort, "2525", kGreeting, "Hi")
	rev, entries := r.dbRevision(), len(r.rec.Recorded())

	got, err := r.svc.Set(r.actx(), changes(kPort, "2525", kGreeting, "Hi"))
	wantNoErr(t, err)
	if int64(got) != rev {
		t.Errorf("a no-op Set returned revision %d, want the current %d", got, rev)
	}
	if r.dbRevision() != rev {
		t.Error("a no-op Set bumped the revision")
	}
	if len(r.rec.Recorded()) != entries {
		t.Errorf("a no-op Set recorded %d audit entries", len(r.rec.Recorded())-entries)
	}
	// Resetting what is already default is a no-op too.
	_, err = r.svc.Set(r.actx(), resetting(kWait, kHost))
	wantNoErr(t, err)
	if r.dbRevision() != rev || len(r.rec.Recorded()) != entries {
		t.Error("resetting default values wrote something")
	}
	// A whitespace-only difference is no difference: values are trimmed first.
	_, err = r.svc.Set(r.actx(), changes(kGreeting, "  Hi  "))
	wantNoErr(t, err)
	if r.dbRevision() != rev {
		t.Error("a trim-equal value bumped the revision")
	}
}

func TestSet_TrimsValues_ExceptSecrets(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kGreeting, "  padded \n", kPort, " 25 ", kPassword, "  pw with spaces  ")
	ctx := ctxT(t)
	if v := first(r.reader("hello").Text(ctx, "greeting")); v != "padded" {
		t.Errorf("greeting = %q", v)
	}
	if v := first(r.reader("mail").Int(ctx, "port")); v != 25 {
		t.Errorf("port = %d", v)
	}
	if v, err := r.reader("mail").Secret(ctx, "password"); err != nil || v.Value() != "  pw with spaces  " {
		t.Errorf("secret was trimmed or lost: %v", err)
	}
	if v := first(r.stored(kGreeting)); v != "padded" {
		t.Errorf("stored greeting = %q", v)
	}
}

func TestSet_TextAllowsNewlineAndTab_AtTheLimit(t *testing.T) {
	r := newIRig(t)
	ok := "line one\n\tline two"
	r.mustSet(kNote, ok)
	if v := first(r.reader("mail").Text(ctxT(t), "note")); v != ok {
		t.Errorf("note = %q", v)
	}
	r.mustSet(kNote, repeat("a", settings.MaxTextBytes))
}

// ---- Set: audit --------------------------------------------------------------------------

func TestSet_Audit_OneEntryPerKey_NeverTheValue(t *testing.T) {
	r := newIRig(t)
	const marker = "zq-marker-8841"
	r.mustSet(kGreeting, marker, kPort, "2525", kPassword, marker+"-secret")
	es := r.rec.Recorded()
	if len(es) != 3 {
		t.Fatalf("%d entries (%v), want 3 (one per key)", len(es), actionsOf(es))
	}
	for _, key := range []string{kGreeting, kPort, kPassword} {
		var found *audit.Entry
		for i := range es {
			if es[i].Reason == "key:"+key {
				found = &es[i]
			}
		}
		if found == nil {
			t.Errorf("no entry with reason key:%s", key)
			continue
		}
		if found.Action != "settings.setting.changed" {
			t.Errorf("%s: action = %s", key, found.Action)
		}
		if found.Record.Kind != "settings.setting" || found.Record.ID != settings.IDOf(key) {
			t.Errorf("%s: record = %+v, want settings.setting/IDOf", key, found.Record)
		}
		if len(found.Fields) != 1 {
			t.Errorf("%s: fields = %v, want exactly the key", key, found.Fields)
		}
	}
	for _, e := range es {
		if strings.Contains(fmt.Sprintf("%+v", e), marker) {
			t.Errorf("an audit entry carries a value: %+v", e)
		}
	}

	r.mustReset(kGreeting)
	last := r.rec.Recorded()[len(r.rec.Recorded())-1]
	if last.Action != "settings.setting.reset" || last.Reason != "key:"+kGreeting {
		t.Errorf("reset entry = %s / %s", last.Action, last.Reason)
	}
}

func TestSet_AuditFails_RollsBack(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPort, "2525")
	rev, rows := r.dbRevision(), r.rowCount()
	r.rec.Fail(errs.E("test", errs.Unavailable, errors.New("audit store down")))

	_, err := r.svc.Set(r.actx(), changes(kPort, "3000", kGreeting, "Hi"))
	wantKind(t, err, errs.Unavailable)

	if r.dbRevision() != rev || r.rowCount() != rows {
		t.Error("a failed audit record left a write or a revision bump behind")
	}
	if v := first(r.stored(kPort)); v != "2525" {
		t.Errorf("stored port = %q, want 2525", v)
	}
	ctx := ctxT(t)
	if v := first(r.reader("mail").Int(ctx, "port")); v != 2525 {
		t.Errorf("the snapshot took a rolled-back value: %d", v)
	}
	if v := first(r.reader("hello").Text(ctx, "greeting")); v != "Hello, burner" {
		t.Errorf("greeting = %q", v)
	}

	r.rec.Fail(nil)
	r.mustSet(kPort, "3000")
}

// ---- Set: validation (F19-F23) ------------------------------------------------------------------

func TestSet_ValueShape_Table(t *testing.T) {
	r := newIRig(t)
	tests := []struct {
		name  string
		key   string
		value string
		code  string // "" = accepted
	}{
		{"bool true", kEnabled, "true", ""},
		{"bool false", kEnabled, "false", ""},
		{"bool yes", kEnabled, "yes", "malformed"},
		{"bool 1", kEnabled, "1", "malformed"},
		{"bool empty", kEnabled, "", "malformed"},
		{"bool upper case", kEnabled, "TRUE", "malformed"},
		{"int negative", kLogin, "-3", ""},
		{"int word", kLogin, "abc", "malformed"},
		{"int fraction", kLogin, "1.5", "malformed"},
		{"int empty", kLogin, "", "malformed"},
		{"int overflow", kLogin, "9223372036854775808", "malformed"},
		{"int hex", kLogin, "0x10", "malformed"},
		{"duration hours", kWait, "72h", ""},
		{"duration zero", kWait, "0", ""},
		{"duration days", kWait, "3d", "malformed"},
		{"duration word", kWait, "soon", "malformed"},
		{"duration empty", kWait, "", "malformed"},
		{"enum member", kSecurity, "none", ""},
		{"enum other", kSecurity, "carrier-pigeon", "one_of"},
		{"enum case", kSecurity, "TLS", "one_of"},
		{"enum empty", kSecurity, "", "one_of"},
		{"text at the limit", kNote, repeat("a", settings.MaxTextBytes), ""},
		{"text over the limit", kNote, repeat("a", settings.MaxTextBytes+1), "too_long"},
		{"text multi-byte over the limit", kNote, repeat("é", settings.MaxTextBytes/2+1), "too_long"},
		{"text NUL", kNote, "a\x00b", "control_chars"},
		{"text escape", kNote, "a\x1bb", "control_chars"},
		{"text carriage return", kNote, "a\rb", "control_chars"},
		{"text invalid utf-8", kNote, "a\xffb", ""}, // any Invalid; code unspecified
		{"secret over the limit", kPassword, repeat("a", settings.MaxTextBytes+1), "too_long"},
		{"secret control char", kPassword, "pa\x00ss", "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := r.dbRevision()
			_, err := r.svc.Set(r.actx(), changes(tc.key, tc.value))
			switch {
			case tc.name == "text invalid utf-8":
				wantField(t, err, tc.key, "")
			case tc.code == "":
				wantNoErr(t, err)
			default:
				wantField(t, err, tc.key, tc.code)
				if tc.code == "too_long" {
					if got := paramOf(err, tc.key, "too_long", "max"); got != "4096" {
						t.Errorf("max = %q, want 4096", got)
					}
				}
				if r.dbRevision() != before {
					t.Error("a rejected Set bumped the revision")
				}
				if strings.Contains(fmt.Sprint(err), "carrier-pigeon") || strings.Contains(fmt.Sprint(err), "pa\x00ss") {
					t.Error("the error echoes the rejected value")
				}
			}
		})
	}

	t.Run("int max", func(t *testing.T) {
		// both halves of the area rule move together, so the area check passes
		r.mustSet(kLogin, "9223372036854775807", kClient, "9223372036854775807")
		if v := first(r.reader("accounts").Int(ctxT(t), "login_failure_limit")); v != 9223372036854775807 {
			t.Errorf("login = %d", v)
		}
		r.mustReset(kLogin, kClient)
	})

	t.Run("every problem in a batch is collected", func(t *testing.T) {
		_, err := r.svc.Set(r.actx(), changes(kEnabled, "maybe", kLogin, "x", kSecurity, "?", kNote, "ok"))
		for _, key := range []string{kEnabled, kLogin, kSecurity} {
			if !hasField(err, key, "") {
				t.Errorf("no field error for %s in %v", key, describe(err))
			}
		}
		if hasField(err, kNote, "") {
			t.Error("the valid key was reported")
		}
	})
}

func TestSet_AllOrNothing(t *testing.T) {
	r := newIRig(t)
	rev, rows, entries := r.dbRevision(), r.rowCount(), len(r.rec.Recorded())
	_, err := r.svc.Set(r.actx(), changes(kGreeting, "fine", kPort, "not-a-number"))
	wantField(t, err, kPort, "malformed")
	if r.dbRevision() != rev || r.rowCount() != rows || len(r.rec.Recorded()) != entries {
		t.Error("a rejected batch wrote something")
	}
	if v := first(r.reader("hello").Text(ctxT(t), "greeting")); v != "Hello, burner" {
		t.Errorf("the valid half of a rejected batch took effect: %q", v)
	}
}

func TestSet_CheckRejects_ReturnsInvalid(t *testing.T) {
	r := newIRig(t)
	t.Run("a FieldError keeps its code under the setting key", func(t *testing.T) {
		for _, v := range []string{"0", "70000", "-1"} {
			_, err := r.svc.Set(r.actx(), changes(kPort, v))
			wantField(t, err, kPort, "range")
		}
		r.mustSet(kPort, "465") // inside the range
	})
	t.Run("a plain error is code rejected", func(t *testing.T) {
		_, err := r.svc.Set(r.actx(), changes(kHost, "forbidden.example.org"))
		wantField(t, err, kHost, "rejected")
		for _, fe := range validate.Fields(err) {
			if fe.Field == kHost && fe.Code == "rejected" && !strings.Contains(fe.Reason, "not allowed") {
				t.Errorf("reason = %q, want the check's text", fe.Reason)
			}
		}
	})
	t.Run("the check does not run on a reset", func(_ *testing.T) {
		r.mustReset(kPort)
	})
}

func TestSet_AreaCheckRejects_ReturnsInvalid(t *testing.T) {
	r := newIRig(t)
	t.Run("batch against defaults", func(t *testing.T) {
		_, err := r.svc.Set(r.actx(), changes(kClient, "3")) // login default is 5
		wantField(t, err, kClient, "range")
	})
	t.Run("a change that breaks the stored other half", func(t *testing.T) {
		_, err := r.svc.Set(r.actx(), changes(kLogin, "30")) // client default is 20
		wantField(t, err, "accounts.client_failure_limit", "range")
	})
	t.Run("the merged batch is what is checked", func(t *testing.T) {
		r.mustSet(kLogin, "30", kClient, "40")
		if v := first(r.reader("accounts").Int(ctxT(t), "login_failure_limit")); v != 30 {
			t.Errorf("login = %d", v)
		}
	})
	t.Run("stored values count", func(t *testing.T) {
		_, err := r.svc.Set(r.actx(), changes(kClient, "10")) // login is stored as 30
		wantField(t, err, kClient, "range")
	})
	t.Run("a reset is checked against the rest", func(t *testing.T) {
		_, err := r.svc.Set(r.actx(), resetting(kClient)) // back to 20, below the stored 30
		wantField(t, err, kClient, "range")
	})
	t.Run("nothing was written", func(t *testing.T) {
		if v := first(r.reader("accounts").Int(ctxT(t), "client_failure_limit")); v != 40 {
			t.Errorf("client = %d, want 40", v)
		}
	})
}

func TestSet_Regional_Table(t *testing.T) {
	r := newIRig(t)
	long := func(n int) string { return repeat("a", n) }
	tests := []struct {
		name  string
		key   string
		value string
		code  string // "" accepted
	}{
		{"name ok", "regional.name", "Camp Quill", ""},
		{"name at 80 bytes", "regional.name", long(settings.MaxNameBytes), ""},
		{"name empty", "regional.name", "", "required"},
		{"name spaces", "regional.name", "    ", "required"},
		{"name 81 bytes", "regional.name", long(settings.MaxNameBytes + 1), "too_long"},
		{"name 82 bytes of 2-byte runes", "regional.name", repeat("é", 41), "too_long"},
		{"name NUL", "regional.name", "Camp\x00", "control_chars"},
		{"name newline", "regional.name", "Camp\nQuill", "control_chars"},

		{"email empty", "regional.contact_email", "", ""},
		{"email ok", "regional.contact_email", "hello@example.org", ""},
		{"email no at", "regional.contact_email", "not-an-address", "malformed"},
		{"email two ats", "regional.contact_email", "a@@example.org", "malformed"},
		{"email with a name", "regional.contact_email", "Wren <wren@example.org>", "malformed"},

		{"url empty", "regional.public_url", "", ""},
		{"url https", "regional.public_url", "https://burn.example.org", ""},
		{"url http", "regional.public_url", "http://burn.example.org:8080", ""},
		{"url relative", "regional.public_url", "/burn", "url"},
		{"url no scheme", "regional.public_url", "burn.example.org", "url"},
		{"url ftp", "regional.public_url", "ftp://burn.example.org", "url"},
		{"url javascript", "regional.public_url", "javascript:alert(1)", "url"},
		{"url no host", "regional.public_url", "https://", "url"},
		{"url userinfo", "regional.public_url", "https://wren:pw@burn.example.org", "url"},
		{"url user only", "regional.public_url", "https://wren@burn.example.org", "url"},
		{"url query", "regional.public_url", "https://burn.example.org/?a=1", "url"},
		{"url fragment", "regional.public_url", "https://burn.example.org/#top", "url"},
		{"url 257 bytes", "regional.public_url", "https://burn.example.org/" + long(settings.MaxURLBytes-len("https://burn.example.org/")+1), "too_long"},
		{"url 256 bytes", "regional.public_url", "https://burn.example.org/" + long(settings.MaxURLBytes-len("https://burn.example.org/")), ""},

		{"language en", "regional.language", "en", ""},
		{"language es", "regional.language", "es", ""},
		{"language fr", "regional.language", "fr", "one_of"},
		{"language empty", "regional.language", "", "one_of"},

		{"zone empty", "regional.timezone", "", ""},
		{"zone ok", "regional.timezone", "America/Denver", ""},
		{"zone UTC", "regional.timezone", "UTC", ""},
		{"zone unknown", "regional.timezone", "Mars/Olympus_Mons", "unknown"},
		{"zone Local", "regional.timezone", "Local", "unknown"},
		{"zone 65 bytes", "regional.timezone", long(65), "too_long"},

		{"color empty", "regional.color_primary", "", ""},
		{"color passes on white", "regional.color_primary", "#9a3412", ""},
		{"color word", "regional.color_primary", "red", "malformed"},
		{"color short hex", "regional.color_primary", "#fff", "malformed"},
		{"color not hex", "regional.color_primary", "#gggggg", "malformed"},
		{"color no hash", "regional.color_primary", "9a3412", "malformed"},
		{"color 8 digits", "regional.color_primary", "#9a3412ff", "malformed"},
		{"color fails on white", "regional.color_primary", "#f97316", "contrast"},
		{"color white on white", "regional.color_primary", "#ffffff", "contrast"},
		{"dark empty", "regional.color_primary_dark", "", ""},
		{"dark passes on near-black", "regional.color_primary_dark", "#f97316", ""},
		{"dark fails on near-black", "regional.color_primary_dark", "#9a3412", "contrast"},
		{"dark equal to the background", "regional.color_primary_dark", "#121212", "contrast"},
		{"dark malformed", "regional.color_primary_dark", "orange", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.svc.Set(r.actx(), changes(tc.key, tc.value))
			if tc.code == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.key, tc.code)
			if tc.code == "contrast" {
				if lo := paramOf(err, tc.key, "contrast", "min"); lo != "4.5" {
					t.Errorf("min = %q, want 4.5", lo)
				}
				if ratio := paramOf(err, tc.key, "contrast", "ratio"); ratio == "" {
					t.Error("no ratio parameter")
				}
			}
			if strings.Contains(fmt.Sprint(err), "wren:pw") {
				t.Error("the error echoes the URL's userinfo")
			}
		})
	}
}

func TestSet_Regional_ContrastKnownPairs(t *testing.T) {
	r := newIRig(t)
	// #9a3412 on white is about 7.3:1; #f97316 on white is about 2.8:1.
	_, err := r.svc.Set(r.actx(), changes("regional.color_primary", "#f97316"))
	ratio := paramOf(err, "regional.color_primary", "contrast", "ratio")
	if !strings.HasPrefix(ratio, "2.") && !strings.HasPrefix(ratio, "3.") {
		t.Errorf("ratio for #f97316 on white = %q, want about 2.8", ratio)
	}
}

func TestSet_Regional_PublicURLTrailingSlashRemoved(t *testing.T) {
	r := newIRig(t)
	r.mustSet("regional.public_url", "https://burn.example.org/")
	got, err := r.svc.Regional(ctxT(t))
	wantNoErr(t, err)
	if got.PublicURL != "https://burn.example.org" {
		t.Errorf("PublicURL = %q", got.PublicURL)
	}
	r.mustSet("regional.public_url", "http://example.org/burn/")
	got = first(r.svc.Regional(ctxT(t)))
	if got.PublicURL != "http://example.org/burn" {
		t.Errorf("PublicURL = %q", got.PublicURL)
	}
	// Saving the same URL with a slash again is a no-op.
	rev := r.dbRevision()
	r.mustSet("regional.public_url", "http://example.org/burn")
	if r.dbRevision() != rev {
		t.Error("a normalised-equal URL bumped the revision")
	}
}

func TestRegional_DefaultsAndOverrides(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	got, err := r.svc.Regional(ctx)
	wantNoErr(t, err)
	if got.Name != "BMS" || got.Language != "en" || got.ContactEmail != "" || got.PublicURL != "" || got.Timezone != "" {
		t.Errorf("defaults = %+v", got)
	}
	if !slices.Equal(got.Languages, settings.Languages) {
		t.Errorf("Languages = %v", got.Languages)
	}
	got.Languages[0] = "zz" // a copy: the next call is unaffected
	again := first(r.svc.Regional(ctx))
	if again.Languages[0] != "en" {
		t.Error("Regional shares its Languages slice with the caller")
	}

	r.mustSet("regional.name", "Camp Quill", "regional.contact_email", "hello@example.org",
		"regional.language", "es", "regional.timezone", "America/Denver")
	got = first(r.svc.Regional(ctx))
	if got.Name != "Camp Quill" || got.ContactEmail != "hello@example.org" || got.Language != "es" || got.Timezone != "America/Denver" {
		t.Errorf("Regional = %+v", got)
	}
}

// ---- Set: store failures (F24, F26, F27) -----------------------------------------------------

func TestSet_ReadOnly_ReturnsUnavailable(t *testing.T) {
	base := newIRig(t)
	base.mustSet(kPort, "2525")
	f := &faultDB{DB: base.pool}
	r := newIRigOn(t, f, base.pool)
	ctx := ctxT(t)
	// Warm the snapshot, then freeze writes.
	if v, err := r.reader("mail").Int(ctx, "port"); err != nil || v != 2525 {
		t.Fatalf("port = %v, %v", v, err)
	}
	f.set(nil, nil, true)
	rev, rows := base.dbRevision(), base.rowCount()

	_, err := r.svc.Set(r.actx(), changes(kPort, "3000"))
	wantReadOnly(t, err)
	_, err = r.svc.Set(r.actx(), resetting(kPort))
	wantReadOnly(t, err)
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"event": {Singular: str("gathering")}}))
	wantReadOnly(t, err)

	if base.dbRevision() != rev || base.rowCount() != rows {
		t.Error("a write on a read-only copy changed something")
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("a refused write left audit entries")
	}
	// Reads keep working: the snapshot, the public document and the admin document.
	if v, err := r.reader("mail").Int(ctx, "port"); err != nil || v != 2525 {
		t.Errorf("read on a read-only copy: %v, %v", v, err)
	}
	if _, err := r.svc.Public(ctx); err != nil {
		t.Errorf("Public on a read-only copy: %v", err)
	}
	if _, err := r.svc.Document(r.actx()); err != nil {
		t.Errorf("Document on a read-only copy: %v", err)
	}
}

func TestSet_NoRevisionRow_ReturnsInternal(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPort, "2525")
	rows := r.rowCount()
	r.exec("DELETE FROM core.setting_revision")
	_, err := r.svc.Set(r.actx(), changes(kGreeting, "Hi"))
	wantKind(t, err, errs.Internal)
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"event": {Singular: str("gathering")}}))
	wantKind(t, err, errs.Internal)
	if r.rowCount() != rows {
		t.Error("the failed Set left a row behind")
	}
	if len(r.rec.Recorded()) != 1 { // only the first Set's entry
		t.Errorf("audit entries = %v", actionsOf(r.rec.Recorded()))
	}
	if v := first(r.reader("hello").Text(ctxT(t), "greeting")); v != "Hello, burner" {
		t.Errorf("greeting = %q after a failed Set", v)
	}
}

func TestSet_StoreFails_ReturnsKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("down")), errs.Unavailable},
		{"timeout", errs.E("test", errs.Timeout, errors.New("slow")), errs.Timeout},
		{"canceled", errs.E("test", errs.Canceled, context.Canceled), errs.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := newIRig(t)
			f := &faultDB{DB: base.pool}
			r := newIRigOn(t, f, base.pool)
			r.mustSet(kPort, "2525")
			f.set(nil, tc.err, false)
			_, err := r.svc.Set(r.actx(), changes(kPort, "3000"))
			wantKind(t, err, tc.kind)
			_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"event": {Singular: str("gathering")}}))
			wantKind(t, err, tc.kind)
			if v := first(r.reader("mail").Int(ctxT(t), "port")); v != 2525 {
				t.Errorf("port = %d after a failed Set", v)
			}
			f.set(nil, nil, false)
			r.mustSet(kPort, "3000") // recovers
		})
	}
}

func TestSet_CancelledContext_ReturnsCanceled(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPort, "2525")
	ctx, cancel := context.WithCancel(r.actx())
	cancel()
	_, err := r.svc.Set(ctx, changes(kPort, "3000"))
	wantKind(t, err, errs.Canceled)
	if v := first(r.stored(kPort)); v != "2525" {
		t.Errorf("stored = %q", v)
	}
}

func TestReader_FirstLoad_CancelledAndRecovers(t *testing.T) {
	base := newIRig(t)
	base.mustSet(kPort, "2525")
	f := &faultDB{DB: base.pool}
	r := newIRigOn(t, f, base.pool)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := r.reader("mail").Int(ctx, "port")
	wantKind(t, err, errs.Canceled)

	f.set(unavailable(), nil, false)
	_, err = r.reader("mail").Int(ctxT(t), "port")
	wantKind(t, err, errs.Unavailable)

	// A failed first load is not cached: the next read after recovery loads.
	f.set(nil, nil, false)
	if v, err := r.reader("mail").Int(ctxT(t), "port"); err != nil || v != 2525 {
		t.Errorf("after recovery: %v, %v", v, err)
	}
	// Once loaded, a database outage does not fail reads (a read is a map lookup).
	f.set(unavailable(), unavailable(), false)
	if v, err := r.reader("mail").Int(ctxT(t), "port"); err != nil || v != 2525 {
		t.Errorf("after the first load: %v, %v", v, err)
	}
}

// ---- snapshot ---------------------------------------------------------------------------------

func TestSnapshot_LoadsOnce_ReadsDoNoIO(t *testing.T) {
	base := newIRig(t)
	f := &faultDB{DB: base.pool}
	r := newIRigOn(t, f, base.pool)
	ctx := ctxT(t)
	if f.transactions() != 0 {
		t.Fatal("New loaded")
	}
	_, err := r.reader("mail").Int(ctx, "port")
	wantNoErr(t, err)
	loaded := f.transactions()
	if loaded != 1 {
		t.Errorf("first read used %d transactions, want 1", loaded)
	}
	for range 50 {
		ignore(r.reader("mail").Int(ctx, "port"))
		ignore(r.reader("hello").Text(ctx, "greeting"))
		ignore(r.svc.Regional(ctx))
		ignore(r.svc.TermsFor(ctx, "en"))
		ignore(r.svc.Public(ctx))
		ignore(r.svc.ThemeCSS(ctx))
		ignore(r.reader("mail").Secret(ctx, "password"))
	}
	if got := f.transactions(); got != loaded {
		t.Errorf("reads opened %d more transactions", got-loaded)
	}
}

func TestSnapshot_ConcurrentFirstReads_LoadOnce(t *testing.T) {
	base := newIRig(t)
	base.mustSet(kPort, "2525")
	f := &faultDB{DB: base.pool}
	r := newIRigOn(t, f, base.pool)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, err := r.reader("mail").Int(ctxT(t), "port")
			if err != nil || v != 2525 {
				t.Errorf("port = %v, %v", v, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := f.transactions(); got != 1 {
		t.Errorf("%d loads for concurrent first reads, want 1 (single-flight)", got)
	}
}

func TestSnapshot_ConcurrentSets_LastCommitWins(t *testing.T) {
	r := newIRig(t)
	start := r.dbRevision()
	const n = 20
	revs := make([]settings.Revision, n)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			rev, err := r.svc.Set(r.actx(), changes(kGreeting, fmt.Sprintf("g%02d", i)))
			if err != nil {
				t.Errorf("Set %d: %v", i, err)
			}
			revs[i] = rev
		}()
	}
	close(gate)
	wg.Wait()

	seen := map[settings.Revision]bool{}
	for _, rev := range revs {
		if seen[rev] {
			t.Errorf("revision %d returned twice", rev)
		}
		seen[rev] = true
	}
	if got := r.dbRevision(); got != start+n {
		t.Errorf("revision = %d, want %d", got, start+n)
	}
	if got := len(r.rec.Recorded()); got != n {
		t.Errorf("%d audit entries, want %d", got, n)
	}
	// Two writers in one process cannot install out of order: the snapshot holds what the
	// database holds.
	stored := first(r.stored(kGreeting))
	if v := first(r.reader("hello").Text(ctxT(t), "greeting")); v != stored {
		t.Errorf("snapshot says %q, database says %q", v, stored)
	}
	if doc := r.doc(); int64(doc.Revision) != start+n {
		t.Errorf("Document revision = %d", doc.Revision)
	}
}

func TestReader_StaleValue_ReturnsDefaultAndWarnsOnce(t *testing.T) {
	base := newIRig(t)
	base.exec("INSERT INTO core.setting (key, value, updated_at) VALUES ('mail.port', 'not-a-number', now())")
	base.exec("INSERT INTO core.setting (key, value, updated_at) VALUES ('mail.security', 'carrier-pigeon', now())")
	base.exec("INSERT INTO core.setting (key, value, updated_at) VALUES ('mail.wait', '3d', now())")
	r := newIRigOn(t, base.pool, base.pool)
	ctx := ctxT(t)
	for range 5 {
		if v, err := r.reader("mail").Int(ctx, "port"); err != nil || v != 587 {
			t.Fatalf("stale int = %v, %v; want the default and no error", v, err)
		}
	}
	if v, err := r.reader("mail").Text(ctx, "security"); err != nil || v != "starttls" {
		t.Errorf("stale enum = %q, %v", v, err)
	}
	if v, err := r.reader("mail").Duration(ctx, "wait"); err != nil || v != 30*time.Second {
		t.Errorf("stale duration = %v, %v", v, err)
	}
	for _, key := range []string{"mail.port", "mail.security", "mail.wait"} {
		if n := len(warnings(r.rig, key)); n != 1 {
			t.Errorf("%s: %d warnings, want exactly 1 per key per process", key, n)
		}
	}
	if countMeasures(r.cap.Measurements(), "bms.settings.stale_values", "", "") == 0 {
		t.Error("bms.settings.stale_values was not counted")
	}
	s, ok := settingOf(r.doc(), kPort)
	if !ok || !s.Stale {
		t.Errorf("Document does not flag the stale value: %+v", s)
	}
	if s.Value == nil {
		t.Log("Document shows no stored value for a stale key (acceptable: the default applies)")
	}
	// An admin can repair it.
	r.mustSet(kPort, "2525")
	if v := first(r.reader("mail").Int(ctx, "port")); v != 2525 {
		t.Errorf("port = %d after repair", v)
	}
	if s := first(settingOf(r.doc(), kPort)); s.Stale {
		t.Error("still stale after repair")
	}
}

func TestUndeclaredRows_SurviveAndStayInvisible(t *testing.T) {
	base := newIRig(t)
	base.exec("INSERT INTO core.setting (key, value, updated_at) VALUES ('gone.module_key', 'kept', now())")
	r := newIRigOn(t, base.pool, base.pool)
	r.mustSet(kPort, "2525")
	r.mustReset(kPort)
	r.mustSet(kGreeting, "Hi")
	if v, ok := r.stored("gone.module_key"); !ok || v != "kept" {
		t.Errorf("the undeclared row = %q, %v; want it kept", v, ok)
	}
	for _, a := range r.doc().Areas {
		for _, s := range a.Settings {
			if s.Key == "gone.module_key" {
				t.Error("an undeclared row is shown in the Document")
			}
		}
	}
	_, err := r.svc.Set(r.actx(), changes("gone.module_key", "x"))
	wantField(t, err, "gone.module_key", "unknown")
	// The module returns: a service that declares the area reads the kept value.
	back := newIRigOn(t, base.pool, base.pool, func(o *settings.Options) {
		o.Areas = append(o.Areas, settings.Area{Name: "gone", Decls: []settings.Decl{{Key: "module_key", Kind: settings.Text}}})
	})
	if v, err := back.reader("gone").Text(ctxT(t), "module_key"); err != nil || v != "kept" {
		t.Errorf("returned module reads %q, %v", v, err)
	}
}

// ---- terminology ---------------------------------------------------------------------------------

var teamForms = settings.FormChanges{
	Singular: str("team"), Plural: str("teams"), SingularTitle: str("Team"), PluralTitle: str("Teams"),
}

func TestTermsFor_Defaults(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	en, err := r.svc.TermsFor(ctx, "en")
	wantNoErr(t, err)
	for _, term := range settings.CoreTerms {
		f, ok := en[term]
		if !ok {
			t.Errorf("no defaults for %s", term)
			continue
		}
		if f.Singular == "" || f.Plural == "" || f.SingularTitle == "" || f.PluralTitle == "" {
			t.Errorf("%s has an empty form: %+v", term, f)
		}
	}
	if len(en) != len(settings.CoreTerms) {
		t.Errorf("%d terms, want %d", len(en), len(settings.CoreTerms))
	}
	if got := en["department"]; got != (settings.Forms{Singular: "department", Plural: "departments", SingularTitle: "Department", PluralTitle: "Departments"}) {
		t.Errorf("department defaults = %+v", got)
	}
	if got := en["event"]; got != (settings.Forms{Singular: "event", Plural: "events", SingularTitle: "Event", PluralTitle: "Events"}) {
		t.Errorf("event defaults = %+v", got)
	}
	// A language without a defaults file uses en's (M4 ships en only).
	es, err := r.svc.TermsFor(ctx, "es")
	wantNoErr(t, err)
	if es["department"] != en["department"] {
		t.Errorf("es = %+v, want en's while there is no es file", es["department"])
	}
}

func TestSetTerms_OverridesApplyPerLanguage(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	rev := r.dbRevision()
	got, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": teamForms}))
	wantNoErr(t, err)
	if int64(got) != rev+1 {
		t.Errorf("revision = %d, want %d", got, rev+1)
	}
	en := first(r.svc.TermsFor(ctx, "en"))
	if en["department"] != (settings.Forms{Singular: "team", Plural: "teams", SingularTitle: "Team", PluralTitle: "Teams"}) {
		t.Errorf("en department = %+v", en["department"])
	}
	if en["event"].Singular != "event" {
		t.Errorf("an untouched term changed: %+v", en["event"])
	}
	es := first(r.svc.TermsFor(ctx, "es"))
	if es["department"].Singular == "team" {
		t.Error("an en override leaked into es")
	}

	pub, err := r.svc.Public(ctx)
	wantNoErr(t, err)
	flat := pub.Terms["en"]
	for k, want := range map[string]string{
		"term_department": "team", "term_department_plural": "teams",
		"term_department_title": "Team", "term_department_plural_title": "Teams",
		"term_event": "event", "term_event_plural": "events", "term_event_title": "Event", "term_event_plural_title": "Events",
	} {
		if flat[k] != want {
			t.Errorf("Public.Terms[en][%s] = %q, want %q", k, flat[k], want)
		}
	}
	for _, lang := range settings.Languages {
		for _, term := range settings.CoreTerms {
			for _, suffix := range []string{"", "_plural", "_title", "_plural_title"} {
				if pub.Terms[lang]["term_"+term+suffix] == "" {
					t.Errorf("Public.Terms[%s][term_%s%s] is empty", lang, term, suffix)
				}
			}
		}
	}

	_, err = r.svc.SetTerms(r.actx(), "es", termsOf(map[string]settings.FormChanges{"department": {Singular: str("equipo")}}))
	wantNoErr(t, err)
	en2 := first(r.svc.TermsFor(ctx, "en"))
	if en2["department"].Singular != "team" {
		t.Error("an es override changed en")
	}
	es2 := first(r.svc.TermsFor(ctx, "es"))
	if es2["department"].Singular != "equipo" || es2["department"].Plural == "" {
		t.Errorf("es department = %+v", es2["department"])
	}
}

func TestSetTerms_PartialChange_KeepsTheOtherForms(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": teamForms}))
	wantNoErr(t, err)
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": {Plural: str("crews")}}))
	wantNoErr(t, err)
	got := first(r.svc.TermsFor(ctxT(t), "en"))
	want := settings.Forms{Singular: "team", Plural: "crews", SingularTitle: "Team", PluralTitle: "Teams"}
	if got["department"] != want {
		t.Errorf("department = %+v, want %+v (a nil field is unchanged)", got["department"], want)
	}
}

func TestSetTerms_Reset_RestoresDefaults(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": teamForms}))
	wantNoErr(t, err)
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": {Reset: []string{"singular", "singular_title"}}}))
	wantNoErr(t, err)
	got := first(r.svc.TermsFor(ctxT(t), "en"))
	want := settings.Forms{Singular: "department", Plural: "teams", SingularTitle: "Department", PluralTitle: "Teams"}
	if got["department"] != want {
		t.Errorf("department = %+v, want %+v", got["department"], want)
	}
	doc, err := r.svc.Terms(r.actx())
	wantNoErr(t, err)
	o := doc.Terms["en"]["department"].Override
	if o.Singular != nil || o.SingularTitle != nil || o.Plural == nil || *o.Plural != "teams" || o.PluralTitle == nil {
		t.Errorf("override = %+v after a partial reset", o)
	}
	var n int
	wantNoErr(t, r.pool.QueryRow(ctxT(t), "SELECT count(*) FROM core.terminology WHERE lang = 'en' AND term = 'department'").Scan(&n))
	if n != 2 {
		t.Errorf("%d override rows, want 2 (a reset deletes the row)", n)
	}
}

func TestSetTerms_TrimsAndNoOps(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"event": {Singular: str("  gathering  ")}}))
	wantNoErr(t, err)
	got := first(r.svc.TermsFor(ctxT(t), "en"))
	if got["event"].Singular != "gathering" {
		t.Errorf("singular = %q, want trimmed", got["event"].Singular)
	}
	rev, entries := r.dbRevision(), len(r.rec.Recorded())
	for name, c := range map[string]settings.FormChanges{
		"same value":             {Singular: str("gathering")},
		"default as override":    {Plural: str("events")},
		"reset of an unset form": {Reset: []string{"plural_title"}},
		"nothing but nil fields": {},
	} {
		got, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"event": c}))
		wantNoErr(t, err)
		if int64(got) != rev || r.dbRevision() != rev {
			t.Errorf("%s: revision moved (%d)", name, got)
		}
		if len(r.rec.Recorded()) != entries {
			t.Errorf("%s: recorded %d entries", name, len(r.rec.Recorded())-entries)
		}
	}
}

func TestSetTerms_Audit(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": {Singular: str("team"), Plural: str("teams")}}))
	wantNoErr(t, err)
	es := entriesFor(r.rec.Recorded(), "settings.term.changed")
	if len(es) != 1 {
		t.Fatalf("%d changed entries (%v), want 1 per term", len(es), actionsOf(r.rec.Recorded()))
	}
	e := es[0]
	if e.Reason != "term:en:department" || e.Record.Kind != "settings.term" || e.Record.ID != settings.IDOf("en:department") {
		t.Errorf("entry = %+v", e)
	}
	for _, f := range []string{"singular", "plural"} {
		if !slices.Contains(e.Fields, f) {
			t.Errorf("fields %v lack %s", e.Fields, f)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", e), "teams") {
		t.Error("the entry carries a value")
	}

	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": {Reset: []string{"plural"}}}))
	wantNoErr(t, err)
	rs := entriesFor(r.rec.Recorded(), "settings.term.reset")
	if len(rs) != 1 || rs[0].Reason != "term:en:department" || !slices.Contains(rs[0].Fields, "plural") {
		t.Errorf("reset entries = %+v", rs)
	}
}

func TestSetTerms_AuditFails_RollsBack(t *testing.T) {
	r := newIRig(t)
	rev := r.dbRevision()
	r.rec.Fail(errs.E("test", errs.Unavailable, errors.New("audit down")))
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": teamForms}))
	wantKind(t, err, errs.Unavailable)
	var n int
	wantNoErr(t, r.pool.QueryRow(ctxT(t), "SELECT count(*) FROM core.terminology").Scan(&n))
	if n != 0 || r.dbRevision() != rev {
		t.Error("a failed audit record left terms or a revision bump behind")
	}
	got := first(r.svc.TermsFor(ctxT(t), "en"))
	if got["department"].Singular != "department" {
		t.Error("the snapshot took a rolled-back term")
	}
}

func TestTerms_Document(t *testing.T) {
	r := newIRig(t)
	doc, err := r.svc.Terms(r.actx())
	wantNoErr(t, err)
	if !slices.Equal(doc.Languages, settings.Languages) {
		t.Errorf("Languages = %v", doc.Languages)
	}
	for _, lang := range settings.Languages {
		for _, term := range settings.CoreTerms {
			td, ok := doc.Terms[lang][term]
			if !ok {
				t.Errorf("no %s/%s", lang, term)
				continue
			}
			if td.Default.Singular == "" || td.Default.PluralTitle == "" {
				t.Errorf("%s/%s default = %+v", lang, term, td.Default)
			}
			if td.Override.Singular != nil || td.Override.Plural != nil || td.Override.SingularTitle != nil || td.Override.PluralTitle != nil {
				t.Errorf("%s/%s has an override on a fresh database", lang, term)
			}
		}
	}
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"department": teamForms}))
	wantNoErr(t, err)
	doc = first(r.svc.Terms(r.actx()))
	td := doc.Terms["en"]["department"]
	if td.Default.Singular != "department" {
		t.Errorf("the default changed: %+v", td.Default)
	}
	if td.Override.Singular == nil || *td.Override.Singular != "team" || td.Override.PluralTitle == nil || *td.Override.PluralTitle != "Teams" {
		t.Errorf("override = %+v", td.Override)
	}
}

// ---- permissions against the real store -------------------------------------------------------

func TestAdminUseCases_PermissionMatrix(t *testing.T) {
	r := newIRig(t)
	reader := r.person(settings.PermissionRead)
	manager := r.person(settings.PermissionManage)
	both := r.person(settings.PermissionRead, settings.PermissionManage)
	su := newPerson()
	r.az.Superuser(su)
	anyone := newPerson()

	type who struct {
		name string
		ctx  context.Context
		read bool
		wrt  bool
	}
	people := []who{
		{"anyone", asPerson(ctxT(t), anyone), false, false},
		{"reader", asPerson(ctxT(t), reader), true, false},
		{"manager", asPerson(ctxT(t), manager), false, true},
		{"both", asPerson(ctxT(t), both), true, true},
		{"superuser", asSuper(ctxT(t), su), true, true},
	}
	check := func(t *testing.T, err error, allowed bool) {
		t.Helper()
		if allowed {
			wantNoErr(t, err)
		} else {
			wantKind(t, err, errs.Permission)
		}
	}
	for _, p := range people {
		t.Run(p.name, func(t *testing.T) {
			_, err := r.svc.Document(p.ctx)
			check(t, err, p.read)
			_, err = r.svc.Terms(p.ctx)
			check(t, err, p.read)
			_, err = r.svc.Set(p.ctx, changes(kNote, "by "+p.name))
			check(t, err, p.wrt)
			_, err = r.svc.SetTerms(p.ctx, "en", termsOf(map[string]settings.FormChanges{"event": {Singular: str("do-" + p.name)}}))
			check(t, err, p.wrt)
		})
	}
	// Public reads and module readers need no permission and no session.
	if _, err := r.svc.Public(t.Context()); err != nil {
		t.Errorf("Public: %v", err)
	}
	if _, err := r.svc.ThemeCSS(t.Context()); err != nil {
		t.Errorf("ThemeCSS: %v", err)
	}
	if _, err := r.svc.Regional(t.Context()); err != nil {
		t.Errorf("Regional: %v", err)
	}
	if _, err := r.reader("mail").Int(t.Context(), "port"); err != nil {
		t.Errorf("Reader.Int without a session: %v", err)
	}
	// A denied write leaves no trace.
	_, err := r.svc.Set(asPerson(ctxT(t), anyone), changes(kNote, "denied"))
	wantKind(t, err, errs.Permission)
	if v := first(r.reader("mail").Text(t.Context(), "note")); v == "denied" {
		t.Error("a denied Set took effect")
	}
}

func TestDocument_NeverShowsSecretValues(t *testing.T) {
	r := newIRig(t)
	r.mustSet(kPassword, "invented-pass-9", kToken, "invented-token-9")
	doc := r.doc()
	for _, key := range []string{kPassword, kToken} {
		s := first(settingOf(doc, key))
		if !s.SecretSet {
			t.Errorf("%s: secret_set = false", key)
		}
		if s.Value != nil || s.Default != "" {
			t.Errorf("%s: value=%v default=%q; a secret shows neither", key, s.Value, s.Default)
		}
		if strings.Contains(fmt.Sprintf("%+v", s), "invented-") {
			t.Errorf("%s: SettingDoc carries the secret: %+v", key, s)
		}
	}
	r.mustReset(kToken)
	if s := first(settingOf(r.doc(), kToken)); s.SecretSet {
		t.Error("secret_set still true after clearing")
	}
	if v, err := r.reader("mail").Secret(ctxT(t), "token"); err != nil || v.IsSet() {
		t.Errorf("cleared secret = %v, %v", v.IsSet(), err)
	}
}
