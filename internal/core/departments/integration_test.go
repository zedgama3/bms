//go:build integration

package departments_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func payloadDept(t testing.TB, s signals.Signal) model.DepartmentID {
	t.Helper()
	id, err := signals.Get[model.DepartmentID](s.Payload, departments.PayloadDepartment)
	if err != nil {
		t.Fatalf("signal %s has no department payload: %v", s.Name, err)
	}
	return id
}

func isRef(e audit.Entry, id model.DepartmentID) bool {
	return e.Record.Kind == "departments.department" && e.Record.ID == [16]byte(id)
}

// ---- migrations ------------------------------------------------------------------------

func TestMigrations_ApplyOnEmptyDatabase_AndAgain(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateDepartments(t, pool)
	migrateDepartments(t, pool) // idempotent: nothing left to apply, no error
	r := newRig(t, pool)
	got, err := r.svc.List(asSuper(ctxT(t), newPerson()), departments.ListQuery{IncludeArchived: true})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("a fresh database holds departments: %v", got)
	}
}

// ---- Create ----------------------------------------------------------------------------

func TestCreate_Succeeds_StoresRecordsAndPublishes(t *testing.T) {
	r := newIRig(t)
	advance(t, r.clk, time.Hour)
	d, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: "Rangers", Description: "Keep people safe", ContactEmail: "rangers@example.org"})
	wantNoErr(t, err)
	if d.ID.IsZero() || d.Name != "Rangers" || d.Description != "Keep people safe" || d.ContactEmail != "rangers@example.org" {
		t.Errorf("Create returned %+v", d)
	}
	if d.Archived || !d.ArchivedAt.IsZero() {
		t.Errorf("a new department is archived: %+v", d)
	}
	if !d.CreatedAt.Equal(r.clk.Now()) || !d.UpdatedAt.Equal(r.clk.Now()) {
		t.Errorf("times = %v, %v; want the clock's %v", d.CreatedAt, d.UpdatedAt, r.clk.Now())
	}
	got, err := r.svc.Get(asPerson(ctxT(t), newPerson()), d.ID)
	wantNoErr(t, err)
	if got.ID != d.ID || got.Name != "Rangers" || got.ContactEmail != "rangers@example.org" {
		t.Errorf("Get = %+v", got)
	}

	es := r.rec.Recorded()
	if len(es) != 1 || es[0].Action != "departments.department.created" {
		t.Fatalf("audit = %v, want exactly departments.department.created", actionsOf(es))
	}
	if !isRef(es[0], d.ID) {
		t.Errorf("audit record = %+v, want departments.department/%s", es[0].Record, d.ID)
	}
	if !es[0].Subject.IsZero() {
		t.Error("the audit entry has a subject; departments hold nothing about a person")
	}
	if es[0].Outcome != audit.Succeeded {
		t.Errorf("outcome = %v", es[0].Outcome)
	}
	if len(es[0].Fields) > 0 && es[0].Level != model.SensitivityInternal {
		t.Errorf("level = %v, want internal", es[0].Level)
	}
	sigs := sigsOf(r.bus.Published(), departments.SignalCreated)
	if len(sigs) != 1 || payloadDept(t, sigs[0]) != d.ID {
		t.Errorf("signals = %v", r.bus.Published())
	}
	if len(r.bus.Published()) != 1 {
		t.Errorf("Create published %d signals, want 1", len(r.bus.Published()))
	}
}

func TestCreate_BoundaryValues_Accepted(t *testing.T) {
	r := newIRig(t)
	for name, in := range map[string]departments.CreateInput{
		"name at 100 bytes":         {Name: repeat("a", 100)},
		"name of 50 two-byte runes": {Name: repeat("é", 50)},
		"description at 2000":       {Name: "Desc", Description: repeat("d", 2000)},
		"no contact":                {Name: "No contact"},
		"empty description":         {Name: "Empty desc", Description: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := r.svc.Create(r.sctx(), in); err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

// F11
func TestCreate_NameTaken_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	first := r.mk("Rangers")
	r.mkArchived("Gate")
	for _, name := range []string{"Rangers", "rangers", "RANGERS", "rAnGeRs", "Gate", "gate"} {
		t.Run(name, func(t *testing.T) {
			e0, s0 := r.mark()
			_, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: name})
			wantCoded(t, err, errs.Conflict, "name", "taken")
			if e, s := r.mark(); e != e0 || s != s0 {
				t.Error("a refused create recorded an audit entry or published a signal")
			}
		})
	}
	got, err := r.svc.Get(r.sctx(), first.ID)
	wantNoErr(t, err)
	if got.Name != "Rangers" {
		t.Errorf("the original was changed: %+v", got)
	}
}

// F11 under concurrency: the unique index is the serialisation.
func TestCreate_ConcurrentSameName_OneWins(t *testing.T) {
	r := newIRig(t)
	const n = 8
	errsOut := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "Rangers"
			if i%2 == 1 {
				name = "RANGERS"
			}
			_, errsOut[i] = r.svc.Create(r.sctx(), departments.CreateInput{Name: name})
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errsOut {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && hasField(err, "name", "taken"):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d creates succeeded, want 1", wins)
	}
	if got := len(sigsOf(r.bus.Published(), departments.SignalCreated)); got != 1 {
		t.Errorf("%d created signals, want 1", got)
	}
	if got := len(entriesOf(r.rec.Recorded(), "departments.department.created")); got != 1 {
		t.Errorf("%d created audit entries, want 1", got)
	}
}

