//go:build integration

package notify_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// TestNoPersonalDataInTelemetry (F62) drives every path that could log or trace a message
// with invented personal data and checks no address, subject, body, var or token reaches a
// log line, span, metric, audit entry or error text. telemetrytest also fails the test at
// cleanup on any policy violation.
func TestNoPersonalDataInTelemetry(t *testing.T) {
	r := newIRig(t)
	lc := &leakCheck{t: t, r: r.rig}
	ctx := r.admin(string(permTemplates), string(permOutbox))

	_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", notify.TemplateText{
		Subject: mSubj + " {{.Recipient.PlayaName}}", Body: mBody + " {{.Vars.note}} {{.Vars.shift}} {{.Link}}"})
	wantNoErr(t, err)

	p1, p2, p3 := r.dbPerson(mAddr), r.dbPerson(mAddr), r.dbPerson("")
	r.names.m[p1] = mName
	m := greet(p1, p2, p3)
	m.Vars = map[string]string{"note": mVar, "shift": mVar}
	m.Link = "/accounts/password-reset?token=" + mTok
	m.Key = "k:" + "1"
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	wantNoErr(t, r.vol.Send(ctxT(t), m)) // duplicate skip path
	wantNoErr(t, r.svc.SendTo(ctxT(t), mAddr2, noTo(m)))
	wantNoErr(t, r.svc.SendTo(ctxT(t), mAddr2, noTo(m))) // duplicate skip path

	// Delivery failures whose own text carries the address, the subject and the body.
	leaky := func(kind errs.Kind) error {
		return errs.E("test.ch", kind, errors.New("550 5.1.1 <"+mAddr+"> "+mSubj+" "+mBody+" "+mTok))
	}
	r.ch.failWith(leaky(errs.Unavailable))
	_, err = r.svc.SendOnce(ctxT(t))
	lc.note(err)
	r.advance(2 * time.Hour)
	r.ch.failWith(leaky(errs.Timeout))
	_, err = r.svc.SendOnce(ctxT(t))
	lc.note(err)
	r.advance(2 * time.Hour)
	r.ch.failWith(errs.E("test.ch", errs.Invalid, errors.Join(notify.ErrRejected, errors.New(mAddr+" "+mBody))))
	_, err = r.svc.SendOnce(ctxT(t))
	lc.note(err)
	r.ch.script(nil)

	// Retry, cancel, outbox, export, test message, ping, purge, expiry.
	v, err := r.svc.Outbox(ctx)
	lc.note(err)
	for _, rec := range v.Recent {
		lc.note(r.svc.Retry(ctx, rec.ID))
		break
	}
	r.advance(2 * time.Hour)
	_, err = r.svc.SendOnce(ctxT(t))
	lc.note(err)
	// A Channel's contract is to drop server reply text itself (the smtp channel does), so
	// Ping's error is injected without it; Send's leaky errors above prove the sender maps
	// whatever a channel says to a token.
	r.ch.pingErr = errs.E("test.ch", errs.Unavailable, errors.New("connection refused"))
	lc.note(r.svc.Ping(ctxT(t)))
	_, err = r.svc.ExportPerson(ctxT(t), p1)
	lc.note(err)
	_, err = r.svc.SendTest(as(ctxT(t), p1))
	lc.note(err)
	r.ret.set(notify.MinRetention)
	_, err = r.svc.Purge(ctxT(t))
	lc.note(err)
	r.advance(8 * 24 * time.Hour)
	_, err = r.svc.ExpireStale(ctxT(t))
	lc.note(err)
	_, err = r.svc.Purge(ctxT(t))
	lc.note(err)
	_, err = r.svc.DeletePerson(ctxT(t), p1)
	lc.note(err)
	_, err = r.svc.Preview(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: mSubj, Body: mBody})
	lc.note(err)
	lc.note(r.svc.ResetTemplate(ctx, "volunteers.greet", "en"))

	// The loop itself.
	r.addrs.set(p2, mustEmail(t, mAddr))
	wantNoErr(t, r.vol.Send(ctxT(t), toOne(m, p2)))
	rctx, cancel := context.WithCancel(ctxT(t))
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(rctx) }()
	r.drive("the loop to deliver", func() bool { return len(r.ch.envelopes()) > 0 })
	cancel()
	lc.note(<-done)

	// Read-only copy and a failing store.
	r.flt.set(true, nil)
	lc.note(r.vol.Send(ctxT(t), noTo(m)))
	_, err = r.svc.SendOnce(ctxT(t))
	lc.note(err)
	r.flt.set(false, errs.E("test.store", errs.Unavailable, errors.New("connection reset")))
	lc.note(r.vol.Send(ctxT(t), noTo(m)))
	_, err = r.svc.SendOnce(ctxT(t))
	lc.note(err)
	r.flt.set(false, nil)

	lc.verify()
}
