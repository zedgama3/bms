package volunteers_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// ---- F1, F141, F125: construction, manifest, lifecycle ----

// F1
func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	base := func(t *testing.T) module.Deps {
		r := newRig(t, opts{noStart: true})
		return module.Deps{
			Logger: r.cap.Logger(), Tracer: r.cap.Tracer("module.volunteers"), Clock: r.clk, DB: r.db, Signals: r.sig, Audit: r.aud,
			Authz: r.az, People: r.ppl, Departments: r.dept, Events: r.evt, Settings: r.set, Notify: r.mail, Jobs: r.jobs, Files: r.fl,
		}
	}
	tests := []struct {
		name string
		drop func(*module.Deps)
	}{
		{"DB", func(d *module.Deps) { d.DB = nil }},
		{"Authz", func(d *module.Deps) { d.Authz = nil }},
		{"People", func(d *module.Deps) { d.People = nil }},
		{"Departments", func(d *module.Deps) { d.Departments = nil }},
		{"Events", func(d *module.Deps) { d.Events = nil }},
		{"Audit", func(d *module.Deps) { d.Audit = nil }},
		{"Signals", func(d *module.Deps) { d.Signals = nil }},
		{"Settings", func(d *module.Deps) { d.Settings = nil }},
		{"Notify", func(d *module.Deps) { d.Notify = nil }},
		{"Jobs", func(d *module.Deps) { d.Jobs = nil }},
		{"Files", func(d *module.Deps) { d.Files = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := base(t)
			tc.drop(&d)
			m, err := volunteers.New(ctxT(t), d)
			wantKind(t, err, errs.Invalid)
			if m != nil {
				t.Errorf("New returned a module (%T) together with an error", m)
			}
		})
	}
}

func TestNew_PerformsNoDatabaseIO(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, opts{db: fake, noStart: true})
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("New issued %d database calls, want none (bms openapi builds every module against a refusing DB)", len(calls))
	}
	_ = r
}

// The hello rule: the service never degrades to "no permission check". A deps set whose
// Authz denies everything denies every guarded use case.
func TestNew_AuthzDeniesEverything_StaysDenied(t *testing.T) {
	r := newRig(t, opts{})
	nobody := r.person("")
	d := r.department("Gate")
	_, err := r.svc.CreatePosition(r.as(nobody), d, vol.PositionInput{Name: "Greeter", Access: vol.AccessOpen})
	wantKind(t, err, errs.Permission)
}

// F141
func TestManifest_Valid(t *testing.T) {
	if err := volunteers.Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
	if volunteers.Manifest.Name != "volunteers" {
		t.Errorf("Name = %q", volunteers.Manifest.Name)
	}
}

func TestViews_Tagged(t *testing.T) {
	if len(volunteers.Manifest.Views) == 0 {
		t.Fatal("Manifest.Views is empty; every vol response type must be listed")
	}
	if err := privacy.Check(volunteers.Manifest.Views, volunteers.Manifest.Groups); err != nil {
		t.Fatalf("privacy.Check: %v", err)
	}
	listed := map[string]bool{}
	for _, v := range volunteers.Manifest.Views {
		listed[typeName(v)] = true
	}
	for _, v := range []any{
		vol.Qualification{}, vol.Record{}, vol.Position{}, vol.Member{}, vol.Shift{}, vol.Signup{},
		vol.ShiftSummary{}, vol.Roster{}, vol.RosterEntry{}, vol.ScheduleEntry{}, vol.MemberView{},
	} {
		if !listed[typeName(v)] {
			t.Errorf("Manifest.Views lacks %s", typeName(v))
		}
	}
}

