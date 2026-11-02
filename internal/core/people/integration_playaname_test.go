//go:build integration

package people_test

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Amendment P1 (D48): a playa name is unique under NameKey, self-appointed and permanent.
// Every name below is invented. The unique index decides every clash, so these run over
// a real, migrated database.

const (
	nmZorn   = "Zorn Plume"
	nmZornUp = "  ZORN   plume "
	nmOther  = "Quill Marrow"
)

func (r *irig) nameOf(p model.PersonID) string {
	r.t.Helper()
	n, err := r.svc.PlayaNameOf(ctxT(r.t), p)
	wantNoErr(r.t, err)
	return n
}

func (r *irig) keyHolders(key string) int {
	return r.scalarInt("SELECT count(*) FROM core.person WHERE playa_name_key = $1", key)
}

// quiet fails when a refused write left an entry or a signal behind.
func (r *irig) quiet(what string) {
	r.t.Helper()
	for _, e := range r.rec.Recorded() {
		if a := string(e.Action); a == "people.person.created" || a == "people.person.changed" {
			r.t.Errorf("%s: left an audit entry %s", what, a)
		}
	}
	if n := len(r.published(sigCreated)); n != 0 {
		r.t.Errorf("%s: published %d people.person-created signals", what, n)
	}
}

// F74
func TestUpdateMe_NameTaken_ReturnsConflict(t *testing.T) {
	for name, claim := range map[string]string{
		"same string":      nmZorn,
		"other case":       "zorn PLUME",
		"extra whitespace": nmZornUp,
	} {
		t.Run(name, func(t *testing.T) {
			r := newIRig(t, false)
			holder := r.insertPerson(seed{Playa: nmZorn})
			me := newPerson()
			_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(claim)})
			wantKind(t, err, errs.Conflict)
			if !hasField(err, "playa_name", "taken") {
				t.Fatalf("no playa_name/taken field in %v", err)
			}
			if r.rowExists(me) {
				t.Error("the refused first save left a row")
			}
			if r.keyHolders(people.NameKey(nmZorn)) != 1 {
				t.Error("the key is not held by exactly one row")
			}
			if r.nameOf(holder) != nmZorn {
				t.Error("the holder's name changed")
			}
		})
	}
}

// F74: the repair path (a nameless row that is then claimed) and a named-then-clash are
// both Conflict; a nameless row may take a free name.
func TestUpdateMe_NamelessRow_TakesFreeName_ButNotTaken(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: nmZorn})
	me := newPerson()
	r.insertPersonAs(me, seed{})
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(nmZorn)})
	wantCode(t, err, errs.Conflict, "taken")
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(nmOther)})
	wantNoErr(t, err)
	if r.nameOf(me) != nmOther {
		t.Error("the free name was not saved")
	}
}

func TestCreate_NameTaken_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: nmZorn})
	_, ctx := r.admin(permManage)
	before := r.personRows()
	_, err := r.svc.Create(ctx, people.ProfileInput{PlayaName: str(nmZornUp)})
	wantKind(t, err, errs.Conflict)
	if !hasField(err, "playa_name", "taken") {
		t.Fatalf("no playa_name/taken field in %v", err)
	}
	if n := r.personRows(); n != before {
		t.Errorf("%d rows after a refused create, want %d", n, before)
	}
	if r.count("people.person.created") != 0 || len(r.published(sigCreated)) != 0 {
		t.Error("a refused create audited or published")
	}
}

// F74 on the manager's path.
func TestUpdate_RenameToTakenName_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: nmZorn})
	target := r.insertPerson(seed{Playa: nmOther})
	_, ctx := r.admin(permManage)
	_, err := r.svc.Update(ctx, target, people.ProfileInput{PlayaName: str("zorn plume")})
	wantKind(t, err, errs.Conflict)
	if !hasField(err, "playa_name", "taken") {
		t.Fatalf("no playa_name/taken field in %v", err)
	}
	if r.nameOf(target) != nmOther {
		t.Error("the refused rename changed the name")
	}
	if r.count("people.person.changed") != 0 {
		t.Error("a refused rename was audited")
	}
}

