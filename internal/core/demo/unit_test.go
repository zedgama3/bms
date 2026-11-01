package demo_test

// Unit tests of core/demo that need no database: plan/design/demo.md (D53), failure modes
// F4, F5, F6, F9, F15, F18, F20 (read-only part), F27 to F30, F32 to F37, F39, and the
// constants. Mail, marker and reset behaviour that needs rows is in integration_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func TestConstants_Pinned(t *testing.T) {
	if demo.Password != "burn-demo-password" || demo.Regional != "Ember Flats" || demo.Domain != "demo.invalid" || demo.Zone != "America/Chicago" {
		t.Errorf("identity constants changed: %q %q %q %q", demo.Password, demo.Regional, demo.Domain, demo.Zone)
	}
	if demo.MaxUploadBytes != 2<<20 || demo.MaxQuotaBytes != 256<<20 || demo.MaxPerRecord != 5 {
		t.Errorf("caps changed: %d %d %d", demo.MaxUploadBytes, demo.MaxQuotaBytes, demo.MaxPerRecord)
	}
	if demo.MaxMail != 200 || demo.MaxMailList != 50 || demo.SeedVersion != 1 {
		t.Errorf("mail constants changed: %d %d %d", demo.MaxMail, demo.MaxMailList, demo.SeedVersion)
	}
	if demo.DefaultContributorTimeout != 2*time.Minute || demo.SessionLifetime != time.Hour {
		t.Errorf("durations changed: %v %v", demo.DefaultContributorTimeout, demo.SessionLifetime)
	}
	if demoseed.Point != "demo.seed" || demoseed.WalkOns != 12 {
		t.Errorf("demoseed constants changed: %q %d", demoseed.Point, demoseed.WalkOns)
	}
}

// ---- demoseed ---------------------------------------------------------------------------

func TestWalkOnKey_FormatAndBounds(t *testing.T) {
	if got := demoseed.WalkOnKey(1); got != "walkon-01" {
		t.Errorf("WalkOnKey(1) = %q", got)
	}
	if got := demoseed.WalkOnKey(12); got != "walkon-12" {
		t.Errorf("WalkOnKey(12) = %q", got)
	}
	for _, n := range []int{0, -1, 13, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("WalkOnKey(%d) did not panic", n)
				}
			}()
			demoseed.WalkOnKey(n)
		})
	}
}

// ---- New (F4) ---------------------------------------------------------------------------

func TestNew_MissingDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*demo.Options)
	}{
		{"demo.db", func(o *demo.Options) { o.DB = nil }},
		{"demo.clock", func(o *demo.Options) { o.Clock = nil }},
		{"demo.logger", func(o *demo.Options) { o.Logger = nil }},
		{"demo.tracer", func(o *demo.Options) { o.Tracer = nil }},
		{"demo.audit", func(o *demo.Options) { o.Audit = nil }},
		{"demo.actor", func(o *demo.Options) { o.Actor = nil }},
		{"demo.accounts", func(o *demo.Options) { o.Accounts = nil }},
		{"demo.access", func(o *demo.Options) { o.Access = nil }},
		{"demo.addresses", func(o *demo.Options) { o.Addresses = nil }},
		{"demo.profiles", func(o *demo.Options) { o.Profiles = nil }},
		{"demo.departments", func(o *demo.Options) { o.Departments = nil }},
		{"demo.events", func(o *demo.Options) { o.Events = nil }},
		{"demo.settings", func(o *demo.Options) { o.Settings = nil }},
		{"demo.schema", func(o *demo.Options) { o.Schema = nil }},
		{"demo.objects", func(o *demo.Options) { o.Objects = nil }},
		{"demo.schemas", func(o *demo.Options) { o.Schemas = nil }},
		{"demo.schemas", func(o *demo.Options) { o.Schemas = []string{} }},
		{"demo.schemas", func(o *demo.Options) { o.Schemas = []string{"core", "Bad Name"} }},
		{"demo.schemas", func(o *demo.Options) { o.Schemas = []string{"core; drop schema x"} }},
		{"demo.data_dir", func(o *demo.Options) { o.DataDir = "" }},
		{"demo.contributor_timeout", func(o *demo.Options) { o.ContributorTimeout = 9 * time.Second }},
		{"demo.contributor_timeout", func(o *demo.Options) { o.ContributorTimeout = 31 * time.Minute }},
		{"demo.contributor_timeout", func(o *demo.Options) { o.ContributorTimeout = -time.Second }},
		{"demo.public_url", func(o *demo.Options) { o.PublicURL = "ftp://demo.example.org" }},
		{"demo.public_url", func(o *demo.Options) { o.PublicURL = "https://demo.example.org/?q=1" }},
		{"demo.public_url", func(o *demo.Options) { o.PublicURL = "https://user:pw@demo.example.org" }},
		{"demo.public_url", func(o *demo.Options) { o.PublicURL = "not a url" }},
	}
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%02d %s", i, tc.field), func(t *testing.T) {
			o := newRig(t, db.None()).opts
			tc.mod(&o)
			svc, err := demo.New(o)
			wantKind(t, err, errs.Invalid)
			if svc != nil {
				t.Error("New returned a service with its error")
			}
			found := false
			for _, f := range validate.Fields(err) {
				found = found || f.Field == tc.field
			}
			if !found {
				t.Errorf("no field error for %s in %v", tc.field, validate.Fields(err))
			}
		})
	}
}

