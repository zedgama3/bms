package datarights_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// B17
func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*datarights.Options)
	}{
		{"datarights.db", func(o *datarights.Options) { o.DB = nil }},
		{"datarights.clock", func(o *datarights.Options) { o.Clock = nil }},
		{"datarights.logger", func(o *datarights.Options) { o.Logger = nil }},
		{"datarights.tracer", func(o *datarights.Options) { o.Tracer = nil }},
		{"datarights.audit", func(o *datarights.Options) { o.Audit = nil }},
		{"datarights.signals", func(o *datarights.Options) { o.Signals = nil }},
		{"datarights.authz", func(o *datarights.Options) { o.Authz = nil }},
		{"datarights.actor", func(o *datarights.Options) { o.Actor = nil }},
		{"datarights.holders", func(o *datarights.Options) { o.Holders = nil }},
		{"datarights.notify", func(o *datarights.Options) { o.Notify = nil }},
		{"datarights.audit_log", func(o *datarights.Options) { o.AuditLog = nil }},
		{"datarights.accounts", func(o *datarights.Options) { o.Accounts = nil }},
		{"datarights.access", func(o *datarights.Options) { o.Access = nil }},
		{"datarights.outbox", func(o *datarights.Options) { o.Outbox = nil }},
		{"datarights.files", func(o *datarights.Options) { o.Files = nil }},
		{"datarights.people", func(o *datarights.Options) { o.People = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o, _ := validOptions(t, dbtest.NewFake(), nil)
			tc.mod(&o)
			wantField(t, o.Validate(), tc.field, "")
			svc, err := datarights.New(o)
			wantField(t, err, tc.field, "")
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

// B17: the name the spec gives the failure mode.
func TestNew_NilPeople_ReturnsInvalid(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake(), nil)
	o.People = nil
	svc, err := datarights.New(o)
	wantField(t, err, "datarights.people", "")
	if svc != nil {
		t.Error("New returned a service with its error")
	}
}

func TestOptions_Validate_ReportsEveryProblemAtOnce(t *testing.T) {
	err := datarights.Options{}.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{
		"db", "clock", "logger", "tracer", "audit", "signals", "authz", "actor",
		"holders", "notify", "audit_log", "accounts", "access", "outbox", "files", "people",
	} {
		if !hasField(err, "datarights."+f, "") {
			t.Errorf("no field error for datarights.%s", f)
		}
	}
}

func TestNew_OptionalMeter_MayBeNil(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake(), nil)
	o.Meter = nil
	wantNoErr(t, o.Validate())
	if _, err := datarights.New(o); err != nil {
		t.Fatalf("New without a Meter: %v", err)
	}
}

func TestNew_WorksAgainstNoDatabase_AndSubscribesToNothing(t *testing.T) {
	r := newRig(t, db.None())
	if got := r.bus.Handlers(); len(got) != 0 {
		t.Fatalf("subscriptions = %v, want none", got)
	}
}

// B18
func TestLink_Twice_ReturnsConflict(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.svc.Link(nil))
	wantKind(t, r.svc.Link(nil), errs.Conflict)
	// A second Link with sources is refused as well, and does not replace the first.
	good := &fakeSource{name: "hello", log: r.log}
	wantKind(t, r.svc.Link([]datarights.Source{good.source()}), errs.Conflict)
}

// B19
func TestLink_DuplicateName_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	a := &fakeSource{name: "hello", log: r.log}
	b := &fakeSource{name: "hello", log: r.log}
	wantKind(t, r.svc.Link([]datarights.Source{a.source(), b.source()}), errs.Invalid)
}

func TestLink_DistinctNames_Succeeds(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	a := &fakeSource{name: "hello", log: r.log}
	b := &fakeSource{name: "volunteers", log: r.log}
	wantNoErr(t, r.svc.Link([]datarights.Source{a.source(), b.source()}))
}

func TestDeletionRequested_IsTheOnlyPublishedSignal(t *testing.T) {
	if string(datarights.SignalDeletionRequested) != "datarights.deletion-requested" {
		t.Errorf("SignalDeletionRequested = %q", datarights.SignalDeletionRequested)
	}
	if !slices.Equal(datarights.PublishedSignals, []signals.Name{"datarights.deletion-requested"}) {
		t.Errorf("PublishedSignals = %v", datarights.PublishedSignals)
	}
}

func TestPermissions_KeepTheirNames(t *testing.T) {
	var got []string
	for _, d := range datarights.Permissions {
		got = append(got, string(d.Name))
	}
	slices.Sort(got)
	want := []string{string(permDelete), string(permExport)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Permissions = %v, want %v (names do not change, decision 12)", got, want)
	}
	if datarights.PermissionDataExport != permExport || datarights.PermissionDataDelete != permDelete {
		t.Errorf("permission constants = %q, %q", datarights.PermissionDataExport, datarights.PermissionDataDelete)
	}
}

