//go:build integration

package people_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// tableSource makes a module table that references core.person with ON DELETE RESTRICT
// and a PersonData that deletes its rows through the ctx it is given (so inside people's
// transaction), as the module-contract says a module must.
func (r *irig) tableSource(name string) *fakeSource {
	r.t.Helper()
	r.exec("CREATE TABLE core.zz_" + name + " (person_id uuid NOT NULL REFERENCES core.person(id) ON DELETE RESTRICT)")
	s := &fakeSource{name: name, log: r.log, data: json.RawMessage(`{"rows":1}`)}
	s.del = func(ctx context.Context, p model.PersonID) (people.ModuleDeletion, error) {
		tag, err := r.pool.Exec(ctx, "DELETE FROM core.zz_"+name+" WHERE person_id = $1", [16]byte(p))
		if err != nil {
			return people.ModuleDeletion{}, err //nolint:wrapcheck // a test module
		}
		return people.ModuleDeletion{Deleted: int(tag.RowsAffected())}, nil
	}
	return s
}

func (r *irig) addRows(table string, p model.PersonID, n int) {
	r.t.Helper()
	for range n {
		r.exec("INSERT INTO core.zz_"+table+" (person_id) VALUES ($1)", [16]byte(p))
	}
}

func (r *irig) tableRows(table string) int {
	return r.scalarInt("SELECT count(*) FROM core.zz_" + table)
}

// delFixture: a person with data in two module tables, a manager who may delete.
type delFixture struct {
	*irig
	alpha, beta *fakeSource
	target      model.PersonID
	adminID     model.PersonID
	ctx         context.Context
}

func newDelFixture(t *testing.T, realIdentity bool) *delFixture {
	t.Helper()
	r := newIRig(t, realIdentity)
	f := &delFixture{irig: r}
	f.alpha, f.beta = r.tableSource("alpha"), r.tableSource("beta")
	r.link(f.alpha.source(), f.beta.source())
	f.target = r.insertPerson(seed{Playa: mPlaya, Legal: mLegal, Phone: mPhone})
	r.insertContact(f.target, mECName, mECPhone, mRelationship, 1)
	r.exec(`INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at) VALUES ($1, 'volunteers.share-contact', true, $1, $2)`,
		[16]byte(f.target), r.clk.Now())
	_, err := r.svc.RequestDeletion(asPerson(ctxT(t), f.target), mNote)
	wantNoErr(t, err)
	r.addRows("alpha", f.target, 2)
	r.addRows("beta", f.target, 1)
	f.adminID, f.ctx = r.admin(permDelete)
	r.counter.n = 7
	r.rec.Fail(nil)
	return f
}

// intact fails the test when anything of the target was deleted or announced.
func (f *delFixture) intact() {
	f.t.Helper()
	if !f.rowExists(f.target) {
		f.t.Error("the person row is gone")
	}
	if n := f.contactRows(f.target); n != 1 {
		f.t.Errorf("%d contacts, want 1", n)
	}
	if n := f.consentRows(f.target); n != 1 {
		f.t.Errorf("%d consents, want 1", n)
	}
	if n := f.requestRows(f.target); n != 1 {
		f.t.Errorf("%d requests, want 1", n)
	}
	if n := f.tableRows("alpha"); n != 2 {
		f.t.Errorf("alpha has %d rows, want 2: a module's deletion was not rolled back", n)
	}
	if n := f.tableRows("beta"); n != 1 {
		f.t.Errorf("beta has %d rows, want 1: a module's deletion was not rolled back", n)
	}
	for _, s := range f.published(sigDeleted) {
		if payloadPerson(s) == f.target {
			f.t.Error("people.person-deleted was published for a person who is still there")
		}
	}
	if f.count("people.person.deleted") != 0 {
		f.t.Error("people.person.deleted was recorded for a person who is still there")
	}
}

func keptCount(kept []people.Kept, kind string) int {
	for _, k := range kept {
		if k.Kind == kind {
			return k.Count
		}
	}
	return 0
}

// ---- delete -----------------------------------------------------------------------------