func TestNew_ValidOptions_NoIO(t *testing.T) {
	fake := dbtest.NewFake()
	for _, enabled := range []bool{true, false} {
		r := newRig(t, fake, func(o *demo.Options) { o.Enabled = enabled; o.PublicURL = "https://demo.example.org" })
		if r.svc.Enabled() != enabled {
			t.Errorf("Enabled() = %v, want %v", r.svc.Enabled(), enabled)
		}
	}
	// works against a DB that refuses every statement
	newRig(t, db.None())
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("New made %d database calls, want none", n)
	}
	if r := newRig(t, db.None(), func(o *demo.Options) { o.PublicURL = "" }); r.svc == nil {
		t.Error("an empty PublicURL must be accepted")
	}
}

// ---- Link (F5) --------------------------------------------------------------------------

func TestLink_Twice_ReturnsInternal(t *testing.T) {
	r := newRig(t, db.None())
	wantNoErr(t, r.svc.Link([]demo.Contributor{{Module: "alpha", Provider: &fakeProvider{}}}))
	wantKind(t, r.svc.Link([]demo.Contributor{{Module: "beta", Provider: &fakeProvider{}}}), errs.Internal)
	// Twice even with an empty list the first time.
	r2 := newRig(t, db.None())
	wantNoErr(t, r2.svc.Link(nil))
	wantKind(t, r2.svc.Link(nil), errs.Internal)
}

func TestLink_NilProvider_ReturnsInternal(t *testing.T) {
	r := newRig(t, db.None())
	wantKind(t, r.svc.Link([]demo.Contributor{{Module: "alpha", Provider: nil}}), errs.Internal)
}

func TestLink_EmptyModuleName_ReturnsInvalid(t *testing.T) {
	r := newRig(t, db.None())
	wantKind(t, r.svc.Link([]demo.Contributor{{Module: "", Provider: &fakeProvider{}}}), errs.Invalid)
}

// ---- Info (F39) -------------------------------------------------------------------------

