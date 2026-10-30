//go:build integration

package notify_test

// D57/D58: the Skipped status. F22, F45, F48, F49, purge and the outbox. Every address is
// invented.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/model"
)

// skippedRow queues a message for a person with no address and ticks, so the row ends skipped.
func skippedRow(t *testing.T, r *irig) (model.PersonID, row) {
	t.Helper()
	p := r.queue("gone@example.org")
	r.addrs.set(p, "")
	if got := r.sendOnce(); got.Skipped != 1 {
		t.Fatalf("setup: Sent = %+v", got)
	}
	x := r.one(p)
	if x.Status != "skipped" {
		t.Fatalf("setup: %+v", x)
	}
	return p, x
}

// F48
func TestRetry_Skipped_Conflict(t *testing.T) {
	r := newIRig(t)
	_, x := skippedRow(t, r)
	err := r.svc.Retry(r.admin(string(permOutbox)), notify.MessageID(x.ID))
	wantConflictField(t, err, "status", "")
	if got := r.byID(x.ID); got.Status != "skipped" {
		t.Fatalf("status changed to %s", got.Status)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("a refused Retry was audited")
	}
}

// F49
func TestCancel_Skipped_Conflict(t *testing.T) {
	r := newIRig(t)
	_, x := skippedRow(t, r)
	err := r.svc.Cancel(r.admin(string(permOutbox)), notify.MessageID(x.ID))
	wantConflictField(t, err, "status", "")
	if r.count() != 1 {
		t.Fatal("the skipped row was deleted by Cancel")
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("a refused Cancel was audited")
	}
}

// F22: purge covers skipped rows, by their end instant in failed_at.
func TestPurge_DeletesSkippedRows(t *testing.T) {
	r := newIRig(t)
	r.ret.set(notify.MinRetention)
	oldP, _ := skippedRow(t, r)
	newP, _ := skippedRow(t, r)
	old := r.clk.Now().Add(-notify.MinRetention - time.Hour)
	recent := r.clk.Now().Add(-notify.MinRetention + time.Hour)
	r.exec("UPDATE core.notify_message SET failed_at=$1 WHERE person_id=$2", old, [16]byte(oldP))
	r.exec("UPDATE core.notify_message SET failed_at=$1 WHERE person_id=$2", recent, [16]byte(newP))
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 1 {
		t.Fatalf("Purge = %d, want 1", n)
	}
	if len(r.rowsFor(oldP)) != 0 {
		t.Error("an old skipped row survived Purge")
	}
	if len(r.rowsFor(newP)) != 1 {
		t.Error("a recent skipped row was purged")
	}
}

// Retention "forever" keeps skipped rows too.
func TestPurge_Forever_KeepsSkippedRows(t *testing.T) {
	r := newIRig(t)
	p, _ := skippedRow(t, r)
	r.exec("UPDATE core.notify_message SET failed_at=$1 WHERE person_id=$2", r.clk.Now().Add(-100000*time.Hour), [16]byte(p))
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 0 || len(r.rowsFor(p)) != 1 {
		t.Errorf("Purge = %d, rows = %d, want nothing deleted", n, len(r.rowsFor(p)))
	}
}

// Outbox: skipped counted, listed with its reason, no address or text.
func TestOutbox_SkippedCountedAndListed(t *testing.T) {
	r := newIRig(t)
	p, _ := skippedRow(t, r)
	v, err := r.svc.Outbox(r.admin(string(permOutbox)))
	wantNoErr(t, err)
	skipped := statusOf(t, "skipped")
	if v.Counts[skipped] != 1 {
		t.Errorf("Counts = %v, want one skipped", v.Counts)
	}
	var found bool
	for _, rec := range v.Recent {
		if rec.Person == p {
			found = true
			if rec.Status != skipped || rec.LastError != "no_address" {
				t.Errorf("record = %+v, want skipped with no_address", rec)
			}
			if rec.Subject != "" {
				t.Errorf("a skipped record exposes a subject: %q", rec.Subject)
			}
		}
	}
	if !found {
		t.Error("the skipped row is not among Recent")
	}
	raw, err := json.Marshal(v)
	wantNoErr(t, err)
	if !strings.Contains(string(raw), `"skipped"`) {
		t.Errorf("the wire shape lacks the skipped status: %s", raw)
	}
	if strings.Contains(string(raw), "gone@example.org") {
		t.Errorf("Outbox JSON carries an address: %s", raw)
	}
	// Every status key is present in Counts, even at zero.
	for _, name := range []string{"queued", "sending", "sent", "failed", "skipped"} {
		if _, ok := v.Counts[statusOf(t, name)]; !ok {
			t.Errorf("Counts lacks %s: %v", name, v.Counts)
		}
	}
}

// The ExportPerson of a person with a skipped row works and carries no address.
func TestExportPerson_SkippedRow_NoAddress(t *testing.T) {
	r := newIRig(t)
	p, _ := skippedRow(t, r)
	raw, err := r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	if strings.Contains(string(raw), "gone@example.org") {
		t.Errorf("export carries an address: %s", raw)
	}
}