func TestDelete_AcrossModules_OneTransaction_ReportsKept(t *testing.T) {
	f := newDelFixture(t, false)
	f.alpha.del = wrapKept(f.alpha.del, people.Kept{Kind: "alpha.note", Count: 3})
	rep, err := f.svc.Delete(f.ctx, f.target)
	wantNoErr(t, err)
	if rep.Person != f.target {
		t.Errorf("report person = %v", rep.Person)
	}
	if rep.People != 4 {
		t.Errorf("People = %d, want 4 (person, contact, consent, request)", rep.People)
	}
	if rep.Identity != 1 || rep.Access != 2 {
		t.Errorf("Identity/Access = %d/%d, want the seams' counts 1/2", rep.Identity, rep.Access)
	}
	if rep.Modules["alpha"].Deleted != 2 || rep.Modules["beta"].Deleted != 1 {
		t.Errorf("Modules = %+v", rep.Modules)
	}
	if keptCount(rep.Modules["alpha"].Kept, "alpha.note") != 3 {
		t.Errorf("the module's own Kept is lost: %+v", rep.Modules["alpha"])
	}
	if got := keptCount(rep.Kept, "audit.entry"); got != 7 {
		t.Errorf("Kept audit.entry = %d, want 7 (%+v)", got, rep.Kept)
	}
	if f.rowExists(f.target) || f.contactRows(f.target) != 0 || f.consentRows(f.target) != 0 || f.requestRows(f.target) != 0 {
		t.Error("people's rows survive")
	}
	if f.tableRows("alpha") != 0 || f.tableRows("beta") != 0 {
		t.Error("module rows survive")
	}
	order := f.log.all()
	want := []string{"module.alpha.delete", "module.beta.delete", "access.delete", "accounts.delete"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order of deletion = %v, want modules in build order, then access, then identity", order)
	}
	e := f.only("people.person.deleted")
	if e.Subject != f.target || e.Reason != "admin" || e.Outcome != audit.Succeeded {
		t.Errorf("entry = %+v", e)
	}
	if e.Record.Kind != "people.person" || e.Record.ID != [16]byte(f.target) {
		t.Errorf("entry record = %+v", e.Record)
	}
	sigs := f.published(sigDeleted)
	if len(sigs) != 1 || payloadPerson(sigs[0]) != f.target {
		t.Errorf("published %v", sigs)
	}
	f.noLeak(allMarkers()...)
	if f.cap.Contains("alpha.note") {
		t.Error("the report reached telemetry")
	}
}

func wrapKept(inner func(context.Context, model.PersonID) (people.ModuleDeletion, error), k people.Kept) func(context.Context, model.PersonID) (people.ModuleDeletion, error) {
	return func(ctx context.Context, p model.PersonID) (people.ModuleDeletion, error) {
		d, err := inner(ctx, p)
		d.Kept = append(d.Kept, k)
		return d, err
	}
}

func TestDelete_NoAuditEntries_NoKeptLine(t *testing.T) {
	f := newDelFixture(t, false)
	f.counter.n = 0
	rep, err := f.svc.Delete(f.ctx, f.target)
	wantNoErr(t, err)
	if got := keptCount(rep.Kept, "audit.entry"); got != 0 {
		t.Errorf("Kept audit.entry = %d with no entries about the person", got)
	}
}

func TestDelete_IsIdempotentAsNotFound(t *testing.T) {
	f := newDelFixture(t, false)
	_, err := f.svc.Delete(f.ctx, f.target)
	wantNoErr(t, err)
	_, err = f.svc.Delete(f.ctx, f.target)
	wantKind(t, err, errs.NotFound)
	_, err = f.svc.Get(asSuper(ctxT(t), newPerson()), f.target)
	wantKind(t, err, errs.NotFound)
	got, err := f.svc.NamesOf(ctxT(t), []model.PersonID{f.target})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Error("the deleted person still has a name")
	}
}

// F11
func TestDelete_UnknownPerson_ReturnsNotFound(t *testing.T) {
	f := newDelFixture(t, false)
	_, err := f.svc.Delete(f.ctx, newPerson())
	wantKind(t, err, errs.NotFound)
	f.intact()
}

// F32
func TestDelete_NoPermission_ReturnsPermission(t *testing.T) {
	f := newDelFixture(t, false)
	_, ctx := f.admin(permExport, permManage, permDirectory, permContacts)
	_, err := f.svc.Delete(ctx, f.target)
	wantKind(t, err, errs.Permission)
	f.intact()
	if len(f.log.all()) != 0 {
		t.Errorf("modules were called before the permission check: %v", f.log.all())
	}
}

// F33
func TestDelete_Self_ReturnsInvalid(t *testing.T) {
	f := newDelFixture(t, false)
	f.grant(f.target, permDelete)
	_, err := f.svc.Delete(asPerson(ctxT(t), f.target), f.target)
	wantField(t, err, "person", "self")
	_, err = f.svc.Delete(asSuper(ctxT(t), f.target), f.target)
	wantField(t, err, "person", "self")
	f.intact()
}

