//go:build integration

package demo_test

// Integration tests of core/demo against a real, throw-away PostgreSQL database (BMS_TEST_DB_DSN):
// the marker rules, the seed through fake seams that act as the demo accounts, the reset,
// the guard's store and own-only captured mail. plan/design/demo.md (D53).

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // test double implements db.DB
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/demo"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var coreSets = []db.Set{identity.Set, people.Set, authz.Set, demo.Set}

func migrate(t testing.TB, pool *db.Pool) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), coreSets)
	wantNoErr(t, err)
}

// irig is a rig over a migrated pool; the Accounts fake writes the rows the foreign keys need.
type irig struct {
	*rig
	pool *db.Pool
}

func newIRig(t *testing.T, mods ...func(*demo.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	r := newRig(t, pool, mods...)
	r.accounts.rows = pool
	return &irig{rig: r, pool: pool}
}

func (r *irig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), sql, args...).Scan(&n); err != nil {
		r.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (r *irig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("%s: %v", sql, err)
	}
}

func (r *irig) markerRows() int { return r.count("SELECT count(*) FROM core.demo_state") }
func (r *irig) seededAtSet() bool {
	return r.count("SELECT count(*) FROM core.demo_state WHERE seeded_at IS NOT NULL") == 1
}

func (r *irig) insertMarker(seeded bool) {
	r.t.Helper()
	if seeded {
		r.exec("INSERT INTO core.demo_state (id, seed_version, instance, started_at, seeded_at) VALUES (1, 1, 'test', now(), now())")
	} else {
		r.exec("INSERT INTO core.demo_state (id, seed_version, instance, started_at) VALUES (1, 1, 'test', now())")
	}
}

// insertAccount makes a person and an account row directly (as a sign-up would).
func (r *irig) insertAccount(email string, superuser bool) (model.AccountID, model.PersonID) {
	r.t.Helper()
	a, p := model.NewID[model.AccountID](), model.NewID[model.PersonID]()
	r.exec("INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(p))
	r.exec(`INSERT INTO core.account (id, person_id, email, password_hash, superuser, password_changed_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'x', $4, now(), now(), now())`, [16]byte(a), [16]byte(p), email, superuser)
	return a, p
}

// ---- Protected (F25, F26) ---------------------------------------------------------------

func TestProtected_Unknown_False(t *testing.T) {
	r := newIRig(t)
	ok, err := r.svc.Protected(ctxT(t), model.NewID[model.AccountID]())
	wantNoErr(t, err)
	if ok {
		t.Error("an unknown account is protected")
	}
}

func TestProtected_DemoAccountTrue_OthersFalse(t *testing.T) {
	r := newIRig(t)
	demoAcc, demoPerson := r.insertAccount("participant@demo.invalid", false)
	other, _ := r.insertAccount("tester.one@demo.invalid", false)
	r.exec("INSERT INTO core.demo_account (account_id, person_id, key, email, position) VALUES ($1, $2, 'participant', 'participant@demo.invalid', 0)",
		[16]byte(demoAcc), [16]byte(demoPerson))
	ok, err := r.svc.Protected(ctxT(t), demoAcc)
	wantNoErr(t, err)
	if !ok {
		t.Error("a demo account is not protected")
	}
	ok, err = r.svc.Protected(ctxT(t), other)
	wantNoErr(t, err)
	if ok {
		t.Error("a self-signed-up tester is protected")
	}
	// The foreign keys are the backstop: a direct delete cannot remove a demo account.
	if _, err := r.pool.Exec(ctxT(t), "DELETE FROM core.account WHERE id = $1", [16]byte(demoAcc)); err == nil {
		t.Error("a protected account's row was deleted directly")
	}
	if _, err := r.pool.Exec(ctxT(t), "DELETE FROM core.person WHERE id = $1", [16]byte(demoPerson)); err == nil {
		t.Error("a protected account's person row was deleted directly")
	}
}

func TestProtected_WorksOnAReadOnlyCopy(t *testing.T) {
	r := newIRig(t)
	if _, err := r.pool.Exec(ctxT(t), "SELECT 1"); err != nil {
		t.Fatalf("warm the pool: %v", err)
	}
	ro := newRig(t, readOnlyDB{r.pool})
	if _, err := ro.svc.Protected(ctxT(t), model.NewID[model.AccountID]()); err != nil {
		t.Fatalf("Protected on a read-only copy: %v", err)
	}
}

// readOnlyDB reports read-only and refuses writes, but reads.
type readOnlyDB struct{ *db.Pool }

func (readOnlyDB) ReadOnly() bool { return true }

// ---- mail capture and own-only ListMail (F27, F31) ---------------------------------------

func (r *irig) send(to, subject string) {
	r.t.Helper()
	e := goodEnvelope()
	e.To = model.Email(to)
	e.Subject = subject
	e.MessageID = fmt.Sprintf("m%d@demo.invalid", r.clk.Now().UnixNano())
	wantNoErr(r.t, r.svc.Channel().Send(ctxT(r.t), e))
	wantNoErr(r.t, r.clk.Advance(time.Second))
}

func (r *irig) mailRows() int { return r.count("SELECT count(*) FROM core.demo_mail") }

func TestCapture_Send_StoresTheEnvelope(t *testing.T) {
	r := newIRig(t)
	r.send("tester.one@demo.invalid", "Hello")
	if r.mailRows() != 1 {
		t.Fatalf("%d rows, want 1", r.mailRows())
	}
	if r.count("SELECT count(*) FROM core.demo_mail WHERE lower(to_address) = 'tester.one@demo.invalid' AND subject = 'Hello'") != 1 {
		t.Error("the row does not hold the envelope's address and subject")
	}
}

func TestCapture_TrimsToMaxMail_KeepingTheNewest(t *testing.T) {
	r := newIRig(t)
	total := demo.MaxMail + 7
	for i := range total {
		r.send("tester.one@demo.invalid", fmt.Sprintf("Message %03d", i))
	}
	if got := r.mailRows(); got != demo.MaxMail {
		t.Fatalf("%d rows kept, want %d", got, demo.MaxMail)
	}
	if r.count("SELECT count(*) FROM core.demo_mail WHERE subject = $1", "Message 000") != 0 {
		t.Error("the oldest message survived the trim")
	}
	if r.count("SELECT count(*) FROM core.demo_mail WHERE subject = $1", fmt.Sprintf("Message %03d", total-1)) != 1 {
		t.Error("the newest message was trimmed")
	}
}

func TestListMail_OtherAccountsMail_NotListed(t *testing.T) {
	r := newIRig(t)
	ctxA, pA := viewer(t, r.rig, "tester.one@demo.invalid")
	ctxB, _ := viewer(t, r.rig, "tester.two@demo.invalid")
	ctxPart, _ := viewer(t, r.rig, "participant@demo.invalid") // a demo account
	// A superuser and an account-admin are viewers like any other.
	pSu := model.NewID[model.PersonID]()
	r.addresses.set(pSu, "root.person@demo.invalid")
	ctxSu := asPerson(ctxT(t), pSu, true)
	ctxAdmin, _ := viewer(t, r.rig, "account-admin@demo.invalid")
	_ = pA

	r.send("tester.one@demo.invalid", "For one A")
	r.send("Tester.One@Demo.Invalid", "For one B") // case-insensitive match
	r.send("tester.two@demo.invalid", "For two")
	r.send("participant@demo.invalid", "For the participant")
	r.send("account-admin@demo.invalid", "For the admin")
	r.send("root.person@demo.invalid", "For the superuser")

	subjects := func(ctx context.Context, q demo.MailQuery) []string {
		t.Helper()
		got, err := r.svc.ListMail(ctx, q)
		wantNoErr(t, err)
		var out []string
		for _, m := range got {
			out = append(out, m.Subject)
		}
		return out
	}
	eq := func(name string, got []string, want ...string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s sees %v, want %v", name, got, want)
		}
	}
	eq("tester one (newest first)", subjects(ctxA, demo.MailQuery{}), "For one B", "For one A")
	eq("tester two", subjects(ctxB, demo.MailQuery{}), "For two")
	eq("a demo account", subjects(ctxPart, demo.MailQuery{}), "For the participant")
	eq("the account-admin", subjects(ctxAdmin, demo.MailQuery{}), "For the admin")
	eq("the superuser", subjects(ctxSu, demo.MailQuery{}), "For the superuser")
	eq("limit 1", subjects(ctxA, demo.MailQuery{Limit: 1}), "For one B")

	// A viewer whose address has no mail sees nothing, and one with no account sees nothing.
	ctxNone, _ := viewer(t, r.rig, "nobody.here@demo.invalid")
	eq("a viewer with no mail", subjects(ctxNone, demo.MailQuery{}))
	ctxNoAcct, _ := viewer(t, r.rig, "")
	eq("a viewer with no account", subjects(ctxNoAcct, demo.MailQuery{}))

	// Reads are not audited.
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("%d audit entries from reading one's own mail, want none", n)
	}
}