// F74: a clash names nobody.
func TestUpdateMe_NameTaken_NamesNobody(t *testing.T) {
	r := newIRig(t, false)
	holder := r.insertPerson(seed{Playa: nmZorn})
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{PlayaName: str(nmZorn)})
	wantKind(t, err, errs.Conflict)
	msg := err.Error()
	for _, leak := range []string{holder.String(), "person_playa_name_key_idx"} {
		if leak != "" && strings.Contains(msg, leak) {
			t.Errorf("the error leaks %q", leak)
		}
	}
	for _, fe := range validate.Fields(err) {
		if fe.Field == "playa_name" && fe.Code == "taken" && len(fe.Params) != 0 {
			t.Errorf("taken carries params %v", fe.Params)
		}
	}
}

// F74
func TestUpdateMe_NameTaken_NothingAuditedOrPublished(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: nmZorn})
	before := len(r.rec.Recorded())
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{PlayaName: str(nmZorn), Pronouns: str("they/them")})
	wantKind(t, err, errs.Conflict)
	if got := len(r.rec.Recorded()); got != before {
		t.Errorf("%d audit entries after a clash, want %d", got, before)
	}
	r.quiet("clash")
}

// F75
func TestUpdateMe_ChangeSetName_ReturnsInvalidPermanent(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: nmZorn})
	for _, to := range []string{nmOther, "Zorn Plumes", "Zorn"} {
		_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(to), Pronouns: str("they/them")})
		wantField(t, err, "playa_name", "permanent")
	}
	if r.nameOf(me) != nmZorn {
		t.Error("the name changed")
	}
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if prof.Person.Pronouns != "" {
		t.Error("a refused save wrote another field")
	}
	r.quiet("permanent")
}

// F75: even a manager cannot use UpdateMe to rename; and a name that is free is still permanent.
func TestUpdateMe_ManagerRenamesThroughUpdateMe_ReturnsInvalidPermanent(t *testing.T) {
	r := newIRig(t, false)
	pid, ctx := r.admin(permManage)
	before := r.nameOf(pid)
	_, err := r.svc.UpdateMe(ctx, people.ProfileInput{PlayaName: str(nmOther)})
	wantField(t, err, "playa_name", "permanent")
	if r.nameOf(pid) != before {
		t.Error("the name changed")
	}
}

// F5 runs before F75: a blank is required, not permanent.
func TestUpdateMe_BlankWhileSet_RequiredBeatsPermanent(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: nmZorn})
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str("  ")})
	wantField(t, err, "playa_name", "required")
	if hasField(err, "playa_name", "permanent") {
		t.Error("permanent reported next to required")
	}
}

// F5/F75 order: a malformed new name reports its shape problem, not permanent.
func TestUpdateMe_MalformedNewNameWhileSet_ReportsShapeFirst(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: nmZorn})
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str("Bad\x07Name")})
	wantField(t, err, "playa_name", "control_chars")
}

// F76
func TestUpdateMe_SameName_NoOp(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: nmZorn, Pronouns: "they/them"})
	updated := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND updated_at = $2", [16]byte(me), r.clk.Now())
	wantNoErr(t, r.clk.Advance(time.Hour))
	prof, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(nmZorn), Pronouns: str("they/them")})
	wantNoErr(t, err)
	if prof.Person.PlayaName != nmZorn {
		t.Errorf("name = %q", prof.Person.PlayaName)
	}
	if r.count("people.person.changed") != 0 {
		t.Error("an identical save was audited")
	}
	if got := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND updated_at = $2", [16]byte(me), r.clk.Now()); got != 0 || updated != 1 {
		t.Error("an identical save touched updated_at")
	}
}

// F77
func TestUpdateMe_RespellSameKey_Saves(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: "zorn plume"})
	prof, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(nmZorn)})
	wantNoErr(t, err)
	if prof.Person.PlayaName != nmZorn {
		t.Errorf("name = %q, want the new spelling", prof.Person.PlayaName)
	}
	e := r.only("people.person.changed")
	if e.Subject != me || !slices.Equal(e.Fields, []string{"playa_name"}) {
		t.Errorf("entry = %+v", e)
	}
	if e.Reason != "" {
		t.Errorf("a self respelling carries Reason %q", e.Reason)
	}
	if got := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND playa_name_key = $2", [16]byte(me), people.NameKey(nmZorn)); got != 1 {
		t.Error("the stored key changed")
	}
	r.noLeak("zorn plume")
}

