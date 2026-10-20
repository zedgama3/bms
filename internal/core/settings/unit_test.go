package settings_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// unit is a rig over a dbtest.Fake: it proves what happens before the store is reached.
type unit struct {
	*rig
	fake *dbtest.Fake
}

func newUnit(t *testing.T, mods ...func(*settings.Options)) *unit {
	t.Helper()
	f := dbtest.NewFake()
	return &unit{rig: newRig(t, f, mods...), fake: f}
}

func (u *unit) noStatements(t *testing.T, what string) {
	t.Helper()
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for %s", n, what)
	}
	if len(u.rec.Recorded()) != 0 {
		t.Errorf("audit entries %v for %s", actionsOf(u.rec.Recorded()), what)
	}
}

// ---- constants (TestConstants_Pinned) ----------------------------------------------------

func TestConstants_Pinned(t *testing.T) {
	if settings.MaxBatch != 200 || settings.MaxTextBytes != 4096 || settings.MaxNameBytes != 80 ||
		settings.MaxURLBytes != 256 || settings.MaxTermBytes != 60 {
		t.Errorf("bounds = %d %d %d %d %d", settings.MaxBatch, settings.MaxTextBytes, settings.MaxNameBytes, settings.MaxURLBytes, settings.MaxTermBytes)
	}
	if settings.MinContrast != 4.5 {
		t.Errorf("MinContrast = %v", settings.MinContrast)
	}
	if settings.DefaultRefreshInterval != 30*time.Second {
		t.Errorf("DefaultRefreshInterval = %v", settings.DefaultRefreshInterval)
	}
	if string(settings.PermissionRead) != "settings.read" || string(settings.PermissionManage) != "settings.manage" {
		t.Errorf("permissions = %q, %q", settings.PermissionRead, settings.PermissionManage)
	}
	if !slices.Equal(settings.Languages, []string{"en", "es"}) {
		t.Errorf("Languages = %v", settings.Languages)
	}
	if !slices.Equal(settings.CoreTerms, []string{"event", "department", "participant", "theme_camp"}) {
		t.Errorf("CoreTerms = %v", settings.CoreTerms)
	}
	// Kind values are part of the stored contract: zero is invalid, Bool is 1.
	if settings.Bool != 1 || settings.Int != 2 || settings.Duration != 3 || settings.Text != 4 || settings.Enum != 5 || settings.Secret != 6 {
		t.Errorf("kind values = %d %d %d %d %d %d", settings.Bool, settings.Int, settings.Duration, settings.Text, settings.Enum, settings.Secret)
	}
}

func TestKind_String(t *testing.T) {
	for kind, want := range map[settings.Kind]string{
		settings.Bool: "bool", settings.Int: "int", settings.Duration: "duration",
		settings.Text: "text", settings.Enum: "enum", settings.Secret: "secret",
		settings.Kind(0): "Kind(0)", settings.Kind(99): "Kind(99)",
	} {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", uint8(kind), got, want)
		}
	}
}

func TestIDOf_IsFirst16BytesOfSha256(t *testing.T) {
	for _, key := range []string{"regional.name", "mail.password", "en:department"} {
		sum := sha256.Sum256([]byte("settings.setting:" + key))
		if got := settings.IDOf(key); got != [16]byte(sum[:16]) {
			t.Errorf("IDOf(%q) = %x, want %x", key, got, sum[:16])
		}
	}
	if settings.IDOf("a.b") == settings.IDOf("a.c") {
		t.Error("different keys share a reference")
	}
}

// ---- Decl and Area validation (F3, F5) ---------------------------------------------------