func TestListMail_ReturnedFieldsAreComplete(t *testing.T) {
	r := newIRig(t)
	ctx, _ := viewer(t, r.rig, "tester.one@demo.invalid")
	r.send("tester.one@demo.invalid", "Complete")
	got, err := r.svc.ListMail(ctx, demo.MailQuery{})
	wantNoErr(t, err)
	if len(got) != 1 {
		t.Fatalf("%d messages", len(got))
	}
	m := got[0]
	if m.ID.IsZero() || m.To != "tester.one@demo.invalid" || m.Subject != "Complete" || m.Body == "" || m.CapturedAt.IsZero() {
		t.Errorf("mail = %+v", m)
	}
}

// ---- Seed: marker rules (F7, F8, F10) ------------------------------------------------------

func TestSeed_AlreadySeeded_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	r.insertMarker(true)
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Conflict)
	assertCode(t, err, "seeded")
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls after the marker refusal", n)
	}
	if r.markerRows() != 1 {
		t.Error("the marker row changed")
	}
}

func TestSeed_UnfinishedMarker_AlsoRefused(t *testing.T) { // a failed seed leaves the marker; reset is the recovery
	r := newIRig(t)
	r.insertMarker(false)
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Conflict)
	assertCode(t, err, "seeded")
}

func TestSeed_NonEmptyDatabase_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	r.insertAccount("tester.one@demo.invalid", false)
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Conflict)
	assertCode(t, err, "not_empty")
	if r.markerRows() != 0 {
		t.Error("a refused seed left a marker (the check and the insert are one transaction)")
	}
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls after the refusal", n)
	}
}

func TestSeed_OnlyASuperuser_IsStillEmpty(t *testing.T) {
	r := newIRig(t)
	r.insertAccount("root.person@example.org", true)
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantNoErr(t, err)
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	for _, f := range validate.Fields(err) {
		if f.Code == code {
			return
		}
	}
	t.Fatalf("no field error with code %q in %v", code, validate.Fields(err))
}