func TestInfo_NoIO(t *testing.T) {
	r := newRig(t, db.None()) // a DB that refuses every statement
	info := r.svc.Info()
	if !info.Enabled || info.Regional != demo.Regional || info.Password != demo.Password {
		t.Fatalf("Info = %+v", info)
	}
	if len(info.Accounts) != 10 {
		t.Fatalf("%d accounts, want 10", len(info.Accounts))
	}
	for i, key := range actorKeys {
		a := info.Accounts[i]
		if a.Key != key || a.Email != key+"@demo.invalid" {
			t.Errorf("account %d = %+v, want key %s in position order", i, a, key)
		}
		if len(a.Roles) == 0 && key != demoseed.ActorParticipant && key != demoseed.ActorNewcomer {
			t.Errorf("%s has no roles listed", key)
		}
	}
	// A department-scoped role is "<role>@<department key>".
	var lead demo.AccountInfo
	for _, a := range info.Accounts {
		if a.Key == demoseed.ActorLead {
			lead = a
		}
	}
	// D1.1 (D56): the lead is department-admin at the five departments with volunteer data.
	wantLead := []string{"department-admin@rangers", "department-admin@gate", "department-admin@greeters",
		"department-admin@medical", "department-admin@dpw"}
	if !slices.Equal(lead.Roles, wantLead) {
		t.Errorf("lead roles = %v, want %v", lead.Roles, wantLead)
	}
	// Info is a pure function of constants: the same twice.
	again := r.svc.Info()
	a, errA := json.Marshal(info) //nolint:gosec // G117: the demo password is public by design (demo.md)
	wantNoErr(t, errA)
	b, errB := json.Marshal(again) //nolint:gosec // G117: the demo password is public by design (demo.md)
	wantNoErr(t, errB)
	if string(a) != string(b) {
		t.Error("Info differs between calls")
	}
}

func TestInfo_Disabled_OnlyEnabledFalse(t *testing.T) {
	r := newRig(t, db.None(), func(o *demo.Options) { o.Enabled = false })
	info := r.svc.Info()
	if info.Enabled || info.Regional != "" || info.Password != "" || len(info.Accounts) != 0 {
		t.Fatalf("Info = %+v, want only Enabled false", info)
	}
	b, err := json.Marshal(info) //nolint:gosec // G117: the demo password is public by design (demo.md)
	wantNoErr(t, err)
	if string(b) != `{"enabled":false}` {
		t.Errorf("JSON = %s, want {\"enabled\":false}", b)
	}
	if r.svc.Enabled() {
		t.Error("Enabled() = true")
	}
}

func TestInfo_TheSharedPasswordIsTheOnlySecretShown(t *testing.T) {
	r := newRig(t, db.None())
	b, err := json.Marshal(r.svc.Info()) //nolint:gosec // G117: the demo password is public by design (demo.md)
	wantNoErr(t, err)
	if strings.Count(string(b), demo.Password) != 1 {
		t.Errorf("the password appears %d times in GET /api/demo, want once", strings.Count(string(b), demo.Password))
	}
}

// ---- Seed and Reset refuse (F6, F9, F15, F18, F20) ---------------------------------------

func untouched(t *testing.T, r *rig, fake *dbtest.Fake) {
	t.Helper()
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls: %+v; want none", n, r.log.all())
	}
	if fake != nil && len(fake.Calls()) != 0 {
		t.Errorf("%d database calls, want none", len(fake.Calls()))
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("%d audit entries, want none", n)
	}
}

func TestSeed_DemoOff_ReturnsUnsupported(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake, func(o *demo.Options) { o.Enabled = false })
	rep, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Unsupported)
	if len(rep.Steps) != 0 {
		t.Errorf("report has steps: %v", stepNames(rep))
	}
	untouched(t, r, fake)
}

func TestReset_DemoOff_ReturnsUnsupported(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake, func(o *demo.Options) { o.Enabled = false })
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Unsupported)
	untouched(t, r, fake)
}

func TestSeed_ReadOnly_ReturnsUnavailable(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	r := newRig(t, fake)
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantReadOnly(t, err)
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls on a read-only copy, want none", n)
	}
}

func TestReset_ReadOnly_ReturnsUnavailable(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	r := newRig(t, fake)
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantReadOnly(t, err)
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls (objects cleared?) on a read-only copy, want none: %+v", n, r.log.all())
	}
}

func TestSeed_Canceled_ReturnsCanceled(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake)
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := r.svc.Seed(ctx, &strings.Builder{})
	wantKind(t, err, errs.Canceled)
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls after cancellation, want none", n)
	}
}

// ---- FileCeiling (F37, F44) --------------------------------------------------------------