func TestManifest_GroupsAreTheSpecs(t *testing.T) {
	if len(volunteers.Manifest.Groups) != 2 {
		t.Fatalf("Groups = %d, want shift_mates and legal_name", len(volunteers.Manifest.Groups))
	}
	byName := map[string]privacy.Group{}
	for _, g := range volunteers.Manifest.Groups {
		byName[g.Name] = g
	}
	sm, ok := byName[vol.GroupShiftMates]
	if !ok {
		t.Fatal("no shift_mates group")
	}
	if string(sm.Permission) != volunteers.PermissionSignUp || sm.Default != privacy.AudienceHolders {
		t.Errorf("shift_mates = %+v, want permission shifts.signup and default holders (D43)", sm)
	}
	wantSM := []privacy.Audience{privacy.AudienceNobody, vol.AudienceBooked, privacy.AudienceHolders, privacy.AudienceSignedIn}
	if !sameAudiences(sm.Options, wantSM) {
		t.Errorf("shift_mates options = %v, want %v", sm.Options, wantSM)
	}
	if len(sm.Extra) != 1 || sm.Extra[0] != vol.AudienceBooked {
		t.Errorf("shift_mates extra = %v, want [booked]", sm.Extra)
	}
	ln, ok := byName[vol.GroupLegalName]
	if !ok {
		t.Fatal("no legal_name group")
	}
	if string(ln.Permission) != "people.contacts.read" || ln.Default != privacy.AudienceHolders {
		t.Errorf("legal_name = %+v, want permission people.contacts.read and default holders", ln)
	}
	// Q-VOL8: no, decision 27: "self" is removed; only nobody and holders remain.
	wantLN := []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders}
	if !sameAudiences(ln.Options, wantLN) {
		t.Errorf("legal_name options = %v, want %v (no self, decision 27)", ln.Options, wantLN)
	}
}

func TestManifest_SettingsAreTheSpecs(t *testing.T) {
	type want struct {
		kind settings.Kind
		def  string
	}
	specs := map[string]want{
		volunteers.SettingReminderLead:         {settings.Duration, "24h"},
		volunteers.SettingCancelCutoff:         {settings.Duration, "0"},
		volunteers.SettingExpiryNoticeDays:     {settings.Int, "30"},
		volunteers.SettingSelfUpload:           {settings.Bool, "true"},
		volunteers.SettingVisibilityShiftMates: {settings.Enum, "holders"},
		volunteers.SettingVisibilityLegalName:  {settings.Enum, "holders"},
		volunteers.SettingRetention:            {settings.Duration, "0"},
	}
	got := map[string]settings.Decl{}
	for _, d := range volunteers.Manifest.Settings {
		got[d.Key] = d
	}
	if len(got) != 7 {
		t.Errorf("%d settings declared, want 7", len(got))
	}
	for key, w := range specs {
		d, ok := got[key]
		if !ok {
			t.Errorf("setting %q is not declared", key)
			continue
		}
		if d.Kind != w.kind {
			t.Errorf("setting %q kind = %v, want %v", key, d.Kind, w.kind)
		}
		if d.Default != w.def && !durationEqual(d.Default, w.def) {
			t.Errorf("setting %q default = %q, want %q", key, d.Default, w.def)
		}
	}
}

func TestSettings_OutOfRange_IsRejectedByCheck(t *testing.T) {
	bad := map[string][]string{
		volunteers.SettingReminderLead:     {"30m", "169h", "-1h"},
		volunteers.SettingCancelCutoff:     {"169h", "-1h"},
		volunteers.SettingExpiryNoticeDays: {"-1", "366"},
		volunteers.SettingRetention:        {"1h", "8759h", "876001h"},
	}
	good := map[string][]string{
		volunteers.SettingReminderLead:     {"0", "1h", "168h"},
		volunteers.SettingCancelCutoff:     {"0", "168h"},
		volunteers.SettingExpiryNoticeDays: {"0", "365"},
		volunteers.SettingRetention:        {"0", "8760h", "876000h"},
	}
	byKey := map[string]settings.Decl{}
	for _, d := range volunteers.Manifest.Settings {
		byKey[d.Key] = d
	}
	for key, vals := range bad {
		d, ok := byKey[key]
		if !ok || d.Check == nil {
			t.Errorf("setting %q has no Check", key)
			continue
		}
		for _, v := range vals {
			if err := d.Check(v); err == nil {
				t.Errorf("%s=%q accepted, want a range error", key, v)
			}
		}
	}
	for key, vals := range good {
		d := byKey[key]
		if d.Check == nil {
			continue
		}
		for _, v := range vals {
			if err := d.Check(v); err != nil {
				t.Errorf("%s=%q refused: %v", key, v, err)
			}
		}
	}
}