func TestDeclValidate_Table(t *testing.T) {
	opts := []string{"a", "b"}
	tests := []struct {
		name  string
		decl  settings.Decl
		field string // "" = valid
	}{
		{"bool ok", settings.Decl{Key: "on", Kind: settings.Bool, Default: "false"}, ""},
		{"int ok", settings.Decl{Key: "n", Kind: settings.Int, Default: "-7"}, ""},
		{"duration ok", settings.Decl{Key: "d", Kind: settings.Duration, Default: "72h"}, ""},
		{"text ok, empty default", settings.Decl{Key: "t", Kind: settings.Text}, ""},
		{"enum ok", settings.Decl{Key: "e", Kind: settings.Enum, Default: "a", Options: opts}, ""},
		{"secret ok", settings.Decl{Key: "s", Kind: settings.Secret}, ""},
		{"underscore key ok", settings.Decl{Key: "legal_name2", Kind: settings.Text}, ""},
		{"64 byte key ok", settings.Decl{Key: "a" + repeat("b", 63), Kind: settings.Text}, ""},

		{"zero kind", settings.Decl{Key: "x", Kind: 0}, "kind"},
		{"unknown kind", settings.Decl{Key: "x", Kind: 77}, "kind"},
		{"empty key", settings.Decl{Key: "", Kind: settings.Text}, "key"},
		{"upper case key", settings.Decl{Key: "Name", Kind: settings.Text}, "key"},
		{"hyphen key", settings.Decl{Key: "legal-name", Kind: settings.Text}, "key"},
		{"dotted key", settings.Decl{Key: "smtp.host", Kind: settings.Text}, "key"},
		{"leading digit key", settings.Decl{Key: "1a", Kind: settings.Text}, "key"},
		{"leading underscore key", settings.Decl{Key: "_a", Kind: settings.Text}, "key"},
		{"trailing underscore key", settings.Decl{Key: "a_", Kind: settings.Text}, "key"},
		{"double underscore key", settings.Decl{Key: "a__b", Kind: settings.Text}, "key"},
		{"65 byte key", settings.Decl{Key: "a" + repeat("b", 64), Kind: settings.Text}, "key"},

		{"bool default malformed", settings.Decl{Key: "x", Kind: settings.Bool, Default: "yes"}, "default"},
		{"bool empty default", settings.Decl{Key: "x", Kind: settings.Bool, Default: ""}, "default"},
		{"int default malformed", settings.Decl{Key: "x", Kind: settings.Int, Default: "1.5"}, "default"},
		{"duration default days", settings.Decl{Key: "x", Kind: settings.Duration, Default: "7d"}, "default"},
		{"text default too long", settings.Decl{Key: "x", Kind: settings.Text, Default: repeat("a", settings.MaxTextBytes+1)}, "default"},
		{"text default control char", settings.Decl{Key: "x", Kind: settings.Text, Default: "a\x00"}, "default"},
		{"enum default not an option", settings.Decl{Key: "x", Kind: settings.Enum, Default: "c", Options: opts}, "default"},
		{"enum one option", settings.Decl{Key: "x", Kind: settings.Enum, Default: "a", Options: []string{"a"}}, "options"},
		{"enum no options", settings.Decl{Key: "x", Kind: settings.Enum, Default: "a"}, "options"},
		{"enum duplicate options", settings.Decl{Key: "x", Kind: settings.Enum, Default: "a", Options: []string{"a", "a"}}, "options"},
		{"enum option not an ident", settings.Decl{Key: "x", Kind: settings.Enum, Default: "a", Options: []string{"a", "B B"}}, "options"},
		{"options on a text", settings.Decl{Key: "x", Kind: settings.Text, Options: opts}, "options"},
		{"secret with a default (F5)", settings.Decl{Key: "x", Kind: settings.Secret, Default: "hunter2"}, "default"},
		{"secret with options (F5)", settings.Decl{Key: "x", Kind: settings.Secret, Options: opts}, "options"},
		{"default fails its own check", settings.Decl{Key: "x", Kind: settings.Int, Default: "0", Check: checkPort}, "default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.decl.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, "")
		})
	}
}

func TestDeclValidate_SecretWithDefault_ReturnsInvalid(t *testing.T) {
	err := settings.Decl{Key: "smtp_password", Kind: settings.Secret, Default: "hunter2"}.Validate()
	wantField(t, err, "default", "")
	if strings.Contains(err.Error(), "hunter2") {
		t.Error("the error echoes the secret default")
	}
}

func TestDeclValidate_CollectsEveryProblem(t *testing.T) {
	err := settings.Decl{Key: "Bad Key", Kind: 0}.Validate()
	wantKind(t, err, errs.Invalid)
	if !hasField(err, "key", "") || !hasField(err, "kind", "") {
		t.Errorf("fields = %v, want key and kind", describe(err))
	}
}

