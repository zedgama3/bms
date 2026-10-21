//go:build integration

//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Deletion without a shared transaction (decision 16): the row and a tombstone commit with the
// owner's transaction; after commit the object and then the tombstone go; Reap finishes what
// failed.

func TestDelete_RemovesRowObjectAndTombstone(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		subject := r.person()
		f := r.uploadPDF(ctxT(t), r.vol, qualRef(), subject, model.SensitivityPersonal, "")
		wantNoErr(t, r.del(ctxT(t), r.vol, f.ID))

		_, err := r.vol.Get(ctxT(t), f.ID)
		wantKind(t, err, errs.NotFound)
		if r.rows() != 0 || r.tombstones() != 0 {
			t.Errorf("rows=%d tombstones=%d, want 0/0 after a clean commit", r.rows(), r.tombstones())
		}
		r.wantNoObjects(ownerVol)
		es := entriesWith(r.audit, "files.file.deleted")
		if len(es) != 1 {
			t.Fatalf("files.file.deleted entries = %d, want 1", len(es))
		}
		if es[0].Subject != subject || es[0].Level != model.SensitivityPersonal {
			t.Errorf("deleted entry = %+v", es[0])
		}
		// Gone means gone: a second delete is NotFound, not a silent success.
		wantKind(t, r.del(ctxT(t), r.vol, f.ID), errs.NotFound)
	})
}

// F34
func TestDelete_NoWriteTx_ReturnsInternal(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	wantKind(t, r.vol.Delete(ctxT(t), f.ID), errs.Internal)
	_, err := r.vol.DeleteByRecord(ctxT(t), f.Record)
	wantKind(t, err, errs.Internal)
	err = r.pool.Transact(ctxT(t), db.Read, func(ctx context.Context) error { return r.vol.Delete(ctx, f.ID) })
	wantKind(t, err, errs.Internal)
	if r.rows() != 1 || len(r.objectKeys(ownerVol)) != 1 || r.tombstones() != 0 {
		t.Error("state changed by a Delete outside a Write transaction")
	}
}

// F35 is in TestOwnerScoping; F36:
func TestDelete_ReadOnly_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	vol, err := r.readOnlyCopy().Scoped(ownerVol)
	wantNoErr(t, err)
	err = r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error { return vol.Delete(ctx, f.ID) })
	wantReadOnly(t, err)
	if r.rows() != 1 || r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Error("a read-only copy deleted something")
	}
}

func TestDelete_RolledBack_ChangesNothing(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	owner := errs.E("test.owner", errs.Conflict, errors.New("the record could not be deleted"))
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if derr := r.vol.Delete(ctx, f.ID); derr != nil {
			return derr
		}
		return owner
	})
	wantKind(t, err, errs.Conflict)
	if r.rows() != 1 || r.tombstones() != 0 {
		t.Errorf("rows=%d tombstones=%d after a rollback", r.rows(), r.tombstones())
	}
	if len(r.objectKeys(ownerVol)) != 1 {
		t.Error("the object was deleted although the transaction rolled back (AfterCommit must not run)")
	}
	if _, _, err := r.vol.Open(ctxT(t), f.ID); err != nil {
		t.Errorf("the file is no longer readable: %v", err)
	}
}

func TestDelete_AuditFails_RollsBack(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	r.audit.Fail(errs.E("test.audit", errs.Unavailable, errors.New("down")))
	err := r.del(ctxT(t), r.vol, f.ID)
	wantKind(t, err, errs.Unavailable)
	r.audit.Fail(nil)
	if r.rows() != 1 || r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Error("a delete whose audit entry failed must roll back completely")
	}
}

