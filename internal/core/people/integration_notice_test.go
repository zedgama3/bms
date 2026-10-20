//go:build integration

package people_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// stubHolders is a people.Holders the test controls.
type stubHolders struct {
	who []model.PersonID
	err error
}

func (h *stubHolders) HoldersOf(context.Context, authz.Permission) ([]model.PersonID, error) {
	return h.who, h.err
}

// noticeRig is an integration rig with the notice wired.
func noticeRig(t *testing.T) (*irig, *notifytest.Fake, *stubHolders) {
	t.Helper()
	nf := notifytest.NewFake("people")
	h := &stubHolders{who: []model.PersonID{newPerson(), newPerson()}}
	r := newIRig(t, false, func(o *people.Options) { o.Notify, o.Holders = nf, h })
	return r, nf, h
}

// F63: the notice is recorded when the request commits.
func TestRequestDeletion_SendsNoticeToHolders(t *testing.T) {
	r, nf, h := noticeRig(t)
	me := r.insertPerson(seed{Playa: mPlaya})
	req, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
	wantNoErr(t, err)
	sent := nf.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	m := sent[0]
	if string(m.Template) != "people.deletion-requested" || m.Key != "deletion-requested:"+req.ID.String() ||
		m.Link != "/people/admin/requests" || len(m.Vars) != 0 {
		t.Errorf("message = %+v", m)
	}
	if fmt.Sprint(m.To) != fmt.Sprint(h.who) {
		t.Errorf("To = %v, want %v", m.To, h.who)
	}
	dump := fmt.Sprintf("%+v", m)
	for _, secret := range []string{mNote, mPlaya, me.String()} {
		if strings.Contains(dump, secret) {
			t.Errorf("the message carries %q", secret)
		}
	}
	for _, rec := range r.cap.Records() {
		for _, secret := range []string{mNote, mPlaya, me.String()} {
			if strings.Contains(rec.Message, secret) || strings.Contains(fmt.Sprint(rec.Attrs), secret) {
				t.Errorf("a log line carries %q", secret)
			}
		}
	}
}

// F62: a failed lookup or send rolls the whole request back.
func TestRequestDeletion_NotifyFails_RollsBack(t *testing.T) {
	down := errs.E("test.down", errs.Unavailable, errors.New("down"))
	slow := errs.E("test.slow", errs.Timeout, errors.New("slow"))
	tests := []struct {
		name string
		fail func(nf *notifytest.Fake, h *stubHolders)
		kind errs.Kind
	}{
		{"HoldersOf unavailable", func(_ *notifytest.Fake, h *stubHolders) { h.err = down }, errs.Unavailable},
		{"HoldersOf timeout", func(_ *notifytest.Fake, h *stubHolders) { h.err = slow }, errs.Timeout},
		{"Send unavailable", func(nf *notifytest.Fake, _ *stubHolders) { nf.Fail(down) }, errs.Unavailable},
		{"Send timeout", func(nf *notifytest.Fake, _ *stubHolders) { nf.Fail(slow) }, errs.Timeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, nf, h := noticeRig(t)
			me := r.insertPerson(seed{Playa: mPlaya})
			tc.fail(nf, h)
			_, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
			wantKind(t, err, tc.kind)
			if n := r.requestRows(me); n != 0 {
				t.Errorf("%d request rows after a failed notice, want 0", n)
			}
			if len(nf.Sent()) != 0 {
				t.Error("a notice was recorded for a rolled-back request")
			}
			// The failure leaves nothing half-done: the same person can ask again.
			nf.Fail(nil)
			h.err = nil
			_, err = r.svc.RequestDeletion(asPerson(ctxT(t), me), mNote)
			wantNoErr(t, err)
			if n := r.requestRows(me); n != 1 {
				t.Errorf("%d request rows after the retry, want 1", n)
			}
		})
	}
}

// F60 and F61 against a real database.
func TestRequestDeletion_NoEmailOrNoHolders_RequestStillCreated(t *testing.T) {
	tests := []struct {
		name string
		mod  func(nf *notifytest.Fake, h *stubHolders)
	}{
		{"unconfigured", func(nf *notifytest.Fake, _ *stubHolders) { nf.SetConfigured(false) }},
		{"nobody holds the permission", func(_ *notifytest.Fake, h *stubHolders) { h.who = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, nf, h := noticeRig(t)
			me := r.insertPerson(seed{Playa: mPlaya})
			tc.mod(nf, h)
			_, err := r.svc.RequestDeletion(asPerson(ctxT(t), me), "")
			wantNoErr(t, err)
			if n := r.requestRows(me); n != 1 {
				t.Errorf("%d request rows, want 1", n)
			}
			if len(nf.Sent()) != 0 {
				t.Errorf("sent %d messages, want none", len(nf.Sent()))
			}
		})
	}
}

// F64 against a real database: the duplicate is refused before anyone is told.
func TestRequestDeletion_Duplicate_NothingSent(t *testing.T) {
	r, nf, _ := noticeRig(t)
	me := r.insertPerson(seed{Playa: mPlaya})
	ctx := asPerson(ctxT(t), me)
	_, err := r.svc.RequestDeletion(ctx, "")
	wantNoErr(t, err)
	_, err = r.svc.RequestDeletion(ctx, "again")
	wantCode(t, err, errs.Conflict, "duplicate")
	if n := len(nf.Sent()); n != 1 {
		t.Errorf("sent %d messages for two attempts, want 1", n)
	}
}

// F65 against a real database.
func TestLanguageOf_Table(t *testing.T) {
	r := newIRig(t, false)
	es := r.insertPerson(seed{Playa: mPlaya})
	r.exec("UPDATE core.person SET language = 'es-MX' WHERE id = $1", [16]byte(es))
	none := r.insertPerson(seed{Playa: mPlaya})
	tests := []struct {
		name    string
		p       model.PersonID
		want    string
		wantErr errs.Kind
	}{
		{"saved", es, "es-MX", 0},
		{"none saved", none, "", 0},
		{"zero person", model.PersonID{}, "", errs.Invalid},
		{"no row", newPerson(), "", errs.NotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.svc.LanguageOf(ctxT(t), tc.p) // no actor in ctx: a system read
			if tc.wantErr != 0 {
				wantKind(t, err, tc.wantErr)
				return
			}
			wantNoErr(t, err)
			if got != tc.want {
				t.Errorf("LanguageOf = %q, want %q", got, tc.want)
			}
		})
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("a system read recorded %d audit entries", n)
	}
}

// F65 against a real database.
func TestContactEmailOf_Table(t *testing.T) {
	r := newIRig(t, false)
	has := r.insertPerson(seed{ContactEmail: mContactEmail})
	without := r.insertPerson(seed{Playa: mPlaya})
	tests := []struct {
		name    string
		p       model.PersonID
		want    model.Email
		wantErr errs.Kind
	}{
		{"has address", has, mContactEmail, 0},
		{"no address", without, "", errs.NotFound},
		{"no row", newPerson(), "", errs.NotFound},
		{"zero person", model.PersonID{}, "", errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.svc.ContactEmailOf(ctxT(t), tc.p)
			if tc.wantErr != 0 {
				wantKind(t, err, tc.wantErr)
				if got != "" {
					t.Error("an address came back with an error")
				}
				return
			}
			wantNoErr(t, err)
			if got != tc.want {
				t.Errorf("ContactEmailOf = %q, want %q", got, tc.want)
			}
		})
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("a system read recorded %d audit entries", n)
	}
	r.noLeak(mContactEmail) // the address is in no log, span or metric
}