func TestSeed_SuperuserAddress_FailsWithStep(t *testing.T) {
	r := newIRig(t)
	r.accounts.err = func(email string) error {
		if email == "lead@demo.invalid" {
			return errs.E("identity.EnsureAccount", errs.Conflict, errors.New("superuser address"))
		}
		return nil
	}
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Conflict)
	if r.markerRows() != 1 || r.seededAtSet() {
		t.Error("the marker must stay in 'seeding' after a failed step")
	}
	// Reset is the recovery: a second Seed meets the marker.
	_, err = r.svc.Seed(ctxT(t), &strings.Builder{})
	assertCode(t, err, "seeded")
}

// ---- Seed: the happy path through fake seams ----------------------------------------------

func seedOK(t *testing.T, r *irig) (demo.Report, string) {
	t.Helper()
	var out strings.Builder
	rep, err := r.svc.Seed(ctxT(t), &out)
	wantNoErr(t, err)
	return rep, out.String()
}

func TestSeed_BuildsTheFictionalRegional(t *testing.T) {
	prov := &fakeProvider{}
	r := newIRig(t)
	r.link(demo.Contributor{Module: "alpha", Provider: prov})
	rep, out := seedOK(t, r)

	want := []string{"accounts", "profiles", "grants", "departments", "event", "settings", "walk-ons", "module:alpha"}
	if fmt.Sprint(stepNames(rep)) != fmt.Sprint(want) {
		t.Fatalf("steps = %v, want %v", stepNames(rep), want)
	}
	for _, s := range rep.Steps {
		if s.Items <= 0 {
			t.Errorf("step %s reports %d items", s.Name, s.Items)
		}
	}
	if !r.seededAtSet() {
		t.Error("seeded_at is not set after a successful seed")
	}
	if got := r.count("SELECT count(*) FROM core.demo_account"); got != 10 {
		t.Errorf("%d demo accounts, want 10", got)
	}
	// The printed report: each step, the ten addresses and the password, and nothing else personal.
	for _, k := range actorKeys {
		if !strings.Contains(out, k+"@demo.invalid") {
			t.Errorf("the account table lacks %s", k)
		}
	}
	if !strings.Contains(out, demo.Password) {
		t.Error("the output lacks the shared password")
	}
	for _, s := range []string{"accounts", "walk-ons"} {
		if !strings.Contains(out, s) {
			t.Errorf("the output lacks the step %s", s)
		}
	}
	// The accounts are created with the shared password at <key>@demo.invalid, in order.
	ens := r.log.byMethod("accounts", "EnsureAccount")
	if len(ens) != 10 {
		t.Fatalf("%d EnsureAccount calls, want 10", len(ens))
	}
	for i, k := range actorKeys {
		if ens[i].Detail != k+"@demo.invalid|"+demo.Password {
			t.Errorf("EnsureAccount %d = %q, want key %s", i, ens[i].Detail, k)
		}
	}
	// The one actor-less grant is access-admin's own.
	if len(r.access.ensured) != 1 || r.access.roleKey(r.access.ensured[0].Role) != authz.RoleAccessAdmin {
		t.Errorf("EnsureGrant calls = %+v, want exactly access-admin", r.access.ensured)
	}
	// The audit entry: operator, command.
	var found bool
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == "demo.seed.ran" {
			found = true
			if e.Actor.Kind != audit.ActorOperator || e.Reason != "command" {
				t.Errorf("demo.seed.ran = %+v, want ActorOperator and Reason command", e)
			}
		}
	}
	if !found {
		t.Error("demo.seed.ran was not recorded")
	}
}

func personsByKey(t *testing.T, r *irig) map[string]model.PersonID {
	t.Helper()
	out := map[string]model.PersonID{}
	for k, res := range r.accounts.byEm {
		out[strings.TrimSuffix(k, "@demo.invalid")] = res.Person
	}
	return out
}

