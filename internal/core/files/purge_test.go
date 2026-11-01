package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
)

var errSettingsDown = errors.New("settings down")

// strangerBucket is an owner nobody scoped on the service under test.
const strangerBucket = objects.Bucket("stranger")

func (r *rig) putStranger(t *testing.T) {
	t.Helper()
	body := []byte("not ours")
	_, err := r.inner.Put(ctxT(t), objects.PutInput{
		Bucket: strangerBucket, Key: "k1", Body: bytes.NewReader(body), Size: int64(len(body)),
		SHA256: sha256.Sum256(body), ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("Put into the unscoped bucket: %v", err)
	}
}

func (r *rig) tempEntries(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir(r.tmp)
	if err != nil {
		t.Fatalf("ReadDir(tmp): %v", err)
	}
	return len(ents)
}

// Purge needs no actor: demo's Reset calls it from a command. Every object of every scoped
// bucket and every file in TempDir goes, and the count is both; a bucket nobody scoped is not
// this service's to empty.
func TestPurge_LeavesUnscopedBuckets(t *testing.T) {
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	mustStage(t, r.inc, "b.pdf", files.Documents, pdfDoc("x"))
	r.putStranger(t)
	temp := 2
	for i := 0; i < temp; i++ {
		name := filepath.Join(r.tmp, "leftover-"+string(rune('a'+i)))
		if err := os.WriteFile(name, []byte("partial upload"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	wantCount := len(r.objectKeys(ownerVol)) + len(r.objectKeys(ownerInc)) + r.tempEntries(t)

	n, err := r.svc.Purge(context.Background())
	wantNoErr(t, err)
	if n != wantCount {
		t.Errorf("Purge = %d, want %d (scoped objects plus temp files)", n, wantCount)
	}
	r.wantNoObjects(ownerVol)
	r.wantNoObjects(ownerInc)
	if got := r.tempEntries(t); got != 0 {
		t.Errorf("%d files left in TempDir", got)
	}
	if keys := r.objectKeys(strangerBucket); len(keys) != 1 {
		t.Errorf("the unscoped bucket holds %v, want its one object untouched", keys)
	}
}

func TestPurge_NothingToDelete_ReturnsZero(t *testing.T) {
	r := newRig(t)
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("Purge of an empty service = %d, want 0", n)
	}
	// A second purge after a purge is also empty.
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	if _, err := r.svc.Purge(ctxT(t)); err != nil {
		t.Fatalf("first: %v", err)
	}
	n, err = r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second Purge = %d, want 0", n)
	}
}

// F65
func TestPurge_ReadOnlyCopy_ReturnsUnavailable(t *testing.T) {
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	before := len(r.objectKeys(ownerVol))
	r.fake.SetReadOnly(true)
	n, err := r.svc.Purge(ctxT(t))
	wantReadOnly(t, err)
	if n != 0 {
		t.Errorf("Purge = %d on a read-only copy, want 0", n)
	}
	if got := len(r.objectKeys(ownerVol)); got != before {
		t.Errorf("objects = %d, want %d: a read-only copy deleted something", got, before)
	}
}

// F66
func TestPurge_StoreFails_ReturnsUnavailable(t *testing.T) {
	for _, op := range []string{"list", "delete"} {
		t.Run(op, func(t *testing.T) {
			r := newRig(t)
			mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
			r.store.fail(op, unavailable(op))
			_, err := r.svc.Purge(ctxT(t))
			wantKind(t, err, errs.Unavailable)
		})
	}
}

func TestPurge_Canceled_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := r.svc.Purge(ctx)
	wantKind(t, err, errs.Canceled)
	if len(r.objectKeys(ownerVol)) != 1 {
		t.Error("a canceled Purge deleted an object")
	}
}