// Permission table for departments.manage, success side.
func TestCreate_Permission_HolderAndSuperuserSucceed(t *testing.T) {
	r := newIRig(t)
	holder := r.holder(departments.PermissionManage)
	if _, err := r.svc.Create(asPerson(ctxT(t), holder), departments.CreateInput{Name: "By holder"}); err != nil {
		t.Errorf("holder: %v", err)
	}
	if _, err := r.svc.Create(asSuper(ctxT(t), newPerson()), departments.CreateInput{Name: "By superuser"}); err != nil {
		t.Errorf("superuser: %v", err)
	}
	if _, err := r.svc.Create(asPerson(ctxT(t), newPerson()), departments.CreateInput{Name: "By nobody"}); !errs.Is(err, errs.Permission) {
		t.Errorf("non-holder: %v, want Permission", err)
	}
	if _, err := r.svc.Create(asPerson(ctxT(t), r.holder(departments.PermissionDetailsManage)), departments.CreateInput{Name: "By admin"}); !errs.Is(err, errs.Permission) {
		t.Errorf("details holder: %v, want Permission", err)
	}
}

// ---- Get and List ----------------------------------------------------------------------

// F4
func TestGet_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.Get(asPerson(ctxT(t), newPerson()), newDept())
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.Get(r.sctx(), newDept())
	wantKind(t, err, errs.NotFound)
}

// F5: never Permission, so a response cannot confirm that the department exists.
func TestGet_ArchivedWithoutPermission_ReturnsNotFound(t *testing.T) {
	r := newIRig(t)
	d := r.mkArchived("Gate")
	other := newDept()
	tests := map[string]func(p model.PersonID){
		"no grants":                   func(model.PersonID) {},
		"details at another dept":     func(p model.PersonID) { r.az.GrantIn(p, departments.PermissionDetailsManage, other) },
		"manage at a department only": func(p model.PersonID) { r.az.GrantIn(p, departments.PermissionManage, d.ID) },
		"an unrelated permission":     func(p model.PersonID) { r.az.Grant(p, "events.manage") },
	}
	for name, grant := range tests {
		t.Run(name, func(t *testing.T) {
			p := newPerson()
			grant(p)
			_, err := r.svc.Get(asPerson(ctxT(t), p), d.ID)
			wantKind(t, err, errs.NotFound)
			if errs.Is(err, errs.Permission) {
				t.Error("an archived department answered Permission")
			}
		})
	}
}

// F6 and decision 3.
func TestGet_ArchivedWithPermission_Succeeds(t *testing.T) {
	r := newIRig(t)
	d := r.mkArchived("Gate")
	tests := map[string]func(p model.PersonID) context.Context{
		"details at the department": func(p model.PersonID) context.Context {
			r.az.GrantIn(p, departments.PermissionDetailsManage, d.ID)
			return asPerson(ctxT(t), p)
		},
		"details site-wide": func(p model.PersonID) context.Context {
			r.az.Grant(p, departments.PermissionDetailsManage)
			return asPerson(ctxT(t), p)
		},
		"manage": func(p model.PersonID) context.Context {
			r.az.Grant(p, departments.PermissionManage)
			return asPerson(ctxT(t), p)
		},
		"superuser": func(p model.PersonID) context.Context { return asSuper(ctxT(t), p) },
	}
	for name, mk := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := r.svc.Get(mk(newPerson()), d.ID)
			wantNoErr(t, err)
			if !got.Archived || got.ArchivedAt.IsZero() || got.ID != d.ID {
				t.Errorf("Get = %+v, want the archived department", got)
			}
		})
	}
}

func TestList_SortedByName_ArchivedHiddenUnlessAsked(t *testing.T) {
	r := newIRig(t)
	for _, n := range []string{"zeta", "Alpha", "beta", "Gamma"} {
		r.mk(n)
	}
	r.mkArchived("Aardvark")

	names := func(ds []departments.Department) []string {
		var out []string
		for _, d := range ds {
			out = append(out, d.Name)
		}
		return out
	}
	anyone := asPerson(ctxT(t), newPerson())
	got, err := r.svc.List(anyone, departments.ListQuery{})
	wantNoErr(t, err)
	if want := []string{"Alpha", "beta", "Gamma", "zeta"}; !slices.Equal(names(got), want) {
		t.Errorf("List = %v, want %v (by lower(name), no archived)", names(got), want)
	}
	for _, d := range got {
		if d.Archived {
			t.Errorf("%s is archived in the default list", d.Name)
		}
	}

	all, err := r.svc.List(r.sctx(), departments.ListQuery{IncludeArchived: true})
	wantNoErr(t, err)
	if want := []string{"Aardvark", "Alpha", "beta", "Gamma", "zeta"}; !slices.Equal(names(all), want) {
		t.Errorf("List(archived) = %v, want %v", names(all), want)
	}
	for _, d := range all {
		if d.Archived != (d.Name == "Aardvark") {
			t.Errorf("%s: Archived = %v", d.Name, d.Archived)
		}
	}
}

// ---- Update ----------------------------------------------------------------------------