// ---- routes -----------------------------------------------------------------------------

var specRoutes = []string{
	"GET /me/export",
	"GET /me/requests", "POST /me/requests", "POST /me/requests/{request}/withdraw",
	"GET /requests",
	"POST /requests/{request}/decline", "POST /requests/{request}/fulfil",
	"GET /people/{person}/export",
	"DELETE /people/{person}",
}

func TestRoutes_AreTheNineOfTheSpec(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	var got []string
	for _, rt := range r.svc.Routes() {
		if rt.Handler == nil {
			t.Errorf("%s %s: nil handler", rt.Method, rt.Pattern)
		}
		got = append(got, rt.Method+" "+rt.Pattern)
	}
	slices.Sort(got)
	want := slices.Clone(specRoutes)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("routes differ\n got: %v\nwant: %v", got, want)
	}
}

// F53, as it was for people's routes: the same errors, the same statuses.
func TestRoutes_Errors_MapToProblem(t *testing.T) {
	me := newPerson()
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		signed bool
		grant  []authz.Permission
		kind   errs.Kind
		status int
	}{
		{"export without a session", "GET", "/me/export", "", false, nil, errs.Unauthenticated, 401},
		{"my requests without a session", "GET", "/me/requests", "", false, nil, errs.Unauthenticated, 401},
		{"request deletion without a session", "POST", "/me/requests", `{"kind":"delete"}`, false, nil, errs.Unauthenticated, 401},
		{"bad person id on export", "GET", "/people/not-a-uuid/export", "", true, nil, errs.Invalid, 400},
		{"bad person id on delete", "DELETE", "/people/not-a-uuid", "", true, []authz.Permission{permDelete}, errs.Invalid, 400},
		{"bad request id on withdraw", "POST", "/me/requests/nope/withdraw", "", true, nil, errs.Invalid, 400},
		{"export of another without permission", "GET", "/people/" + newPerson().String() + "/export", "", true, nil, errs.Permission, 403},
		{"delete without permission", "DELETE", "/people/" + newPerson().String(), "", true, nil, errs.Permission, 403},
		{"export permission does not delete", "DELETE", "/people/" + newPerson().String(), "", true, []authz.Permission{permExport}, errs.Permission, 403},
		{"requests list without permission", "GET", "/requests", "", true, nil, errs.Permission, 403},
		{"decline without permission", "POST", "/requests/" + newPerson().String() + "/decline", `{"note":""}`, true, nil, errs.Permission, 403},
		{"fulfil without permission", "POST", "/requests/" + newPerson().String() + "/fulfil", "", true, nil, errs.Permission, 403},
		{"body is not json", "POST", "/me/requests", `{nope`, true, nil, errs.Invalid, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			r.link()
			ctx := ctxT(t)
			if tc.signed {
				ctx = asPerson(ctx, me)
			}
			for _, p := range tc.grant {
				r.az.Grant(me, p)
			}
			w, f := r.do(ctx, tc.method, tc.path, tc.body)
			if f.count() != 1 {
				t.Fatalf("%d failures handed to httpx.Fail (status %d), want 1", f.count(), w.Code)
			}
			wantKind(t, f.last(), tc.kind)
			if w.Code != tc.status {
				t.Errorf("status = %d, want %d", w.Code, tc.status)
			}
		})
	}
}

func TestRoutes_OldPeoplePaths_AreNotServed(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	for _, rt := range r.svc.Routes() {
		if strings.HasPrefix(rt.Pattern, "/{person}") {
			t.Errorf("route %s %s keeps people's shape; the person routes are under /people/{person}", rt.Method, rt.Pattern)
		}
	}
}

func TestPermissions_MatchTheSpecTable(t *testing.T) {
	if len(datarights.Permissions) != 2 {
		t.Fatalf("%d declarations, want 2", len(datarights.Permissions))
	}
	for _, d := range datarights.Permissions {
		if d.Name != permExport && d.Name != permDelete {
			t.Errorf("unexpected permission %q", d.Name)
		}
		if d.Scope != authz.ScopeSite {
			t.Errorf("%s: scope %v, want site", d.Name, d.Scope)
		}
		if d.Reaches != model.SensitivitySensitive {
			t.Errorf("%s: reaches %v, want sensitive", d.Name, d.Reaches)
		}
		if !slices.Equal(d.StarterRoles, []string{authz.RoleAccountAdmin}) {
			t.Errorf("%s: starter roles %v, want [account-admin]", d.Name, d.StarterRoles)
		}
		if slices.Contains(d.StarterRoles, authz.RoleParticipant) {
			t.Errorf("%s is seeded into the base role", d.Name)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
	}
}

// people answers as a datarights source: the compile-time check that *people.Service is one.
var _ datarights.PeopleSource = (*people.Service)(nil)
