//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// publicStoreErr is what a store answers when its probe object can be read anonymously.
func publicStoreErr() error {
	return errs.E("test.Ping", errs.Invalid, objects.ErrPublic)
}

// ---- Ping (F56) and Start (F54, F55) ----

func TestPing_Healthy_ReturnsNilAndLeavesNoTempFile(t *testing.T) {
	r := newRig(t)
	wantNoErr(t, r.svc.Ping(ctxT(t)))
	r.wantNoTempFiles()
}

func TestPing_StoreUnusable_ReturnsUnavailable(t *testing.T) { // F56
	r := newRig(t)
	r.store.fail("ping", unavailable("ping"))
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
}

func TestPing_TempDirUnusable_ReturnsUnavailable(t *testing.T) { // F56
	r := newRig(t, withTempDir(filepath.Join(t.TempDir(), "gone", "away")))
	wantKind(t, r.svc.Ping(ctxT(t)), errs.Unavailable)
}

func TestPing_PublicBucket_ReturnsInvalid(t *testing.T) { // F56
	r := newRig(t)
	r.store.fail("ping", publicStoreErr())
	err := r.svc.Ping(ctxT(t))
	wantKind(t, err, errs.Invalid)
	if !errors.Is(err, objects.ErrPublic) {
		t.Errorf("err %v does not wrap objects.ErrPublic", err)
	}
}

func TestPing_ContextDone_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := r.svc.Ping(ctx)
	if !errs.Is(err, errs.Canceled) && !errs.Is(err, errs.Unavailable) {
		t.Errorf("Ping with a canceled ctx = %v", err)
	}
}

func TestProbe_Healthy_ReturnsNil(t *testing.T) {
	r := newRig(t)
	wantNoErr(t, r.svc.Probe(ctxT(t)))
	if n := r.store.count("ping"); n == 0 {
		t.Error("Start did not probe the store")
	}
}

func TestProbe_PublicBucket_ReturnsInvalid(t *testing.T) { // F54, decision 20
	r := newRig(t)
	r.store.fail("ping", publicStoreErr())
	err := r.svc.Probe(ctxT(t))
	wantKind(t, err, errs.Invalid)
	if !errors.Is(err, objects.ErrPublic) {
		t.Errorf("err %v does not wrap objects.ErrPublic", err)
	}
}

func TestProbe_StoreUnreachable_LogsAndContinues(t *testing.T) { // F55
	r := newRig(t)
	r.store.down()
	wantNoErr(t, r.svc.Probe(ctxT(t)))
	logged := false
	for _, rec := range r.cap.Records() {
		if rec.Level >= 8 { // slog.LevelError
			logged = true
		}
	}
	if !logged {
		t.Error("an unreachable store must be logged at error level")
	}
}

func TestProbe_TempDirUnwritable_LogsAndContinues(t *testing.T) { // F55
	r := newRig(t, withTempDir(filepath.Join(t.TempDir(), "gone", "away")))
	wantNoErr(t, r.svc.Probe(ctxT(t)))
}

func TestProbe_Forbidden_LogsAndContinues(t *testing.T) { // F55: forbidden is not a positive public finding
	r := newRig(t)
	r.store.fail("ping", errs.E("test.Ping", errs.Permission, errors.New("403")))
	wantNoErr(t, r.svc.Probe(ctxT(t)))
}

func TestProbe_WritesNoRow(t *testing.T) {
	r := newRig(t)
	wantNoErr(t, r.svc.Probe(ctxT(t)))
	for _, c := range r.fake.Calls() {
		u := strings.ToUpper(c.SQL)
		if strings.Contains(u, "INSERT") || strings.Contains(u, "UPDATE") || strings.Contains(u, "DELETE") {
			t.Errorf("Start ran a write statement: %s", c.SQL)
		}
	}
}

// ---- guards that need no rows ----

func TestExportPerson_ZeroID_ReturnsInvalid(t *testing.T) { // F44
	r := newRig(t)
	_, err := r.svc.ExportPerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
}

func TestDeletePerson_ZeroID_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	err := r.inTx(ctxT(t), func(ctx context.Context) error {
		_, err := r.svc.DeletePerson(ctx, model.PersonID{})
		return err
	})
	wantKind(t, err, errs.Invalid)
}

func TestDeletePerson_NoWriteTx_ReturnsInternal(t *testing.T) { // F47
	r := newRig(t)
	_, err := r.svc.DeletePerson(ctxT(t), newPerson())
	wantKind(t, err, errs.Internal)
	err = r.db.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		_, err := r.svc.DeletePerson(ctx, newPerson())
		return err
	})
	wantKind(t, err, errs.Internal)
}

func TestReap_ReadOnly_ReturnsUnavailable(t *testing.T) { // F48
	r := newRig(t)
	r.fake.SetReadOnly(true)
	_, err := r.svc.Reap(ctxT(t))
	wantReadOnly(t, err)
}

func TestSweepOrphans_ReadOnly_ReturnsUnavailable(t *testing.T) { // F48
	r := newRig(t)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.fake.SetReadOnly(true)
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err := r.svc.SweepOrphans(ctxT(t))
	wantReadOnly(t, err)
	if got := len(r.objectKeys(st.Bucket)); got != 1 {
		t.Errorf("a read-only copy deleted objects (%d left)", got)
	}
}