// F16
func TestUpdate_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.Update(r.sctx(), newDept(), departments.UpdateInput{Description: str("x")})
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.Update(r.sctx(), newDept(), departments.UpdateInput{Name: str("x")})
	wantKind(t, err, errs.NotFound)
}

func TestUpdate_Details_ChangesFieldsAndRecordsChanged(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Rangers")
	advance(t, r.clk, time.Hour)
	e0, s0 := r.mark()
	got, err := r.svc.Update(r.sctx(), d.ID, departments.UpdateInput{Description: str("New text"), ContactEmail: str("rangers@example.org")})
	wantNoErr(t, err)
	if got.Name != "Rangers" || got.Description != "New text" || got.ContactEmail != "rangers@example.org" {
		t.Errorf("Update = %+v", got)
	}
	if !got.UpdatedAt.After(d.UpdatedAt) || !got.UpdatedAt.Equal(r.clk.Now()) || !got.CreatedAt.Equal(d.CreatedAt) {
		t.Errorf("times: created %v updated %v (was %v, clock %v)", got.CreatedAt, got.UpdatedAt, d.UpdatedAt, r.clk.Now())
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "departments.department.changed" || !isRef(es[0], d.ID) {
		t.Fatalf("audit = %v, want one departments.department.changed", actionsOf(es))
	}
	for _, f := range []string{"description", "contact_email"} {
		if !slices.Contains(es[0].Fields, f) {
			t.Errorf("audit fields %v lack %q", es[0].Fields, f)
		}
	}
	if slices.Contains(es[0].Fields, "name") {
		t.Errorf("audit fields %v name an unchanged field", es[0].Fields)
	}
	if len(r.bus.Published()) != s0 {
		t.Error("a detail change published a signal (nobody reacts to it)")
	}
	// Clearing the mailbox with an explicit empty string is a change, not "unchanged".
	got, err = r.svc.Update(r.sctx(), d.ID, departments.UpdateInput{ContactEmail: str("")})
	wantNoErr(t, err)
	if got.ContactEmail != "" {
		t.Errorf("ContactEmail = %q after clearing", got.ContactEmail)
	}
	if got.Description != "New text" {
		t.Errorf("Description = %q, a nil field must stay unchanged", got.Description)
	}
}

func TestUpdate_Rename_RecordsRenamed(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Rangers")
	e0, s0 := r.mark()
	got, err := r.svc.Update(r.sctx(), d.ID, departments.UpdateInput{Name: str("Park Rangers")})
	wantNoErr(t, err)
	if got.Name != "Park Rangers" || got.Description != d.Description {
		t.Errorf("Update = %+v", got)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "departments.department.renamed" || !isRef(es[0], d.ID) {
		t.Fatalf("audit = %v, want one departments.department.renamed", actionsOf(es))
	}
	if len(r.bus.Published()) != s0 {
		t.Error("a rename published a signal")
	}
}

func TestUpdate_RenameAndDetails_RecordsBoth(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Rangers")
	e0, _ := r.mark()
	_, err := r.svc.Update(r.sctx(), d.ID, departments.UpdateInput{Name: str("Park Rangers"), Description: str("x")})
	wantNoErr(t, err)
	acts := actionsOf(r.rec.Recorded()[e0:])
	slices.Sort(acts)
	if want := []string{"departments.department.changed", "departments.department.renamed"}; !slices.Equal(acts, want) {
		t.Errorf("audit = %v, want %v", acts, want)
	}
}

// F13 success side and F14: details permission is scoped to the department.
func TestUpdate_PermissionByField_AllowedHolders(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Rangers")
	other := r.mk("Gate")

	lead := newPerson()
	r.az.GrantIn(lead, departments.PermissionDetailsManage, d.ID)

	_, err := r.svc.Update(asPerson(ctxT(t), lead), d.ID, departments.UpdateInput{Description: str("by the lead")})
	wantNoErr(t, err)
	_, err = r.svc.Update(asPerson(ctxT(t), lead), other.ID, departments.UpdateInput{Description: str("elsewhere")})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Update(asPerson(ctxT(t), lead), d.ID, departments.UpdateInput{Name: str("Renamed by lead")})
	wantKind(t, err, errs.Permission)
	_, err = r.svc.Update(asPerson(ctxT(t), lead), d.ID, departments.UpdateInput{Name: str("Renamed"), Description: str("x")})
	wantKind(t, err, errs.Permission)

	got, err := r.svc.Get(r.sctx(), d.ID)
	wantNoErr(t, err)
	if got.Name != "Rangers" || got.Description != "by the lead" {
		t.Errorf("after refused updates the department is %+v", got)
	}
	gotOther, err := r.svc.Get(r.sctx(), other.ID)
	wantNoErr(t, err)
	if gotOther.Description != other.Description {
		t.Errorf("a refused update changed the other department: %+v", gotOther)
	}

	steward := r.holder(departments.PermissionManage, departments.PermissionDetailsManage)
	_, err = r.svc.Update(asPerson(ctxT(t), steward), d.ID, departments.UpdateInput{Name: str("Renamed"), Description: str("both")})
	wantNoErr(t, err)
	_, err = r.svc.Update(asSuper(ctxT(t), newPerson()), d.ID, departments.UpdateInput{Description: str("by superuser")})
	wantNoErr(t, err)
}

// F15
func TestUpdate_Archived_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	d := r.mkArchived("Gate")
	for name, in := range map[string]departments.UpdateInput{
		"details": {Description: str("x")},
		"rename":  {Name: str("Gate 2")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := r.svc.Update(r.sctx(), d.ID, in)
			wantCoded(t, err, errs.Conflict, "", "archived")
		})
	}
}

