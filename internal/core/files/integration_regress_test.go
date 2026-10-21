//go:build integration

//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Review finding M1: Replace with a Staged that is already one of the record's attached
// objects must not delete the object the existing row points at. The call fails Internal
// (a bug in the owner, as F17), the transaction rolls back, and the row and object stay.
func TestAttach_ReplaceWithAlreadyAttachedStaged_ReturnsInternalKeepsObject(t *testing.T) {
	r := newIRig(t)
	rec := qualRef()
	st := mustStage(t, r.vol, "a.pdf", files.Documents, pdfDoc("keep\n"))
	first, err := r.attach(ctxT(t), r.vol, st, files.Attachment{Record: rec, Level: model.SensitivityInternal})
	wantNoErr(t, err)

	_, err = r.attach(ctxT(t), r.vol, st, files.Attachment{Record: rec, Level: model.SensitivityInternal, Replace: true})
	wantKind(t, err, errs.Internal)

	if r.rows() != 1 {
		t.Errorf("rows = %d, want the existing row intact", r.rows())
	}
	got, gerr := r.vol.Get(ctxT(t), first.ID)
	if gerr != nil {
		t.Fatalf("the existing file is gone: %v", gerr)
	}
	if got.SHA256 != first.SHA256 || got.Record != rec {
		t.Errorf("existing row changed: %+v", got)
	}
	if r.tombstones() != 0 {
		t.Errorf("tombstones = %d; a failed Replace must not queue the object for removal", r.tombstones())
	}
	if keys := r.objectKeys(ownerVol); len(keys) != 1 || keys[0] != first.ID.String() {
		t.Errorf("objects = %v, want exactly the attached one", keys)
	}
	if _, c, oerr := r.vol.Open(ctxT(t), first.ID); oerr != nil {
		t.Errorf("Open after the failed Replace: %v", oerr)
	} else if cerr := c.Close(); cerr != nil {
		t.Errorf("Close: %v", cerr)
	}
}

// hangDelete blocks every Delete until its context ends, as an unreachable store does.
type hangDelete struct {
	objects.Store
	hang    atomic.Bool
	entered atomic.Int32
}

func (h *hangDelete) Delete(ctx context.Context, b objects.Bucket, k objects.Key) error {
	if !h.hang.Load() {
		return h.Store.Delete(ctx, b, k) //nolint:wrapcheck // test double
	}
	h.entered.Add(1)
	<-ctx.Done()
	return errs.E("test.hangDelete", errs.Timeout, ctx.Err())
}

// Review finding M2: the after-commit object removal shares a short budget, so a committed
// Delete against a store that never answers returns in a few seconds, with the row gone and
// a tombstone left for Reap (files.md decision 16, F37). The caller's context carries no
// deadline here: the bound comes from the service.
func TestDelete_StoreHangs_ReturnsPromptlyLeavesTombstones(t *testing.T) {
	h := &hangDelete{Store: objectstest.NewMem()}
	r := newIRig(t, withInner(h))
	rec := qualRef()
	r.uploadPDF(ctxT(t), r.vol, rec, model.PersonID{}, model.SensitivityInternal, "one\n")
	r.uploadPDF(ctxT(t), r.vol, rec, model.PersonID{}, model.SensitivityInternal, "two\n")
	h.hang.Store(true)

	start := time.Now()
	n, err := r.delByRecord(context.Background(), r.vol, rec)
	took := time.Since(start)
	wantNoErr(t, err)
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if took > 15*time.Second {
		t.Errorf("Delete took %v against a hung store; the removal must be bounded to a few seconds", took)
	}
	if h.entered.Load() == 0 {
		t.Error("the store was never asked to delete; the test did not exercise the hang")
	}
	if r.rows() != 0 {
		t.Errorf("rows = %d, want 0: the delete itself committed", r.rows())
	}
	if r.tombstones() != 2 {
		t.Errorf("tombstones = %d, want 2: every object not removed must stay remembered", r.tombstones())
	}
	if len(r.objectKeys(ownerVol)) != 2 {
		t.Errorf("objects = %d, want both waiting for Reap", len(r.objectKeys(ownerVol)))
	}

	// The store recovers; Reap finishes the job.
	h.hang.Store(false)
	got, rerr := r.svc.Reap(ctxT(t))
	wantNoErr(t, rerr)
	if got.Tombstones != 2 || r.tombstones() != 0 {
		t.Errorf("Reap = %+v, tombstones left %d", got, r.tombstones())
	}
	r.wantNoObjects(ownerVol)
}