// F78
func TestUpdate_RenameByManager_SavesAndAuditsReason(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: nmZorn})
	_, ctx := r.admin(permManage)
	prof, err := r.svc.Update(ctx, target, people.ProfileInput{PlayaName: str(nmOther)})
	wantNoErr(t, err)
	if prof.Person.PlayaName != nmOther || r.nameOf(target) != nmOther {
		t.Errorf("name = %q", prof.Person.PlayaName)
	}
	e := r.only("people.person.changed")
	if e.Subject != target || !slices.Equal(e.Fields, []string{"playa_name"}) || e.Reason != "rename" {
		t.Errorf("entry = %+v", e)
	}
	// The old name is free at once.
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{PlayaName: str(nmZorn)})
	wantNoErr(t, err)
	if r.keyHolders(people.NameKey(nmOther)) != 1 {
		t.Error("the new key is not held by exactly one row")
	}
	r.noLeak(nmZorn, nmOther, "zorn plume", "quill marrow")
}

// F78: a manager's own name.
func TestUpdate_RenameOwnNameByManager_SavesAndAuditsReason(t *testing.T) {
	r := newIRig(t, false)
	mgr, ctx := r.admin(permManage)
	_, err := r.svc.Update(ctx, mgr, people.ProfileInput{PlayaName: str(nmOther)})
	wantNoErr(t, err)
	if r.nameOf(mgr) != nmOther {
		t.Errorf("name = %q", r.nameOf(mgr))
	}
	e := r.only("people.person.changed")
	if e.Subject != mgr || !slices.Equal(e.Fields, []string{"playa_name"}) || e.Reason != "rename" {
		t.Errorf("entry = %+v", e)
	}
}

// F78: a manager's own rename still obeys the taken rule.
func TestUpdate_RenameOwnNameByManager_TakenName_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	r.insertPerson(seed{Playa: nmZorn})
	mgr, ctx := r.admin(permManage)
	before := r.nameOf(mgr)
	_, err := r.svc.Update(ctx, mgr, people.ProfileInput{PlayaName: str(nmZorn)})
	wantCode(t, err, errs.Conflict, "taken")
	if r.nameOf(mgr) != before {
		t.Error("name changed")
	}
}

// F78: a respelling by a manager is not a rename (same key); either way the name is saved.
func TestUpdate_ManagerRespellsSameKey_Saves(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "zorn plume"})
	_, ctx := r.admin(permManage)
	_, err := r.svc.Update(ctx, target, people.ProfileInput{PlayaName: str(nmZorn)})
	wantNoErr(t, err)
	if r.nameOf(target) != nmZorn {
		t.Error("not saved")
	}
}

// F78 permission: without people.profiles.manage Update is Permission and the name stays.
func TestUpdate_RenameWithoutManage_ReturnsPermission(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: nmZorn})
	_, ctx := r.admin(permDirectory, permContacts)
	_, err := r.svc.Update(ctx, target, people.ProfileInput{PlayaName: str(nmOther)})
	wantKind(t, err, errs.Permission)
	if r.nameOf(target) != nmZorn {
		t.Error("name changed")
	}
	// Update on your own ID without the permission delegates to UpdateMe: permanent.
	me := r.insertPerson(seed{Playa: "Plain Wren"})
	r.grant(me, permDirectory)
	_, err = r.svc.Update(asPerson(ctxT(t), me), me, people.ProfileInput{PlayaName: str(nmOther)})
	if err == nil {
		t.Fatal("a non-manager renamed themself through Update")
	}
	if !errs.Is(err, errs.Permission) && !hasField(err, "playa_name", "permanent") {
		t.Errorf("kind = %v (%v), want Permission or Invalid permanent", errs.KindOf(err), err)
	}
	if r.nameOf(me) != "Plain Wren" {
		t.Error("name changed")
	}
}

// F79
func TestUpdate_ClearSetName_ReturnsInvalidRequired(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: nmZorn})
	_, ctx := r.admin(permManage)
	for _, blank := range []string{"", "   "} {
		_, err := r.svc.Update(ctx, target, people.ProfileInput{PlayaName: str(blank)})
		wantField(t, err, "playa_name", "required")
	}
	if r.nameOf(target) != nmZorn || r.keyHolders(people.NameKey(nmZorn)) != 1 {
		t.Error("the name or its key was cleared")
	}
}