// F17
func TestUpdate_NameTaken_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	r.mk("Rangers")
	b := r.mk("Gate")
	r.mkArchived("Temple")
	for _, name := range []string{"rangers", "RANGERS", "temple"} {
		t.Run(name, func(t *testing.T) {
			e0, s0 := r.mark()
			_, err := r.svc.Update(r.sctx(), b.ID, departments.UpdateInput{Name: str(name)})
			wantCoded(t, err, errs.Conflict, "name", "taken")
			if e, s := r.mark(); e != e0 || s != s0 {
				t.Error("a refused rename recorded an audit entry or published a signal")
			}
		})
	}
	got, err := r.svc.Get(r.sctx(), b.ID)
	wantNoErr(t, err)
	if got.Name != "Gate" {
		t.Errorf("Name = %q after refused renames", got.Name)
	}
}

// ---- Archive and Restore ---------------------------------------------------------------

func TestArchive_FlipsRecordsAndPublishes(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Gate")
	advance(t, r.clk, time.Hour)
	e0, s0 := r.mark()
	got, err := r.svc.Archive(r.sctx(), d.ID)
	wantNoErr(t, err)
	if !got.Archived || !got.ArchivedAt.Equal(r.clk.Now()) {
		t.Errorf("Archive = %+v, want archived at %v", got, r.clk.Now())
	}
	if got.Name != d.Name || got.Description != d.Description {
		t.Errorf("Archive changed other fields: %+v", got)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "departments.department.archived" || !isRef(es[0], d.ID) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	ss := r.bus.Published()[s0:]
	if len(ss) != 1 || ss[0].Name != departments.SignalArchived || payloadDept(t, ss[0]) != d.ID {
		t.Errorf("signals = %v", ss)
	}
	ok, err := r.svc.Active(ctxT(t), d.ID)
	wantNoErr(t, err)
	if ok {
		t.Error("Active = true for an archived department")
	}
}

func TestRestore_FlipsRecordsAndPublishes(t *testing.T) {
	r := newIRig(t)
	d := r.mkArchived("Gate")
	e0, s0 := r.mark()
	got, err := r.svc.Restore(r.sctx(), d.ID)
	wantNoErr(t, err)
	if got.Archived || !got.ArchivedAt.IsZero() {
		t.Errorf("Restore = %+v, want active", got)
	}
	es := r.rec.Recorded()[e0:]
	if len(es) != 1 || es[0].Action != "departments.department.restored" || !isRef(es[0], d.ID) {
		t.Fatalf("audit = %v", actionsOf(es))
	}
	ss := r.bus.Published()[s0:]
	if len(ss) != 1 || ss[0].Name != departments.SignalRestored || payloadDept(t, ss[0]) != d.ID {
		t.Errorf("signals = %v", ss)
	}
	ok, err := r.svc.Active(ctxT(t), d.ID)
	wantNoErr(t, err)
	if !ok {
		t.Error("Active = false after Restore")
	}
	// A restored department takes edits again.
	_, err = r.svc.Update(r.sctx(), d.ID, departments.UpdateInput{Description: str("back")})
	wantNoErr(t, err)
}

// F20, F24 (unknown halves)
func TestArchiveRestore_Unknown_ReturnsNotFound(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.Archive(r.sctx(), newDept())
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.Restore(r.sctx(), newDept())
	wantKind(t, err, errs.NotFound)
	_, err = r.svc.Archive(r.sctx(), model.DepartmentID{})
	if !errs.Is(err, errs.NotFound) && !errs.Is(err, errs.Invalid) {
		t.Errorf("Archive(zero) = %v, want NotFound or Invalid", err)
	}
}

// F21
func TestArchive_Twice_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Gate")
	_, err := r.svc.Archive(r.sctx(), d.ID)
	wantNoErr(t, err)
	e0, s0 := r.mark()
	_, err = r.svc.Archive(r.sctx(), d.ID)
	wantCoded(t, err, errs.Conflict, "", "archived")
	if e, s := r.mark(); e != e0 || s != s0 {
		t.Error("a refused archive recorded an audit entry or published a signal")
	}
}

// F23
func TestRestore_Active_ReturnsConflict(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Gate")
	e0, s0 := r.mark()
	_, err := r.svc.Restore(r.sctx(), d.ID)
	wantCoded(t, err, errs.Conflict, "", "not_archived")
	if e, s := r.mark(); e != e0 || s != s0 {
		t.Error("a refused restore recorded an audit entry or published a signal")
	}
}