func TestAreaValidate_Table(t *testing.T) {
	good := settings.Decl{Key: "a", Kind: settings.Text}
	tests := []struct {
		name string
		area settings.Area
		ok   bool
	}{
		{"valid", settings.Area{Name: "people", Core: true, Decls: []settings.Decl{good}}, true},
		{"no decls is valid", settings.Area{Name: "quiet"}, true},
		{"empty name", settings.Area{Name: "", Decls: []settings.Decl{good}}, false},
		{"upper case name", settings.Area{Name: "People", Decls: []settings.Decl{good}}, false},
		{"name with a dot", settings.Area{Name: "a.b", Decls: []settings.Decl{good}}, false},
		{"invalid decl", settings.Area{Name: "x", Decls: []settings.Decl{{Key: "BAD", Kind: settings.Text}}}, false},
		{"duplicate keys", settings.Area{Name: "x", Decls: []settings.Decl{good, good}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.area.Validate()
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
}

// ---- New and Options (F1-F4) -------------------------------------------------------------

func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*settings.Options)
	}{
		{"settings.db", func(o *settings.Options) { o.DB = nil }},
		{"settings.clock", func(o *settings.Options) { o.Clock = nil }},
		{"settings.logger", func(o *settings.Options) { o.Logger = nil }},
		{"settings.tracer", func(o *settings.Options) { o.Tracer = nil }},
		{"settings.audit", func(o *settings.Options) { o.Audit = nil }},
		{"settings.authz", func(o *settings.Options) { o.Authz = nil }},
		{"settings.actor", func(o *settings.Options) { o.Actor = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o := first(validOptions(t, dbtest.NewFake()))
			tc.mod(&o)
			wantField(t, o.Validate(), tc.field, "")
			svc, err := settings.New(o)
			wantField(t, err, tc.field, "")
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

func TestOptions_Validate_ReportsEveryMissingField(t *testing.T) {
	o := first(validOptions(t, dbtest.NewFake()))
	wantNoErr(t, o.Validate())

	err := settings.Options{}.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"db", "clock", "logger", "tracer", "audit", "authz", "actor"} {
		if !hasField(err, "settings."+f, "") {
			t.Errorf("zero Options does not report settings.%s", f)
		}
	}
}

func TestNew_RefreshIntervalRange_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		ok   bool
	}{
		{"zero means the default", 0, true},
		{"lower bound", time.Second, true},
		{"upper bound", 10 * time.Minute, true},
		{"just under the lower bound", time.Second - time.Nanosecond, false},
		{"just over the upper bound", 10*time.Minute + time.Nanosecond, false},
		{"negative", -time.Second, false},
		{"an hour", time.Hour, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := first(validOptions(t, dbtest.NewFake()))
			o.RefreshInterval = tc.d
			_, err := settings.New(o)
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "settings.refresh_interval", "")
		})
	}
}

func TestNew_InvalidArea_ReturnsInvalid(t *testing.T) {
	good := settings.Decl{Key: "a", Kind: settings.Text}
	tests := []struct {
		name  string
		areas []settings.Area
		field string
	}{
		{"bad name", []settings.Area{{Name: "Bad Name", Decls: []settings.Decl{good}}}, "settings.areas[0]"},
		{"bad decl", []settings.Area{{Name: "ok", Decls: []settings.Decl{{Key: "X", Kind: settings.Text}}}}, "settings.areas[0]"},
		{"duplicate keys", []settings.Area{{Name: "ok", Decls: []settings.Decl{good, good}}}, "settings.areas[0]"},
		{"second area bad", []settings.Area{{Name: "ok", Decls: []settings.Decl{good}}, {Name: "x", Decls: []settings.Decl{{Key: "k", Kind: 0}}}}, "settings.areas[1]"},
		{"secret with a default", []settings.Area{{Name: "ok", Decls: []settings.Decl{{Key: "s", Kind: settings.Secret, Default: "x"}}}}, "settings.areas[0]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := first(validOptions(t, dbtest.NewFake()))
			o.Areas = tc.areas
			_, err := settings.New(o)
			wantKind(t, err, errs.Invalid)
			found := false
			for _, fe := range validate.Fields(err) {
				if strings.HasPrefix(fe.Field, tc.field) {
					found = true
				}
			}
			if !found {
				t.Errorf("no field under %s in %v", tc.field, describe(err))
			}
		})
	}
}

func TestNew_DuplicateArea_ReturnsInvalid(t *testing.T) {
	good := settings.Decl{Key: "a", Kind: settings.Text}
	tests := []struct {
		name  string
		areas []settings.Area
	}{
		{"two areas with one name", []settings.Area{{Name: "mail", Decls: []settings.Decl{good}}, {Name: "mail", Core: true, Decls: []settings.Decl{good}}}},
		{"an outside area named regional", []settings.Area{{Name: "regional", Decls: []settings.Decl{good}}}},
		{"an outside core area named regional", []settings.Area{{Name: "regional", Core: true, Decls: []settings.Decl{good}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := first(validOptions(t, dbtest.NewFake()))
			o.Areas = tc.areas
			_, err := settings.New(o)
			wantField(t, err, "settings.areas", "")
		})
	}
}

func TestNew_AgainstNoDatabase_Succeeds(t *testing.T) {
	o := first(validOptions(t, db.None()))
	if _, err := settings.New(o); err != nil {
		t.Fatalf("New against db.None(): %v", err)
	}
}

func TestNew_NoAreasNoSealingKey_Succeeds(t *testing.T) {
	o := first(validOptions(t, dbtest.NewFake()))
	o.Areas, o.SealingKey = nil, secret.Secret{}
	svc, err := settings.New(o)
	wantNoErr(t, err)
	if _, err := svc.Scoped("regional"); err != nil {
		t.Errorf("regional is not declared by New: %v", err)
	}
}

func TestNew_DoesNoIO(t *testing.T) {
	f := dbtest.NewFake()
	_ = newRig(t, f)
	if n := len(f.Calls()); n != 0 {
		t.Errorf("New issued %d statements", n)
	}
}

// ---- Scoped and Reader (F6-F8) -----------------------------------------------------------

func TestScoped_UnknownArea_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	for _, area := range []string{"", "nope", "Mail", "mail.host", "regional2"} {
		t.Run(area, func(t *testing.T) {
			rd, err := u.svc.Scoped(area)
			wantKind(t, err, errs.Invalid)
			if rd != nil {
				t.Error("Scoped returned a reader with its error")
			}
		})
	}
}

