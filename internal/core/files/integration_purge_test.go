//go:build integration

package files_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/model"
)

// Purge deletes every object of every bucket the service scoped (attached or merely
// staged), and every file in TempDir, over a real database; the rows are not its business
// (demo's reset drops the schemas).
func TestPurge_ClearsScopedBucketsAndTemp(t *testing.T) {
	r := newIRig(t)
	at := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
	r.upload(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t), at)
	r.upload(ctxT(t), r.inc, "b.pdf", files.Documents, pdfDoc("y"),
		files.Attachment{Record: photoRef(), Level: model.SensitivityInternal})
	mustStage(t, r.vol, "staged.png", files.Images, plainPNG(t)) // an orphan: staged, never attached
	if err := os.WriteFile(filepath.Join(r.tmp, "partial-1"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	want := len(r.objectKeys(ownerVol)) + len(r.objectKeys(ownerInc)) + r.tempEntries(t)
	if want < 4 {
		t.Fatalf("setup holds only %d things to purge", want)
	}

	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != want {
		t.Errorf("Purge = %d, want %d", n, want)
	}
	r.wantNoObjects(ownerVol)
	r.wantNoObjects(ownerInc)
	if got := r.tempEntries(t); got != 0 {
		t.Errorf("%d files left in TempDir", got)
	}
	if r.rows() != 2 {
		t.Errorf("rows = %d, want 2: Purge does not touch the tables", r.rows())
	}
}

// F65 against a replica: reads work, the first write is refused, nothing is deleted.
func TestPurge_ReadOnlyCopy_ReturnsUnavailable_RealDatabase(t *testing.T) {
	r := newIRig(t)
	r.upload(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t),
		files.Attachment{Record: qualRef(), Level: model.SensitivityInternal})
	ro := r.readOnlyCopy()
	if _, err := ro.Scoped(ownerVol); err != nil {
		t.Fatalf("Scoped: %v", err)
	}
	n, err := ro.Purge(ctxT(t))
	wantReadOnly(t, err)
	if n != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Errorf("Purge = %d, objects = %d: a read-only copy deleted something", n, len(r.objectKeys(ownerVol)))
	}
}