// F22
func TestArchive_Concurrent_OneWins(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Gate")
	const n = 8
	errsOut := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errsOut[i] = r.svc.Archive(r.sctx(), d.ID)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errsOut {
		switch {
		case err == nil:
			wins++
		case errs.Is(err, errs.Conflict) && hasField(err, "", "archived"):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d archives succeeded, want 1", wins)
	}
	if got := len(sigsOf(r.bus.Published(), departments.SignalArchived)); got != 1 {
		t.Errorf("%d archived signals, want 1", got)
	}
	if got := len(entriesOf(r.rec.Recorded(), "departments.department.archived")); got != 1 {
		t.Errorf("%d archived audit entries, want 1", got)
	}
}

// Archive then restore under concurrency never leaves the department in a state a later
// call cannot read.
func TestArchiveRestore_ConcurrentMix_LeavesConsistentState(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Gate")
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				if _, err := r.svc.Archive(r.sctx(), d.ID); err != nil && !errs.Is(err, errs.Conflict) {
					t.Errorf("Archive: %v", err)
				}
			} else {
				if _, err := r.svc.Restore(r.sctx(), d.ID); err != nil && !errs.Is(err, errs.Conflict) {
					t.Errorf("Restore: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	got, err := r.svc.Get(r.sctx(), d.ID)
	wantNoErr(t, err)
	ok, err := r.svc.Active(ctxT(t), d.ID)
	wantNoErr(t, err)
	if ok == got.Archived {
		t.Errorf("Active = %v but Archived = %v", ok, got.Archived)
	}
	archivedSigs := len(sigsOf(r.bus.Published(), departments.SignalArchived))
	restoredSigs := len(sigsOf(r.bus.Published(), departments.SignalRestored))
	if d := archivedSigs - restoredSigs; (got.Archived && d != 1) || (!got.Archived && d != 0) {
		t.Errorf("archived %d, restored %d signals, but Archived = %v", archivedSigs, restoredSigs, got.Archived)
	}
}

// Decision 15: a restore never collides, because names stay unique among archived ones too.
func TestRestore_NeverCollides(t *testing.T) {
	r := newIRig(t)
	d := r.mkArchived("Gate")
	_, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: "gate"})
	wantCoded(t, err, errs.Conflict, "name", "taken")
	_, err = r.svc.Restore(r.sctx(), d.ID)
	wantNoErr(t, err)
}

// ---- NamesOf and Active ----------------------------------------------------------------

// F27, F28
func TestNamesOf_UnknownAbsent_ArchivedIncluded(t *testing.T) {
	r := newIRig(t)
	a := r.mk("Rangers")
	b := r.mkArchived("Gate")
	unknown := newDept()
	got, err := r.svc.NamesOf(ctxT(t), []model.DepartmentID{a.ID, b.ID, unknown, a.ID})
	wantNoErr(t, err)
	if len(got) != 2 || got[a.ID] != "Rangers" || got[b.ID] != "Gate" {
		t.Errorf("NamesOf = %v", got)
	}
	if _, present := got[unknown]; present {
		t.Error("an unknown ID is present in the map")
	}
	// A rename shows up at once.
	_, err = r.svc.Update(r.sctx(), a.ID, departments.UpdateInput{Name: str("Park Rangers")})
	wantNoErr(t, err)
	got, err = r.svc.NamesOf(ctxT(t), []model.DepartmentID{a.ID})
	wantNoErr(t, err)
	if got[a.ID] != "Park Rangers" {
		t.Errorf("NamesOf after rename = %v", got)
	}
}

func TestNamesOf_TwoHundredIDs_OneCall(t *testing.T) {
	r := newIRig(t)
	ids := make([]model.DepartmentID, 0, departments.MaxNames)
	for i := range departments.MaxNames {
		if i < 5 {
			ids = append(ids, r.mk(fmt.Sprintf("Dept %d", i)).ID)
		} else {
			ids = append(ids, newDept())
		}
	}
	got, err := r.svc.NamesOf(ctxT(t), ids)
	wantNoErr(t, err)
	if len(got) != 5 {
		t.Errorf("NamesOf returned %d names, want 5", len(got))
	}
	_, err = r.svc.NamesOf(ctxT(t), append(ids, newDept()))
	wantField(t, err, "ids", "too_many")
}

// F30
func TestActive_UnknownOrArchived_False(t *testing.T) {
	r := newIRig(t)
	active := r.mk("Rangers")
	archived := r.mkArchived("Gate")
	for name, tc := range map[string]struct {
		id   model.DepartmentID
		want bool
	}{
		"active":   {active.ID, true},
		"archived": {archived.ID, false},
		"unknown":  {newDept(), false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := r.svc.Active(ctxT(t), tc.id)
			wantNoErr(t, err)
			if got != tc.want {
				t.Errorf("Active = %v, want %v", got, tc.want)
			}
		})
	}
}