func TestScoped_DeclaredAreas_Succeed(t *testing.T) {
	u := newUnit(t)
	for _, area := range []string{"regional", "accounts", "mail", "hello"} {
		if _, err := u.svc.Scoped(area); err != nil {
			t.Errorf("Scoped(%q): %v", area, err)
		}
	}
	u.noStatements(t, "Scoped")
}

func TestReader_UndeclaredKey_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	rd := u.reader("mail")
	ctx := t.Context()
	for _, key := range []string{"", "nope", "mail.port", "Port", "greeting"} { // another area's key, a full key, wrong case
		t.Run(key, func(t *testing.T) {
			_, err := rd.Bool(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = rd.Int(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = rd.Duration(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = rd.Text(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = rd.Secret(ctx, key)
			wantKind(t, err, errs.Invalid)
		})
	}
	// The index is consulted before the snapshot: a bad key never loads anything.
	u.noStatements(t, "an undeclared key")
}

func TestReader_KindMismatch_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	rd := u.reader("mail")
	ctx := t.Context()
	tests := []struct {
		name string
		call func() error
	}{
		{"Int on a text", func() error { return errOf(rd.Int(ctx, "host")) }},
		{"Bool on an int", func() error { return errOf(rd.Bool(ctx, "port")) }},
		{"Duration on an int", func() error { return errOf(rd.Duration(ctx, "port")) }},
		{"Text on an int", func() error { return errOf(rd.Text(ctx, "port")) }},
		{"Text on a bool", func() error { return errOf(rd.Text(ctx, "enabled")) }},
		{"Text on a secret", func() error { return errOf(rd.Text(ctx, "password")) }},
		{"Secret on a text", func() error { return errOf(rd.Secret(ctx, "host")) }},
		{"Secret on an enum", func() error { return errOf(rd.Secret(ctx, "security")) }},
		{"Int on an enum", func() error { return errOf(rd.Int(ctx, "security")) }},
		{"Bool on a duration", func() error { return errOf(rd.Bool(ctx, "wait")) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantKind(t, tc.call(), errs.Invalid) })
	}
	u.noStatements(t, "a kind mismatch")
}

// ---- first load (F9) ----------------------------------------------------------------------

func TestReader_FirstLoadFails_ReturnsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errs.E("test", errs.Unavailable, fmt.Errorf("down")), errs.Unavailable},
		{"timeout", errs.E("test", errs.Timeout, fmt.Errorf("slow")), errs.Timeout},
		{"canceled", errs.E("test", errs.Canceled, context.Canceled), errs.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &faultDB{DB: dbtest.NewFake()}
			f.set(tc.err, nil, false)
			r := newRig(t, f)
			ctx := t.Context()
			rd := r.reader("mail")
			_, err := rd.Int(ctx, "port")
			wantKind(t, err, tc.kind)
			_, err = rd.Secret(ctx, "password")
			wantKind(t, err, tc.kind)
			_, err = r.svc.Regional(ctx)
			wantKind(t, err, tc.kind)
			_, err = r.svc.TermsFor(ctx, "en")
			wantKind(t, err, tc.kind)
			_, err = r.svc.Public(ctx)
			wantKind(t, err, tc.kind)
			_, err = r.svc.ThemeCSS(ctx)
			wantKind(t, err, tc.kind)
		})
	}
}

func TestReader_CancelledContext_BeforeFirstLoad_ReturnsCanceled(t *testing.T) {
	u := newUnit(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := u.reader("mail").Int(ctx, "port")
	if err == nil {
		t.Skip("the fake store ignores a cancelled context; the integration test covers the real store")
	}
	wantKind(t, err, errs.Canceled)
}

// ---- TermsFor (F13) -----------------------------------------------------------------------

func TestTermsFor_UnknownLanguage_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	for _, lang := range []string{"", "fr", "EN", "en-US", "en ", "../en"} {
		t.Run(lang, func(t *testing.T) {
			_, err := u.svc.TermsFor(t.Context(), lang)
			wantField(t, err, "lang", "one_of")
		})
	}
}

// ---- permissions (F14-F16) ----------------------------------------------------------------

type adminCall struct {
	name   string
	perm   authz.Permission // the permission the call needs
	call   func(ctx context.Context, s *settings.Service) error
	badArg bool // the call carries input that fails validation, so a permitted caller gets Invalid
}