// Update leaving the name out never touches it.
func TestUpdate_NameAbsent_KeepsNameAndKey(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: nmZorn})
	_, ctx := r.admin(permManage)
	_, err := r.svc.Update(ctx, target, people.ProfileInput{Pronouns: str("he/him")})
	wantNoErr(t, err)
	e := r.only("people.person.changed")
	if has(e.Fields, "playa_name") || e.Reason == "rename" {
		t.Errorf("entry = %+v", e)
	}
	if r.keyHolders(people.NameKey(nmZorn)) != 1 {
		t.Error("key lost")
	}
}

// Update that sets the identical name is not a rename.
func TestUpdate_SameName_NotAuditedAsRename(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: nmZorn})
	_, ctx := r.admin(permManage)
	_, err := r.svc.Update(ctx, target, people.ProfileInput{PlayaName: str(nmZorn)})
	wantNoErr(t, err)
	if r.count("people.person.changed") != 0 {
		t.Error("an identical name was audited as a change")
	}
}

// F80
func TestUpdateMe_NameRace_OneWins(t *testing.T) {
	r := newIRig(t, false)
	const n = 6
	start := make(chan struct{})
	errsOut := make([]error, n)
	ppl := make([]model.PersonID, n)
	var wg sync.WaitGroup
	for i := range n {
		ppl[i] = newPerson()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claim := nmZorn
			if i%2 == 1 {
				claim = nmZornUp
			}
			_, errsOut[i] = r.svc.UpdateMe(asPerson(ctxT(t), ppl[i]), people.ProfileInput{PlayaName: str(claim)})
		}()
	}
	close(start)
	wg.Wait()
	wins := 0
	for i, err := range errsOut {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && hasField(err, "playa_name", "taken"):
		default:
			t.Errorf("claimant %d: kind %v: %v (want nil or Conflict taken; no deadlock)", i, errs.KindOf(err), err)
		}
	}
	if wins != 1 {
		t.Errorf("%d claimants won, want exactly 1", wins)
	}
	if got := r.keyHolders(people.NameKey(nmZorn)); got != 1 {
		t.Errorf("%d rows hold the key, want 1", got)
	}
	if got := r.personRows(); got != 1 {
		t.Errorf("%d person rows, want 1 (losers leave no row)", got)
	}
	if got := len(r.published(sigCreated)); got != 1 {
		t.Errorf("%d people.person-created signals, want 1", got)
	}
}

// F83
func TestUpdateMe_LanguageOnly_IgnoresPermanence(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	r.insertPersonAs(me, seed{Playa: nmZorn})
	prof, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{Language: str("es")})
	wantNoErr(t, err)
	if prof.Person.Language != "es" || prof.Person.PlayaName != nmZorn {
		t.Errorf("profile = %+v", prof.Person)
	}
	// A nameless row can save a language too.
	nameless := newPerson()
	r.insertPersonAs(nameless, seed{})
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), nameless), people.ProfileInput{Language: str("en")})
	wantNoErr(t, err)
}

// F84
func TestHandleAccountCreated_TwoNamelessRows_BothInserted(t *testing.T) {
	r := newIRig(t, true)
	a := r.signUp("nameless.one", true)
	b := r.signUp("nameless.two", true)
	if !r.rowExists(a.person) || !r.rowExists(b.person) {
		t.Fatal("a nameless row was not inserted")
	}
	if got := r.keyHolders(""); got != 2 {
		t.Errorf("%d rows with the empty key, want 2", got)
	}
	// They can still choose different names afterwards, and not the same one.
	_, err := r.svc.UpdateMe(a.ctx(t), people.ProfileInput{PlayaName: str(nmZorn)})
	wantNoErr(t, err)
	_, err = r.svc.UpdateMe(b.ctx(t), people.ProfileInput{PlayaName: str(nmZornUp)})
	wantCode(t, err, errs.Conflict, "taken")
	_, err = r.svc.UpdateMe(b.ctx(t), people.ProfileInput{PlayaName: str(nmOther)})
	wantNoErr(t, err)
}

// The key is internal: it is never in the profile JSON, logs, spans or audit.
func TestPlayaNameKey_NeverExposed(t *testing.T) {
	r := newIRig(t, false)
	me := newPerson()
	_, err := r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str(nmZornUp)})
	wantNoErr(t, err)
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), me), people.ProfileInput{PlayaName: str("ZORN plume")})
	wantNoErr(t, err)
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), newPerson()), people.ProfileInput{PlayaName: str(nmZorn)})
	wantKind(t, err, errs.Conflict)
	r.noLeak(people.NameKey(nmZorn), nmZorn, "ZORN plume")
}
