//go:build integration

package settings_test

// settings.Revise (boundaries A2, point 1; B34 to B38): the write seam regional's logo writes
// run inside. Every value is invented.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// insertRow adds a core.setting row through the transaction ctx carries, so a rollback
// removes it.
func insertRow(ctx context.Context, r *irig, key string) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO core.setting (key, value, updated_at) VALUES ($1, 'x', now())", key)
	return err //nolint:wrapcheck // the error under test
}

func TestRevise_Changed_BumpsAndInstallsSnapshot_RealDatabase(t *testing.T) {
	r := newIRig(t)
	ctx := r.actx()
	before := r.dbRevision()
	rev, err := r.svc.Revise(ctx, func(context.Context) (bool, error) { return true, nil })
	wantNoErr(t, err)
	if int64(rev) != before+1 || r.dbRevision() != before+1 {
		t.Errorf("Revise = %d, stored %d, want %d", rev, r.dbRevision(), before+1)
	}
	if got := first(r.svc.Revision(ctx)); got != rev {
		t.Errorf("Revision after Revise = %d, want the bumped %d at once", got, rev)
	}
}

// B34
func TestRevise_FnFails_RollsBackAndKeepsRevision(t *testing.T) {
	r := newIRig(t)
	ctx := r.actx()
	ignore(r.svc.Revision(ctx))
	before, rows := r.dbRevision(), r.rowCount()
	boom := errs.E("test.fn", errs.Conflict, errors.New("fn failed"))
	_, err := r.svc.Revise(ctx, func(ctx context.Context) (bool, error) {
		if err := insertRow(ctx, r, "mail.note"); err != nil {
			return false, err
		}
		return true, boom
	})
	wantKind(t, err, errs.Conflict)
	if r.dbRevision() != before || r.rowCount() != rows {
		t.Errorf("revision %d (was %d), rows %d (was %d): fn's work was not rolled back", r.dbRevision(), before, r.rowCount(), rows)
	}
	if got := first(r.svc.Revision(ctx)); int64(got) != before {
		t.Errorf("Revision = %d after a failed Revise, want %d", got, before)
	}
}

// B35
func TestRevise_ReadOnlyCopy_ReturnsUnavailable(t *testing.T) {
	base := newIRig(t)
	f := &faultDB{DB: base.pool}
	r := newIRigOn(t, f, base.pool)
	ignore(r.svc.Revision(r.actx()))
	f.set(nil, nil, true)
	before := base.dbRevision()
	ran := false
	_, err := r.svc.Revise(r.actx(), func(context.Context) (bool, error) { ran = true; return true, nil })
	wantReadOnly(t, err)
	if ran {
		t.Error("fn ran on a read-only copy")
	}
	if base.dbRevision() != before {
		t.Error("the revision moved on a read-only copy")
	}
}

// B36
func TestRevise_Unchanged_ReturnsCurrentRevision(t *testing.T) {
	r := newIRig(t)
	ctx := r.actx()
	r.mustSet(kPort, "2525")
	before, entries := r.dbRevision(), len(r.rec.Recorded())
	for i := range 2 {
		rev, err := r.svc.Revise(ctx, func(context.Context) (bool, error) { return false, nil })
		wantNoErr(t, err)
		if int64(rev) != before {
			t.Errorf("call %d: Revise = %d, want the current %d", i, rev, before)
		}
	}
	if r.dbRevision() != before || len(r.rec.Recorded()) != entries {
		t.Errorf("an unchanged Revise moved the revision to %d or recorded %v", r.dbRevision(), actionsOf(r.rec.Recorded()[entries:]))
	}
}

// B37
func TestRevise_NoRevisionRow_ReturnsInternal(t *testing.T) {
	r := newIRig(t)
	r.exec("DELETE FROM core.setting_revision")
	ran := false
	_, err := r.svc.Revise(r.actx(), func(context.Context) (bool, error) { ran = true; return true, nil })
	wantKind(t, err, errs.Internal)
	if ran {
		t.Error("fn ran without a revision row to lock")
	}
}

// B38: two instances revise at once. fn A starts and blocks inside the transaction; B is then
// asked to revise and its fn must not begin until A has returned and committed (the revision
// row lock, which is the only thing that serialises instances), and it sees A's bump.
func TestRevise_Concurrent_Serialised(t *testing.T) {
	a := newIRig(t)
	b := newIRigOn(t, a.pool, a.pool)
	before := a.dbRevision()
	aStarted, aRelease, bStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	type result struct {
		rev settings.Revision
		err error
	}
	aDone, bDone := make(chan result, 1), make(chan result, 1)
	go func() {
		rev, err := a.svc.Revise(ctxT(t), func(context.Context) (bool, error) {
			close(aStarted)
			<-aRelease
			return true, nil
		})
		aDone <- result{rev, err}
	}()
	<-aStarted
	go func() {
		rev, err := b.svc.Revise(ctxT(t), func(context.Context) (bool, error) {
			close(bStarted)
			return true, nil
		})
		bDone <- result{rev, err}
	}()
	select {
	case <-bStarted:
		close(aRelease)
		t.Fatal("B's fn began while A's was still inside its transaction: the revision row was not locked")
	case <-time.After(500 * time.Millisecond): // B has not started: it is waiting on the lock
	}
	close(aRelease)
	ra, rb := <-aDone, <-bDone
	wantNoErr(t, ra.err)
	wantNoErr(t, rb.err)
	if int64(ra.rev) != before+1 || int64(rb.rev) != before+2 || a.dbRevision() != before+2 {
		t.Errorf("revisions A=%d B=%d stored %d, want %d, %d, %d", ra.rev, rb.rev, a.dbRevision(), before+1, before+2, before+2)
	}
}

// F-Normalize (A2 point 5): the declared rewrite runs on the trimmed value before Check and
// before the value is stored.
func TestSet_NormalizeRunsBeforeCheck(t *testing.T) {
	area := settings.Area{Name: "norm", Decls: []settings.Decl{{
		Key: "code", Kind: settings.Text,
		Normalize: func(v string) string { return upperASCII(v) },
		Check: func(v string) error {
			if v != upperASCII(v) {
				return errors.New("must be upper case")
			}
			return nil
		},
	}}}
	r := newIRig(t, func(o *settings.Options) { o.Areas = append(o.Areas, area) })
	r.mustSet("norm.code", "  abc  ")
	if v, ok := r.stored("norm.code"); !ok || v != "ABC" {
		t.Errorf("stored %q (%v), want the trimmed, normalised ABC", v, ok)
	}
	if got := first(r.reader("norm").Text(ctxT(t), "code")); got != "ABC" {
		t.Errorf("read %q, want ABC", got)
	}
}

func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
