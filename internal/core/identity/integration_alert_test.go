//go:build integration

package identity_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The superuser alert (boundaries step 9; app.md "The superuser alert", identity F27). The
// alert is unexported, so each test signs the superuser in and reads the three Options
// seams (Holders, Notify, Alerts), the failure counter and the warn log.

const (
	alertAuditLink      = "/audit?superuser=1"
	metricAlertFailures = "bms.identity.alert_failures"
)

const tplSuperuserSignedIn notify.Template = "identity.superuser-signed-in"

// alertRig is a pool rig whose three alert seams are the fakes below.
type alertRig struct {
	*prig
	holders *holdersFake
	nf      *notifytest.Fake
	alerts  *alertSenderFake
}

func newAlertRig(t *testing.T, holderIDs ...model.PersonID) *alertRig {
	t.Helper()
	r := &alertRig{holders: &holdersFake{ids: holderIDs}, nf: notifytest.NewFake("identity"), alerts: &alertSenderFake{}}
	r.prig = newPoolRig(t, func(o *identity.Options) { o.Holders, o.Notify, o.Alerts = r.holders, r.nf, r.alerts })
	return r
}

// failureKinds returns the error.kind of each warn record about a failed alert.
func (r *alertRig) failureKinds() []string {
	r.t.Helper()
	var kinds []string
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn {
			if k, ok := rec.Attrs["error.kind"]; ok {
				kinds = append(kinds, k)
			}
		}
	}
	return kinds
}

// wantOneFailure asserts the sign-in's alert failed once, with kind, counted and logged.
func (r *alertRig) wantOneFailure(kind errs.Kind) {
	r.t.Helper()
	if got := r.measured(metricAlertFailures, nil); got != 1 {
		r.t.Errorf("alert_failures = %v, want 1", got)
	}
	ks := r.failureKinds()
	if len(ks) != 1 || ks[0] != kind.String() {
		r.t.Errorf("failure log kinds = %v, want one %q", ks, kind.String())
	}
}

func (r *alertRig) wantNoFailure() {
	r.t.Helper()
	if got := r.measured(metricAlertFailures, nil); got != 0 {
		r.t.Errorf("alert_failures = %v, want 0", got)
	}
	if ks := r.failureKinds(); len(ks) != 0 {
		r.t.Errorf("failure log kinds = %v, want none", ks)
	}
}

func TestAlert_HoldersAndAddresses_SendsToBothWithAnEmptyKey(t *testing.T) {
	holder := model.NewID[model.PersonID]()
	r := newAlertRig(t, holder)
	r.superuser("root.operator@example.org")

	if got := r.holders.permissions(); len(got) != 1 || got[0] != identity.PermissionAccountsManage {
		t.Errorf("holders asked for %v, want accounts.manage once", got)
	}
	sent := r.nf.Sent()
	if len(sent) != 1 || len(sent[0].To) != 1 || sent[0].To[0] != holder || sent[0].Template != tplSuperuserSignedIn ||
		sent[0].Link != alertAuditLink || sent[0].Key != "" {
		t.Errorf("holders' message = %+v, want one to the holder, the alert template, the audit link and no key", sent)
	}
	as := r.alerts.sent()
	if len(as) != 1 || as[0].Template != tplSuperuserSignedIn || as[0].Link != alertAuditLink || as[0].Key != "" || len(as[0].To) != 0 {
		t.Errorf("alert-address message = %+v, want one with the template, link, no key and no recipients", as)
	}
	r.wantNoFailure()
}

func TestAlert_TwoSignIns_EachSendsAnAlert(t *testing.T) {
	holder := model.NewID[model.PersonID]()
	r := newAlertRig(t, holder)
	res, _ := r.superuser("root.operator@example.org")
	_, err := r.svc.SignIn(from(ctxT(t), nextIP()), identity.Credentials{Email: mail(t, "root.operator@example.org"), Password: res.Password.Value()})
	wantNoErr(t, err)

	sent := r.nf.Sent()
	if len(sent) != 2 || sent[0].Key != "" || sent[1].Key != "" {
		t.Errorf("holders' messages = %+v, want two with no key (a key would drop the second)", sent)
	}
	if as := r.alerts.sent(); len(as) != 2 || as[0].Key != "" || as[1].Key != "" {
		t.Errorf("alert-address messages = %+v, want two with no key", as)
	}
}

