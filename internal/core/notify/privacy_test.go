package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const (
	mAddr  = "leakaddr-7f3a@example.org"
	mAddr2 = "leakalert-2b9e@example.org"
	mName  = "NAMEMARK-3c3c"
	mVar   = "VARMARK-1c2d"
	mTok   = "TOKMARK-88aa"
	mSubj  = "SUBJMARK-9b9b"
	mBody  = "BODYMARK-5e5e"
)

var allMarkers = []string{mAddr, mAddr2, mName, mVar, mTok, mSubj, mBody}

// leakCheck fails when any marker appears in the captured logs, spans or metrics, or in
// any error text collected from the calls under test.
type leakCheck struct {
	t    *testing.T
	r    *rig
	errs []error
}

func (l *leakCheck) note(err error) {
	if err != nil {
		l.errs = append(l.errs, err)
	}
}

func (l *leakCheck) verify() {
	l.t.Helper()
	for _, m := range allMarkers {
		if l.r.cap.Contains(m) {
			l.t.Errorf("telemetry carries %q: %s", m, l.where(m))
		}
		for _, err := range l.errs {
			if strings.Contains(err.Error(), m) {
				l.t.Errorf("error text carries %q: %v", m, err)
			}
		}
	}
	for _, e := range l.r.rec.Recorded() {
		for _, s := range append([]string{e.Reason, string(e.Action)}, e.Fields...) {
			for _, m := range allMarkers {
				if strings.Contains(s, m) {
					l.t.Errorf("audit entry carries %q", m)
				}
			}
		}
	}
	if v := l.r.cap.Violations(); len(v) != 0 {
		l.t.Errorf("telemetry policy violations: %v", v)
	}
}

func TestNoPersonalDataInTelemetry_Unit(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	lc := &leakCheck{t: t, r: r}
	p := r.person(mAddr)
	r.names.m[p] = mName
	bad := func(mod func(*notify.Message)) notify.Message {
		m := greet(p)
		m.Vars = map[string]string{"note": mVar, "shift": mVar}
		m.Link = "/reset?token=" + mTok
		mod(&m)
		return m
	}
	// Validation failures that hold the markers in every field.
	lc.note(r.vol.Send(ctxT(t), bad(func(m *notify.Message) { m.Link = "https://" + mTok })))
	lc.note(r.vol.Send(ctxT(t), bad(func(m *notify.Message) { m.Vars = map[string]string{"Bad" + mVar: mVar + "\x07"} })))
	lc.note(r.vol.Send(ctxT(t), bad(func(m *notify.Message) { m.Key = "bad key " + mVar })))
	lc.note(r.vol.Send(ctxT(t), bad(func(m *notify.Message) { m.Template = "people.deleted" })))
	lc.note(r.vol.Send(ctxT(t), bad(func(m *notify.Message) { m.Template = "volunteers.nosuch" })))
	lc.note(r.vol.Send(ctxT(t), bad(func(m *notify.Message) { m.Template = notify.Template("Bad " + mVar) })))
	lc.note(r.svc.SendTo(ctxT(t), model.Email("not an address "+mAddr), bad(func(m *notify.Message) { m.To = nil })))
	lc.note(r.svc.SendTo(ctxT(t), model.Email(mAddr2), bad(func(*notify.Message) {})))
	// Seam failures whose own error text holds personal data must not be copied into telemetry.
	r.langs.fail(errs.E("test.lang", errs.Unavailable, errors.New("lookup for "+mAddr+" failed")))
	// The seam's own error text is the seam's; only telemetry is held to the rule here.
	ignore(r.vol.Send(ctxT(t), bad(func(*notify.Message) {})))
	r.langs.fail(nil)
	// Read-only, not configured, rendered too long.
	d.SetReadOnly(true)
	lc.note(r.vol.Send(ctxT(t), bad(func(*notify.Message) {})))
	_, err := r.svc.SendOnce(ctxT(t))
	lc.note(err)
	d.SetReadOnly(false)
	lc.note(r.vol.Send(ctxT(t), notify.Message{Template: "volunteers.long", To: []model.PersonID{p},
		Vars: map[string]string{"note": strings.Repeat(mVar, 40)}}))
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	lc.note(r.vol.Send(ctxT(t), bad(func(*notify.Message) {})))
	_, err = r.vol.Configured(ctxT(t))
	lc.note(err)
	lc.note(r.svc.Ping(ctxT(t)))
	// Admin paths.
	admin := as(context.Background(), r.holder(permTemplates, permOutbox))
	_, err = r.svc.Preview(admin, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "b"})
	lc.note(err)
	_, err = r.svc.Outbox(admin)
	lc.note(err)
	lc.note(r.svc.Retry(admin, model.NewID[notify.MessageID]()))
	_, err = r.svc.ExportPerson(admin, p)
	lc.note(err)
	lc.verify()
}

// where names the first telemetry item holding m, for the failure message only.
func (l *leakCheck) where(m string) string {
	for _, rec := range l.r.cap.Records() {
		if strings.Contains(rec.Message, m) {
			return "log message " + rec.Message
		}
		for k, v := range rec.Attrs {
			if strings.Contains(v, m) {
				return "log " + rec.Message + " attr " + k
			}
		}
	}
	for _, s := range l.r.cap.Spans() {
		for k, v := range s.Attrs {
			if strings.Contains(v, m) {
				return "span " + s.Name + " attr " + k
			}
		}
	}
	for _, x := range l.r.cap.Measurements() {
		for k, v := range x.Attrs {
			if strings.Contains(v, m) {
				return "metric " + x.Instrument + " attr " + k
			}
		}
	}
	return "stdout"
}