// F31: Active reads through the transaction the ctx carries, so a caller inside a Write
// transaction sees its own uncommitted department, and a rolled-back one never appears.
func TestActive_InsideTransaction_UsesIt(t *testing.T) {
	r := newIRig(t)
	rollback := errors.New("roll this back")
	var inside, outside bool
	var id model.DepartmentID
	err := r.pool.Transact(r.sctx(), db.Write, func(ctx context.Context) error {
		d, err := r.svc.Create(ctx, departments.CreateInput{Name: "Uncommitted"})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		id = d.ID
		if inside, err = r.svc.Active(ctx, d.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		if outside, err = r.svc.Active(r.sctx(), d.ID); err != nil { //nolint:contextcheck // a fresh ctx on purpose: another connection
			return err //nolint:wrapcheck // test
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Transact = %v", err)
	}
	if !inside {
		t.Error("Active inside the transaction did not see the department created in it")
	}
	if outside {
		t.Error("Active outside the transaction saw an uncommitted department")
	}
	ok, err := r.svc.Active(ctxT(t), id)
	wantNoErr(t, err)
	if ok {
		t.Error("a rolled-back department is active")
	}
	if got := sigsOf(r.bus.Published(), departments.SignalCreated); len(got) != 0 {
		t.Errorf("a signal for a rolled-back create was delivered: %v", got)
	}
}

// ---- audit and publish failures roll the write back ------------------------------------

func TestWrite_AuditFails_RollsBack(t *testing.T) {
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit down"))
	type op struct {
		name   string
		action audit.Action
		setup  func(r *irig) model.DepartmentID
		call   func(r *irig, id model.DepartmentID) error
		check  func(t *testing.T, r *irig, id model.DepartmentID)
	}
	ops := []op{
		{"Create", "departments.department.created",
			func(*irig) model.DepartmentID { return model.DepartmentID{} },
			func(r *irig, _ model.DepartmentID) error {
				_, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: "Rangers"})
				return keep(err)
			},
			func(t *testing.T, r *irig, _ model.DepartmentID) {
				got, err := r.svc.List(r.sctx(), departments.ListQuery{IncludeArchived: true})
				wantNoErr(t, err)
				if len(got) != 0 {
					t.Errorf("a department exists after the failed create: %v", got)
				}
			}},
		{"Update rename", "departments.department.renamed",
			func(r *irig) model.DepartmentID { return r.mk("Rangers").ID },
			func(r *irig, id model.DepartmentID) error {
				_, err := r.svc.Update(r.sctx(), id, departments.UpdateInput{Name: str("Renamed")})
				return keep(err)
			},
			func(t *testing.T, r *irig, id model.DepartmentID) {
				got, err := r.svc.Get(r.sctx(), id)
				wantNoErr(t, err)
				if got.Name != "Rangers" {
					t.Errorf("name = %q after the failed rename", got.Name)
				}
			}},
		{"Update details", "departments.department.changed",
			func(r *irig) model.DepartmentID { return r.mk("Rangers").ID },
			func(r *irig, id model.DepartmentID) error {
				_, err := r.svc.Update(r.sctx(), id, departments.UpdateInput{Description: str("changed")})
				return keep(err)
			},
			func(t *testing.T, r *irig, id model.DepartmentID) {
				got, err := r.svc.Get(r.sctx(), id)
				wantNoErr(t, err)
				if got.Description == "changed" {
					t.Error("description changed despite the failed audit write")
				}
			}},
		{"Archive", "departments.department.archived",
			func(r *irig) model.DepartmentID { return r.mk("Rangers").ID },
			func(r *irig, id model.DepartmentID) error { _, err := r.svc.Archive(r.sctx(), id); return keep(err) },
			func(t *testing.T, r *irig, id model.DepartmentID) {
				ok, err := r.svc.Active(ctxT(t), id)
				wantNoErr(t, err)
				if !ok {
					t.Error("the department is archived despite the failed audit write")
				}
			}},
		{"Restore", "departments.department.restored",
			func(r *irig) model.DepartmentID { return r.mkArchived("Rangers").ID },
			func(r *irig, id model.DepartmentID) error { _, err := r.svc.Restore(r.sctx(), id); return keep(err) },
			func(t *testing.T, r *irig, id model.DepartmentID) {
				ok, err := r.svc.Active(ctxT(t), id)
				wantNoErr(t, err)
				if ok {
					t.Error("the department is restored despite the failed audit write")
				}
			}},
	}
	for _, o := range ops {
		t.Run(o.name, func(t *testing.T) {
			r := newIRig(t)
			id := o.setup(r)
			_, s0 := r.mark()
			r.gate.failAction(o.action, boom)
			err := o.call(r, id)
			if err == nil || !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the audit error", err)
			}
			wantKind(t, err, errs.Unavailable)
			if got := len(r.bus.Published()); got != s0 {
				t.Errorf("%d signals published for a rolled-back write", got-s0)
			}
			o.check(t, r, id)
		})
	}
}

func TestWrite_PublishFails_RollsBack(t *testing.T) {
	boom := errs.E("test.bus", errs.Unavailable, errors.New("bus closed"))
	r := newIRig(t)

	// Create
	r.fbus.fail(boom)
	_, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: "Rangers"})
	if !errors.Is(err, boom) {
		t.Fatalf("Create err = %v, want the publish error", err)
	}
	r.fbus.fail(nil)
	got, err := r.svc.List(r.sctx(), departments.ListQuery{IncludeArchived: true})
	wantNoErr(t, err)
	if len(got) != 0 {
		t.Errorf("a department exists after the failed create: %v", got)
	}

	// Archive and Restore
	d := r.mk("Gate")
	r.fbus.fail(boom)
	_, err = r.svc.Archive(r.sctx(), d.ID)
	if !errors.Is(err, boom) {
		t.Fatalf("Archive err = %v, want the publish error", err)
	}
	r.fbus.fail(nil)
	ok, err := r.svc.Active(ctxT(t), d.ID)
	wantNoErr(t, err)
	if !ok {
		t.Error("archived despite the failed publish")
	}
	_, err = r.svc.Archive(r.sctx(), d.ID)
	wantNoErr(t, err)
	r.fbus.fail(boom)
	_, err = r.svc.Restore(r.sctx(), d.ID)
	if !errors.Is(err, boom) {
		t.Fatalf("Restore err = %v, want the publish error", err)
	}
	r.fbus.fail(nil)
	ok, err = r.svc.Active(ctxT(t), d.ID)
	wantNoErr(t, err)
	if ok {
		t.Error("restored despite the failed publish")
	}
}