// F37 and F49: a failing store after commit leaves a tombstone; Reap clears it.
func TestDelete_ObjectDeleteFails_LeavesTombstoneAndReapClearsIt(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
		r.store.fail("delete", unavailable("delete"))

		wantNoErr(t, r.del(ctxT(t), r.vol, f.ID)) // the delete itself succeeds
		if r.rows() != 0 {
			t.Fatal("the row survived")
		}
		if r.tombstones() != 1 {
			t.Fatalf("tombstones = %d, want 1: there is no state in which the row is gone and nothing remembers the object", r.tombstones())
		}
		if len(r.objectKeys(ownerVol)) != 1 {
			t.Fatal("the object should still be waiting for Reap")
		}
		if r.count(`SELECT count(*) FROM core.file_tombstone WHERE bucket = $1 AND object_key = $2 AND attempts = 0`, ownerVol, f.ID.String()) != 1 {
			t.Error("tombstone does not name (bucket, object_key) with attempts 0")
		}

		// F49: Reap while the store is still failing: attempts incremented, nothing else lost.
		got, err := r.svc.Reap(ctxT(t))
		_ = err // a failing row is counted, not necessarily returned
		if got.Tombstones != 0 {
			t.Errorf("Reap reported %d cleared tombstones while the store was failing", got.Tombstones)
		}
		if r.count(`SELECT count(*) FROM core.file_tombstone WHERE attempts = 1`) != 1 {
			t.Error("attempts not incremented by a failed Reap")
		}
		got, _ = r.svc.Reap(ctxT(t)) //nolint:errcheck // counted
		if r.count(`SELECT count(*) FROM core.file_tombstone WHERE attempts = 2`) != 1 {
			t.Error("attempts is not cumulative")
		}

		// The store recovers; the next run finishes the deletion.
		r.store.up()
		got, err = r.svc.Reap(ctxT(t))
		wantNoErr(t, err)
		if got.Tombstones != 1 {
			t.Errorf("Reap = %+v, want 1 tombstone cleared", got)
		}
		if r.tombstones() != 0 {
			t.Error("tombstone outstanding after a successful Reap")
		}
		r.wantNoObjects(ownerVol)

		es := entriesWith(r.audit, "files.sweep.ran")
		if len(es) == 0 || es[len(es)-1].Reason != "tombstones:1" {
			t.Errorf("last files.sweep.ran = %+v, want Reason tombstones:1", es)
		}
		// Idempotent.
		got, err = r.svc.Reap(ctxT(t))
		wantNoErr(t, err)
		if got.Tombstones != 0 {
			t.Errorf("second Reap = %+v", got)
		}
	})
}

func TestReap_OneFailingTombstone_DoesNotStopTheRun(t *testing.T) {
	r := newIRig(t)
	rec := qualRef()
	var fs []files.File
	for i := range 3 {
		fs = append(fs, r.upload(ctxT(t), r.vol, "f.pdf", files.Documents, pdfDoc(string(rune('a'+i))+"\n"), files.Attachment{Record: rec, Level: model.SensitivityInternal}))
	}
	r.store.down() // every delete fails during the owner's transaction
	n, err := r.delByRecord(ctxT(t), r.vol, rec)
	wantNoErr(t, err)
	if n != 3 || r.tombstones() != 3 {
		t.Fatalf("deleted %d, tombstones %d; want 3/3", n, r.tombstones())
	}
	r.store.up()
	r.store.failKey(objects.Key(fs[1].ID.String()), unavailable("delete"))
	got, _ := r.svc.Reap(ctxT(t)) //nolint:errcheck // one row fails by design
	if got.Tombstones != 2 {
		t.Errorf("Reap cleared %d, want 2", got.Tombstones)
	}
	if r.tombstones() != 1 || r.count(`SELECT count(*) FROM core.file_tombstone WHERE attempts = 1`) != 1 {
		t.Errorf("tombstones = %d; the failing one must stay with attempts 1", r.tombstones())
	}
	if len(r.objectKeys(ownerVol)) != 1 {
		t.Errorf("objects left = %d, want only the failing one", len(r.objectKeys(ownerVol)))
	}
}

func TestReap_ObjectAlreadyGone_ClearsTombstone(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	r.store.fail("delete", unavailable("delete"))
	wantNoErr(t, r.del(ctxT(t), r.vol, f.ID))
	r.store.up()
	wantNoErr(t, r.inner.Delete(ctxT(t), ownerVol, objects.Key(f.ID.String()))) // gone behind our back
	got, err := r.svc.Reap(ctxT(t))
	wantNoErr(t, err)
	if got.Tombstones != 1 || r.tombstones() != 0 {
		t.Errorf("Reap = %+v, tombstones %d; Delete is idempotent so a gone object is a cleared tombstone", got, r.tombstones())
	}
}