// F36 and F44 are gone with Service.Policy (no inner source to fail, no gap in the wiring:
// the ceiling is a value); F37 lives in files' TestNew_CeilingNeverRaises.
func TestFileCeiling_IsTheDemoCaps(t *testing.T) {
	want := files.Policy{MaxBytes: demo.MaxUploadBytes, QuotaBytes: demo.MaxQuotaBytes, MaxPerRecord: demo.MaxPerRecord}
	if demo.FileCeiling != want {
		t.Errorf("FileCeiling = %+v, want %+v", demo.FileCeiling, want)
	}
	// Every field is set: a zero field would leave that limit to the settings.
	if demo.FileCeiling.MaxBytes <= 0 || demo.FileCeiling.QuotaBytes <= 0 || demo.FileCeiling.MaxPerRecord <= 0 {
		t.Errorf("a FileCeiling field is zero (no ceiling): %+v", demo.FileCeiling)
	}
	if err := demo.FileCeiling.Validate(); err != nil {
		t.Errorf("FileCeiling.Validate: %v", err)
	}
	if demo.FileCeiling.MaxBytes > files.DefaultPolicy().MaxBytes {
		t.Error("the demo cap is above files' own default: it would raise, not lower")
	}
}

// ---- NewGuard and NewCapture: nil dependencies --------------------------------------------

func TestNewGuard_NilDB_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	tests := []struct {
		name string
		o    demo.GuardOptions
	}{
		{"zero options", demo.GuardOptions{}},
		{"no db", demo.GuardOptions{Tracer: c.Tracer("demo")}},
		{"no tracer", demo.GuardOptions{DB: dbtest.NewFake()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := demo.NewGuard(tc.o)
			wantKind(t, err, errs.Invalid)
			if g != nil {
				t.Error("a Guard came back with the error")
			}
		})
	}
	g, err := demo.NewGuard(demo.GuardOptions{DB: db.None(), Tracer: c.Tracer("demo")})
	wantNoErr(t, err) // no I/O: db.None() is fine
	if g == nil {
		t.Fatal("nil Guard")
	}
}

func TestNewCapture_NilDependency_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	full := func() demo.CaptureOptions {
		return demo.CaptureOptions{DB: dbtest.NewFake(), Clock: clock.NewFake(seedDay), Logger: c.Logger(), Tracer: c.Tracer("demo")}
	}
	tests := []struct {
		name string
		mod  func(*demo.CaptureOptions)
	}{
		{"no db", func(o *demo.CaptureOptions) { o.DB = nil }},
		{"no clock", func(o *demo.CaptureOptions) { o.Clock = nil }},
		{"no logger", func(o *demo.CaptureOptions) { o.Logger = nil }},
		{"no tracer", func(o *demo.CaptureOptions) { o.Tracer = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := full()
			tc.mod(&o)
			cp, err := demo.NewCapture(o)
			wantKind(t, err, errs.Invalid)
			if cp != nil {
				t.Error("a Capture came back with the error")
			}
		})
	}
	wantKind(t, errOf(demo.NewCapture(demo.CaptureOptions{})), errs.Invalid)
	// The meter is optional, and construction does no I/O.
	cp, err := demo.NewCapture(full())
	wantNoErr(t, err)
	if cp == nil {
		t.Fatal("nil Capture")
	}
	o := full()
	o.DB = db.None()
	_, err = demo.NewCapture(o)
	wantNoErr(t, err)
}

func errOf[T any](_ T, err error) error { return err }

// ---- StateOf -----------------------------------------------------------------------------

func TestStateOf_NilDB_ReturnsInvalid(t *testing.T) {
	_, err := demo.StateOf(ctxT(t), nil)
	wantKind(t, err, errs.Invalid)
}

func TestStateOf_StoreDown_ReturnsUnavailable(t *testing.T) {
	down := errs.E("test.down", errs.Unavailable, errors.New("database down"))
	_, err := demo.StateOf(ctxT(t), &faultDB{DB: dbtest.NewFake(), err: down})
	wantKind(t, err, errs.Unavailable)
}

// ---- Channel (F27 to F29) ---------------------------------------------------------------

