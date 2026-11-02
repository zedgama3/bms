//go:build integration

package datarights_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// boundaries.md step 12 (decision 5): modules in build order, then files, notify, access,
// identity, and people last, because every foreign key points at core.person with RESTRICT.
// The fakes record the order of their DeletePerson calls in one log.
func TestDelete_OrderIsModulesFilesNotifyAccessIdentityPeople(t *testing.T) {
	r := newIRig(t, false)
	alpha := r.tableSource("alpha")
	beta := r.tableSource("beta")
	gamma := r.tableSource("gamma")
	// Link in an order that is not alphabetical: the build order is the link order.
	r.link(gamma.source(), alpha.source(), beta.source())
	target := r.insertPerson(seed{Playa: mPlaya})
	r.addRows("alpha", target, 1)
	_, ctx := r.admin(permDelete)
	r.counter.n = 0

	_, err := r.svc.Delete(ctx, target)
	wantNoErr(t, err)

	want := []string{
		"module.gamma.delete", "module.alpha.delete", "module.beta.delete",
		"files.delete", "notify.delete", "access.delete", "accounts.delete", "people.delete",
	}
	if got := r.log.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("deletion order = %v\nwant           = %v", got, want)
	}
	if r.rowExists(target) {
		t.Error("the person survives")
	}
}

// The same order holds for a fulfilled request, the other way into the same deletion.
func TestFulfilRequest_OrderIsModulesFilesNotifyAccessIdentityPeople(t *testing.T) {
	r := newIRig(t, false)
	alpha := r.tableSource("alpha")
	r.link(alpha.source())
	target := r.insertPerson(seed{Playa: mPlaya})
	_, ctx := r.admin(permDelete)
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), target), "")
	wantNoErr(t, err)

	_, err = r.svc.FulfilRequest(ctx, req.ID)
	wantNoErr(t, err)

	want := []string{"module.alpha.delete", "files.delete", "notify.delete", "access.delete", "accounts.delete", "people.delete"}
	if got := r.log.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("deletion order = %v, want %v", got, want)
	}
}

// A failure stops the steps after it: no later source is asked.
func TestDelete_FailureStopsLaterSteps(t *testing.T) {
	steps := []struct {
		name  string
		fail  func(r *irig)
		later []string
	}{
		{"files", func(r *irig) { r.files.deleteErr = errBoom() }, []string{"notify.delete", "access.delete", "accounts.delete", "people.delete"}},
		{"notify", func(r *irig) { r.outbox.deleteErr = errBoom() }, []string{"access.delete", "accounts.delete", "people.delete"}},
		{"access", func(r *irig) { r.access.deleteErr = errBoom() }, []string{"accounts.delete", "people.delete"}},
		{"identity", func(r *irig) { r.accts.deleteErr = errBoom() }, []string{"people.delete"}},
	}
	for _, tc := range steps {
		t.Run(tc.name, func(t *testing.T) {
			f := newDelFixture(t, false)
			tc.fail(f.irig)
			_, err := f.svc.Delete(f.ctx, f.target)
			if err == nil {
				t.Fatal("a failing step did not fail the deletion")
			}
			for _, c := range f.log.all() {
				for _, l := range tc.later {
					if c == l {
						t.Errorf("%s ran after the %s step failed", c, tc.name)
					}
				}
			}
			f.intact()
		})
	}
}

// B-signal: the request signal is datarights', published once per request after commit,
// with the request and the person; people publishes nothing for it any more.
func TestPublishes_DatarightsDeletionRequested(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: mPlaya})
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
	wantNoErr(t, err)

	got := r.published(datarights.SignalDeletionRequested)
	if len(got) != 1 {
		t.Fatalf("published %d datarights.deletion-requested, want 1 (all: %v)", len(got), r.bus.Published())
	}
	if got[0].Name != "datarights.deletion-requested" {
		t.Errorf("name = %q", got[0].Name)
	}
	gotReq, err := signals.Get[datarights.RequestID](got[0].Payload, datarights.PayloadRequest)
	wantNoErr(t, err)
	if gotReq != req.ID || payloadPerson(got[0]) != me {
		t.Errorf("payload = %v, want request %v and person %v", got[0].Payload, req.ID, me)
	}
	for _, s := range r.pbus.Published() {
		if strings.HasSuffix(string(s.Name), "deletion-requested") {
			t.Errorf("people published %q for a request", s.Name)
		}
	}
	// Nothing for the refused duplicate.
	_, err = r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
	wantKind(t, err, errs.Conflict)
	if n := len(r.published(datarights.SignalDeletionRequested)); n != 1 {
		t.Errorf("%d signals after a refused duplicate, want still 1", n)
	}
}

func errBoom() error { return errs.E("test.step", errs.Unavailable, errors.New("down")) }

// F70: files' DeletePerson refuses (Conflict in_use) when a module left a file about the
// person. This rig's FilesSource is a fake that answers as files.Service does for that case
// (the real one needs an object store); the test shows datarights surfaces the refusal and
// rolls everything back.
func TestDelete_ModuleLeftFile_ReturnsConflictInUse(t *testing.T) {
	f := newDelFixture(t, false)
	f.files.deleteErr = errs.E("files.Service.DeletePerson", errs.Conflict,
		&validate.FieldError{Field: "files", Code: "in_use", Reason: "files about the person remain"})
	_, err := f.svc.Delete(f.ctx, f.target)
	wantCode(t, err, errs.Conflict, "in_use")
	f.intact()
	for _, c := range f.log.all() {
		if c == "notify.delete" || c == "access.delete" || c == "accounts.delete" || c == "people.delete" {
			t.Errorf("%s ran after files refused", c)
		}
	}
}