func TestSeed_StepsRunAsTheRightActor(t *testing.T) {
	r := newIRig(t)
	seedOK(t, r)
	who := personsByKey(t, r)
	if len(who) != 10 {
		t.Fatalf("%d accounts", len(who))
	}
	only := func(seam, method string, want model.PersonID, n int) {
		t.Helper()
		cs := r.log.byMethod(seam, method)
		if n >= 0 && len(cs) != n {
			t.Errorf("%s.%s called %d times, want %d", seam, method, len(cs), n)
		}
		for _, c := range cs {
			if c.Person != want {
				t.Errorf("%s.%s ran as %v, want %v", seam, method, c.Person, want)
			}
		}
	}
	only("departments", "Create", who[demoseed.ActorSteward], 6)
	only("events", "Create", who[demoseed.ActorEventAdmin], 1)
	only("events", "AddScheduleItem", who[demoseed.ActorEventAdmin], 4)
	only("settings", "Set", who[demoseed.ActorSettingsAdmin], -1)
	only("profiles", "Create", who[demoseed.ActorAccountAdmin], 12)
	only("access", "Grant", who[demoseed.ActorAccessAdmin], 11) // the lead at five departments (D1.1)
	// EnsureAccount and EnsureGrant are the actor-less bootstrap.
	only("accounts", "EnsureAccount", model.PersonID{}, 10)
	only("access", "EnsureGrant", model.PersonID{}, 1)
	// UpdateMe runs as each account itself, and the newcomer is left with an empty profile.
	updates := r.log.byMethod("profiles", "UpdateMe")
	if len(updates) != 9 {
		t.Fatalf("%d UpdateMe calls, want 9 (not the newcomer)", len(updates))
	}
	seen := map[model.PersonID]bool{}
	for _, c := range updates {
		seen[c.Person] = true
		if c.Person == who[demoseed.ActorNewcomer] {
			t.Error("the newcomer's profile was filled in")
		}
	}
	if len(seen) != 9 {
		t.Errorf("UpdateMe ran as %d distinct people, want 9", len(seen))
	}
	// Granted roles: six by access-admin plus the lead's department-admin at Rangers.
	var roles []string
	for _, g := range r.access.grants {
		roles = append(roles, r.access.roleKey(g.Role))
	}
	for _, want := range []string{authz.RoleDepartmentSteward, authz.RoleEventAdmin, authz.RoleSettingsAdmin, authz.RoleAccountAdmin,
		authz.RoleAuditReviewer, authz.RoleVolunteerCoordinator, authz.RoleDepartmentAdmin} {
		if !contains(roles, want) {
			t.Errorf("no grant of %s in %v", want, roles)
		}
	}
	for _, g := range r.access.grants {
		if r.access.roleKey(g.Role) == authz.RoleDepartmentAdmin {
			if g.Person != who[demoseed.ActorLead] || g.Department.IsZero() {
				t.Errorf("department-admin grant = %+v, want the lead at a department", g)
			}
		} else if g.Person == who[demoseed.ActorAccessAdmin] {
			t.Error("access-admin granted itself a role (the self-grant rule keeps it to its one role)")
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestSeed_DepartmentsEventAndSettings_FollowTheSpec(t *testing.T) {
	r := newIRig(t)
	seedOK(t, r)
	names := map[string]string{}
	for _, d := range r.departments.in {
		names[d.Name] = d.ContactEmail
	}
	for _, n := range []string{"Rangers", "Gate", "Greeters", "Medical", "DPW", "Sanctuary"} {
		email, ok := names[n]
		if !ok {
			t.Errorf("no department %s", n)
			continue
		}
		if email != strings.ToLower(n)+"@demo.invalid" {
			t.Errorf("%s contact = %q", n, email)
		}
	}
	// The event: Thursday 10:00 to Monday 12:00 in Zone, first Thursday >= 21 days after the seed day.
	if len(r.events.created) != 1 {
		t.Fatalf("%d events", len(r.events.created))
	}
	ev := r.events.created[0]
	loc, err := time.LoadLocation(demo.Zone)
	wantNoErr(t, err)
	start, end := ev.Dates.Start.In(loc), ev.Dates.End.In(loc)
	if start.Weekday() != time.Thursday || start.Hour() != 10 || start.Minute() != 0 {
		t.Errorf("start = %v, want a Thursday at 10:00", start)
	}
	if end.Weekday() != time.Monday || end.Hour() != 12 || end.Minute() != 0 {
		t.Errorf("end = %v, want a Monday at 12:00", end)
	}
	day := time.Date(seedDay.In(loc).Year(), seedDay.In(loc).Month(), seedDay.In(loc).Day(), 0, 0, 0, 0, loc)
	earliest := day.AddDate(0, 0, 21)
	if start.Before(earliest) || start.After(earliest.AddDate(0, 0, 7)) {
		t.Errorf("start %v is not the first Thursday on or after %v", start, earliest)
	}
	if ev.Zone != demo.Zone || ev.Kind != "burn" || ev.Name != fmt.Sprintf("Ember Flats %d", start.Year()) {
		t.Errorf("event = %+v", ev)
	}
	if err := ev.Dates.Validate(); err != nil {
		t.Errorf("event dates do not validate: %v", err)
	}
	keys := map[string]bool{}
	for _, s := range r.events.schedule {
		keys[s.Key] = true
		if err := s.Validate(); err != nil {
			t.Errorf("schedule item %s does not validate: %v", s.Key, err)
		}
	}
	for _, k := range []string{"gate-open", "effigy-burn", "temple-burn", "exodus"} {
		if !keys[k] {
			t.Errorf("no schedule item %s", k)
		}
	}
	vals := r.settings.values()
	wantVals := map[string]string{
		"regional.name": demo.Regional, "regional.language": "en", "regional.timezone": demo.Zone,
		"notify.smtp_host": "demo.invalid", "notify.sender_address": "noreply@demo.invalid", "notify.sender_name": "Ember Flats (demo)",
	}
	for k, v := range wantVals {
		if vals[k] != v {
			t.Errorf("setting %s = %q, want %q", k, vals[k], v)
		}
	}
	if _, set := vals["regional.public_url"]; set {
		t.Error("regional.public_url was set although PublicURL is empty")
	}
}

func TestSeed_PublicURL_WrittenWhenSet(t *testing.T) {
	r := newIRig(t, func(o *demo.Options) { o.PublicURL = "https://demo.example.org" })
	seedOK(t, r)
	if got := r.settings.values()["regional.public_url"]; got != "https://demo.example.org" {
		t.Errorf("regional.public_url = %q", got)
	}
}

var phoneRE = regexp.MustCompile(`^\+1 555 01\d\d$`)

func TestSeed_NoRealLookingData(t *testing.T) {
	r := newIRig(t)
	seedOK(t, r)
	for _, c := range r.log.byMethod("accounts", "EnsureAccount") {
		if !strings.HasSuffix(strings.SplitN(c.Detail, "|", 2)[0], "@demo.invalid") {
			t.Errorf("account address %q is not at demo.invalid", c.Detail)
		}
	}
	for _, d := range r.departments.in {
		if !strings.HasSuffix(d.ContactEmail, "@demo.invalid") {
			t.Errorf("department contact %q", d.ContactEmail)
		}
	}
	all := append(append([]people.ProfileInput(nil), r.profiles.updates...), r.profiles.creates...)
	if len(all) != 9+12 {
		t.Fatalf("%d profiles, want 21", len(all))
	}
	phones, legal := 0, 0
	for _, p := range all {
		if p.Phone != nil && *p.Phone != "" {
			phones++
			if !phoneRE.MatchString(*p.Phone) {
				t.Errorf("phone %q is not +1 555 01xx", *p.Phone)
			}
		}
		if p.LegalName != nil && *p.LegalName != "" {
			legal++
		}
		if p.ContactEmail != nil && *p.ContactEmail != "" {
			t.Errorf("walk-ons and demo profiles have no contact e-mail, got %q", *p.ContactEmail)
		}
		if p.PlayaName == nil || *p.PlayaName == "" {
			t.Error("a seeded profile has no playa name")
		}
	}
	// Four named demo accounts and six walk-ons have a legal name and phone.
	if phones != 10 || legal != 10 {
		t.Errorf("%d phones and %d legal names, want 10 each (4 accounts, 6 walk-ons)", phones, legal)
	}
	for _, p := range r.profiles.creates {
		if p.ContactEmail != nil && *p.ContactEmail != "" {
			t.Error("a walk-on has a contact e-mail")
		}
	}
}

func TestSeed_Deterministic(t *testing.T) {
	summarise := func() string {
		r := newIRig(t)
		seedOK(t, r)
		var b strings.Builder
		for _, c := range r.log.all() {
			fmt.Fprintf(&b, "%s.%s|%s\n", c.Seam, c.Method, c.Detail)
		}
		for _, d := range r.departments.in {
			fmt.Fprintf(&b, "%+v\n", d)
		}
		for _, s := range r.events.schedule {
			fmt.Fprintf(&b, "%+v\n", s)
		}
		fmt.Fprintf(&b, "%+v\n", r.events.created)
		fmt.Fprintf(&b, "%v\n", r.settings.values())
		return b.String()
	}
	if a, b := summarise(), summarise(); a != b {
		t.Errorf("two seeds on the same day differ:\n--- first\n%s\n--- second\n%s", a, b)
	}
}

func TestSeed_ContributorsGetTheRequest(t *testing.T) {
	prov := &fakeProvider{}
	prov.fn = func(ctx context.Context, req demoseed.Request) (demoseed.Result, error) {
		if req.Event.IsZero() {
			return demoseed.Result{}, errors.New("no event")
		}
		if len(req.Departments) != 6 {
			return demoseed.Result{}, fmt.Errorf("%d departments", len(req.Departments))
		}
		for _, k := range deptKeys {
			if req.Departments[k].IsZero() {
				return demoseed.Result{}, fmt.Errorf("department %s missing", k)
			}
		}
		if len(req.People) != 22 {
			return demoseed.Result{}, fmt.Errorf("%d people, want 22", len(req.People))
		}
		for _, k := range actorKeys {
			if req.People[k].IsZero() {
				return demoseed.Result{}, fmt.Errorf("person %s missing", k)
			}
		}
		for n := 1; n <= demoseed.WalkOns; n++ {
			if req.People[demoseed.WalkOnKey(n)].IsZero() {
				return demoseed.Result{}, fmt.Errorf("walk-on %d missing", n)
			}
		}
		actx, err := req.Actors.As(ctx, demoseed.ActorLead)
		if err != nil {
			return demoseed.Result{}, fmt.Errorf("as lead: %w", err)
		}
		p, ok := identity.PersonFrom(actx)
		if !ok || p != req.People[demoseed.ActorLead] {
			return demoseed.Result{}, errors.New("As(lead) does not carry the lead's session")
		}
		if s, _ := identity.SessionFrom(actx); s.ExpiresAt.Sub(s.IssuedAt) != demo.SessionLifetime {
			return demoseed.Result{}, errors.New("session lifetime is not SessionLifetime")
		}
		if _, err := req.Actors.As(ctx, "no-such-actor"); !errs.Is(err, errs.Invalid) {
			return demoseed.Result{}, fmt.Errorf("unknown actor key: %w, want Invalid", err)
		}
		return demoseed.Result{Items: 3}, nil
	}
	r := newIRig(t)
	r.link(demo.Contributor{Module: "alpha", Provider: prov})
	rep, _ := seedOK(t, r)
	if prov.calls != 1 {
		t.Fatalf("provider called %d times", prov.calls)
	}
	last := rep.Steps[len(rep.Steps)-1]
	if last.Name != "module:alpha" || last.Items != 3 {
		t.Errorf("last step = %+v", last)
	}
}

func TestSeed_Contributors_RunInOrderAfterCore(t *testing.T) {
	var order []string
	mk := func(n string) demo.Contributor {
		return demo.Contributor{Module: n, Provider: &fakeProvider{fn: func(context.Context, demoseed.Request) (demoseed.Result, error) {
			order = append(order, n)
			return demoseed.Result{Items: 1}, nil
		}}}
	}
	r := newIRig(t)
	r.link(mk("alpha"), mk("beta"), mk("gamma"))
	rep, _ := seedOK(t, r)
	if fmt.Sprint(order) != "[alpha beta gamma]" {
		t.Errorf("order = %v", order)
	}
	n := len(rep.Steps)
	if fmt.Sprint(stepNames(rep)[n-3:]) != "[module:alpha module:beta module:gamma]" {
		t.Errorf("steps = %v", stepNames(rep))
	}
}

// ---- Seed failures (F11, F12, F13, F14, F15, F16) -------------------------------------------

func failedSeed(t *testing.T, r *irig, kind errs.Kind) {
	t.Helper()
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, kind)
	if r.markerRows() != 1 || r.seededAtSet() {
		t.Error("a failed seed must leave the marker in 'seeding'")
	}
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == "demo.seed.ran" {
			t.Error("demo.seed.ran recorded for a failed seed")
		}
	}
}

func TestSeed_ActorLacksPermission_ReturnsPermission(t *testing.T) {
	tests := map[string]func(r *irig){
		"profiles": func(r *irig) {
			r.profiles.err = func(_ string, _ model.PersonID) error {
				return errs.E("people.UpdateMe", errs.Permission, errors.New("denied"))
			}
		},
		"grants": func(r *irig) {
			r.access.grantErr = func(authz.GrantInput) error { return errs.E("authz.Grant", errs.Permission, errors.New("denied")) }
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t)
			setup(r)
			failedSeed(t, r, errs.Permission)
		})
	}
}