func goodEnvelope() notify.Envelope {
	return notify.Envelope{
		From:      notify.Address{Name: "Ember Flats (demo)", Email: "noreply@demo.invalid"},
		To:        "tester.one@demo.invalid",
		Subject:   "Reset your password",
		Body:      "Follow the link.",
		MessageID: "m1@demo.invalid",
	}
}

func TestCapture_BadEnvelope_Rejected(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*notify.Envelope)
	}{
		{"zero To", func(e *notify.Envelope) { e.To = "" }},
		{"empty subject", func(e *notify.Envelope) { e.Subject = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := dbtest.NewFake()
			r := newRig(t, fake)
			e := goodEnvelope()
			tc.mod(&e)
			err := r.capture().Send(ctxT(t), e)
			wantKind(t, err, errs.Invalid)
			if !errors.Is(err, notify.ErrRejected) {
				t.Errorf("error does not wrap notify.ErrRejected: %v", err)
			}
			if n := len(fake.Calls()); n != 0 {
				t.Errorf("a rejected envelope reached the store (%d calls)", n)
			}
		})
	}
}

func TestCapture_Canceled(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	wantKind(t, r.capture().Send(ctx, goodEnvelope()), errs.Canceled)
}

func TestCapture_StoreDown_ReturnsUnavailable(t *testing.T) {
	down := errs.E("test.down", errs.Unavailable, errors.New("database down"))
	r := newRig(t, &faultDB{DB: dbtest.NewFake(), err: down})
	wantKind(t, r.capture().Send(ctxT(t), goodEnvelope()), errs.Unavailable)
}

func TestCapture_ReadOnly_ReturnsUnavailable(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	r := newRig(t, fake)
	wantReadOnly(t, r.capture().Send(ctxT(t), goodEnvelope()))
}

func TestCapture_Ping_AnswersNil(t *testing.T) {
	r := newRig(t, db.None()) // no database needed to answer a ping
	wantNoErr(t, r.capture().Ping(ctxT(t)))
}

// ---- ListMail (F30, F32 to F35) ---------------------------------------------------------

func viewer(t *testing.T, r *rig, email string) (context.Context, model.PersonID) {
	t.Helper()
	p := model.NewID[model.PersonID]()
	if email != "" {
		r.addresses.set(p, email)
	}
	return asPerson(ctxT(t), p, false), p
}

func TestListMail_NoActor_ReturnsUnauthenticated(t *testing.T) {
	for _, enabled := range []bool{true, false} { // authentication is checked before the switch
		r := newRig(t, dbtest.NewFake(), func(o *demo.Options) { o.Enabled = enabled })
		_, err := r.svc.ListMail(ctxT(t), demo.MailQuery{})
		wantKind(t, err, errs.Unauthenticated)
	}
}

func TestListMail_DemoOff_ReturnsUnsupported(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake, func(o *demo.Options) { o.Enabled = false })
	ctx, _ := viewer(t, r, "tester.one@demo.invalid")
	_, err := r.svc.ListMail(ctx, demo.MailQuery{})
	wantKind(t, err, errs.Unsupported)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d database calls with demo off", n)
	}
}

func TestListMail_BadLimit_ReturnsInvalid(t *testing.T) {
	for _, limit := range []int{-1, -100, demo.MaxMailList + 1, 1000} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			ctx, _ := viewer(t, r, "tester.one@demo.invalid")
			_, err := r.svc.ListMail(ctx, demo.MailQuery{Limit: limit})
			wantKind(t, err, errs.Invalid)
			found := false
			for _, f := range validate.Fields(err) {
				found = found || (f.Field == "limit" && f.Code == validate.CodeRange)
			}
			if !found {
				t.Errorf("no field error limit/range in %v", validate.Fields(err))
			}
		})
	}
}

func TestListMail_LimitBoundaries_Accepted(t *testing.T) {
	for _, limit := range []int{0, 1, demo.MaxMailList} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			ctx, _ := viewer(t, r, "tester.one@demo.invalid")
			got, err := r.svc.ListMail(ctx, demo.MailQuery{Limit: limit})
			wantNoErr(t, err)
			if len(got) != 0 {
				t.Errorf("%d messages from an empty store", len(got))
			}
		})
	}
}