// Holders exist, no alert address is configured (SendAlert: Unsupported wrapping
// ErrNoAlertAddress), SMTP is configured: the holders are told and the alert succeeded.
func TestAlert_HoldersButNoAddress_Succeeds(t *testing.T) {
	r := newAlertRig(t, model.NewID[model.PersonID]())
	r.alerts.noAddress = true
	r.superuser("root.operator@example.org")
	if len(r.nf.Sent()) != 1 {
		t.Errorf("holders sent = %d, want 1", len(r.nf.Sent()))
	}
	r.wantNoFailure()
}

// The holders lookup fails and there is no alert address: the failure is the holders', not
// "nobody to tell".
func TestAlert_HoldersLookupFailsNoAddress_KeepsTheHoldersKind(t *testing.T) {
	r := newAlertRig(t)
	r.holders.err = errs.E("test", errs.Unavailable, errors.New("down"))
	r.alerts.noAddress = true
	r.superuser("root.operator@example.org")
	r.wantOneFailure(errs.Unavailable)
}

func TestAlert_PartialFailure_SendsTheRestAndJoinsTheErrors(t *testing.T) {
	t.Run("the holders' send fails, the addresses are still told", func(t *testing.T) {
		r := newAlertRig(t, model.NewID[model.PersonID]())
		r.nf.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
		r.superuser("root.operator@example.org")
		if len(r.alerts.sent()) != 1 {
			t.Errorf("alert-address sends = %d, want 1 (the holders' failure must not stop the addresses)", len(r.alerts.sent()))
		}
		r.wantOneFailure(errs.Unavailable)
	})
	t.Run("the addresses fail, the holders are still told", func(t *testing.T) {
		r := newAlertRig(t, model.NewID[model.PersonID]())
		r.alerts.err = errs.E("test", errs.Unavailable, errors.New("down"))
		r.superuser("root.operator@example.org")
		if len(r.nf.Sent()) != 1 {
			t.Errorf("holders' sends = %d, want 1", len(r.nf.Sent()))
		}
		r.wantOneFailure(errs.Unavailable)
	})
}

func TestAlert_FailuresAreJoinedUnderTheFirstKind(t *testing.T) {
	r := newAlertRig(t, model.NewID[model.PersonID]())
	r.holders.err = errs.E("test", errs.Unavailable, errors.New("down"))
	r.alerts.err = errs.E("test", errs.Timeout, errors.New("slow"))
	r.superuser("root.operator@example.org")
	r.wantOneFailure(errs.Unavailable) // the holders lookup fails first
}

func TestAlert_HoldersLookupFails_StillTellsTheAddresses(t *testing.T) {
	r := newAlertRig(t)
	r.holders.err = errs.E("test", errs.Unavailable, errors.New("down"))
	r.superuser("root.operator@example.org")
	if len(r.alerts.sent()) != 1 || len(r.nf.Sent()) != 0 {
		t.Errorf("address sends = %d, holder sends = %d, want 1 and 0", len(r.alerts.sent()), len(r.nf.Sent()))
	}
	r.wantOneFailure(errs.Unavailable)
}

func TestAlert_AddressesReadFails_StillTellsTheHolders(t *testing.T) {
	r := newAlertRig(t, model.NewID[model.PersonID]())
	r.alerts.err = errs.E("test", errs.Timeout, errors.New("slow"))
	r.superuser("root.operator@example.org")
	if len(r.nf.Sent()) != 1 {
		t.Errorf("holders sent = %d, want 1", len(r.nf.Sent()))
	}
	r.wantOneFailure(errs.Timeout)
}

func TestAlert_NotConfigured_ReturnsUnsupported(t *testing.T) {
	r := newAlertRig(t, model.NewID[model.PersonID]())
	r.nf.SetConfigured(false)
	r.alerts.err = errs.E("test", errs.Unsupported, notify.ErrNotConfigured)
	_, iss := r.superuser("root.operator@example.org")
	if !iss.Session.Superuser {
		t.Error("the sign-in did not succeed")
	}
	r.wantOneFailure(errs.Unsupported)
}

