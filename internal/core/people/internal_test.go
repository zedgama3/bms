package people

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func ptr(s string) *string { return &s }

func TestCooldown_Take_Table(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	p := model.NewID[model.PersonID]()
	tests := []struct {
		name string
		at   time.Duration
		want bool
	}{
		{"first export", 0, true},
		{"a minute later", time.Minute, false},
		{"just before the cooldown ends", ExportCooldown - time.Nanosecond, false},
		{"when the cooldown ends", ExportCooldown, true},
	}
	c := newCooldown()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.take(p, start.Add(tc.at)); got != tc.want {
				t.Errorf("take at +%v = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestCooldown_Release_AllowsAnImmediateRetry(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	p := model.NewID[model.PersonID]()
	c := newCooldown()
	if !c.take(p, now) {
		t.Fatal("first take refused")
	}
	c.release(p, now)
	if !c.take(p, now) {
		t.Error("take after release refused")
	}
	// A later reservation is not released by an earlier one's failure.
	c.release(p, now.Add(-time.Second))
	if c.take(p, now.Add(time.Second)) {
		t.Error("a stale release removed a newer reservation")
	}
}

func TestCooldown_AtTheBound_EvictsTheOldest(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	c := newCooldown()
	first := model.NewID[model.PersonID]()
	c.take(first, now)
	for range MaxTrackedExports - 1 {
		c.take(model.NewID[model.PersonID](), now.Add(time.Second))
	}
	if len(c.last) != MaxTrackedExports {
		t.Fatalf("tracked %d, want %d", len(c.last), MaxTrackedExports)
	}
	c.take(model.NewID[model.PersonID](), now.Add(2*time.Second))
	if len(c.last) != MaxTrackedExports {
		t.Errorf("tracked %d after the bound, want %d", len(c.last), MaxTrackedExports)
	}
	if _, kept := c.last[first]; kept {
		t.Error("the oldest entry was not evicted")
	}
}

func TestProfileInput_Normalize_Table(t *testing.T) {
	tests := []struct {
		name  string
		in    ProfileInput
		field string // "" means valid
		code  string
	}{
		{"empty input", ProfileInput{}, "", ""},
		{"trims", ProfileInput{PlayaName: ptr("  Dusty Ember  ")}, "", ""},
		{"clearing is allowed", ProfileInput{Pronouns: ptr("")}, "", ""},
		{"playa name at the limit", ProfileInput{PlayaName: ptr(strings.Repeat("x", MaxPlayaNameBytes))}, "", ""},
		{"playa name over the limit", ProfileInput{PlayaName: ptr(strings.Repeat("x", MaxPlayaNameBytes+1))}, "playa_name", "too_long"},
		{"newline in a name", ProfileInput{PlayaName: ptr("Dusty\nEmber")}, "playa_name", "control_chars"},
		{"bad UTF-8", ProfileInput{Pronouns: ptr("\xff")}, "pronouns", "malformed"},
		{"language tag", ProfileInput{Language: ptr("pt-BR")}, "", ""},
		{"language not a tag", ProfileInput{Language: ptr("English")}, "language", "malformed"},
		{"phone with extension", ProfileInput{Phone: ptr("+1 (555) 010-0142 x77")}, "", ""},
		{"phone with letters", ProfileInput{Phone: ptr("call me")}, "phone", "malformed"},
		{"contact e-mail", ProfileInput{ContactEmail: ptr("dusty.ember@example.org")}, "", ""},
		{"contact e-mail malformed", ProfileInput{ContactEmail: ptr("not-an-address")}, "contact_email", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.normalize("test")
			if tc.field == "" {
				if err != nil {
					t.Fatalf("normalize: %v", err)
				}
				if tc.in.PlayaName != nil && got.playaName == nil {
					t.Error("a provided field became unset")
				}
				if got.playaName != nil && *got.playaName != strings.TrimSpace(*tc.in.PlayaName) {
					t.Errorf("playa name = %q, not trimmed", *got.playaName)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid (%v)", errs.KindOf(err), err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				found = found || (fe.Field == tc.field && fe.Code == tc.code)
			}
			if !found {
				t.Errorf("no field error %s/%s in %v", tc.field, tc.code, validate.Fields(err))
			}
		})
	}
}

func TestPatchOf_OnlyChangedFieldsAreNamed(t *testing.T) {
	id := model.NewID[model.PersonID]()
	cur := rows.Person{ID: id, PlayaName: "Dusty Ember", Pronouns: "they/them"}
	f := profileFields{playaName: ptr("Dusty Ember"), pronouns: ptr("she/her"), legalName: ptr("")}
	patch, changed := patchOf(id, cur, f)
	if len(changed) != 1 || changed[0] != FieldPronouns {
		t.Fatalf("changed = %v, want only pronouns", changed)
	}
	if patch.PlayaName != nil || patch.LegalName != nil || patch.Pronouns == nil {
		t.Errorf("patch = %+v", patch)
	}
	if got := merged(cur, patch); got.Pronouns != "she/her" || got.PlayaName != "Dusty Ember" {
		t.Errorf("merged = %+v", got)
	}
}

func TestHighest_PersonalWinsOverInternal(t *testing.T) {
	if got := highest([]string{FieldPlayaName, FieldPronouns}); got != model.SensitivityInternal {
		t.Errorf("internal fields: %v", got)
	}
	if got := highest([]string{FieldPlayaName, FieldPhone}); got != model.SensitivityPersonal {
		t.Errorf("with a phone: %v", got)
	}
}

func TestComplete_Table(t *testing.T) {
	named := rows.Person{PlayaName: "Dusty Ember"}
	tests := []struct {
		name string
		row  rows.Person
		pol  Policy
		want bool
	}{
		{"no playa name", rows.Person{}, DefaultPolicy(), false},
		{"named, nothing required", named, DefaultPolicy(), true},
		{"legal name required and missing", named, Policy{LegalName: FieldRequired, Phone: FieldOptional, EmergencyContacts: FieldOptional}, false},
		{"legal name required and set", rows.Person{PlayaName: "x", LegalName: "y"}, Policy{LegalName: FieldRequired, Phone: FieldOptional, EmergencyContacts: FieldOptional}, true},
		{"phone required and missing", named, Policy{LegalName: FieldOptional, Phone: FieldRequired, EmergencyContacts: FieldOptional}, false},
		{"hidden fields do not count", named, Policy{LegalName: FieldHidden, Phone: FieldHidden, EmergencyContacts: FieldHidden}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := complete(tc.row, tc.pol); got != tc.want {
				t.Errorf("complete = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteReport_OneLinePerPart(t *testing.T) {
	r := DeletionReport{
		Person: model.NewID[model.PersonID](), People: 4, Identity: 1, Access: 2,
		Modules: map[string]ModuleDeletion{
			"hello": {Deleted: 3},
			"alpha": {Anonymized: 1, Kept: []Kept{{Kind: "report", Count: 2}}},
		},
		Kept: []Kept{{Kind: auditEntryKind, Count: 9}},
	}
	var b bytes.Buffer
	if err := writeReport(&b, r); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	want := "person " + r.Person.String() + "\npeople 4\nidentity 1\naccess 2\n" +
		"module alpha deleted=0 anonymized=1\nmodule alpha kept report 2\nmodule hello deleted=3 anonymized=0\n" +
		"kept audit.entry 9\n"
	if got := b.String(); got != want {
		t.Errorf("report:\n%s\nwant:\n%s", got, want)
	}
}

func TestStatusOf_RoundTrips(t *testing.T) {
	for _, st := range []RequestStatus{RequestOpen, RequestWithdrawn, RequestDeclined} {
		if got := statusOf(st.String()); got != st {
			t.Errorf("statusOf(%q) = %v, want %v", st.String(), got, st)
		}
	}
	if statusOf("nonsense") != 0 {
		t.Error("an unknown word has a status")
	}
}

// ---- white-box rig: a service over a recording fake store -------------------------------

// fakeStore implements the part of store a test overrides; calling any other method
// panics on the nil embedded interface, which fails the test loudly.
type fakeStore struct {
	store
	calls []string

	person     rows.Person
	personErr  error // GetPerson, GetForUpdate and ExportPerson answer this when set
	upserted   bool
	orphanGone bool
	exportErr  error // ExportPerson's failure

	markDue    bool  // MarkNamePrompted's answer
	markErr    error // MarkNamePrompted's failure
	marks      int   // how many times MarkNamePrompted ran
	markPerson model.PersonID
	markIssued time.Time // the issued argument of the last mark
	markNow    time.Time // the now argument of the last mark
}

func (f *fakeStore) add(c string) { f.calls = append(f.calls, c) }

func (f *fakeStore) GetPerson(context.Context, model.PersonID) (rows.Person, error) {
	f.add("GetPerson")
	return f.person, f.personErr
}

func (f *fakeStore) MarkNamePrompted(_ context.Context, person model.PersonID, issued, now time.Time) (bool, error) {
	f.add("MarkNamePrompted")
	f.marks++
	f.markPerson, f.markIssued, f.markNow = person, issued, now
	return f.markDue, f.markErr
}

func (f *fakeStore) GetForUpdate(context.Context, model.PersonID) (rows.Person, error) {
	f.add("GetForUpdate")
	return f.person, f.personErr
}

func (f *fakeStore) LockDeletion(context.Context, model.PersonID) error {
	f.add("LockDeletion")
	return nil
}

func (f *fakeStore) Upsert(_ context.Context, p rows.PersonPatch, now time.Time) (rows.Person, bool, error) {
	f.add("Upsert")
	f.upserted = true
	return rows.Person{ID: p.ID, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (f *fakeStore) Contacts(context.Context, model.PersonID) ([]rows.Contact, error) {
	return nil, nil
}
func (f *fakeStore) Consents(context.Context, model.PersonID) ([]rows.Consent, error) {
	return nil, nil
}

func (f *fakeStore) DeleteOrphan(context.Context, model.PersonID) (bool, error) {
	f.add("DeleteOrphan")
	return f.orphanGone, nil
}

func (f *fakeStore) DeleteRequestsOf(context.Context, model.PersonID) (int, error) { return 0, nil }
func (f *fakeStore) DeleteConsentsOf(context.Context, model.PersonID) (int, error) { return 0, nil }
func (f *fakeStore) DeleteContactsOf(context.Context, model.PersonID) (int, error) { return 0, nil }

func (f *fakeStore) DeletePerson(context.Context, model.PersonID) (int, error) {
	f.add("DeletePerson")
	return 1, nil
}

func (f *fakeStore) ExportPerson(context.Context, model.PersonID) (rows.Person, error) {
	f.add("ExportPerson")
	if f.exportErr != nil {
		return rows.Person{}, f.exportErr
	}
	return f.person, nil
}

func (f *fakeStore) ExportContacts(context.Context, model.PersonID) ([]rows.Contact, error) {
	return nil, nil
}

func (f *fakeStore) ExportConsents(context.Context, model.PersonID) ([]rows.Consent, error) {
	return nil, nil
}

func (f *fakeStore) ExportRequests(context.Context, model.PersonID) ([]rows.Request, error) {
	return nil, nil
}

// wbAccounts is an Accounts whose account exists or not.
type wbAccounts struct {
	has      bool
	orphan   model.PersonID
	err      error // LinkAccount's failure
	emailErr error // EmailOf's failure, when set
	calls    []string
}

func (a *wbAccounts) EmailOf(context.Context, model.PersonID) (model.Email, error) {
	if a.emailErr != nil {
		return "", a.emailErr
	}
	if !a.has {
		return "", errs.E("test.EmailOf", errs.NotFound, errors.New("no account"))
	}
	return "dusty.ember@example.org", nil
}

func (a *wbAccounts) LinkAccount(context.Context, model.Email, model.PersonID) (model.AccountID, model.PersonID, error) {
	a.calls = append(a.calls, "LinkAccount")
	return model.NewID[model.AccountID](), a.orphan, a.err
}

func (*wbAccounts) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	return json.RawMessage(`{"account":null}`), nil
}

func (*wbAccounts) DeletePerson(context.Context, model.PersonID) (int, error) { return 1, nil }

type wbAccess struct{}

func (wbAccess) ExportPerson(context.Context, model.PersonID) (json.RawMessage, error) {
	return json.RawMessage(`{"grants":[]}`), nil
}
func (wbAccess) DeletePerson(context.Context, model.PersonID) (int, error) { return 0, nil }

type wbCounter struct{}

func (wbCounter) CountAbout(context.Context, model.PersonID) (int, error) { return 0, nil }

// failingSource is a PersonData whose export fails.
type failingSource struct{ err error }

func (s failingSource) ExportPerson(context.Context, model.PersonID) (Export, error) {
	return Export{}, s.err
}
func (failingSource) DeletePerson(context.Context, model.PersonID) (ModuleDeletion, error) {
	return ModuleDeletion{}, nil
}

type wbRig struct {
	svc      *Service
	st       *fakeStore
	accounts *wbAccounts
	rec      *audittest.Fake
	bus      *signalstest.Fake
	az       *authztest.Fake
	me       model.PersonID
	ctx      context.Context
}

// newWB builds a service for one signed-in actor over the fake store.
func newWB(t *testing.T, grants ...authz.Permission) *wbRig {
	t.Helper()
	me := model.NewID[model.PersonID]()
	c := telemetrytest.New(t)
	rec := audittest.NewFake("people")
	bus, err := signalstest.NewFake("people", PublishedSignals, SubscribedSignals)
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	az := authztest.NewFake()
	az.SetActor(func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: me}, true })
	for _, g := range grants {
		az.Grant(me, g)
	}
	st := &fakeStore{person: rows.Person{ID: me, PlayaName: "Dusty Ember"}}
	acc := &wbAccounts{has: true}
	svc, err := newService(Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("people"), Audit: rec, Signals: bus, Authz: az,
		Actor:    func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: me}, true },
		Session:  identity.SessionFrom,
		Accounts: acc, Access: wbAccess{}, AuditCount: wbCounter{},
	}, st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return &wbRig{svc: svc, st: st, accounts: acc, rec: rec, bus: bus, az: az, me: me, ctx: t.Context()}
}

func (r *wbRig) actions() []string {
	var out []string
	for _, e := range r.rec.Recorded() {
		out = append(out, string(e.Action))
	}
	return out
}

func TestUpdateMe_RepairWithoutAnAccount_DoesNotResurrectThePerson(t *testing.T) {
	r := newWB(t)
	r.st.personErr = errs.E("test", errs.NotFound, errors.New("no row"))
	r.accounts.has = false // a fulfilled deletion removed the account
	_, err := r.svc.UpdateMe(r.ctx, ProfileInput{PlayaName: ptr("Dusty Ember")})
	if !errs.Is(err, errs.Unauthenticated) {
		t.Fatalf("kind = %v (%v), want Unauthenticated", errs.KindOf(err), err)
	}
	if r.st.upserted {
		t.Error("the row was upserted")
	}
	if len(r.rec.Recorded()) != 0 || len(r.bus.Published()) != 0 {
		t.Errorf("recorded %v and published %v for a refused repair", r.actions(), r.bus.Published())
	}
	if want := []string{"GetForUpdate", "LockDeletion"}; !slices.Equal(r.st.calls, want) {
		t.Errorf("store calls = %v, want %v", r.st.calls, want)
	}
}

func TestUpdateMe_RepairWithAnAccount_CreatesTheRowUnderTheDeletionLock(t *testing.T) {
	r := newWB(t)
	r.st.personErr = errs.E("test", errs.NotFound, errors.New("no row"))
	if _, err := r.svc.UpdateMe(r.ctx, ProfileInput{PlayaName: ptr("Dusty Ember")}); err != nil {
		t.Fatalf("UpdateMe: %v", err)
	}
	if want := []string{"GetForUpdate", "LockDeletion", "Upsert"}; !slices.Equal(r.st.calls[:3], want) {
		t.Errorf("store calls = %v, want to begin %v", r.st.calls, want)
	}
	if got := r.actions(); !slices.Equal(got, []string{"people.person.created"}) {
		t.Errorf("recorded %v", got)
	}
	if got := r.bus.Published(); len(got) != 1 || got[0].Name != SignalPersonCreated {
		t.Errorf("published %v", got)
	}
}

func TestUpdateMe_ExistingRow_NeedsNoDeletionLock(t *testing.T) {
	r := newWB(t)
	if _, err := r.svc.UpdateMe(r.ctx, ProfileInput{PlayaName: ptr("Dusty Ember")}); err != nil {
		t.Fatalf("UpdateMe: %v", err)
	}
	if slices.Contains(r.st.calls, "LockDeletion") {
		t.Errorf("an update of an existing row took the deletion lock: %v", r.st.calls)
	}
}

func TestLinkAccount_TakesTheDeletionLockFirst(t *testing.T) {
	r := newWB(t, PermissionProfilesManage)
	target := model.NewID[model.PersonID]()
	r.accounts.has = false
	r.accounts.orphan = model.NewID[model.PersonID]()
	res, err := r.svc.LinkAccount(r.ctx, target, "dusty.ember@example.org")
	if err != nil {
		t.Fatalf("LinkAccount: %v", err)
	}
	if len(r.st.calls) < 2 || r.st.calls[0] != "LockDeletion" || r.st.calls[1] != "GetPerson" {
		t.Errorf("store calls = %v, want LockDeletion then GetPerson first", r.st.calls)
	}
	if res.OrphanDeleted {
		t.Error("the orphan was reported deleted though the store kept it")
	}
}

func TestLinkAccount_DroppedOrphan_IsAuditedAsTheSystemWithALinkReason(t *testing.T) {
	r := newWB(t, PermissionProfilesManage)
	target := model.NewID[model.PersonID]()
	r.accounts.has = false
	r.accounts.orphan = model.NewID[model.PersonID]()
	r.st.orphanGone = true
	res, err := r.svc.LinkAccount(r.ctx, target, "dusty.ember@example.org")
	if err != nil {
		t.Fatalf("LinkAccount: %v", err)
	}
	if !res.OrphanDeleted || res.Orphan != r.accounts.orphan {
		t.Fatalf("result = %+v", res)
	}
	var deleted []audit.Entry
	for _, e := range r.rec.Recorded() {
		if e.Action == actionDeleted {
			deleted = append(deleted, e)
		}
	}
	if len(deleted) != 1 {
		t.Fatalf("%d people.person.deleted entries, want 1 (%v)", len(deleted), r.actions())
	}
	e := deleted[0]
	if e.Actor.Kind != audit.ActorSystem || e.Reason != "link" || e.Subject != r.accounts.orphan ||
		e.Record != audit.RefOf(kindPerson, r.accounts.orphan) {
		t.Errorf("entry = %+v", e)
	}
	var sig bool
	for _, p := range r.bus.Published() {
		sig = sig || (p.Name == SignalPersonDeleted && p.Payload[PayloadPerson] == signals.RecordID(r.accounts.orphan))
	}
	if !sig {
		t.Errorf("people.person-deleted not published for the orphan: %v", r.bus.Published())
	}
}

func TestDelete_LocksThePersonRow(t *testing.T) {
	r := newWB(t, PermissionDataDelete)
	if err := r.svc.Link(nil, nil, nil); err != nil {
		t.Fatalf("Link: %v", err)
	}
	victim := model.NewID[model.PersonID]()
	if _, err := r.svc.Delete(r.ctx, victim); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !slices.Contains(r.st.calls, "GetForUpdate") || slices.Contains(r.st.calls, "GetPerson") {
		t.Errorf("store calls = %v, want GetForUpdate and no plain GetPerson", r.st.calls)
	}
	if i, j := slices.Index(r.st.calls, "GetForUpdate"), slices.Index(r.st.calls, "DeletePerson"); i < 0 || j < i {
		t.Errorf("the row is locked after it is deleted: %v", r.st.calls)
	}
}

// exportTwice exports the actor's own data twice and returns both errors and how many times
// the profile row was read.
func exportTwice(r *wbRig) (reads int, first, second error) {
	_, first = r.svc.Export(r.ctx, r.me)
	_, second = r.svc.Export(r.ctx, r.me)
	return strings.Count(strings.Join(r.st.calls, ","), "ExportPerson"), first, second
}

func TestExport_FailureBeforeTheAuditEntry_ReleasesTheCooldown(t *testing.T) {
	r := newWB(t)
	if err := r.svc.Link(nil, nil, nil); err != nil {
		t.Fatalf("Link: %v", err)
	}
	r.st.exportErr = errs.E("test", errs.Unavailable, errors.New("down"))
	reads, first, second := exportTwice(r)
	if !errs.Is(first, errs.Unavailable) || !errs.Is(second, errs.Unavailable) || errors.Is(second, httpx.ErrRateLimited) {
		t.Fatalf("errors = %v, %v; want the store's twice", first, second)
	}
	if reads != 2 {
		t.Errorf("the second export did not run (%d reads)", reads)
	}
}

// A save by a deleted person is Unauthenticated whatever the body says.
func TestUpdateMe_RepairWithoutAnAccount_IsUnauthenticatedEvenForAnInvalidBody(t *testing.T) {
	r := newWB(t)
	r.st.personErr = errs.E("test", errs.NotFound, errors.New("no row"))
	r.accounts.has = false
	_, err := r.svc.UpdateMe(r.ctx, ProfileInput{Pronouns: ptr("they/them")}) // no playa name: Invalid for a live person
	if !errs.Is(err, errs.Unauthenticated) {
		t.Fatalf("kind = %v (%v), want Unauthenticated", errs.KindOf(err), err)
	}
}

func TestExport_FailureAfterTheAuditEntry_KeepsTheCooldown(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Canceled} {
		t.Run(kind.String(), func(t *testing.T) {
			r := newWB(t)
			bad := failingSource{err: errs.E("test", kind, errors.New("down"))}
			if err := r.svc.Link([]Source{{Name: "gamma", Exporter: bad}}, nil, nil); err != nil {
				t.Fatalf("Link: %v", err)
			}
			_, first, second := exportTwice(r)
			if !errs.Is(first, kind) {
				t.Fatalf("first error = %v, want kind %v", first, kind)
			}
			if !errors.Is(second, httpx.ErrRateLimited) {
				t.Errorf("second error = %v, want the rate limit", second)
			}
			if got := r.actions(); len(got) != 1 || got[0] != "people.person.exported" {
				t.Errorf("recorded %v", got)
			}
		})
	}
}

func TestExport_CanceledBeforeTheAuditEntry_KeepsTheCooldown(t *testing.T) {
	r := newWB(t)
	if err := r.svc.Link(nil, nil, nil); err != nil {
		t.Fatalf("Link: %v", err)
	}
	r.st.exportErr = errs.E("test", errs.Canceled, errors.New("gone"))
	_, first, second := exportTwice(r)
	if !errs.Is(first, errs.Canceled) {
		t.Fatalf("first error = %v, want Canceled", first)
	}
	if !errors.Is(second, httpx.ErrRateLimited) {
		t.Errorf("second error = %v, want the rate limit", second)
	}
}

// sessionCtx is the rig's ctx carrying a session issued at issued (what identity.SessionFrom
// hands Me).
func (r *wbRig) sessionCtx(issued time.Time) context.Context {
	return identity.WithSession(r.ctx, identity.Session{
		ID: model.NewID[identity.SessionID](), Account: model.NewID[model.AccountID](), Person: r.me,
		IssuedAt: issued, ExpiresAt: issued.Add(time.Hour),
	})
}

func TestPromptName_MarkReceivesTheSessionIssueTimeAndTheClock(t *testing.T) {
	r := newWB(t)
	r.st.person = rows.Person{ID: r.me}
	r.st.markDue = true
	issued := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
	prof, err := r.svc.Me(r.sessionCtx(issued))
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if !prof.FirstPrompt {
		t.Error("FirstPrompt = false, want true when the mark updated the row")
	}
	if r.st.marks != 1 || r.st.markPerson != r.me {
		t.Errorf("marks = %d for %v, want one for the actor", r.st.marks, r.st.markPerson)
	}
	if !r.st.markIssued.Equal(issued) {
		t.Errorf("issued = %v, want the session's %v", r.st.markIssued, issued)
	}
	if want := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC); !r.st.markNow.Equal(want) {
		t.Errorf("now = %v, want the clock's %v", r.st.markNow, want)
	}
}

func TestPromptName_StatementDecides_FalseWhenNothingUpdated(t *testing.T) {
	r := newWB(t)
	r.st.person = rows.Person{ID: r.me}
	r.st.markDue = false
	prof, err := r.svc.Me(r.sessionCtx(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if prof.FirstPrompt {
		t.Error("FirstPrompt = true although the statement updated no row")
	}
}

func TestPromptName_NoSessionForAnActor_IsInternal(t *testing.T) {
	r := newWB(t)
	r.st.person = rows.Person{ID: r.me}
	_, err := r.svc.Me(r.ctx) // the rig's actor func answers without a session in ctx
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("kind = %v (%v), want Internal", errs.KindOf(err), err)
	}
	if r.st.marks != 0 {
		t.Errorf("marks = %d, want 0", r.st.marks)
	}
}

// modeSpy counts the transactions a use case opens, by mode.
type modeSpy struct {
	*dbtest.Fake
	modes []db.Mode
}

func (m *modeSpy) Transact(ctx context.Context, mode db.Mode, fn func(ctx context.Context) error) error {
	m.modes = append(m.modes, mode)
	return m.Fake.Transact(ctx, mode, fn) //nolint:wrapcheck // a test spy
}

func TestPromptName_BuildFails_LeavesNoMarkSoTheNextReadPrompts(t *testing.T) {
	r := newWB(t)
	r.st.person = rows.Person{ID: r.me}
	r.st.markDue = true
	ctx := r.sessionCtx(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC))

	r.accounts.emailErr = errs.E("test.EmailOf", errs.Unavailable, errors.New("down"))
	if _, err := r.svc.Me(ctx); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("kind = %v (%v), want Unavailable from the build", errs.KindOf(err), err)
	}
	if r.st.marks != 0 {
		t.Fatalf("marks = %d after a failed build, want 0", r.st.marks)
	}

	r.accounts.emailErr = nil
	prof, err := r.svc.Me(ctx)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if !prof.FirstPrompt || r.st.marks != 1 {
		t.Errorf("FirstPrompt = %v with %d marks, want the next read to prompt once", prof.FirstPrompt, r.st.marks)
	}
}

func TestPromptName_MarkRunsInAWriteTransactionOfItsOwn(t *testing.T) {
	r := newWB(t)
	r.st.person = rows.Person{ID: r.me}
	r.st.markDue = true
	spy := &modeSpy{Fake: dbtest.NewFake()}
	r.svc.db = spy
	if _, err := r.svc.Me(r.sessionCtx(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if !slices.Equal(spy.modes, []db.Mode{db.Write}) {
		t.Errorf("transactions = %v, want exactly one Write", spy.modes)
	}
}

func TestWireProfileOf_FirstPrompt_OnlyWhenTrue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first bool
		want  bool
	}{
		{"prompted", true, true},
		{"not prompted", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(wireProfileOf(Profile{FirstPrompt: tc.first}))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			v, present := m["first_prompt"]
			if present != tc.want || (present && v != true) {
				t.Errorf("first_prompt = %v (present %v), want present %v", v, present, tc.want)
			}
		})
	}
}