// ---- read-only copy over a real database -----------------------------------------------

type roDB struct{ db.DB }

func (roDB) ReadOnly() bool { return true }

func (r roDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	if mode == db.Write {
		return errs.E("test.roDB.Transact", errs.Unavailable, db.ErrReadOnly)
	}
	return r.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

func TestReadOnlyCopy_ReadsWork_WritesRefused(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateDepartments(t, pool)
	w := newRig(t, pool)
	steward := newPerson()
	w.az.Grant(steward, departments.PermissionManage)
	d, err := w.svc.Create(asPerson(ctxT(t), steward), departments.CreateInput{Name: "Rangers"})
	wantNoErr(t, err)

	ro := newRig(t, roDB{pool})
	ro.az.Grant(steward, departments.PermissionManage)
	ctx := asPerson(ctxT(t), steward)
	if got, err := ro.svc.Get(ctx, d.ID); err != nil || got.Name != "Rangers" {
		t.Errorf("Get on a read-only copy = %+v, %v", got, err)
	}
	_, err = ro.svc.Archive(ctx, d.ID)
	wantReadOnly(t, err)
}

// ---- concurrency of reads --------------------------------------------------------------

func TestService_ConcurrentUse_Safe(t *testing.T) {
	r := newIRig(t)
	d := r.mk("Rangers")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := asPerson(ctxT(t), newPerson())
			if _, err := r.svc.List(ctx, departments.ListQuery{}); err != nil {
				t.Errorf("List: %v", err)
			}
			if _, err := r.svc.Get(ctx, d.ID); err != nil {
				t.Errorf("Get: %v", err)
			}
			if _, err := r.svc.NamesOf(ctx, []model.DepartmentID{d.ID}); err != nil {
				t.Errorf("NamesOf: %v", err)
			}
			if _, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: fmt.Sprintf("Dept %d", i)}); err != nil {
				t.Errorf("Create: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ---- nothing personal in telemetry -----------------------------------------------------

func TestNoPersonalDataInLogs(t *testing.T) {
	const marker = "Marker Zephyr Quince Department"
	const mailMarker = "marker.shared.mailbox@example.org"
	r := newIRig(t)
	d, err := r.svc.Create(r.sctx(), departments.CreateInput{Name: marker, Description: "marker description 4471", ContactEmail: mailMarker})
	wantNoErr(t, err)
	_, err = r.svc.Update(r.sctx(), d.ID, departments.UpdateInput{Name: str(marker + " 2")})
	wantNoErr(t, err)
	_, err = r.svc.Archive(r.sctx(), d.ID)
	wantNoErr(t, err)
	_, err = r.svc.Restore(r.sctx(), d.ID)
	wantNoErr(t, err)
	_, err = r.svc.Create(r.sctx(), departments.CreateInput{Name: marker + " 2"}) // a conflict, logged too
	wantKind(t, err, errs.Conflict)
	for _, needle := range []string{marker, mailMarker, "marker description 4471"} {
		if r.cap.Contains(needle) {
			t.Errorf("telemetry contains %q: names and mailboxes are never logged", needle)
		}
	}
	// And the audit entries carry field names, never values.
	for _, e := range r.rec.Recorded() {
		for _, f := range e.Fields {
			if f == marker || f == mailMarker {
				t.Errorf("audit field list holds a value: %q", f)
			}
		}
		if e.Reason == marker {
			t.Error("audit reason holds a name")
		}
	}
}

// ---- with a real authz.Service ---------------------------------------------------------

// authzRig wires departments as the DepartmentSource of a real authz.Service over one
// database.
type authzRig struct {
	*irig
	az2   *authz.Service
	super model.PersonID
}

func newAuthzRig(t *testing.T) *authzRig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrateDepartments(t, pool, identity.Set, people.Set, authz.Set)
	// departments.Set must come after authz.Set in db.md's order; migrateDepartments puts it last.
	dr := newRig(t, pool)
	steward := newPerson()
	dr.az.Grant(steward, departments.PermissionManage)
	dr.az.Grant(steward, departments.PermissionDetailsManage)
	ir := &irig{rig: dr, pool: pool, steward: steward}

	c := telemetrytest.New(t)
	bus, err := signalstest.NewFake("access", []signals.Name{authz.SignalGrantCreated, authz.SignalGrantRevoked}, nil)
	wantNoErr(t, err)
	super := newPerson()
	declared := append(append([]authz.PermissionDecl(nil), authz.Permissions...), departments.Permissions...)
	svc, err := authz.New(authz.Options{
		DB:          pool,
		Clock:       clock.NewFake(epoch),
		Logger:      c.Logger(),
		Tracer:      c.Tracer("authz"),
		Meter:       c.Meter("authz"),
		Audit:       audittest.NewFake("access"),
		Signals:     bus,
		Actor:       actorOf,
		Declared:    declared,
		Departments: dr.svc,
	})
	wantNoErr(t, err)
	_, err = svc.Seed(ctxT(t), declared)
	wantNoErr(t, err)
	ar := &authzRig{irig: ir, az2: svc, super: super}
	ar.person(super)
	return ar
}

func (a *authzRig) person(p model.PersonID) {
	a.t.Helper()
	if _, err := a.pool.Exec(ctxT(a.t), "INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now()) ON CONFLICT DO NOTHING", [16]byte(p)); err != nil {
		a.t.Fatalf("inserting a person: %v", err)
	}
}

func (a *authzRig) deptAdminRole() authz.Role {
	a.t.Helper()
	roles, err := a.az2.ListRoles(asSuper(ctxT(a.t), a.super))
	wantNoErr(a.t, err)
	for _, ro := range roles {
		if ro.Key == authz.RoleDepartmentAdmin {
			return ro
		}
	}
	a.t.Fatal("no department-admin starter role after Seed")
	return authz.Role{}
}

func (a *authzRig) grant(p model.PersonID, d model.DepartmentID) error {
	_, err := a.az2.Grant(asSuper(ctxT(a.t), a.super), authz.GrantInput{Person: p, Role: a.deptAdminRole().ID, Department: d})
	return err //nolint:wrapcheck // test
}

// Wiring: department grants work for an active department and are refused for an archived
// or unknown one (authz: "Invalid department unknown").
func TestAuthz_GrantAtDepartment_FollowsActive(t *testing.T) {
	a := newAuthzRig(t)
	d := a.mk("Rangers")
	lead := newPerson()
	a.person(lead)
	wantNoErr(t, a.grant(lead, d.ID))

	archived := a.mkArchived("Gate")
	other := newPerson()
	a.person(other)
	wantField(t, a.grant(other, archived.ID), "department", "unknown")
	wantField(t, a.grant(other, newDept()), "department", "unknown")

	_, err := a.svc.Restore(a.sctx(), archived.ID)
	wantNoErr(t, err)
	wantNoErr(t, a.grant(other, archived.ID))
}

// F25: archiving leaves the grants in place; Active turns false and no new grant is possible.
func TestArchive_KeepsGrants_ActiveFalse(t *testing.T) {
	a := newAuthzRig(t)
	d := a.mk("Rangers")
	lead := newPerson()
	a.person(lead)
	wantNoErr(t, a.grant(lead, d.ID))

	_, err := a.svc.Archive(a.sctx(), d.ID)
	wantNoErr(t, err)

	page, err := a.az2.ListGrants(asSuper(ctxT(t), a.super), authz.GrantQuery{Person: lead, Limit: authz.MaxPageSize})
	wantNoErr(t, err)
	if len(page.Grants) != 1 || page.Grants[0].Department != d.ID {
		t.Fatalf("grants after archive = %+v, want the one grant kept", page.Grants)
	}
	ok, err := a.svc.Active(ctxT(t), d.ID)
	wantNoErr(t, err)
	if ok {
		t.Error("Active = true after Archive")
	}
	another := newPerson()
	a.person(another)
	wantField(t, a.grant(another, d.ID), "department", "unknown")

	// Restore keeps the leadership.
	_, err = a.svc.Restore(a.sctx(), d.ID)
	wantNoErr(t, err)
	page, err = a.az2.ListGrants(asSuper(ctxT(t), a.super), authz.GrantQuery{Person: lead, Limit: authz.MaxPageSize})
	wantNoErr(t, err)
	if len(page.Grants) != 1 {
		t.Errorf("grants after restore = %+v", page.Grants)
	}
}

// failCommitDB runs fn inside the real transaction, then reports that the commit failed.
type failCommitDB struct{ db.DB }

func (f failCommitDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	return f.DB.Transact(ctx, mode, func(ctx context.Context) error { //nolint:wrapcheck // test double
		if err := fn(ctx); err != nil {
			return err
		}
		if mode == db.Write {
			return errs.E("test.failCommitDB", errs.Unavailable, errors.New("commit failed"))
		}
		return nil
	})
}

func TestCreate_PublishesAfterCommitOnly(t *testing.T) {
	pool := dbtest.TempDB(t)
	migrateDepartments(t, pool)
	bad := newRig(t, failCommitDB{pool})
	steward := newPerson()
	bad.az.Grant(steward, departments.PermissionManage)
	_, err := bad.svc.Create(asPerson(ctxT(t), steward), departments.CreateInput{Name: "Rangers"})
	wantKind(t, err, errs.Unavailable)
	if got := bad.bus.Published(); len(got) != 0 {
		t.Errorf("signals delivered for a transaction that did not commit: %v", got)
	}
	good := newRig(t, pool)
	good.az.Grant(steward, departments.PermissionManage)
	list, err := good.svc.List(asPerson(ctxT(t), steward), departments.ListQuery{IncludeArchived: true})
	wantNoErr(t, err)
	if len(list) != 0 {
		t.Errorf("a department exists after the failed commit: %v", list)
	}
	_, err = good.svc.Create(asPerson(ctxT(t), steward), departments.CreateInput{Name: "Rangers"})
	wantNoErr(t, err)
	if got := len(good.bus.Published()); got != 1 {
		t.Errorf("%d signals after a good commit, want 1", got)
	}
}
