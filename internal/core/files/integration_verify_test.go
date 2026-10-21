//go:build integration

package files_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// F52 and decision 17: rows without objects are visible, never silently deleted.
func TestVerify_MissingAndMismatched_Counted(t *testing.T) {
	eachStore(t, func(t *testing.T, mk func(*testing.T) objects.Store) {
		r := newIRig(t, withInner(mk(t)))
		at := func() files.Attachment { return files.Attachment{Record: qualRef(), Level: model.SensitivityInternal} }
		ok := r.upload(ctxT(t), r.vol, "ok.pdf", files.Documents, pdfDoc("ok\n"), at())
		missing := r.upload(ctxT(t), r.vol, "gone.pdf", files.Documents, pdfDoc("gone\n"), at())
		damaged := r.upload(ctxT(t), r.inc, "bad.pdf", files.Documents, pdfDoc("bad\n"), files.Attachment{Record: photoRef(), Level: model.SensitivityInternal})

		// Clean first.
		got, err := r.svc.Verify(ctxT(t))
		wantNoErr(t, err)
		if got.Missing != 0 || got.Mismatched != 0 || got.Tombstones != 0 || got.Checked < 3 {
			t.Fatalf("clean Verify = %+v", got)
		}

		wantNoErr(t, r.inner.Delete(ctxT(t), ownerVol, objects.Key(missing.ID.String())))
		// Same size, different bytes under the damaged file's key.
		wantNoErr(t, r.inner.Delete(ctxT(t), ownerInc, objects.Key(damaged.ID.String())))
		other := pdfDoc("bxd\n")
		if len(other) != int(damaged.Size) {
			t.Fatalf("fixture sizes differ: %d vs %d", len(other), damaged.Size)
		}
		putRaw(t, r.inner, ownerInc, objects.Key(damaged.ID.String()), other)
		// And one outstanding tombstone.
		gone := r.upload(ctxT(t), r.vol, "t.pdf", files.Documents, pdfDoc("t\n"), at())
		r.store.fail("delete", unavailable("delete"))
		wantNoErr(t, r.del(ctxT(t), r.vol, gone.ID))
		r.store.up()

		rows, tombs, objs := r.rows(), r.tombstones(), len(r.objectKeys(ownerVol))+len(r.objectKeys(ownerInc))
		auditBefore := len(entriesWith(r.audit, "files.verify.ran"))
		got, err = r.svc.Verify(ctxT(t))
		wantNoErr(t, err)
		if got.Missing != 1 || got.Mismatched != 1 || got.Tombstones != 1 {
			t.Errorf("Verify = %+v, want Missing 1, Mismatched 1, Tombstones 1", got)
		}
		if got.Checked < 2 {
			t.Errorf("Checked = %d, want at least the sound rows", got.Checked)
		}
		if r.rows() != rows || r.tombstones() != tombs || len(r.objectKeys(ownerVol))+len(r.objectKeys(ownerInc)) != objs {
			t.Error("Verify changed data; it deletes nothing")
		}
		if _, err := r.vol.Get(ctxT(t), missing.ID); err != nil {
			t.Errorf("the row with a missing object was removed: %v", err)
		}
		_ = ok
		if len(entriesWith(r.audit, "files.verify.ran")) != auditBefore+1 {
			t.Error("files.verify.ran not recorded exactly once per run")
		}
	})
}

func TestVerify_ReadOnlyCopy_Works(t *testing.T) {
	r := newIRig(t)
	f := r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	wantNoErr(t, r.inner.Delete(ctxT(t), ownerVol, objects.Key(f.ID.String())))
	got, err := r.readOnlyCopy().Verify(ctxT(t))
	wantNoErr(t, err)
	if got.Missing != 1 {
		t.Errorf("Verify on a read-only copy = %+v, want Missing 1 (a sync gap)", got)
	}
}

// F53
func TestVerify_StoreUnreachable_ReturnsUnavailable(t *testing.T) {
	r := newIRig(t)
	r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	r.store.down()
	_, err := r.svc.Verify(ctxT(t))
	wantKind(t, err, errs.Unavailable)
}

func TestVerify_UnreachableIsNotCountedAsMissing(t *testing.T) {
	r := newIRig(t)
	r.uploadPDF(ctxT(t), r.vol, qualRef(), model.PersonID{}, model.SensitivityInternal, "")
	r.store.fail("head", unavailable("head"))
	got, err := r.svc.Verify(ctxT(t))
	if err == nil && got.Missing != 0 {
		t.Errorf("a store error was counted as Missing: %+v", got)
	}
}