// F34
func TestDelete_LastAccessAdmin_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.access.deleteErr = errs.E("authz.DeletePerson", errs.Conflict,
		&validate.FieldError{Field: "person", Code: "last_access_admin", Reason: "last access admin"})
	_, err := f.svc.Delete(f.ctx, f.target)
	wantCode(t, err, errs.Conflict, "last_access_admin")
	f.intact()
	for _, c := range f.log.all() {
		if c == "accounts.delete" {
			t.Error("identity was asked to delete after authz refused")
		}
	}
}

// F35: real identity refuses a superuser's account.
func TestDelete_SuperuserPerson_RollsBack(t *testing.T) {
	f := newDelFixture(t, true)
	su := f.superuser("root.operator")
	f.insertPersonAs(su.person, seed{Playa: "Root Operator"})
	f.addRows("alpha", su.person, 1)
	_, err := f.svc.Delete(f.ctx, su.person)
	wantKind(t, err, errs.Permission)
	if !f.rowExists(su.person) || f.accountRows(su.person) != 1 {
		t.Error("the superuser's person or account was deleted")
	}
	if f.tableRows("alpha") != 3 {
		t.Errorf("alpha has %d rows, want 3: the module deletion was not rolled back", f.tableRows("alpha"))
	}
}

// F36
func TestDelete_ModuleFails_RollsBack(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Conflict, errs.Internal, errs.Timeout} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newDelFixture(t, false)
			boom := errs.E("beta.DeletePerson", kind, errors.New("beta is unhappy"))
			f.beta.del = func(context.Context, model.PersonID) (people.ModuleDeletion, error) {
				return people.ModuleDeletion{}, boom
			}
			_, err := f.svc.Delete(f.ctx, f.target)
			wantKind(t, err, kind)
			f.intact() // alpha, which ran first, is restored
			for _, c := range f.log.all() {
				if c == "access.delete" || c == "accounts.delete" {
					t.Errorf("%s ran after a module failed", c)
				}
			}
		})
	}
}

// F37
func TestDelete_ForeignKeyLeft_ReturnsConflict(t *testing.T) {
	f := newDelFixture(t, false)
	f.exec("CREATE TABLE core.zz_forgotten (person_id uuid NOT NULL REFERENCES core.person(id) ON DELETE RESTRICT)")
	f.exec("INSERT INTO core.zz_forgotten (person_id) VALUES ($1)", [16]byte(f.target))
	_, err := f.svc.Delete(f.ctx, f.target)
	wantCode(t, err, errs.Conflict, "in_use")
	f.intact()
}

// F39
func TestDelete_AuditFails_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("spool full")))
	_, err := f.svc.Delete(f.ctx, f.target)
	wantKind(t, err, errs.Unavailable)
	f.rec.Fail(nil)
	f.intact()
}

func TestDelete_CountingAuditRowsFails_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.counter.err = errs.E("audit.CountAbout", errs.Unavailable, errors.New("down"))
	_, err := f.svc.Delete(f.ctx, f.target)
	wantKind(t, err, errs.Unavailable)
	f.intact()
}

func TestDelete_AccountsFail_RollsBack(t *testing.T) {
	f := newDelFixture(t, false)
	f.accts.deleteErr = errs.E("identity.DeletePerson", errs.Unavailable, errors.New("down"))
	_, err := f.svc.Delete(f.ctx, f.target)
	wantKind(t, err, errs.Unavailable)
	f.intact()
}

// F31
func TestDelete_BeforeLink_ReturnsInternal(t *testing.T) {
	r := newIRig(t, false)
	p := r.insertPerson(seed{Playa: "Dusty Ember"})
	_, ctx := r.admin(permDelete, permExport)
	_, err := r.svc.Delete(ctx, p)
	wantKind(t, err, errs.Internal)
	if !r.rowExists(p) {
		t.Error("an unlinked service deleted a person")
	}
	_, err = r.svc.Export(ctx, p)
	wantKind(t, err, errs.Internal)
	_, err = r.svc.Export(asPerson(ctxT(t), p), p)
	wantKind(t, err, errs.Internal)
}