// F138a (decision 27): visibility_legal_name = self is refused: the option is not offered and
// a value outside the options fails Invalid when read.
func TestSettings_LegalNameSelf_Refused(t *testing.T) {
	var d settings.Decl
	for _, got := range volunteers.Manifest.Settings {
		if got.Key == volunteers.SettingVisibilityLegalName {
			d = got
		}
	}
	if d.Key == "" {
		t.Fatalf("setting %q is not declared", volunteers.SettingVisibilityLegalName)
	}
	if d.Kind != settings.Enum {
		t.Fatalf("kind = %v, want enum", d.Kind)
	}
	if !sameStrings(d.Options, []string{"nobody", "holders"}) {
		t.Errorf("options = %v, want exactly nobody and holders (decision 27)", d.Options)
	}
	if d.Check != nil {
		if err := d.Check("self"); err == nil {
			t.Error(`the Check accepts "self"`)
		}
	}
	fake := settingstest.NewFake(volunteers.Manifest.Name, volunteers.Manifest.Settings)
	for _, ok := range []string{"nobody", "holders"} {
		fake.Set(volunteers.SettingVisibilityLegalName, ok)
		if _, err := fake.Text(ctxT(t), volunteers.SettingVisibilityLegalName); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	fake.Set(volunteers.SettingVisibilityLegalName, "self")
	_, err := fake.Text(ctxT(t), volunteers.SettingVisibilityLegalName)
	wantKind(t, err, errs.Invalid)
}

func sameStrings(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

func TestManifest_PermissionsScopesAndRoles(t *testing.T) {
	byName := map[string]authz.PermissionDecl{}
	for _, p := range volunteers.Manifest.Permissions {
		byName[string(p.Name)] = p
	}
	if len(byName) != 8 {
		t.Errorf("%d permissions, want 8", len(byName))
	}
	for _, name := range []string{
		volunteers.PermissionShiftsManage, volunteers.PermissionRostersRead, volunteers.PermissionPositionsManage,
		volunteers.PermissionMembersManage, volunteers.PermissionQualificationsManage, volunteers.PermissionQualificationsRead,
		volunteers.PermissionQualificationsVerify,
	} {
		p, ok := byName[name]
		if !ok {
			t.Errorf("permission %s is not declared", name)
			continue
		}
		if p.Scope != authz.ScopeDepartment {
			t.Errorf("%s scope = %v, want department", name, p.Scope)
		}
	}
	su, ok := byName[volunteers.PermissionSignUp]
	if !ok || su.Scope != authz.ScopeSite {
		t.Errorf("shifts.signup = %+v, want a site-scoped permission", su)
	}
	if volunteers.PermissionSignUp != "volunteers.shifts.signup" {
		t.Errorf("PermissionSignUp = %q", volunteers.PermissionSignUp)
	}
	// D41: only department leads verify. The verify permission is seeded into department-admin
	// and into no other role, never the participant.
	verify := byName[volunteers.PermissionQualificationsVerify]
	if len(verify.StarterRoles) != 1 || verify.StarterRoles[0] != authz.RoleDepartmentAdmin {
		t.Errorf("qualifications.verify starter roles = %v, want [department-admin] only (D41)", verify.StarterRoles)
	}
	for name, p := range byName {
		if name == volunteers.PermissionSignUp {
			continue
		}
		for _, role := range p.StarterRoles {
			if role == authz.RoleParticipant {
				t.Errorf("%s is seeded into the participant role; no lead permission may be", name)
			}
		}
	}
	if su.StarterRoles == nil || !contains(su.StarterRoles, authz.RoleParticipant) {
		t.Errorf("shifts.signup starter roles = %v, want participant", su.StarterRoles)
	}
	qm := byName[volunteers.PermissionQualificationsManage]
	if !contains(qm.StarterRoles, authz.RoleDepartmentAdmin) || !contains(qm.StarterRoles, authz.RoleDepartmentSteward) {
		t.Errorf("qualifications.manage roles = %v, want department-admin and department-steward (D42)", qm.StarterRoles)
	}
	for _, name := range []string{volunteers.PermissionRostersRead, volunteers.PermissionQualificationsRead} {
		if !contains(byName[name].StarterRoles, authz.RoleVolunteerCoordinator) {
			t.Errorf("%s is not seeded into volunteer-coordinator (D43)", name)
		}
	}
}

func TestManifest_SignalsJobsTemplates(t *testing.T) {
	pub := map[signals.Name]bool{}
	for _, n := range volunteers.Manifest.Publishes {
		pub[n] = true
	}
	for _, n := range []string{volunteers.SignalSignupBooked, volunteers.SignalSignupCanceled, volunteers.SignalQualificationVerified} {
		if !pub[signals.Name(n)] {
			t.Errorf("signal %s is not published", n)
		}
	}
	sub := map[signals.Name]bool{}
	for _, n := range volunteers.Manifest.Subscribes {
		sub[n] = true
	}
	if !sub["departments.department-archived"] || !sub["events.event-closed"] || len(sub) != 2 {
		t.Errorf("Subscribes = %v, want exactly departments.department-archived and events.event-closed", volunteers.Manifest.Subscribes)
	}
	if volunteers.SignalSignupBooked != "volunteers.signup-booked" || volunteers.SignalSignupCanceled != "volunteers.signup-canceled" ||
		volunteers.SignalQualificationVerified != "volunteers.qualification-verified" {
		t.Error("a signal constant differs from the spec")
	}
	if volunteers.Manifest.Templates == nil || volunteers.Manifest.Migrations == nil {
		t.Error("the manifest must carry templates and migrations")
	}
}

// F125, and the Start rules in the spec: two subscriptions, four jobs (V1: cancel-shifts), no writes.
func TestStart_RegistersSubscriptionsAndJobs(t *testing.T) {
	r := newRig(t, opts{})
	handlers := map[signals.Name]bool{}
	for _, n := range r.sig.Handlers() {
		handlers[n] = true
	}
	if !handlers["departments.department-archived"] || !handlers["events.event-closed"] || len(handlers) != 2 {
		t.Errorf("subscribed to %v, want the two signals", r.sig.Handlers())
	}
	byName := map[jobs.Name]jobs.Decl{}
	for _, d := range r.jobs.Registered() {
		byName[d.Name] = d
	}
	if len(byName) != 4 {
		t.Errorf("registered %d jobs, want 4", len(byName))
	}
	cancel, ok := byName[jobs.Name(volunteers.JobCancelShifts)]
	if !ok || cancel.Every != 0 || cancel.Timeout != 5*time.Minute {
		t.Errorf("cancel-shifts = %+v, want an on-demand job (Every 0) with Timeout 5m", cancel)
	}
	reminder, ok := byName[jobs.Name(volunteers.JobShiftReminder)]
	if !ok || reminder.Every != 0 || reminder.Timeout != 30*time.Second {
		t.Errorf("shift reminder = %+v, want a deferred job (Every 0) with Timeout 30s", reminder)
	}
	for _, n := range []string{volunteers.JobExpiryScan, volunteers.JobPurge} {
		d, ok := byName[jobs.Name(n)]
		if !ok || d.Every != 24*time.Hour || d.Timeout != 5*time.Minute {
			t.Errorf("%s = %+v, want Every 24h and Timeout 5m", n, d)
		}
	}
	if volunteers.JobShiftReminder != "volunteers.shift-reminder" || volunteers.JobExpiryScan != "volunteers.expiry-scan" || volunteers.JobPurge != "volunteers.purge" || volunteers.JobCancelShifts != "volunteers.cancel-shifts" {
		t.Error("a job constant differs from the spec")
	}
	fake, ok := r.db.(*dbtest.Fake)
	if !ok {
		t.Fatalf("the rig's DB is %T, want a *dbtest.Fake", r.db)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Errorf("Start issued %d database calls, want none", len(calls))
	}
}

func TestStart_Twice_ReturnsConflict(t *testing.T) {
	r := newRig(t, opts{noStart: true})
	wantNoErr(t, r.mod.Start(ctxT(t)))
	t.Cleanup(func() {
		if err := r.mod.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	wantKind(t, r.mod.Start(ctxT(t)), errs.Conflict)
}

func TestStart_ReadOnlyCopy_Succeeds(t *testing.T) {
	fake := dbtest.NewFake()
	fake.SetReadOnly(true)
	newRig(t, opts{db: fake}) // newRig starts the module and fails the test on error
}

func TestStop_WithoutStart_IsNil(t *testing.T) {
	r := newRig(t, opts{noStart: true})
	wantNoErr(t, r.mod.Stop(ctxT(t)))
}

// ---- common failure modes that need no rows ----

// TestUseCases_NoActor_ReturnUnauthenticated runs every use case with no session.
func TestUseCases_NoActor_ReturnUnauthenticated(t *testing.T) {
	r := newRig(t, opts{})
	ctx := ctxT(t) // no session
	d, p, s := model.NewID[model.DepartmentID](), model.NewID[model.PersonID](), model.NewID[vol.ShiftID]()
	q, rec, pos, su := model.NewID[vol.QualificationID](), model.NewID[vol.RecordID](), model.NewID[vol.PositionID](), model.NewID[vol.SignupID]()
	ev := model.NewID[model.EventID]()
	tests := []struct {
		name string
		call func() error
	}{
		{"ListQualifications", func() error { return errOf(r.svc.ListQualifications(ctx, vol.QualificationQuery{})) }},
		{"CreateQualification", func() error { return errOf(r.svc.CreateQualification(ctx, vol.QualificationInput{Name: "x"})) }},
		{"UpdateQualification", func() error {
			return errOf(r.svc.UpdateQualification(ctx, q, vol.QualificationInput{Name: "x"}))
		}},
		{"ArchiveQualification", func() error { return r.svc.ArchiveQualification(ctx, q) }},
		{"RecordsOf", func() error { return errOf(recordItems(r.svc.RecordsOf(ctx, p))) }},
		{"RecordQualification", func() error {
			return errOf(r.svc.RecordQualification(ctx, vol.RecordInput{Qualification: q, Person: p}))
		}},
		{"UploadCertificate", func() error { return errOf(r.svc.UploadCertificate(ctx, vol.UploadInput{Qualification: q})) }},
		{"VerifyRecord", func() error { return errOf(r.svc.VerifyRecord(ctx, rec, vol.VerifyInput{})) }},
		{"RejectRecord", func() error { return r.svc.RejectRecord(ctx, rec) }},
		{"RemoveRecord", func() error { return r.svc.RemoveRecord(ctx, rec) }},
		{"PendingRecords", func() error { return errOf(r.svc.PendingRecords(ctx, vol.PendingQuery{})) }},
		{"OpenCertificate", func() error { return errOf3(r.svc.OpenCertificate(ctx, rec)) }},
		{"ListPositions", func() error { return errOf(r.svc.ListPositions(ctx, d, false)) }},
		{"CreatePosition", func() error {
			return errOf(r.svc.CreatePosition(ctx, d, vol.PositionInput{Name: "x", Access: vol.AccessOpen}))
		}},
		{"UpdatePosition", func() error {
			return errOf(r.svc.UpdatePosition(ctx, pos, vol.PositionInput{Name: "x", Access: vol.AccessOpen}))
		}},
		{"ArchivePosition", func() error { return r.svc.ArchivePosition(ctx, pos) }},
		{"SetRequirements", func() error { return errOf(r.svc.SetRequirements(ctx, pos, nil)) }},
		{"ListMembers", func() error { return errOf(memberItems(r.svc.ListMembers(ctx, vol.MemberQuery{Department: d}))) }},
		{"AddMember", func() error { return errOf(r.svc.AddMember(ctx, d, p)) }},
		{"RemoveMember", func() error { return r.svc.RemoveMember(ctx, d, p) }},
		{"MyDepartments", func() error { return errOf(r.svc.MyDepartments(ctx)) }},
		{"Leave", func() error { return r.svc.Leave(ctx, d) }},
		{"ListShifts", func() error { return errOf(shiftItems(r.svc.ListShifts(ctx, vol.ShiftQuery{}))) }},
		{"GetShift", func() error { return errOf(r.svc.GetShift(ctx, s)) }},
		{"CreateShift", func() error { return errOf(r.svc.CreateShift(ctx, d, vol.ShiftInput{Position: pos})) }},
		{"UpdateShift", func() error { return errOf(r.svc.UpdateShift(ctx, s, vol.ShiftInput{Position: pos})) }},
		{"CancelShift", func() error { return r.svc.CancelShift(ctx, s) }},
		{"CopyShifts", func() error { return errOf(r.svc.CopyShifts(ctx, d, ev, model.NewID[model.EventID]())) }},
		{"SignUp", func() error { return errOf(r.svc.SignUp(ctx, s)) }},
		{"Assign", func() error { return errOf(r.svc.Assign(ctx, s, vol.AssignInput{Person: p})) }},
		{"CancelSignup", func() error { return r.svc.CancelSignup(ctx, su) }},
		{"Roster", func() error { return errOf(r.svc.Roster(ctx, s)) }},
		{"MySchedule", func() error { return errOf(scheduleItems(r.svc.MySchedule(ctx, vol.ScheduleQuery{}))) }},
		{"Listed", func() error { return errOf(r.svc.Listed(ctx)) }},
		{"SetListed", func() error { return r.svc.SetListed(ctx, false) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantKind(t, tc.call(), errs.Unauthenticated) })
	}
}

// ---- permission rules that are decided before any row is read ----

func TestPermissions_RefusedBeforeAnyRead_Table(t *testing.T) {
	r := newRig(t, opts{})
	d := r.department("Gate")
	other := r.department("Greeters")
	nobody := r.person("")
	leadElsewhere := r.lead(other) // every lead permission, but at another department
	pos, q := model.NewID[vol.PositionID](), model.NewID[vol.QualificationID]()
	_ = pos
	_ = q
	for _, actor := range []struct {
		name string
		p    model.PersonID
	}{{"no permission", nobody}, {"lead of another department", leadElsewhere}} {
		t.Run(actor.name, func(t *testing.T) {
			ctx := r.as(actor.p)
			tests := []struct {
				name string
				call func() error
			}{
				// F3
				{"CreateQualification department", func() error {
					return errOf(r.svc.CreateQualification(ctx, vol.QualificationInput{Department: d, Name: "Radio"}))
				}},
				// F45
				{"CreatePosition", func() error {
					return errOf(r.svc.CreatePosition(ctx, d, vol.PositionInput{Name: "Greeter", Access: vol.AccessOpen}))
				}},
				// F44
				{"ListPositions archived", func() error { return errOf(r.svc.ListPositions(ctx, d, true)) }},
				// F56
				{"ListMembers", func() error { return errOf(memberItems(r.svc.ListMembers(ctx, vol.MemberQuery{Department: d}))) }},
				// F58
				{"AddMember", func() error { return errOf(r.svc.AddMember(ctx, d, nobody)) }},
				// F64
				{"RemoveMember", func() error { return r.svc.RemoveMember(ctx, d, nobody) }},
				// F69
				{"CreateShift", func() error {
					return errOf(r.svc.CreateShift(ctx, d, vol.ShiftInput{Position: pos, Label: "x", Starts: when(0), Ends: when(2), Zone: "UTC", Slots: 1}))
				}},
				// F84
				{"CopyShifts", func() error {
					return errOf(r.svc.CopyShifts(ctx, d, model.NewID[model.EventID](), model.NewID[model.EventID]()))
				}},
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) { wantKind(t, tc.call(), errs.Permission) })
			}
		})
	}
}

// F2: an org-wide entry needs the permission site-wide; a department lead is not enough.
func TestCreateQualification_OrgWideWithoutSitePermission_ReturnsPermission(t *testing.T) {
	r := newRig(t, opts{})
	d := r.department("Rangers")
	lead := r.lead(d)
	_, err := r.svc.CreateQualification(r.as(lead), vol.QualificationInput{Name: "Over 18"})
	wantKind(t, err, errs.Permission)
}

// F89
func TestSignUp_NoPermission_ReturnsPermission(t *testing.T) {
	r := newRig(t, opts{})
	p := r.person("Tumbleweed") // holds no shifts.signup
	_, err := r.svc.SignUp(r.as(p), model.NewID[vol.ShiftID]())
	wantKind(t, err, errs.Permission)
}

// F39
func TestPendingRecords_NoExtent_ReturnsPermission(t *testing.T) {
	r := newRig(t, opts{})
	_, err := r.svc.PendingRecords(r.as(r.person("Tumbleweed")), vol.PendingQuery{})
	wantKind(t, err, errs.Permission)
}

// D41: a participant (someone who may volunteer) can never verify.
func TestVerify_Participant_ReturnsPermission(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Tumbleweed")
	_, err := r.svc.VerifyRecord(r.as(p), model.NewID[vol.RecordID](), vol.VerifyInput{})
	if !errs.Is(err, errs.Permission) && !errs.Is(err, errs.NotFound) {
		t.Fatalf("VerifyRecord by a participant = %v, want Permission (or NotFound for an unknown record)", err)
	}
	_, err = r.svc.RecordQualification(r.as(p), vol.RecordInput{
		Qualification: model.NewID[vol.QualificationID](), Person: r.person(""), AcquiredOn: vol.Date{Year: 2026, Month: 1, Day: 1},
	})
	if !errs.Is(err, errs.Permission) && !errs.Is(err, errs.NotFound) {
		t.Fatalf("RecordQualification by a participant = %v, want Permission or NotFound", err)
	}
}

// ---- writes on a read-only copy (the ones that fail before a row is needed) ----

func TestWrites_ReadOnlyCopy_ReturnUnavailableReadOnly(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, opts{db: fake})
	d := r.department("Rangers")
	member := r.person("Tumbleweed")
	fake.SetReadOnly(true)
	boss := r.as(r.boss)
	tests := []struct {
		name string
		call func() error
	}{
		{"CreateQualification", func() error {
			return errOf(r.svc.CreateQualification(boss, vol.QualificationInput{Department: d, Name: "Radio"}))
		}},
		{"CreatePosition", func() error {
			return errOf(r.svc.CreatePosition(boss, d, vol.PositionInput{Name: "Greeter", Access: vol.AccessOpen}))
		}},
		{"AddMember", func() error { return errOf(r.svc.AddMember(boss, d, member)) }},
		{"SetListed", func() error { return r.svc.SetListed(r.as(member), false) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			wantKind(t, err, errs.Unavailable)
			if !isReadOnly(err) {
				t.Fatalf("error %v does not wrap db.ErrReadOnly", err)
			}
		})
	}
}

// ---- helpers ----

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sameAudiences(a, b []privacy.Audience) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[privacy.Audience]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, x := range b {
		if !seen[x] {
			return false
		}
	}
	return true
}

func durationEqual(a, b string) bool {
	da, ea := time.ParseDuration(a)
	db, eb := time.ParseDuration(b)
	return ea == nil && eb == nil && da == db
}