func adminCalls() []adminCall {
	return []adminCall{
		{"Document", settings.PermissionRead, func(ctx context.Context, s *settings.Service) error {
			return errOf(s.Document(ctx))
		}, false},
		{"Terms", settings.PermissionRead, func(ctx context.Context, s *settings.Service) error {
			return errOf(s.Terms(ctx))
		}, false},
		{"Set", settings.PermissionManage, func(ctx context.Context, s *settings.Service) error {
			return errOf(s.Set(ctx, settings.Changes{}))
		}, true},
		{"SetTerms", settings.PermissionManage, func(ctx context.Context, s *settings.Service) error {
			return errOf(s.SetTerms(ctx, "xx", settings.TermChanges{}))
		}, true},
	}
}

func TestAdminUseCases_NoActor_ReturnsUnauthenticated(t *testing.T) {
	for _, tc := range adminCalls() {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			wantKind(t, tc.call(t.Context(), u.svc), errs.Unauthenticated)
			u.noStatements(t, tc.name+" without an actor")
		})
	}
}

func TestAdminUseCases_RestrictedSession_ReturnsUnauthenticated(t *testing.T) {
	for _, tc := range adminCalls() {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			p := u.person(settings.PermissionRead, settings.PermissionManage)
			wantKind(t, tc.call(asRestricted(t.Context(), p), u.svc), errs.Unauthenticated)
		})
	}
}

func TestDocument_NoActor_ReturnsUnauthenticated(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Document(t.Context())
	wantKind(t, err, errs.Unauthenticated)
}

func TestDocument_NoPermission_ReturnsPermission(t *testing.T) {
	u := newUnit(t)
	nobody := u.person()
	manager := u.person(settings.PermissionManage) // manage does not imply read
	other := u.person(authz.Permission("departments.manage"))
	for name, who := range map[string]context.Context{
		"nobody":              asPerson(t.Context(), nobody),
		"manage without read": asPerson(t.Context(), manager),
		"another permission":  asPerson(t.Context(), other),
		"an unknown person":   asPerson(t.Context(), newPerson()),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := u.svc.Document(who)
			wantKind(t, err, errs.Permission)
			_, err = u.svc.Terms(who)
			wantKind(t, err, errs.Permission)
		})
	}
	u.noStatements(t, "a denied read")
}

func TestSet_NoPermission_ReturnsPermission(t *testing.T) {
	u := newUnit(t)
	nobody := u.person()
	reader := u.person(settings.PermissionRead) // read does not imply manage
	for name, p := range map[string]context.Context{
		"nobody":              asPerson(t.Context(), nobody),
		"read without manage": asPerson(t.Context(), reader),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := u.svc.Set(p, changes(kGreeting, "hi"))
			wantKind(t, err, errs.Permission)
			_, err = u.svc.SetTerms(p, "en", settings.TermChanges{Terms: map[string]settings.FormChanges{"event": {Singular: str("gathering")}}})
			wantKind(t, err, errs.Permission)
		})
	}
	u.noStatements(t, "a denied write")
}

// The permission check comes before validation: a caller without the permission learns
// nothing about the input.
func TestAdminUseCases_PermissionBeforeValidation(t *testing.T) {
	u := newUnit(t)
	ctx := asPerson(t.Context(), u.person())
	_, err := u.svc.Set(ctx, settings.Changes{}) // an empty batch is Invalid for a permitted caller
	wantKind(t, err, errs.Permission)
	_, err = u.svc.SetTerms(ctx, "xx", settings.TermChanges{})
	wantKind(t, err, errs.Permission)
	_, err = u.svc.Set(ctx, changes("no.such", "x"))
	wantKind(t, err, errs.Permission)
}

func TestSet_ManageWithoutRead_PassesPermission(t *testing.T) {
	u := newUnit(t)
	ctx := asPerson(t.Context(), u.person(settings.PermissionManage))
	_, err := u.svc.Set(ctx, settings.Changes{})
	wantField(t, err, "values", "required") // got past Allow, failed validation
	_, err = u.svc.Document(ctx)
	wantKind(t, err, errs.Permission) // manage alone cannot read
}

func TestAdminUseCases_Superuser_PassesPermission(t *testing.T) {
	u := newUnit(t)
	ctx := asSuper(t.Context(), newPerson())
	_, err := u.svc.Set(ctx, settings.Changes{})
	wantField(t, err, "values", "required")
	_, err = u.svc.SetTerms(ctx, "xx", settings.TermChanges{})
	wantField(t, err, "lang", "one_of")
}

// ---- Set validation before the store (F17, F18, F22) --------------------------------------

func (u *unit) manager() context.Context {
	return asPerson(context.Background(), u.person(settings.PermissionManage, settings.PermissionRead))
}