func TestSeed_StarterRoleMissing_ReturnsNotFound(t *testing.T) {
	for _, missing := range []string{authz.RoleDepartmentSteward, authz.RoleVolunteerCoordinator, authz.RoleDepartmentAdmin} {
		t.Run(missing, func(t *testing.T) {
			r := newIRig(t)
			r.access = newFakeAccess(r.log, missing)
			r.opts.Access = r.access
			svc, err := demo.New(r.opts)
			wantNoErr(t, err)
			r.svc = svc
			failedSeed(t, r, errs.NotFound)
		})
	}
}

func TestSeed_ContributorFails_FailsSeed(t *testing.T) {
	boom := errs.E("alpha.demo", errs.Unavailable, errors.New("alpha is unhappy"))
	r := newIRig(t)
	r.link(demo.Contributor{Module: "alpha", Provider: &fakeProvider{fn: func(context.Context, demoseed.Request) (demoseed.Result, error) {
		return demoseed.Result{}, boom
	}}}, demo.Contributor{Module: "beta", Provider: &fakeProvider{}})
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, boom) {
		t.Error("the contributor's error is not wrapped")
	}
	if !strings.Contains(err.Error(), "alpha") { // the spec fixes the wrapper: "module <name>: ..."
		t.Errorf("the error does not name the module: %v", err)
	}
	if r.markerRows() != 1 || r.seededAtSet() {
		t.Error("marker must stay in 'seeding'")
	}
}