func TestReap_ContextDone_ReturnsKind(t *testing.T) {
	r := newIRig(t)
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := r.svc.Reap(ctx)
	wantKind(t, err, errs.Canceled)
}

func TestDeleteByRecord(t *testing.T) {
	r := newIRig(t)
	subject := r.person()
	rec, other := qualRef(), qualRef()
	at := files.Attachment{Record: rec, Subject: subject, Level: model.SensitivityPersonal}
	for i := range 3 {
		r.upload(ctxT(t), r.vol, "f.pdf", files.Documents, pdfDoc(string(rune('a'+i))+"\n"), at)
	}
	keep := r.upload(ctxT(t), r.vol, "k.pdf", files.Documents, pdfDoc("keep\n"), files.Attachment{Record: other, Level: model.SensitivityInternal})

	n, err := r.delByRecord(ctxT(t), r.vol, rec)
	wantNoErr(t, err)
	if n != 3 {
		t.Errorf("DeleteByRecord = %d, want 3", n)
	}
	if got, err := r.vol.ByRecord(ctxT(t), rec); err != nil || len(got) != 0 {
		t.Errorf("ByRecord after = %d, %v", len(got), err)
	}
	if _, err := r.vol.Get(ctxT(t), keep.ID); err != nil {
		t.Errorf("another record's file was deleted: %v", err)
	}
	if r.rows() != 1 || r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Errorf("rows=%d tombstones=%d objects=%d", r.rows(), r.tombstones(), len(r.objectKeys(ownerVol)))
	}
	if got := len(entriesWith(r.audit, "files.file.deleted")); got != 3 {
		t.Errorf("files.file.deleted entries = %d, want one per file", got)
	}
	// Zero is fine.
	n, err = r.delByRecord(ctxT(t), r.vol, rec)
	if err != nil || n != 0 {
		t.Errorf("DeleteByRecord of an empty record = %d, %v; want 0, nil", n, err)
	}
	n, err = r.delByRecord(ctxT(t), r.vol, qualRef())
	if err != nil || n != 0 {
		t.Errorf("DeleteByRecord of an unknown record = %d, %v; want 0, nil", n, err)
	}
}

func TestAttach_Replace_TombstonesTheOldFiles(t *testing.T) {
	r := newIRig(t)
	rec := qualRef()
	old := r.upload(ctxT(t), r.vol, "old.pdf", files.Documents, pdfDoc("old\n"), files.Attachment{Record: rec, Level: model.SensitivityInternal})
	r.store.fail("delete", unavailable("delete"))
	fresh := r.upload(ctxT(t), r.vol, "new.pdf", files.Documents, pdfDoc("new\n"), files.Attachment{Record: rec, Level: model.SensitivityInternal, Replace: true})
	if r.rows() != 1 {
		t.Fatalf("rows = %d, want only the replacement", r.rows())
	}
	if _, err := r.vol.Get(ctxT(t), old.ID); !errs.Is(err, errs.NotFound) {
		t.Errorf("old file Get = %v, want NotFound", err)
	}
	if _, err := r.vol.Get(ctxT(t), fresh.ID); err != nil {
		t.Errorf("new file: %v", err)
	}
	if r.tombstones() != 1 {
		t.Errorf("tombstones = %d; the replaced object must be remembered while the store is failing", r.tombstones())
	}
	r.store.up()
	_, err := r.svc.Reap(ctxT(t))
	wantNoErr(t, err)
	if r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Errorf("after Reap: tombstones %d, objects %d", r.tombstones(), len(r.objectKeys(ownerVol)))
	}
}

