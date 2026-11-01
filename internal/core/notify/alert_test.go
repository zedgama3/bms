package notify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The AlertSender (boundaries step 9). Addresses below are invented.

// alertSender returns the AlertSender of owner over a rig whose alert addresses are addrs.
func alertSender(t *testing.T, r *rig, owner string, addrs ...model.Email) notify.AlertSender {
	t.Helper()
	r.smtp.set(func(c *notify.SMTPConfig) { c.AlertAddresses = addrs })
	a, err := r.svc.Alerts(owner)
	wantNoErr(t, err)
	if a == nil {
		t.Fatal("nil AlertSender")
	}
	return a
}

// B11
func TestSendAlert_NoAddress_ReturnsUnsupported(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	a := alertSender(t, r, "volunteers") // no alert address
	err := a.SendAlert(ctxT(t), greet())
	wantKind(t, err, errs.Unsupported)
	if !errors.Is(err, notify.ErrNoAlertAddress) {
		t.Errorf("err = %v, want it to wrap ErrNoAlertAddress", err)
	}
	if n := len(d.Calls()); n != 0 {
		t.Errorf("%d store calls, want none: nothing is queued with nobody to tell", n)
	}
}

// B12
func TestSendAlert_OtherOwnersTemplate_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		addrs []model.Email
	}{
		{"with an address configured", []model.Email{"desk@example.org"}},
		{"with no address configured", nil}, // a programming error is reported before "nobody to tell"
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			a := alertSender(t, r, "people", tc.addrs...)
			err := a.SendAlert(ctxT(t), greet()) // volunteers.greet, sent by "people"
			wantKind(t, err, errs.Invalid)
			if n := len(d.Calls()); n != 0 {
				t.Errorf("%d store calls, want none", n)
			}
		})
	}
}

func TestSendAlert_InvalidMessage_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	a := alertSender(t, r, "volunteers", "desk@example.org")
	wantKind(t, a.SendAlert(ctxT(t), notify.Message{}), errs.Invalid)
}

func TestAlerts_BadOwner_ReturnsInvalid(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	for _, owner := range []string{"", "Bad Owner", "has.dot", "1abc"} {
		a, err := r.svc.Alerts(owner)
		wantKind(t, err, errs.Invalid)
		if a != nil {
			t.Errorf("Alerts(%q) returned a sender with an error", owner)
		}
	}
}

func TestAlerts_AfterRun_ReturnsInternal(t *testing.T) {
	d := dbtest.NewFake()
	d.SetReadOnly(true) // the loop idles; nothing is written
	r := newRig(t, d)
	ctx, cancel := context.WithCancel(ctxT(t))
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	var got error
	eventually(t, "Run to start", func() bool {
		_, got = r.svc.Alerts("identity")
		return got != nil
	})
	wantKind(t, got, errs.Internal)
	cancel()
	wantNoErr(t, <-done)
}

func TestSendAlert_ConcurrentCalls_AreSafe(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	a := alertSender(t, r, "volunteers") // Unsupported: no store needed
	done := make(chan error, 8)
	for range 8 {
		go func() { done <- a.SendAlert(ctxT(t), greet()) }()
	}
	for range 8 {
		wantKind(t, <-done, errs.Unsupported)
	}
}

func TestSendAlert_ReadOnlyCopy_ReturnsUnavailableWrappingReadOnly(t *testing.T) {
	d := dbtest.NewFake()
	d.SetReadOnly(true)
	r := newRig(t, d)
	a := alertSender(t, r, "volunteers", "desk@example.org")
	err := a.SendAlert(ctxT(t), greet())
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("err = %v, want it to wrap db.ErrReadOnly", err)
	}
}
