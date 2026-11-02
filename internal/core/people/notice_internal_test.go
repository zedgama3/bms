package people

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Every person and address here is invented. The deletion-request notice tests moved to
// datarights with the notice; the system reads of the Service stay.
const noticeEmail = "marker.notice@example.org"

type noticeRig struct {
	svc *Service
	st  *fakeStore
	cap *telemetrytest.Capture
	me  model.PersonID
}

func newNoticeRig(t *testing.T, mods ...func(*Options)) *noticeRig {
	t.Helper()
	me := model.NewID[model.PersonID]()
	c := telemetrytest.New(t)
	bus, err := signalstest.NewFake("people", PublishedSignals, SubscribedSignals)
	if err != nil {
		t.Fatalf("signalstest.NewFake: %v", err)
	}
	actor := func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: me}, true }
	az := authztest.NewFake()
	az.SetActor(actor)
	st := &fakeStore{person: rows.Person{ID: me, PlayaName: "Dusty Ember"}}
	o := Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("people"), Audit: audittest.NewFake("people"), Signals: bus, Authz: az,
		Actor: actor, Session: identity.SessionFrom, Accounts: &wbAccounts{has: true},
		Settings: settingstest.NewFake("people", Settings),
	}
	lk, err := newLookup(LookupOptions{DB: dbtest.NewFake(), Tracer: c.Tracer("people")}, st)
	if err != nil {
		t.Fatalf("newLookup: %v", err)
	}
	o.Lookup = lk
	for _, m := range mods {
		m(&o)
	}
	svc, err := newService(o, st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return &noticeRig{svc: svc, st: st, cap: c, me: me}
}

// F65
func TestLanguageOf_Table(t *testing.T) {
	notFound := errs.E("test", errs.NotFound, errors.New("no row"))
	down := errs.E("test", errs.Unavailable, errors.New("down"))
	tests := []struct {
		name    string
		person  model.PersonID
		row     rows.Person
		rowErr  error
		want    string
		wantErr errs.Kind
	}{
		{"saved", model.NewID[model.PersonID](), rows.Person{Language: "es-MX"}, nil, "es-MX", 0},
		{"none saved", model.NewID[model.PersonID](), rows.Person{}, nil, "", 0},
		{"zero person", model.PersonID{}, rows.Person{}, nil, "", errs.Invalid},
		{"no row", model.NewID[model.PersonID](), rows.Person{}, notFound, "", errs.NotFound},
		{"store down", model.NewID[model.PersonID](), rows.Person{}, down, "", errs.Unavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newNoticeRig(t)
			r.st.person, r.st.personErr = tc.row, tc.rowErr
			got, err := r.svc.LanguageOf(t.Context(), tc.person)
			if tc.wantErr != 0 {
				if !errs.Is(err, tc.wantErr) {
					t.Fatalf("LanguageOf = %q, %v; want kind %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("LanguageOf = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// F65
func TestContactEmailOf_Table(t *testing.T) {
	notFound := errs.E("test", errs.NotFound, errors.New("no row"))
	down := errs.E("test", errs.Unavailable, errors.New("down"))
	tests := []struct {
		name    string
		person  model.PersonID
		row     rows.Person
		rowErr  error
		want    model.Email
		wantErr errs.Kind
	}{
		{"has address", model.NewID[model.PersonID](), rows.Person{ContactEmail: noticeEmail}, nil, noticeEmail, 0},
		{"no address", model.NewID[model.PersonID](), rows.Person{}, nil, "", errs.NotFound},
		{"no row", model.NewID[model.PersonID](), rows.Person{}, notFound, "", errs.NotFound},
		{"zero person", model.PersonID{}, rows.Person{ContactEmail: noticeEmail}, nil, "", errs.Invalid},
		{"store down", model.NewID[model.PersonID](), rows.Person{}, down, "", errs.Unavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newNoticeRig(t)
			r.st.person, r.st.personErr = tc.row, tc.rowErr
			got, err := r.svc.ContactEmailOf(t.Context(), tc.person)
			if tc.wantErr != 0 {
				if !errs.Is(err, tc.wantErr) {
					t.Fatalf("ContactEmailOf = %q, %v; want kind %v", got, err, tc.wantErr)
				}
				if got != "" {
					t.Errorf("an address came back with an error")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ContactEmailOf = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// The system reads need no actor and no permission and record no audit entry.
func TestLanguageOf_And_ContactEmailOf_NeedNoActor(t *testing.T) {
	r := newNoticeRig(t, func(o *Options) {
		o.Actor = func(context.Context) (authz.Actor, bool) { return authz.Actor{}, false }
	})
	r.st.person = rows.Person{Language: "en", ContactEmail: noticeEmail}
	p := model.NewID[model.PersonID]()
	if _, err := r.svc.LanguageOf(t.Context(), p); err != nil {
		t.Errorf("LanguageOf without an actor: %v", err)
	}
	if _, err := r.svc.ContactEmailOf(t.Context(), p); err != nil {
		t.Errorf("ContactEmailOf without an actor: %v", err)
	}
}