func TestSeed_ContributorTimeout_ReturnsTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("the shortest ContributorTimeout is 10s of real time")
	}
	r := newIRig(t, func(o *demo.Options) { o.ContributorTimeout = 10 * time.Second })
	r.link(demo.Contributor{Module: "slow", Provider: &fakeProvider{fn: func(ctx context.Context, _ demoseed.Request) (demoseed.Result, error) {
		<-ctx.Done()
		return demoseed.Result{}, ctx.Err()
	}}})
	failedSeed(t, r, errs.Timeout)
}

func TestSeed_CanceledMidway_ReturnsCanceled(t *testing.T) {
	r := newIRig(t)
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	r.profiles.err = func(string, model.PersonID) error { cancel(); return nil }
	_, err := r.svc.Seed(ctx, &strings.Builder{})
	wantKind(t, err, errs.Canceled)
	if r.seededAtSet() {
		t.Error("a canceled seed was marked done")
	}
}

func TestSeed_AuditFails_FailsSeed(t *testing.T) {
	r := newIRig(t)
	r.rec.Fail(errs.E("audit.Record", errs.Unavailable, errors.New("audit down")))
	_, err := r.svc.Seed(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Unavailable)
}

func TestSeed_NeverLogsPersonalDataOrPasswords(t *testing.T) {
	r := newIRig(t)
	seedOK(t, r)
	for _, s := range []string{demo.Password, "Alex Example", "Sam Sample", "Jordan Placeholder", "Casey Fictional", "@demo.invalid", "555 01"} {
		if r.cap.Contains(s) {
			t.Errorf("a log, span or metric contains %q", s)
		}
	}
}

// ---- Reset (F19 to F24) ---------------------------------------------------------------------

// sentinel creates a table in schema s so tests can see whether a schema was dropped.
func (r *irig) sentinel(s string) {
	r.t.Helper()
	r.exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", s))
	r.exec(fmt.Sprintf("CREATE TABLE %s.zq_sentinel (n int)", s))
}

func (r *irig) hasTable(s, table string) bool {
	return r.count("SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2", s, table) == 1
}

func (r *irig) spoolDir() string {
	dir := filepath.Join(r.opts.DataDir, filepath.FromSlash(audit.SpoolDirName))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		r.t.Fatalf("spool dir: %v", err)
	}
	return dir
}

func (r *irig) spoolFile(name string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.spoolDir(), name), []byte("{}\n"), 0o600); err != nil {
		r.t.Fatalf("spool file: %v", err)
	}
}