func TestSet_BatchSize_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx := u.manager()
	big := func(n int) settings.Changes {
		c := settings.Changes{Values: map[string]*string{}}
		for i := range n {
			c.Values[fmt.Sprintf("hello.k%d", i)] = str("x")
		}
		return c
	}
	t.Run("nil changes", func(t *testing.T) {
		_, err := u.svc.Set(ctx, settings.Changes{})
		wantField(t, err, "values", "required")
	})
	t.Run("empty map", func(t *testing.T) {
		_, err := u.svc.Set(ctx, settings.Changes{Values: map[string]*string{}})
		wantField(t, err, "values", "required")
	})
	t.Run("one over the maximum", func(t *testing.T) {
		_, err := u.svc.Set(ctx, big(settings.MaxBatch+1))
		wantField(t, err, "values", "too_many")
		if got := paramOf(err, "values", "too_many", "max"); got != "200" {
			t.Errorf("max = %q, want 200", got)
		}
	})
	t.Run("exactly the maximum is not too many", func(t *testing.T) {
		_, err := u.svc.Set(ctx, big(settings.MaxBatch)) // keys are undeclared, so Invalid for another reason
		wantKind(t, err, errs.Invalid)
		if hasField(err, "values", "too_many") {
			t.Error("200 keys reported as too many")
		}
	})
	u.noStatements(t, "a bad batch size")
}

func TestSet_UnknownKey_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx := u.manager()
	for _, key := range []string{"nope.x", "mail.nope", "regional", "noperiod", "", "mail.", ".port", "MAIL.PORT", "mail.port.extra"} {
		t.Run(key, func(t *testing.T) {
			_, err := u.svc.Set(ctx, changes(key, "1"))
			wantField(t, err, key, "unknown")
			// a reset of an unknown key is unknown too
			_, err = u.svc.Set(ctx, resetting(key))
			wantField(t, err, key, "unknown")
		})
	}
	t.Run("all collected with the valid one untouched", func(t *testing.T) {
		_, err := u.svc.Set(ctx, changes("a.b", "1", "c.d", "2"))
		if !hasField(err, "a.b", "unknown") || !hasField(err, "c.d", "unknown") {
			t.Errorf("fields = %v, want both unknown keys", describe(err))
		}
	})
	u.noStatements(t, "an unknown key")
}

func TestSet_SecretWithoutSealingKey_ReturnsUnsupported(t *testing.T) {
	u := newUnit(t, func(o *settings.Options) { o.SealingKey = secret.Secret{} })
	_, err := u.svc.Set(u.manager(), changes(kPassword, "hunter2-invented"))
	wantKind(t, err, errs.Unsupported)
	if strings.Contains(err.Error(), "hunter2-invented") {
		t.Error("the error echoes the secret")
	}
	// The snapshot may load first (the area checks need it); nothing may be written.
	for _, c := range u.fake.Calls() {
		if c.InTx && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(c.SQL)), "SELECT") {
			t.Errorf("a write statement ran: %.60s", c.SQL)
		}
	}
	if len(u.rec.Recorded()) != 0 {
		t.Error("audit entries for a refused secret")
	}
}

// ---- SetTerms validation (F28-F31) ---------------------------------------------------------

func termsOf(m map[string]settings.FormChanges) settings.TermChanges {
	return settings.TermChanges{Terms: m}
}

func TestSetTerms_UnknownLanguage_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx := u.manager()
	for _, lang := range []string{"", "fr", "EN", "en-US", "xx"} {
		t.Run(lang, func(t *testing.T) {
			_, err := u.svc.SetTerms(ctx, lang, termsOf(map[string]settings.FormChanges{"event": {Singular: str("gathering")}}))
			wantField(t, err, "lang", "one_of")
		})
	}
	u.noStatements(t, "an unknown language")
}

func TestSetTerms_UnknownTerm_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx := u.manager()
	t.Run("a term not in CoreTerms", func(t *testing.T) {
		_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{"planet": {Singular: str("x")}}))
		wantField(t, err, "terms.planet", "unknown")
	})
	t.Run("a reset naming an unknown form", func(t *testing.T) {
		_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{"event": {Reset: []string{"bogus"}}}))
		wantField(t, err, "terms.event.reset", "unknown")
	})
	u.noStatements(t, "an unknown term")
	t.Run("a reset naming a known form is not unknown", func(t *testing.T) {
		_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{"event": {Reset: []string{"singular", "plural", "singular_title", "plural_title"}}}))
		if hasField(err, "terms.event.reset", "unknown") {
			t.Errorf("a valid reset reported unknown: %v", describe(err))
		}
	})
}

