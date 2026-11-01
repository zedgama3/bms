//go:build integration

package notify_test

import (
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestSendAlert_QueuesOneRowPerAddress_NoPersonNoKey(t *testing.T) {
	r := newIRig(t)
	r.smtp.set(func(c *notify.SMTPConfig) { c.AlertAddresses = []model.Email{"desk@example.org", "night@example.org"} })
	a, err := r.svc.Alerts("volunteers")
	wantNoErr(t, err)
	wantNoErr(t, a.SendAlert(ctxT(t), greet()))
	rs := r.rows("")
	if len(rs) != 2 {
		t.Fatalf("%d rows, want one per alert address", len(rs))
	}
	got := map[string]bool{}
	for _, x := range rs {
		if x.Person != nil || x.Address == nil || x.Owner != "volunteers" || x.Key != nil {
			t.Errorf("row = %+v, want an address row of the owner with no person and no key", x)
		}
		got[*x.Address] = true
	}
	if !got["desk@example.org"] || !got["night@example.org"] {
		t.Errorf("addresses = %v", got)
	}
	// Twice is two alerts: every sign-in is its own event.
	wantNoErr(t, a.SendAlert(ctxT(t), greet()))
	if n := r.count(); n != 4 {
		t.Errorf("%d rows after a second alert, want 4", n)
	}
}

func TestSendAlert_NotConfigured_ReturnsUnsupportedWrappingErrNotConfigured(t *testing.T) {
	r := newIRig(t)
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = ""; c.AlertAddresses = []model.Email{"desk@example.org"} })
	a, err := r.svc.Alerts("volunteers")
	wantNoErr(t, err)
	err = a.SendAlert(ctxT(t), greet())
	wantKind(t, err, errs.Unsupported)
	if !errors.Is(err, notify.ErrNotConfigured) {
		t.Errorf("err = %v, want it to wrap ErrNotConfigured", err)
	}
	if n := r.count(); n != 0 {
		t.Errorf("%d rows queued with SMTP unset", n)
	}
}