func TestVerify_StoreUnreachableNoRows_IsNotAnError(t *testing.T) { // F53 (rows version is the integration test)
	r := newRig(t)
	r.store.down()
	_, err := r.svc.Verify(ctxT(t))
	// With no rows nothing needs the store; a service that heads nothing is acceptable. The
	// integration test with rows (TestVerify_StoreUnreachable...) holds the real rule.
	if err != nil {
		wantKind(t, err, errs.Unavailable)
	}
}

// ---- SweepOrphans over an empty catalogue (F50, F51) ----
//
// dbtest.NewFake holds no rows, so every object here has "neither row nor tombstone"; what
// the tests check is age, ProbeKey, buckets and temp files. Objects whose row or tombstone
// keeps them alive are in the integration tests.

func TestSweep_YoungOrphan_Kept(t *testing.T) { // F51
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.clk.Advance(files.OrphanAge - time.Hour) //nolint:errcheck,gosec // fake clock
	got, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.Orphans != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Errorf("swept %+v; an object younger than OrphanAge must be kept", got)
	}
}

func TestSweep_OldOrphan_Removed(t *testing.T) {
	r := newRig(t)
	a := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	b := mustStage(t, r.inc, "b.pdf", files.Documents, pdfDoc(""))
	r.clk.Advance(files.OrphanAge + time.Minute) //nolint:errcheck,gosec // fake clock
	got, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.Orphans != 2 {
		t.Errorf("Swept = %+v, want 2 orphans", got)
	}
	r.wantNoObjects(a.Bucket)
	r.wantNoObjects(b.Bucket)
	// Idempotent.
	got, err = r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.Orphans != 0 || got.Tombstones != 0 || got.TempFiles != 0 {
		t.Errorf("second sweep = %+v, want zeros", got)
	}
}

func TestSweep_RemovesOrphanAfterOrphanAgeAndKeepsBefore(t *testing.T) { // the F51 sequence
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.clk.Advance(23 * time.Hour) //nolint:errcheck,gosec // fake clock
	got, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.Orphans != 0 || len(r.objectKeys(ownerVol)) != 1 {
		t.Fatalf("at 23h: swept %+v", got)
	}
	r.clk.Advance(2 * time.Hour) //nolint:errcheck,gosec // fake clock
	got, err = r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.Orphans != 1 {
		t.Errorf("at 25h: swept %+v, want 1 orphan", got)
	}
	r.wantNoObjects(ownerVol)
}

func TestSweep_ProbeKeyIsSkipped(t *testing.T) {
	r := newRig(t)
	mustPutRaw(t, r, ownerVol, objects.ProbeKey, []byte("0123456789abcdef"))
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if _, err := r.inner.Head(ctxT(t), ownerVol, objects.ProbeKey); err != nil {
		t.Errorf("the sweep deleted ProbeKey: %v", err)
	}
}

func TestSweep_TempFiles_OldRemovedYoungKept(t *testing.T) {
	r := newRig(t)
	oldF := filepath.Join(r.tmp, "0190a1b2-c3d4-4e5f-8a6b-000000000001")
	youngF := filepath.Join(r.tmp, "0190a1b2-c3d4-4e5f-8a6b-000000000002")
	for _, p := range []string{oldF, youngF} {
		if err := os.WriteFile(p, []byte("spool"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-(files.OrphanAge + time.Hour))
	if err := os.Chtimes(oldF, old, old); err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	if got.TempFiles != 1 {
		t.Errorf("Swept = %+v, want 1 temp file", got)
	}
	if _, err := os.Stat(oldF); !os.IsNotExist(err) {
		t.Error("an old temp file survived")
	}
	if _, err := os.Stat(youngF); err != nil {
		t.Errorf("a young temp file (an upload in flight) was removed: %v", err)
	}
}

func TestSweep_ListFails_ReturnsUnavailable(t *testing.T) { // F50
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.store.fail("list", unavailable("list"))
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err := r.svc.SweepOrphans(ctxT(t))
	wantKind(t, err, errs.Unavailable)
}

func TestSweep_RecordsFilesSweepRan(t *testing.T) {
	r := newRig(t)
	mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err := r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)
	es := entriesWith(r.audit, "files.sweep.ran")
	if len(es) != 1 {
		t.Fatalf("files.sweep.ran entries = %d, want 1", len(es))
	}
	if es[0].Reason != "orphans:1,temp:0" {
		t.Errorf("Reason = %q, want orphans:1,temp:0", es[0].Reason)
	}
}

func TestSweep_ObjectDeleteFails_DoesNotStopTheRun(t *testing.T) {
	r := newRig(t)
	a := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	b := mustStage(t, r.vol, "b.png", files.Images, plainPNG(t))
	r.store.failKey(a.Key, unavailable("delete"))
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err := r.svc.SweepOrphans(ctxT(t))
	_ = err // either a joined Unavailable or nil is acceptable; the other orphan must still go
	if _, herr := r.inner.Head(ctxT(t), b.Bucket, b.Key); herr == nil {
		t.Error("one failing delete stopped the sweep of the other orphan")
	}
	if _, herr := r.inner.Head(ctxT(t), a.Bucket, a.Key); herr != nil {
		t.Error("the failing object disappeared anyway")
	}
}

func mustPutRaw(t testing.TB, r *rig, b objects.Bucket, k objects.Key, data []byte) {
	t.Helper()
	putRaw(t, r.inner, b, k, data)
}