func TestSetTerms_FormShape_Table(t *testing.T) {
	u := newUnit(t)
	ctx := u.manager()
	form := func(name string, v *string) settings.FormChanges {
		switch name {
		case "singular":
			return settings.FormChanges{Singular: v}
		case "plural":
			return settings.FormChanges{Plural: v}
		case "singular_title":
			return settings.FormChanges{SingularTitle: v}
		default:
			return settings.FormChanges{PluralTitle: v}
		}
	}
	tests := []struct {
		name  string
		value string
		code  string
	}{
		{"empty", "", "required"},
		{"spaces only", "   ", "required"},
		{"tab only", "\t", "required"},
		{"61 bytes", repeat("a", settings.MaxTermBytes+1), "too_long"},
		{"31 two-byte runes is 62 bytes", repeat("é", 31), "too_long"},
		{"NUL", "te\x00am", "control_chars"},
		{"escape", "te\x1bam", "control_chars"},
		{"newline", "te\nam", "control_chars"},
	}
	for _, f := range []string{"singular", "plural", "singular_title", "plural_title"} {
		for _, tc := range tests {
			t.Run(f+"/"+tc.name, func(t *testing.T) {
				_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{"department": form(f, str(tc.value))}))
				wantField(t, err, "terms.department."+f, tc.code)
				if tc.code == "too_long" {
					if got := paramOf(err, "terms.department."+f, "too_long", "max"); got != "60" {
						t.Errorf("max = %q, want 60", got)
					}
				}
			})
		}
	}
	t.Run("all problems are collected", func(t *testing.T) {
		_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{
			"department": {Singular: str(""), Plural: str("a\x00")},
			"event":      {SingularTitle: str(repeat("a", 61))},
		}))
		for _, want := range []string{"terms.department.singular", "terms.department.plural", "terms.event.singular_title"} {
			if !hasField(err, want, "") {
				t.Errorf("missing field error %s in %v", want, describe(err))
			}
		}
	})
	u.noStatements(t, "a bad term form")
}

func TestSetTerms_Empty_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx := u.manager()
	t.Run("nil", func(t *testing.T) {
		_, err := u.svc.SetTerms(ctx, "en", settings.TermChanges{})
		wantField(t, err, "terms", "required")
	})
	t.Run("empty map", func(t *testing.T) {
		_, err := u.svc.SetTerms(ctx, "en", termsOf(map[string]settings.FormChanges{}))
		wantField(t, err, "terms", "required")
	})
	t.Run("more terms than CoreTerms", func(t *testing.T) {
		m := map[string]settings.FormChanges{}
		for _, term := range settings.CoreTerms {
			m[term] = settings.FormChanges{Singular: str("x")}
		}
		m["extra"] = settings.FormChanges{Singular: str("x")}
		_, err := u.svc.SetTerms(ctx, "en", termsOf(m))
		wantField(t, err, "terms", "too_many")
	})
	u.noStatements(t, "an empty term batch")
}

// ---- Run (F32, F34) ------------------------------------------------------------------------