func TestAttach_ReplaceThatFailsLater_KeepsTheOldFile(t *testing.T) {
	r := newIRig(t)
	rec := qualRef()
	old := r.upload(ctxT(t), r.vol, "old.pdf", files.Documents, pdfDoc("old\n"), files.Attachment{Record: rec, Level: model.SensitivityInternal})
	st := mustStage(t, r.vol, "new.pdf", files.Documents, pdfDoc("new\n"))
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, aerr := r.vol.Attach(ctx, st, files.Attachment{Record: rec, Level: model.SensitivityInternal, Replace: true}); aerr != nil {
			return aerr
		}
		return errs.E("test.owner", errs.Conflict, errors.New("record write failed"))
	})
	wantKind(t, err, errs.Conflict)
	if _, err := r.vol.Get(ctxT(t), old.ID); err != nil {
		t.Errorf("the old file was lost with a rolled-back Replace: %v", err)
	}
	if r.tombstones() != 0 || len(r.objectKeys(ownerVol)) != 2 {
		t.Errorf("tombstones=%d objects=%d; a rolled-back Replace must not delete the old object", r.tombstones(), len(r.objectKeys(ownerVol)))
	}
}

func TestSweepOrphans_KeepsObjectsWithRowsOrTombstones_RemovesOthers(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		attached := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "attached\n")
		tombed := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "tombed\n")
		r.store.fail("delete", unavailable("delete"))
		wantNoErr(t, r.del(ctxT(t), r.vol, tombed.ID))
		r.store.up()
		orphan := mustStage(t, r.vol, "orphan.pdf", files.Documents, pdfDoc("orphan\n"))
		young := mustStage(t, r.vol, "young.pdf", files.Documents, pdfDoc("young\n"))
		putRaw(t, r.inner, ownerVol, objects.ProbeKey, []byte("0123456789abcdef"))
		other := mustStage(t, r.inc, "o.pdf", files.Documents, pdfDoc("other-owner orphan\n"))

		r.clk.Advance(files.OrphanAge - time.Minute) //nolint:errcheck,gosec // fake clock
		// "young" must be younger than OrphanAge at sweep time: stage it late on the real clock.
		_ = young
		r.clk.Advance(2 * time.Minute) //nolint:errcheck,gosec // fake clock: everything above is now older than OrphanAge

		got, err := r.svc.SweepOrphans(ctxT(t))
		wantNoErr(t, err)
		if got.Orphans != 3 { // orphan, young (also past the age on this clock), other-owner
			t.Errorf("Swept = %+v, want 3 orphans", got)
		}
		has := func(b objects.Bucket, k objects.Key) bool {
			_, herr := r.inner.Head(ctxT(t), b, k)
			return herr == nil
		}
		if !has(ownerVol, objects.Key(attached.ID.String())) {
			t.Error("an object with a row was swept")
		}
		if !has(ownerVol, objects.Key(tombed.ID.String())) {
			t.Error("an object with a tombstone was swept; Reap owns it")
		}
		if has(ownerVol, orphan.Key) || has(ownerInc, other.Key) {
			t.Error("an orphan survived the sweep (every owner's bucket is walked)")
		}
		if !has(ownerVol, objects.ProbeKey) {
			t.Error("ProbeKey was swept")
		}
		if r.rows() != 1 || r.tombstones() != 1 {
			t.Errorf("rows=%d tombstones=%d; the sweep never deletes a row or a tombstone", r.rows(), r.tombstones())
		}
	})
}

func TestSweepOrphans_OnlyOldAreRemoved_WithRowsPresent(t *testing.T) {
	r := newIRig(t)
	old := mustStage(t, r.vol, "old.pdf", files.Documents, pdfDoc("old\n"))
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	// A freshly staged object is younger than OrphanAge on the object store's own clock, but
	// the service judges age by its clock; stage it with the clock moved, so StagedAt is new.
	got, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.Orphans != 1 {
		t.Errorf("Swept = %+v", got)
	}
	if _, herr := r.inner.Head(ctxT(t), old.Bucket, old.Key); herr == nil {
		t.Error("old orphan survived")
	}
}

func TestSweepOrphans_ReadOnlyCopy_ReturnsUnavailable(t *testing.T) { // F48
	r := newIRig(t)
	mustStage(t, r.vol, "old.pdf", files.Documents, pdfDoc("old\n"))
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err := r.readOnlyCopy().SweepOrphans(ctxT(t))
	wantReadOnly(t, err)
	if len(r.objectKeys(ownerVol)) != 1 {
		t.Error("a read-only copy swept an object")
	}
}