func TestListMail_NoAccountAddress_Empty(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake)
	ctx, _ := viewer(t, r, "") // EmailOf answers NotFound
	got, err := r.svc.ListMail(ctx, demo.MailQuery{})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Fatalf("%d messages for a viewer without an address, want an empty list", len(got))
	}
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("the store was queried (%d calls) although the viewer has no address; nothing may match", n)
	}
}

func TestListMail_AddressLookupFails_ReturnsItsKind(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, _ := viewer(t, r, "tester.one@demo.invalid")
	r.addresses.err = errs.E("test.addresses", errs.Unavailable, errors.New("identity down"))
	_, err := r.svc.ListMail(ctx, demo.MailQuery{})
	wantKind(t, err, errs.Unavailable)
}

func TestListMail_StoreDown_ReturnsUnavailable(t *testing.T) {
	down := errs.E("test.down", errs.Unavailable, errors.New("database down"))
	r := newRig(t, &faultDB{DB: dbtest.NewFake(), err: down})
	ctx, _ := viewer(t, r, "tester.one@demo.invalid")
	_, err := r.svc.ListMail(ctx, demo.MailQuery{})
	wantKind(t, err, errs.Unavailable)
}

func TestListMail_NotAudited_NothingPersonalLogged(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, _ := viewer(t, r, "ZQ9VIEWER@demo.invalid")
	_, err := r.svc.ListMail(ctx, demo.MailQuery{})
	wantNoErr(t, err)
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("%d audit entries for a read of the viewer's own mail, want none", n)
	}
	if r.cap.Contains("ZQ9VIEWER") {
		t.Error("the viewer's address is in a log, span or metric")
	}
}

func TestListMail_ConcurrentCalls_AreRaceFree(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	ctx, _ := viewer(t, r, "tester.one@demo.invalid")
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := r.svc.ListMail(ctx, demo.MailQuery{Limit: 5}); err != nil {
				t.Errorf("ListMail: %v", err)
			}
			_ = r.svc.Info()
			_ = r.svc.Enabled()
		})
	}
	wg.Wait()
}

// ---- Routes ----------------------------------------------------------------------------

func TestRoutes_InfoPublic_MailSignedIn(t *testing.T) {
	r := newRig(t, db.None())
	routes := r.svc.Routes()
	if len(routes) != 2 {
		t.Fatalf("%d routes, want 2 (GET / and GET /mail)", len(routes))
	}
	var info, mail bool
	for _, rt := range routes {
		if rt.Method != "GET" {
			t.Errorf("route %s %s is not a GET", rt.Method, rt.Pattern)
		}
		switch rt.Pattern {
		case "/", "/{$}":
			info = true
			if !rt.Public {
				t.Error("GET /api/demo must be public")
			}
		case "/mail":
			mail = true
			if rt.Public {
				t.Error("GET /api/demo/mail must need a session")
			}
		}
		if rt.Handler == nil {
			t.Errorf("route %s has no handler", rt.Pattern)
		}
	}
	if !info || !mail {
		t.Errorf("routes = %+v", routes)
	}
	var doc map[string]any
	if err := json.Unmarshal(r.svc.OpenAPI(), &doc); err != nil {
		t.Errorf("OpenAPI is not JSON: %v", err)
	}
}

// ---- Protected on a failing store (F25) --------------------------------------------------

func TestProtected_StoreDown_ReturnsUnavailable(t *testing.T) {
	down := errs.E("test.down", errs.Unavailable, errors.New("database down"))
	r := newRig(t, &faultDB{DB: dbtest.NewFake(), err: down})
	ok, err := r.guard().Protected(ctxT(t), model.NewID[model.AccountID]())
	wantKind(t, err, errs.Unavailable)
	if ok {
		t.Error("Protected = true on error; the caller fails closed on the error, but must never be told true by accident")
	}
}