func (r *irig) spoolFiles() int {
	es, err := os.ReadDir(r.spoolDir())
	wantNoErr(r.t, err)
	return len(es)
}

func TestReset_NoMarker_ReturnsConflict_TouchesNothing(t *testing.T) {
	r := newIRig(t)
	r.insertAccount("tester.one@example.org", false)
	r.spoolFile("a.json")
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Conflict)
	assertCode(t, err, "not_demo")
	if n := r.log.count(); n != 0 {
		t.Errorf("%d seam calls (objects, migrate, seed) with no marker: %+v", n, r.log.all())
	}
	if r.count("SELECT count(*) FROM core.account") != 1 {
		t.Error("the account was dropped")
	}
	if r.spoolFiles() != 1 {
		t.Error("the spool was cleared")
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("%d audit entries from a refused reset", n)
	}
}

func TestReset_ObjectsFail_LeavesDatabase(t *testing.T) {
	r := newIRig(t)
	r.insertMarker(true)
	r.spoolFile("a.json")
	r.objects.err = errs.E("objects.Clear", errs.Unavailable, errors.New("store down"))
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Unavailable)
	if r.markerRows() != 1 {
		t.Error("the database was touched although objects failed first")
	}
	if r.spoolFiles() != 1 {
		t.Error("the spool was cleared after the objects step failed")
	}
	if len(r.log.byMethod("schema", "Migrate")) != 0 {
		t.Error("migrate ran after the objects step failed")
	}
}

func TestReset_SpoolUnreadable_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	r.insertMarker(true)
	// The spool "directory" is a regular file, so it can be neither read nor listed.
	if err := os.MkdirAll(filepath.Join(r.opts.DataDir, "audit"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.opts.DataDir, filepath.FromSlash(audit.SpoolDirName)), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantKind(t, err, errs.Unavailable)
	if r.markerRows() != 1 {
		t.Error("the schema was dropped although the spool step failed")
	}
}

func TestReset_DropFails_KeepsMarker(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	denied := errs.E("test.drop", errs.Permission, errors.New("must be owner of schema"))
	r := newRig(t, dropDenied{DB: pool, err: denied})
	ir := &irig{rig: r, pool: pool}
	ir.insertMarker(true)
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	if err == nil || (!errs.Is(err, errs.Permission) && !errs.Is(err, errs.Unavailable)) {
		t.Fatalf("err = %v, want the store's kind (Permission or Unavailable)", err)
	}
	if ir.markerRows() != 1 {
		t.Error("the marker went although the drop was refused")
	}
	if len(r.log.byMethod("schema", "Migrate")) != 0 {
		t.Error("migrate ran after the drop failed")
	}
}

// dropDenied refuses statements that drop a schema.
type dropDenied struct {
	db.DB
	err error
}

func (d dropDenied) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(strings.ToUpper(sql), "DROP SCHEMA") {
		return pgconn.CommandTag{}, d.err
	}
	return d.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

func (d dropDenied) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	return d.DB.Transact(ctx, mode, func(ctx context.Context) error { return fn(ctx) }) //nolint:wrapcheck // test double
}

func TestReset_MigrateFails_SaysHowToRecover(t *testing.T) {
	r := newIRig(t)
	r.insertMarker(true)
	conflict := errs.E("db.Migrate", errs.Conflict, errors.New("another run holds the lock"))
	r.schema.err = conflict
	var out strings.Builder
	_, err := r.svc.Reset(ctxT(t), &out)
	wantKind(t, err, errs.Conflict) // the command turns a migrator Conflict into exit 3
	if !strings.Contains(err.Error()+out.String(), "bms migrate") {
		t.Errorf("neither the error nor the output says to run `bms migrate`, then `bms demo seed`:\n%v\n%s", err, out.String())
	}
	if len(r.log.byMethod("accounts", "EnsureAccount")) != 0 {
		t.Error("the seed ran after the migration failed")
	}
}

func TestReset_Success_WipesAndSeedsAgain(t *testing.T) {
	r := newIRig(t, func(o *demo.Options) { o.Schemas = []string{"core", "zqmod"} })
	r.schema.fn = func(ctx context.Context) error {
		r.accounts.mu.Lock()
		r.accounts.byEm = nil // the old rows are gone with the schema
		r.accounts.mu.Unlock()
		mig, err := db.NewMigrator(r.pool, slog.New(slog.DiscardHandler))
		if err != nil {
			return err //nolint:wrapcheck // test double
		}
		_, err = mig.Migrate(ctx, coreSets)
		return err //nolint:wrapcheck // test double
	}
	prov := &fakeProvider{}
	r.link(demo.Contributor{Module: "alpha", Provider: prov})
	seedOK(t, r)
	firstAccounts := r.count("SELECT count(*) FROM core.demo_account")
	r.sentinel("zqmod")
	r.send("tester.one@demo.invalid", "stranger's mail")
	r.spoolFile("a.json")
	r.spoolFile("b.json")
	r.log.mu.Lock()
	r.log.calls = nil
	r.log.mu.Unlock()

	var out strings.Builder
	rep, err := r.svc.Reset(ctxT(t), &out)
	wantNoErr(t, err)

	names := stepNames(rep)
	head := []string{"objects", "spool", "drop", "migrate", "accounts"}
	if len(names) < len(head) || fmt.Sprint(names[:len(head)]) != fmt.Sprint(head) {
		t.Fatalf("steps = %v, want to begin %v", names, head)
	}
	if names[len(names)-1] != "module:alpha" {
		t.Errorf("steps = %v, want the module last", names)
	}
	if r.hasTable("zqmod", "zq_sentinel") {
		t.Error("a module schema survived the reset")
	}
	if r.mailRows() != 0 {
		t.Error("captured mail survived the reset")
	}
	if r.spoolFiles() != 0 {
		t.Error("the audit spool was not emptied")
	}
	if got := r.count("SELECT count(*) FROM core.demo_account"); got != firstAccounts {
		t.Errorf("%d demo accounts after the reset, want %d (same as the first seed)", got, firstAccounts)
	}
	if !r.seededAtSet() || r.markerRows() != 1 {
		t.Error("the marker is not in the seeded state after the reset")
	}
	// Order: objects cleared before the migration, the migration before the first account.
	idx := map[string]int{}
	for i, c := range r.log.all() {
		if _, ok := idx[c.Seam+"."+c.Method]; !ok {
			idx[c.Seam+"."+c.Method] = i
		}
	}
	if idx["objects.Clear"] >= idx["schema.Migrate"] || idx["schema.Migrate"] >= idx["accounts.EnsureAccount"] {
		t.Errorf("wrong order: %v", idx)
	}
	if prov.calls != 2 {
		t.Errorf("provider ran %d times in total, want 2 (seed and reset)", prov.calls)
	}
}