func TestDelete_Concurrent_OneSucceedsTheOtherNotFound(t *testing.T) {
	f := newDelFixture(t, false)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Delete(f.ctx, f.target)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, nf := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errs.Is(err, errs.NotFound):
			nf++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || nf != 3 {
		t.Errorf("%d succeeded and %d not found, want 1 and 3", ok, nf)
	}
	n := 0
	for _, c := range f.log.all() {
		if c == "module.alpha.delete" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a module's DeletePerson ran %d times, want once", n)
	}
	if f.count("people.person.deleted") != 1 {
		t.Errorf("%d deleted entries, want 1", f.count("people.person.deleted"))
	}
}

func TestDelete_ReferencesToThePersonAsActor_DoNotBlock(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	mgr, mctx := r.admin(permManage, permDirectory)
	created := r.create(mctx, "Walk On").Person.ID
	_, err := r.svc.SetConsent(mctx, created, "volunteers.share-contact", true)
	wantNoErr(t, err)
	// a request the manager decided
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), created), "")
	wantNoErr(t, err)
	r.grant(mgr, permDelete)
	_, err = r.svc.DeclineRequest(mctx, req.ID, "")
	wantNoErr(t, err)

	_, dctx := r.admin(permDelete)
	_, err = r.svc.Delete(dctx, mgr)
	wantNoErr(t, err)
	if !r.rowExists(created) {
		t.Fatal("deleting the creator deleted the created person")
	}
	if n := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND created_by IS NULL", [16]byte(created)); n != 1 {
		t.Error("created_by was not cleared")
	}
	ok, err := r.svc.ConsentOf(ctxT(t), created, "volunteers.share-contact")
	wantNoErr(t, err)
	if !ok {
		t.Error("deleting the manager who recorded a consent removed the consent")
	}
	if r.requestRows(created) != 1 {
		t.Error("deleting the decider removed the request")
	}
}

// ---- export -----------------------------------------------------------------------------

type exportFixture struct {
	*irig
	me model.PersonID
}

func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	r := newIRig(t, false)
	a := &fakeSource{name: "alpha", log: r.log, data: json.RawMessage(`{"greetings":3}`)}
	b := &fakeSource{name: "beta", log: r.log} // holds nothing: nil data
	r.link(a.source(), b.source())
	me := r.insertPerson(seed{Playa: mPlaya, Pronouns: "they/them", Legal: mLegal, Phone: mPhone, ContactEmail: mContactEmail})
	r.insertContact(me, mECName, mECPhone, mRelationship, 1)
	r.exec(`INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at) VALUES ($1, 'volunteers.share-contact', true, $1, $2)`,
		[16]byte(me), r.clk.Now())
	r.accts.exportOut = json.RawMessage(`{"account":{"marker":"identity-part"}}`)
	r.access.exportOut = json.RawMessage(`{"grants":["marker-grant"]}`)
	return &exportFixture{irig: r, me: me}
}

