package identity

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// White-box table for sendAlert: holders or none, an alert address or none, SMTP configured
// or not, and each failure. All addresses and people are invented.

type tHolders struct {
	ids []model.PersonID
	err error
}

func (h tHolders) HoldersOf(context.Context, authz.Permission) ([]model.PersonID, error) {
	return h.ids, h.err
}

type tAlerts struct {
	mu   sync.Mutex
	err  error
	msgs []notify.Message
}

func (a *tAlerts) SendAlert(_ context.Context, m notify.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.msgs = append(a.msgs, m)
	return nil
}

func TestSendAlert_Table(t *testing.T) {
	down := func() error { return errs.E("test", errs.Unavailable, errors.New("down")) }
	slow := func() error { return errs.E("test", errs.Timeout, errors.New("slow")) }
	noAddr := func() error { return errs.E("test", errs.Unsupported, notify.ErrNoAlertAddress) }
	noSMTP := func() error { return errs.E("test", errs.Unsupported, notify.ErrNotConfigured) }
	ro := func() error { return errs.E("test", errs.Unavailable, db.ErrReadOnly) }
	one := []model.PersonID{model.NewID[model.PersonID]()}

	tests := []struct {
		name        string
		holders     tHolders
		sendErr     error // Notify.Send
		alertErr    error // Alerts.SendAlert
		smtpOff     bool  // Notify.SetConfigured(false) (the fake then fails Send)
		want        errs.Kind
		wantNil     bool
		wantHolders int // messages the Notify fake recorded
		wantAlerts  int
	}{
		{name: "holders and addresses", holders: tHolders{ids: one}, wantNil: true, wantHolders: 1, wantAlerts: 1},
		{name: "holders only: no address", holders: tHolders{ids: one}, alertErr: noAddr(), wantNil: true, wantHolders: 1},
		{name: "address only: no holders", wantNil: true, wantAlerts: 1},
		{name: "a plain Unsupported from the sender is a failure", holders: tHolders{ids: one}, alertErr: errs.E("test", errs.Unsupported, errors.New("something else")), want: errs.Unsupported, wantHolders: 1},
		{name: "nobody to tell", alertErr: noAddr(), want: errs.Unsupported},
		{name: "nobody to tell, smtp not configured", alertErr: noSMTP(), want: errs.Unsupported},
		{name: "holders, smtp not configured", holders: tHolders{ids: one}, smtpOff: true, alertErr: noSMTP(), want: errs.Unsupported},
		{name: "holders lookup fails, address told", holders: tHolders{err: down()}, want: errs.Unavailable, wantAlerts: 1},
		{name: "holders lookup fails, no address: the holders' kind", holders: tHolders{err: down()}, alertErr: noAddr(), want: errs.Unavailable},
		{name: "address send fails, holders told", holders: tHolders{ids: one}, alertErr: slow(), want: errs.Timeout, wantHolders: 1},
		{name: "holders send fails, address told", holders: tHolders{ids: one}, sendErr: down(), want: errs.Unavailable, wantAlerts: 1},
		{name: "lookup then address fail: first kind wins", holders: tHolders{err: down()}, alertErr: slow(), want: errs.Unavailable},
		{name: "holders send then address fail: first kind wins", holders: tHolders{ids: one}, sendErr: down(), alertErr: slow(), want: errs.Unavailable},
		{name: "lookup, send then address fail: lookup first", holders: tHolders{ids: one, err: slow()}, sendErr: down(), alertErr: noSMTP(), want: errs.Timeout, wantHolders: 0},
		{name: "read-only copy", holders: tHolders{ids: one}, sendErr: ro(), alertErr: ro(), want: errs.Unavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := bareService(t)
			nf := notifytest.NewFake("identity")
			if tc.sendErr != nil {
				nf.Fail(tc.sendErr)
			}
			if tc.smtpOff {
				nf.SetConfigured(false)
			}
			al := &tAlerts{err: tc.alertErr}
			s.holders, s.notify, s.alerts = tc.holders, nf, al

			err := s.sendAlert(t.Context())
			if tc.wantNil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			} else if !errs.Is(err, tc.want) {
				t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, tc.want)
			}
			if tc.name == "read-only copy" && !errors.Is(err, db.ErrReadOnly) {
				t.Errorf("err = %v, want it to wrap db.ErrReadOnly", err)
			}
			if tc.name == "holders, smtp not configured" && !errors.Is(err, notify.ErrNotConfigured) {
				t.Errorf("err = %v, want it to wrap ErrNotConfigured", err)
			}
			if got := len(nf.Sent()); got != tc.wantHolders {
				t.Errorf("holders' messages = %d, want %d", got, tc.wantHolders)
			}
			if got := len(al.msgs); got != tc.wantAlerts {
				t.Errorf("alert-address messages = %d, want %d", got, tc.wantAlerts)
			}
			for _, m := range append(nf.Sent(), al.msgs...) {
				if m.Key != "" || m.Template != templateSuperuserSignedIn || m.Link != alertLink {
					t.Errorf("message = %+v, want the alert template, the audit link and no key", m)
				}
			}
		})
	}
}