func TestReset_RecordsResetRanAsOperator(t *testing.T) {
	r := newIRig(t)
	r.insertMarker(true)
	r.schema.fn = func(ctx context.Context) error {
		mig, err := db.NewMigrator(r.pool, slog.New(slog.DiscardHandler))
		if err != nil {
			return err //nolint:wrapcheck // test double
		}
		_, err = mig.Migrate(ctx, coreSets)
		return err //nolint:wrapcheck // test double
	}
	_, err := r.svc.Reset(ctxT(t), &strings.Builder{})
	wantNoErr(t, err)
	var found bool
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == "demo.reset.ran" {
			found = true
			if e.Actor.Kind != audit.ActorOperator {
				t.Errorf("demo.reset.ran actor = %v, want operator", e.Actor.Kind)
			}
		}
	}
	if !found {
		t.Error("demo.reset.ran was not recorded")
	}
}

var _ = notify.ErrRejected

func TestCapture_Send_NeverLogsAddressSubjectOrBody(t *testing.T) {
	r := newIRig(t)
	e := goodEnvelope()
	e.To = "ZQ9ADDRESS@demo.invalid"
	e.Subject = "ZQ9SUBJECT"
	e.Body = "ZQ9BODY https://demo.example.org/accounts/password-reset?token=ZQ9TOKEN"
	wantNoErr(t, r.svc.Channel().Send(ctxT(t), e))
	for _, m := range []string{"ZQ9ADDRESS", "ZQ9SUBJECT", "ZQ9BODY", "ZQ9TOKEN"} {
		if r.cap.Contains(m) {
			t.Errorf("a log, span or metric contains %q", m)
		}
	}
}

func TestReset_Canceled_ReturnsCanceled(t *testing.T) {
	r := newIRig(t)
	r.insertMarker(true)
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := r.svc.Reset(ctx, &strings.Builder{})
	wantKind(t, err, errs.Canceled)
	if n := len(r.log.byMethod("objects", "Clear")); n != 0 {
		t.Error("objects were cleared after cancellation")
	}
}

// F17: every row the seed table hands a service passes the validators that are exported (the
// others, in the services' own use cases, run in the app integration tests), and a service
// that rejects a row fails the seed with Invalid.
func TestSeed_TableValidates(t *testing.T) {
	r := newIRig(t)
	r.link()
	seedOK(t, r)

	if len(r.access.ensured) == 0 {
		t.Fatal("no grant reached the access seam")
	}
	for i, g := range r.access.ensured {
		if err := g.Validate(); err != nil {
			t.Errorf("grant %d does not validate: %v", i, err)
		}
	}
	for i, it := range r.events.schedule {
		if err := it.Validate(); err != nil {
			t.Errorf("schedule item %d (%s) does not validate: %v", i, it.Key, err)
		}
	}
	seen := map[string]bool{}
	for _, in := range append(append([]people.ProfileInput(nil), r.profiles.updates...), r.profiles.creates...) {
		name := deref(in.PlayaName)
		if name == "" || len(name) > people.MaxPlayaNameBytes {
			t.Errorf("a profile has playa name %q", name)
		}
		if seen[name] {
			t.Errorf("playa name %q is used twice; people refuses duplicates", name)
		}
		seen[name] = true
	}
	for email := range r.accounts.byEm {
		if err := model.Email(email).Validate(); err != nil {
			t.Errorf("account address %q does not validate: %v", email, err)
		}
	}
	for _, d := range r.departments.in {
		if d.ContactEmail != "" {
			if err := model.Email(d.ContactEmail).Validate(); err != nil {
				t.Errorf("department %q contact address does not validate: %v", d.Name, err)
			}
		}
	}

	// A service that rejects a row fails the seed with Invalid, wrapped with the step.
	bad := newIRig(t)
	bad.link()
	bad.profiles.err = func(string, model.PersonID) error {
		return errs.E("people.Create", errs.Invalid, errors.New("playa name too long"))
	}
	var out strings.Builder
	_, err := bad.svc.Seed(ctxT(t), &out)
	wantKind(t, err, errs.Invalid)
}