func TestRun_Twice_ReturnsConflict(t *testing.T) {
	u := newUnit(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { first <- u.svc.Run(ctx) }()

	// Wait until the first Run is parked on the fake clock; then it is certainly running.
	bctx, bcancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer bcancel()
	wantNoErr(t, u.clk.BlockUntil(bctx, 1))

	wantKind(t, u.svc.Run(t.Context()), errs.Conflict)

	cancel()
	select {
	case err := <-first:
		wantNoErr(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

func TestRun_ContextDone_ReturnsNil(t *testing.T) {
	t.Run("already cancelled", func(t *testing.T) {
		u := newUnit(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		done := make(chan error, 1)
		go func() { done <- u.svc.Run(ctx) }()
		select {
		case err := <-done:
			wantNoErr(t, err)
		case <-time.After(30 * time.Second):
			t.Fatal("Run ignored a cancelled context")
		}
	})
	t.Run("cancelled while waiting", func(t *testing.T) {
		u := newUnit(t)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- u.svc.Run(ctx) }()
		bctx, bcancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer bcancel()
		wantNoErr(t, u.clk.BlockUntil(bctx, 1))
		cancel()
		select {
		case err := <-done:
			wantNoErr(t, err)
		case <-time.After(30 * time.Second):
			t.Fatal("Run did not return after its context ended")
		}
	})
}

// ---- routes, OpenAPI, handlers (F35) --------------------------------------------------------

type routeSpec struct {
	method, pattern string
	public          bool
}

var specRoutes = []routeSpec{
	{"GET", "/public", true},
	{"GET", "/theme.css", true},
	{"GET", "/", false},
	{"PUT", "/values", false},
	{"GET", "/terms", false},
	{"PUT", "/terms/{lang}", false},
}

func TestRoutes_MatchTheSpecTable(t *testing.T) {
	u := newUnit(t)
	got := map[routeSpec]bool{}
	for _, rt := range u.svc.Routes() {
		if rt.Handler == nil {
			t.Errorf("%s %s: nil handler", rt.Method, rt.Pattern)
		}
		got[routeSpec{rt.Method, normPattern(rt.Pattern), rt.Public}] = true
	}
	for _, want := range specRoutes {
		if !got[want] {
			t.Errorf("missing route %+v", want)
		}
		delete(got, want)
	}
	for extra := range got {
		t.Errorf("unexpected route %+v (a wrong Public flag shows here too)", extra)
	}
}

func TestOpenAPI_NamesExactlyTheRoutes(t *testing.T) {
	u := newUnit(t)
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(u.svc.OpenAPI(), &doc); err != nil {
		t.Fatalf("OpenAPI is not JSON: %v", err)
	}
	described := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			described[strings.ToUpper(method)+" "+normPattern(path)] = true
		}
	}
	for _, rt := range specRoutes {
		key := rt.method + " " + rt.pattern
		if !described[key] {
			t.Errorf("%s is not described", key)
		}
		delete(described, key)
	}
	for extra := range described {
		if !strings.HasPrefix(extra, "GET ") && !strings.HasPrefix(extra, "PUT ") {
			continue // tolerate non-operation keys such as "parameters"
		}
		t.Errorf("the fragment describes %s, which has no route", extra)
	}
	// The returned slice is the caller's: mutating it changes nothing.
	a := u.svc.OpenAPI()
	if len(a) > 0 {
		a[0] ^= 0xff
		if b := u.svc.OpenAPI(); len(b) > 0 && b[0] == a[0] {
			t.Error("OpenAPI shares its backing array")
		}
	}
}

func TestPutValues_BadBody_Returns400(t *testing.T) {
	tests := []struct {
		name, method, path, body, ctype string
	}{
		{"not json", "PUT", "/values", `{nope`, "application/json"},
		{"an array", "PUT", "/values", `[]`, "application/json"},
		{"values is a string", "PUT", "/values", `{"values":"x"}`, "application/json"},
		{"a value that is a number", "PUT", "/values", `{"values":{"hello.greeting":5}}`, "application/json"},
		{"wrong content type", "PUT", "/values", `{"values":{"hello.greeting":"hi"}}`, "text/plain"},
		{"terms not json", "PUT", "/terms/en", `{nope`, "application/json"},
		{"terms wrong content type", "PUT", "/terms/en", `{"terms":{}}`, "text/plain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx := asPerson(t.Context(), u.person(settings.PermissionManage, settings.PermissionRead))
			w, f := u.do(ctx, tc.method, tc.path, tc.body, "Content-Type", tc.ctype)
			if f.count() != 1 {
				t.Fatalf("%d failures (status %d), want 1", f.count(), w.Code)
			}
			wantKind(t, f.last(), errs.Invalid)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
			if n := len(u.fake.Calls()); n != 0 {
				t.Errorf("%d statements for a bad body", n)
			}
		})
	}
}

func TestHandlers_Errors_MapToStatus(t *testing.T) {
	u := newUnit(t)
	reader := u.person(settings.PermissionRead)
	nobody := u.person()
	tests := []struct {
		name         string
		method, path string
		body         string
		ctx          context.Context
		status       int
	}{
		{"GET / without a session", "GET", "/", "", t.Context(), 401},
		{"GET / without read", "GET", "/", "", asPerson(t.Context(), nobody), 403},
		{"GET /terms without a session", "GET", "/terms", "", t.Context(), 401},
		{"GET /terms without read", "GET", "/terms", "", asPerson(t.Context(), nobody), 403},
		{"PUT /values without a session", "PUT", "/values", `{"values":{"hello.greeting":"x"}}`, t.Context(), 401},
		{"PUT /values with read only", "PUT", "/values", `{"values":{"hello.greeting":"x"}}`, asPerson(t.Context(), reader), 403},
		{"PUT /terms without manage", "PUT", "/terms/en", `{"terms":{"event":{"singular":"x"}}}`, asPerson(t.Context(), reader), 403},
		{"PUT /terms without a session", "PUT", "/terms/en", `{"terms":{"event":{"singular":"x"}}}`, t.Context(), 401},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := first(u.do(tc.ctx, tc.method, tc.path, tc.body))
			if w.Code != tc.status {
				t.Errorf("status = %d, want %d", w.Code, tc.status)
			}
		})
	}
	u.noStatements(t, "denied requests")
}

// ---- concurrency ------------------------------------------------------------------------------

func TestService_ConcurrentUse_IsRaceFree(t *testing.T) {
	u := newUnit(t)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := asPerson(t.Context(), u.person())
			rd, err := u.svc.Scoped("mail")
			if err != nil {
				t.Errorf("Scoped: %v", err)
				return
			}
			ignore(rd.Int(ctx, "nope"))
			ignore(rd.Text(ctx, "port"))
			ignore(u.svc.TermsFor(ctx, "xx"))
			ignore(u.svc.Document(ctx))
			ignore(u.svc.Set(ctx, changes(kGreeting, fmt.Sprint(i))))
		}()
	}
	wg.Wait()
}