func TestExport_Self_ReturnsTheDocument(t *testing.T) {
	f := newExportFixture(t)
	// A declined request with a decision note belongs in the document.
	_, dctx := f.admin(permDelete)
	req, err := f.svc.RequestDeletion(asPerson(ctxT(t), f.me), mNote)
	wantNoErr(t, err)
	_, err = f.svc.DeclineRequest(dctx, req.ID, mDecision)
	wantNoErr(t, err)

	raw, err := f.svc.Export(asPerson(ctxT(t), f.me), f.me)
	wantNoErr(t, err)
	var doc struct {
		Format     int       `json:"format"`
		ExportedAt time.Time `json:"exported_at"`
		Person     struct {
			ID           string `json:"id"`
			PlayaName    string `json:"playa_name"`
			Pronouns     string `json:"pronouns"`
			Language     string `json:"language"`
			LegalName    string `json:"legal_name"`
			Phone        string `json:"phone"`
			ContactEmail string `json:"contact_email"`
			CreatedAt    string `json:"created_at"`
			UpdatedAt    string `json:"updated_at"`
		} `json:"person"`
		Contacts []struct {
			Name         string `json:"name"`
			Phone        string `json:"phone"`
			Relationship string `json:"relationship"`
			Position     int    `json:"position"`
		} `json:"emergency_contacts"`
		Consents []struct {
			Purpose   string `json:"purpose"`
			Granted   bool   `json:"granted"`
			ChangedAt string `json:"changed_at"`
		} `json:"consents"`
		Requests []struct {
			Kind         string `json:"kind"`
			Status       string `json:"status"`
			Note         string `json:"note"`
			CreatedAt    string `json:"created_at"`
			DecisionNote string `json:"decision_note"`
		} `json:"requests"`
		Identity json.RawMessage            `json:"identity"`
		Access   json.RawMessage            `json:"access"`
		Modules  map[string]json.RawMessage `json:"modules"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the export is not the documented JSON: %v\n%s", err, raw)
	}
	if doc.Format != people.ExportFormat || !doc.ExportedAt.Equal(epoch) {
		t.Errorf("format/exported_at = %d/%v", doc.Format, doc.ExportedAt)
	}
	p := doc.Person
	if p.ID != f.me.String() || p.PlayaName != mPlaya || p.Pronouns != "they/them" || p.LegalName != mLegal || p.Phone != mPhone || p.ContactEmail != mContactEmail {
		t.Errorf("person = %+v", p)
	}
	if p.CreatedAt == "" || p.UpdatedAt == "" {
		t.Error("created_at/updated_at missing")
	}
	if len(doc.Contacts) != 1 || doc.Contacts[0].Name != mECName || doc.Contacts[0].Phone != mECPhone || doc.Contacts[0].Relationship != mRelationship || doc.Contacts[0].Position != 1 {
		t.Errorf("emergency_contacts = %+v", doc.Contacts)
	}
	if len(doc.Consents) != 1 || doc.Consents[0].Purpose != "volunteers.share-contact" || !doc.Consents[0].Granted {
		t.Errorf("consents = %+v", doc.Consents)
	}
	if len(doc.Requests) != 1 || doc.Requests[0].Kind != "delete" || doc.Requests[0].Status != "declined" ||
		doc.Requests[0].Note != mNote || doc.Requests[0].DecisionNote != mDecision {
		t.Errorf("requests = %+v", doc.Requests)
	}
	if !bytes.Contains(doc.Identity, []byte("identity-part")) {
		t.Errorf("identity = %s, want the accounts part passed through", doc.Identity)
	}
	if !bytes.Contains(doc.Access, []byte("marker-grant")) {
		t.Errorf("access = %s", doc.Access)
	}
	if string(doc.Modules["alpha"]) != `{"greetings":3}` && !bytes.Contains(doc.Modules["alpha"], []byte("greetings")) {
		t.Errorf("modules.alpha = %s", doc.Modules["alpha"])
	}
	beta, ok := doc.Modules["beta"]
	if !ok || strings.TrimSpace(string(beta)) != "{}" {
		t.Errorf("modules.beta = %q (present %v), want an empty object", beta, ok)
	}
	if bytes.Contains(raw, []byte("people.person.exported")) || bytes.Contains(raw, []byte("audit")) {
		t.Error("audit rows are not part of the export")
	}
}

func TestExport_RecordsOneSensitiveEntry_ForSelfToo(t *testing.T) {
	f := newExportFixture(t)
	_, err := f.svc.Export(asPerson(ctxT(t), f.me), f.me)
	wantNoErr(t, err)
	e := f.only("people.person.exported")
	if e.Subject != f.me || e.Level != model.SensitivitySensitive || len(e.Fields) != 1 || e.Fields[0] != "all" || e.Outcome != audit.Succeeded {
		t.Errorf("entry = %+v", e)
	}
	// An admin's export of someone else records the same, about that person.
	_, actx := f.admin(permExport)
	_, err = f.svc.Export(actx, f.me)
	wantNoErr(t, err)
	if es := f.entries("people.person.exported"); len(es) != 2 || es[1].Subject != f.me {
		t.Errorf("entries = %+v", es)
	}
	f.noLeak(allMarkers()...)
}

func TestExport_RunsInNoWriteTransaction_AndCallsEachPartOnce(t *testing.T) {
	f := newExportFixture(t)
	f.spy.reset()
	_, err := f.svc.Export(asPerson(ctxT(t), f.me), f.me)
	wantNoErr(t, err)
	if c := f.spy.counts(); c.WriteTx != 0 {
		t.Errorf("%d write transactions for an export", c.WriteTx)
	}
	calls := f.log.all()
	idx := func(s string) int {
		for i, c := range calls {
			if c == s {
				return i
			}
		}
		return -1
	}
	for _, c := range []string{"accounts.export", "access.export", "module.alpha.export", "module.beta.export"} {
		if idx(c) < 0 {
			t.Errorf("%s was not called (calls: %v)", c, calls)
		}
	}
	if idx("module.alpha.export") > idx("module.beta.export") {
		t.Errorf("modules not exported in build order: %v", calls)
	}
}

// F28
func TestExport_SelfWithinCooldown_ReturnsRateLimited(t *testing.T) {
	f := newExportFixture(t)
	ctx := asPerson(ctxT(t), f.me)
	_, err := f.svc.Export(ctx, f.me)
	wantNoErr(t, err)
	_, err = f.svc.Export(ctx, f.me)
	wantRateLimited(t, err)

	wantNoErr(t, f.clk.Advance(people.ExportCooldown-time.Second))
	_, err = f.svc.Export(ctx, f.me)
	wantRateLimited(t, err)

	// A person's cooldown does not affect another's.
	other := f.insertPerson(seed{Playa: "Pell Mallow"})
	_, err = f.svc.Export(asPerson(ctxT(t), other), other)
	wantNoErr(t, err)

	wantNoErr(t, f.clk.Advance(2*time.Second))
	_, err = f.svc.Export(ctx, f.me)
	wantNoErr(t, err)
	_, err = f.svc.Export(ctx, f.me)
	wantRateLimited(t, err)
}

func TestExport_RefusedAttemptsDoNotExtendTheCooldown(t *testing.T) {
	f := newExportFixture(t)
	ctx := asPerson(ctxT(t), f.me)
	_, err := f.svc.Export(ctx, f.me)
	wantNoErr(t, err)
	wantNoErr(t, f.clk.Advance(5*time.Minute))
	_, err = f.svc.Export(ctx, f.me)
	wantRateLimited(t, err)
	wantNoErr(t, f.clk.Advance(5*time.Minute+time.Second))
	_, err = f.svc.Export(ctx, f.me)
	wantNoErr(t, err) // ten minutes since the last export that happened
}

func TestExport_AdminAndCommandAreNotRateLimited(t *testing.T) {
	f := newExportFixture(t)
	_, actx := f.admin(permExport)
	for range 3 {
		_, err := f.svc.Export(actx, f.me)
		wantNoErr(t, err)
	}
	for range 3 {
		wantNoErr(t, f.svc.ExportCommand(ctxT(t), &bytes.Buffer{}, f.me.String()))
	}
}

// F29
func TestExport_NoPermission_ReturnsPermission(t *testing.T) {
	f := newExportFixture(t)
	other := newPerson()
	f.grant(other, permDirectory, permContacts, permManage, permDelete)
	_, err := f.svc.Export(asPerson(ctxT(t), other), f.me)
	wantKind(t, err, errs.Permission)
	if f.count("people.person.exported") != 0 || len(f.log.all()) != 0 {
		t.Error("a refused export recorded an entry or called a source")
	}
}

// F11
func TestExport_UnknownPerson_ReturnsNotFound(t *testing.T) {
	f := newExportFixture(t)
	_, actx := f.admin(permExport)
	_, err := f.svc.Export(actx, newPerson())
	wantKind(t, err, errs.NotFound)
}

// F11 applies to a person's own export too: no row, no export. (The spec lists Export
// among the calls that fail NotFound for an unknown person and makes no exception for self.)
func TestExport_SelfWithoutARow_ReturnsNotFound(t *testing.T) {
	f := newExportFixture(t)
	ghost := newPerson()
	_, err := f.svc.Export(asPerson(ctxT(t), ghost), ghost)
	wantKind(t, err, errs.NotFound)
	if f.count("people.person.exported") != 0 {
		t.Error("an export that found nothing was recorded")
	}
}

// F30
func TestExport_ModuleFails_FailsWhole(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			r := newIRig(t, false)
			a := &fakeSource{name: "gamma", log: r.log, exportErr: errs.E("gamma.ExportPerson", kind, errors.New("down"))}
			r.link(a.source())
			p := r.insertPerson(seed{Playa: "Dusty Ember"})
			raw, err := r.svc.Export(asPerson(ctxT(t), p), p)
			wantKind(t, err, kind)
			if len(raw) != 0 {
				t.Errorf("a partial export of %d bytes was returned", len(raw))
			}
		})
	}
}

func TestExport_AccountsOrAccessFail_FailsWhole(t *testing.T) {
	f := newExportFixture(t)
	f.accts.exportErr = errs.E("identity.ExportPerson", errs.Unavailable, errors.New("down"))
	raw, err := f.svc.Export(asPerson(ctxT(t), f.me), f.me)
	wantKind(t, err, errs.Unavailable)
	if len(raw) != 0 {
		t.Error("a partial export was returned")
	}
	f.accts.exportErr = nil
	f.access.exportErr = errs.E("authz.ExportPerson", errs.Timeout, errors.New("slow"))
	_, actx := f.admin(permExport)
	raw, err = f.svc.Export(actx, f.me)
	wantKind(t, err, errs.Timeout)
	if len(raw) != 0 {
		t.Error("a partial export was returned")
	}
}

func TestExport_AuditFails_NoBody(t *testing.T) {
	f := newExportFixture(t)
	f.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("spool full")))
	raw, err := f.svc.Export(asPerson(ctxT(t), f.me), f.me)
	wantKind(t, err, errs.Unavailable)
	if len(raw) != 0 {
		t.Errorf("%d bytes of personal data returned although the export could not be recorded", len(raw))
	}
}

// ---- the commands -----------------------------------------------------------------------

func TestExportCommand_WritesTheDocument_AsTheOperator(t *testing.T) {
	f := newExportFixture(t)
	var out bytes.Buffer
	wantNoErr(t, f.svc.ExportCommand(ctxT(t), &out, f.me.String()))
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("the command's output is not JSON: %v", err)
	}
	if string(doc["format"]) != "1" {
		t.Errorf("format = %s", doc["format"])
	}
	e := f.only("people.person.exported")
	if e.Actor.Kind != audit.ActorOperator || e.Reason != "command" || e.Subject != f.me {
		t.Errorf("entry = %+v, want the operator and Reason command", e)
	}
}

func TestExportCommand_UnknownPerson_ReturnsNotFound(t *testing.T) {
	f := newExportFixture(t)
	wantKind(t, f.svc.ExportCommand(ctxT(t), &bytes.Buffer{}, newPerson().String()), errs.NotFound)
}

func TestDeleteCommand_DeletesAsTheOperator_WritesOneLinePerPart(t *testing.T) {
	f := newDelFixture(t, false)
	var out bytes.Buffer
	wantNoErr(t, f.svc.DeleteCommand(ctxT(t), &out, f.target.String()))
	if f.rowExists(f.target) || f.tableRows("alpha") != 0 {
		t.Error("the person survives the command")
	}
	e := f.only("people.person.deleted")
	if e.Actor.Kind != audit.ActorOperator || e.Reason != "command" || e.Subject != f.target {
		t.Errorf("entry = %+v, want the operator and Reason command", e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 4 {
		t.Errorf("%d output lines, want one per part (people, identity, access, each module, kept):\n%s", len(lines), out.String())
	}
	for _, m := range allMarkers() {
		if strings.Contains(out.String(), m) {
			t.Errorf("the report contains personal data %q", m)
		}
	}
}

func TestDeleteCommand_LastAccessAdmin_Conflict(t *testing.T) {
	f := newDelFixture(t, false)
	f.access.deleteErr = errs.E("authz.DeletePerson", errs.Conflict,
		&validate.FieldError{Field: "person", Code: "last_access_admin", Reason: "last access admin"})
	err := f.svc.DeleteCommand(ctxT(t), &bytes.Buffer{}, f.target.String())
	wantCode(t, err, errs.Conflict, "last_access_admin")
	f.intact()
}

func TestDeleteCommand_UnknownPerson_ReturnsNotFound(t *testing.T) {
	f := newDelFixture(t, false)
	wantKind(t, f.svc.DeleteCommand(ctxT(t), &bytes.Buffer{}, newPerson().String()), errs.NotFound)
}

func TestCommands_BeforeLink_ReturnInternal(t *testing.T) {
	r := newIRig(t, false)
	p := r.insertPerson(seed{Playa: "Dusty Ember"})
	wantKind(t, r.svc.DeleteCommand(ctxT(t), &bytes.Buffer{}, p.String()), errs.Internal)
	wantKind(t, r.svc.ExportCommand(ctxT(t), &bytes.Buffer{}, p.String()), errs.Internal)
	if !r.rowExists(p) {
		t.Error("an unlinked command deleted a person")
	}
}

// ---- the person page's sections ---------------------------------------------------------

func TestGet_ProviderFails_SectionMarkedFailed(t *testing.T) {
	r := newIRig(t, false)
	good := providerFunc(func(_ context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return map[model.PersonID][]personpage.Section{ids[0]: {{Key: "hello.greeting", Items: []personpage.Item{{Label: "module.hello.item.count", Value: "3"}}}}}, nil
	})
	bad := providerFunc(func(context.Context, []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return nil, errs.E("camps.Sections", errs.Unavailable, errors.New("down"))
	})
	later := providerFunc(func(_ context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return map[model.PersonID][]personpage.Section{ids[0]: {{Key: "later.note"}}}, nil
	})
	wantNoErr(t, r.svc.Link(nil, []people.SectionProvider{
		{Module: "hello", Provider: good}, {Module: "camps", Provider: bad}, {Module: "later", Provider: later},
	}))
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if len(prof.Sections) != 3 {
		t.Fatalf("sections = %+v, want three in build order", prof.Sections)
	}
	if prof.Sections[0].Key != "hello.greeting" || prof.Sections[0].Failed || len(prof.Sections[0].Items) != 1 {
		t.Errorf("section 0 = %+v", prof.Sections[0])
	}
	if prof.Sections[1].Key != "camps.unavailable" || !prof.Sections[1].Failed || len(prof.Sections[1].Items) != 0 {
		t.Errorf("section 1 = %+v, want camps.unavailable, Failed", prof.Sections[1])
	}
	if prof.Sections[2].Key != "later.note" || prof.Sections[2].Failed {
		t.Errorf("section 2 = %+v: a failing provider stopped the next", prof.Sections[2])
	}
	if got := r.measured("bms.people.sections", map[string]string{"module": "camps", "outcome": "failed"}); got != 1 {
		t.Errorf("bms.people.sections{camps,failed} = %v, want 1", got)
	}
}

// F48: a provider that blocks until its ctx ends is cut off after SectionTimeout and the
// page still renders, with a Failed section.
func TestGet_ProviderBlocks_SectionTimesOut(t *testing.T) {
	r := newIRig(t, false)
	entered := make(chan struct{})
	blocking := providerFunc(func(ctx context.Context, _ []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		close(entered)
		<-ctx.Done()
		return nil, errs.E("slow.Sections", errs.KindOf(ctx.Err()), ctx.Err())
	})
	wantNoErr(t, r.svc.Link(nil, []people.SectionProvider{{Module: "slow", Provider: blocking}}))
	me := r.insertPerson(seed{Playa: "Dusty Ember"})

	type result struct {
		prof people.Profile
		err  error
	}
	done := make(chan result, 1)
	go func() {
		prof, err := r.svc.Me(asPerson(ctxT(t), me))
		done <- result{prof, err}
	}()
	<-entered
	waitCtx, cancel := context.WithTimeout(ctxT(t), 15*time.Second)
	defer cancel()
	// If the service times a provider out on the injected clock, a timer is armed and we
	// pass SectionTimeout on it; if it uses a context deadline, nothing is armed and the
	// page returns after SectionTimeout of real time. Either way no test code sleeps.
	probe, stop := context.WithTimeout(waitCtx, 500*time.Millisecond)
	if err := r.clk.BlockUntil(probe, 1); err == nil {
		wantNoErr(t, r.clk.Advance(people.SectionTimeout))
	}
	stop()
	select {
	case res := <-done:
		wantNoErr(t, res.err)
		if len(res.prof.Sections) != 1 || !res.prof.Sections[0].Failed || res.prof.Sections[0].Key != "slow.unavailable" {
			t.Errorf("sections = %+v", res.prof.Sections)
		}
		if got := r.measured("bms.people.sections", map[string]string{"module": "slow", "outcome": "failed"}); got != 1 {
			t.Errorf("bms.people.sections{slow,failed} = %v, want 1", got)
		}
	case <-waitCtx.Done():
		t.Fatal("the page did not return after SectionTimeout passed on the injected clock")
	}
}

func TestGet_Providers_SeeTheViewerAndOnePerson(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	viewer, vctx := r.admin(permDirectory)
	var seenViewer model.PersonID
	var seenIDs []model.PersonID
	var calls int
	spy := providerFunc(func(ctx context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		calls++
		seenIDs = ids
		seenViewer = viewerOf(ctx)
		return nil, nil // a person with nothing to show is simply absent
	})
	wantNoErr(t, r.svc.Link(nil, []people.SectionProvider{{Module: "spy", Provider: spy}}))
	prof, err := r.svc.Get(vctx, target)
	wantNoErr(t, err)
	if calls != 1 || len(seenIDs) != 1 || seenIDs[0] != target {
		t.Errorf("provider called %d times with %v, want once with the page's person", calls, seenIDs)
	}
	if seenViewer != viewer {
		t.Errorf("the provider saw viewer %v, want %v", seenViewer, viewer)
	}
	if len(prof.Sections) != 0 {
		t.Errorf("sections = %+v for a provider with nothing to show", prof.Sections)
	}
}

func TestSections_NotLinked_PageCarriesNone(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if len(prof.Sections) != 0 {
		t.Errorf("sections before Link: %+v", prof.Sections)
	}
}