func TestAlert_ReadOnlyCopy_ReturnsUnavailableWrappingReadOnly(t *testing.T) {
	ro := errs.E("test", errs.Unavailable, db.ErrReadOnly)
	r := newAlertRig(t, model.NewID[model.PersonID]())
	r.nf.Fail(ro)
	r.alerts.err = ro
	r.superuser("root.operator@example.org")
	// The alert's error is not visible from outside; its kind is, in the log and the count.
	r.wantOneFailure(errs.Unavailable)
}

func TestAlert_NobodyToTell_ReturnsUnsupported(t *testing.T) {
	r := newAlertRig(t)       // no holders
	r.alerts.noAddress = true // notify's B11
	_, iss := r.superuser("root.operator@example.org")
	if !iss.Session.Superuser {
		t.Error("the sign-in did not succeed")
	}
	if len(r.nf.Sent()) != 0 || len(r.alerts.sent()) != 0 {
		t.Error("something was sent to nobody")
	}
	r.wantOneFailure(errs.Unsupported)
}

func TestAlert_NoHolders_DoesNotSendToHolders(t *testing.T) {
	r := newAlertRig(t)
	r.superuser("root.operator@example.org")
	if len(r.nf.Sent()) != 0 {
		t.Errorf("Notify.Send was used with no holders: %+v", r.nf.Sent())
	}
	if len(r.alerts.sent()) != 1 {
		t.Errorf("alert-address sends = %d, want 1", len(r.alerts.sent()))
	}
	r.wantNoFailure()
}

// The alert runs under its own deadline of 5s that a disconnecting client cannot cancel.
func TestAlert_RunsUnderADetachedFiveSecondDeadline(t *testing.T) {
	r := newAlertRig(t, model.NewID[model.PersonID]())
	res, err := r.svc.EnsureSuperuser(ctxT(t), mail(t, "root.operator@example.org"))
	wantNoErr(t, err)
	ctx, cancel := context.WithCancel(from(ctxT(t), nextIP()))
	defer cancel()
	r.holders.onCall = func(context.Context) { cancel() } // the client goes away mid-alert
	_, err = r.svc.SignIn(ctx, identity.Credentials{Email: mail(t, "root.operator@example.org"), Password: res.Password.Value()})
	wantNoErr(t, err)

	r.alerts.mu.Lock()
	defer r.alerts.mu.Unlock()
	if len(r.alerts.ctxErrs) != 1 {
		t.Fatalf("alert-address sends = %d, want 1", len(r.alerts.ctxErrs))
	}
	if r.alerts.ctxErrs[0] != nil {
		t.Errorf("the alert's ctx was canceled with the client: %v", r.alerts.ctxErrs[0])
	}
	if dl := r.alerts.deadlines[0]; dl.IsZero() || time.Until(dl) > 5*time.Second || time.Until(dl) < 2*time.Second {
		t.Errorf("alert deadline in %v, want about 5s", time.Until(dl))
	}
	if len(r.nf.Sent()) != 1 {
		t.Errorf("holders sent = %d, want 1 after the client went away", len(r.nf.Sent()))
	}
}

// Privacy: identity never logs an alert address (or anything a failing sender says about
// one); the failure line carries the person ID and the kind.
func TestAlert_FailureLog_HoldsNoAddress(t *testing.T) {
	const alertAddr = "desk.alerts@example.org"
	r := newAlertRig(t, model.NewID[model.PersonID]())
	r.alerts.err = errs.E("test", errs.Unavailable, errors.New("relay refused "+alertAddr))
	r.nf.Fail(errs.E("test", errs.Unavailable, errors.New("relay refused "+alertAddr)))
	res, _ := r.superuser("root.operator@example.org")
	if r.cap.Contains(alertAddr) {
		t.Error("a log, span or metric contains an alert address")
	}
	found := false
	for _, rec := range r.cap.Records() {
		if _, ok := rec.Attrs["error.kind"]; ok && rec.Attrs["person_id"] == res.Person.String() {
			found = true
		}
	}
	if !found {
		t.Error("the failure line does not carry the person ID and the kind")
	}
	r.noLeak("root.operator")
}
