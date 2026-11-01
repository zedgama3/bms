package people

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Every person, note and address here is invented.
const (
	noticeNote  = "notice-note-marker-5523"
	noticeEmail = "marker.notice@example.org"
)

// requestStore is a fakeStore that also files requests.
type requestStore struct {
	fakeStore
	inserted  []RequestID
	insertErr error
}

func (f *requestStore) LockPerson(context.Context, model.PersonID) error { return nil }

func (f *requestStore) InsertRequest(_ context.Context, id RequestID, person model.PersonID, note string, at time.Time) (rows.Request, error) {
	if f.insertErr != nil {
		return rows.Request{}, f.insertErr
	}
	f.inserted = append(f.inserted, id)
	return rows.Request{ID: id, Person: person, Kind: "delete", Status: "open", Note: note, CreatedAt: at}, nil
}

// fakeHolders is a Holders the test controls.
type fakeHolders struct {
	people []model.PersonID
	err    error
	asked  []authz.Permission
}

func (f *fakeHolders) HoldersOf(_ context.Context, p authz.Permission) ([]model.PersonID, error) {
	f.asked = append(f.asked, p)
	return f.people, f.err
}

type noticeRig struct {
	svc     *Service
	st      *requestStore
	notify  *notifytest.Fake
	holders *fakeHolders
	cap     *telemetrytest.Capture
	me      model.PersonID
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
	st := &requestStore{fakeStore: fakeStore{person: rows.Person{ID: me, PlayaName: "Dusty Ember"}}}
	nf := notifytest.NewFake("people")
	h := &fakeHolders{people: []model.PersonID{model.NewID[model.PersonID](), model.NewID[model.PersonID]()}}
	o := Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("people"), Audit: audittest.NewFake("people"), Signals: bus, Authz: az,
		Actor: actor, Session: identity.SessionFrom, Accounts: &wbAccounts{has: true}, Access: wbAccess{}, AuditCount: wbCounter{},
		Settings: settingstest.NewFake("people", Settings), Notify: nf, Holders: h,
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
	return &noticeRig{svc: svc, st: st, notify: nf, holders: h, cap: c, me: me}
}

// infoLines returns the info-level log records whose message starts with "deletion request".
func (r *noticeRig) infoLines() []string {
	var out []string
	for _, rec := range r.cap.Records() {
		if rec.Level == slog.LevelInfo && strings.HasPrefix(rec.Message, "deletion request") {
			out = append(out, rec.Message)
		}
	}
	return out
}

// F60
func TestRequestDeletion_NoEmail_RequestStillCreated(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*Options)
	}{
		{"Notify nil", func(o *Options) { o.Notify = nil }},
		{"Holders nil", func(o *Options) { o.Holders = nil }},
		{"both nil", func(o *Options) { o.Notify, o.Holders = nil, nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newNoticeRig(t, tc.mod)
			req, err := r.svc.RequestDeletion(t.Context(), noticeNote)
			if err != nil {
				t.Fatalf("RequestDeletion: %v", err)
			}
			if req.ID.IsZero() || len(r.st.inserted) != 1 {
				t.Fatalf("request = %+v, inserted %d", req, len(r.st.inserted))
			}
			if got := r.notify.Sent(); len(got) != 0 {
				t.Errorf("sent %d messages without a notifier wired", len(got))
			}
			if n := len(r.infoLines()); n != 1 {
				t.Errorf("%d info lines, want 1", n)
			}
		})
	}
	t.Run("Send answers Unsupported", func(t *testing.T) {
		r := newNoticeRig(t)
		r.notify.SetConfigured(false) // Send fails Unsupported wrapping notify.ErrNotConfigured
		_, err := r.svc.RequestDeletion(t.Context(), noticeNote)
		if err != nil {
			t.Fatalf("RequestDeletion with e-mail unconfigured: %v", err)
		}
		if len(r.st.inserted) != 1 || len(r.notify.Sent()) != 0 {
			t.Errorf("inserted %d, sent %d; want 1 and 0", len(r.st.inserted), len(r.notify.Sent()))
		}
		if n := len(r.infoLines()); n != 1 {
			t.Errorf("%d info lines, want 1", n)
		}
	})
}

// F61
func TestRequestDeletion_NoHolders_NothingSent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		people []model.PersonID
	}{{"nil", nil}, {"empty", []model.PersonID{}}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNoticeRig(t)
			r.holders.people = tc.people
			if _, err := r.svc.RequestDeletion(t.Context(), noticeNote); err != nil {
				t.Fatalf("RequestDeletion: %v", err)
			}
			if len(r.st.inserted) != 1 {
				t.Errorf("inserted %d requests, want 1", len(r.st.inserted))
			}
			if got := r.notify.Sent(); len(got) != 0 {
				t.Errorf("sent %d messages to nobody", len(got))
			}
			lines := r.infoLines()
			if len(lines) != 1 {
				t.Fatalf("%d info lines, want 1", len(lines))
			}
		})
	}
}

// F62
func TestRequestDeletion_NotifyFails_ReturnsKind(t *testing.T) {
	tests := []struct {
		name string
		mod  func(r *noticeRig)
		kind errs.Kind
	}{
		{"HoldersOf unavailable", func(r *noticeRig) { r.holders.err = errs.E("test", errs.Unavailable, errors.New("down")) }, errs.Unavailable},
		{"HoldersOf timeout", func(r *noticeRig) { r.holders.err = errs.E("test", errs.Timeout, errors.New("slow")) }, errs.Timeout},
		{"Send unavailable", func(r *noticeRig) { r.notify.Fail(errs.E("test", errs.Unavailable, errors.New("down"))) }, errs.Unavailable},
		{"Send internal", func(r *noticeRig) { r.notify.Fail(errs.E("test", errs.Internal, errors.New("bug"))) }, errs.Internal},
		{"Send invalid", func(r *noticeRig) { r.notify.Fail(errs.E("test", errs.Invalid, errors.New("bad"))) }, errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newNoticeRig(t)
			tc.mod(r)
			_, err := r.svc.RequestDeletion(t.Context(), noticeNote)
			if !errs.Is(err, tc.kind) {
				t.Fatalf("RequestDeletion = %v, want kind %v", err, tc.kind)
			}
			if len(r.notify.Sent()) != 0 {
				t.Error("a notice was recorded for a failed request")
			}
		})
	}
}

// F63
func TestRequestDeletion_SendsNoticeToHolders(t *testing.T) {
	r := newNoticeRig(t)
	req, err := r.svc.RequestDeletion(t.Context(), noticeNote)
	if err != nil {
		t.Fatalf("RequestDeletion: %v", err)
	}
	if !slices.Equal(r.holders.asked, []authz.Permission{PermissionDataDelete}) {
		t.Errorf("HoldersOf asked about %v, want only %s", r.holders.asked, PermissionDataDelete)
	}
	sent := r.notify.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	m := sent[0]
	if m.Template != "people.deletion-requested" || m.Key != "deletion-requested:"+req.ID.String() ||
		m.Link != "/people/admin/requests" || len(m.Vars) != 0 || m.TTL != 0 {
		t.Errorf("message = %+v", m)
	}
	if !slices.Equal(m.To, r.holders.people) {
		t.Errorf("To = %v, want %v", m.To, r.holders.people)
	}
	// The notice names nobody and carries no note.
	dump := fmt.Sprintf("%+v", m)
	for _, secret := range []string{noticeNote, r.me.String(), "Dusty Ember"} {
		if strings.Contains(dump, secret) {
			t.Errorf("the message carries %q", secret)
		}
	}
}

func TestRequestDeletion_ManyHolders_SendsInBatches(t *testing.T) {
	r := newNoticeRig(t)
	r.holders.people = nil
	for range notify.MaxRecipients + 5 {
		r.holders.people = append(r.holders.people, model.NewID[model.PersonID]())
	}
	if _, err := r.svc.RequestDeletion(t.Context(), ""); err != nil {
		t.Fatalf("RequestDeletion: %v", err)
	}
	sent := r.notify.Sent()
	if len(sent) != 2 || len(sent[0].To) != notify.MaxRecipients || len(sent[1].To) != 5 || sent[0].Key != sent[1].Key {
		t.Fatalf("sent %d batches: %d and %d recipients", len(sent), len(sent[0].To), len(sent[len(sent)-1].To))
	}
}

// F64
func TestRequestDeletion_Duplicate_NothingSent(t *testing.T) {
	r := newNoticeRig(t)
	r.st.insertErr = errs.E("test", errs.Conflict, errors.New("open request"))
	_, err := r.svc.RequestDeletion(t.Context(), "")
	if !errs.Is(err, errs.Conflict) {
		t.Fatalf("RequestDeletion = %v, want Conflict", err)
	}
	if len(r.notify.Sent()) != 0 || len(r.holders.asked) != 0 {
		t.Errorf("a duplicate looked up holders (%d) or sent (%d)", len(r.holders.asked), len(r.notify.Sent()))
	}
}

// The logs carry counts and words, never an address, a note or a requester.
func TestRequestDeletion_Notice_LogsNothingPersonal(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*noticeRig)
	}{
		{"sent", func(*noticeRig) {}},
		{"nobody", func(r *noticeRig) { r.holders.people = nil }},
		{"unconfigured", func(r *noticeRig) { r.notify.SetConfigured(false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNoticeRig(t)
			tc.mod(r)
			if _, err := r.svc.RequestDeletion(t.Context(), noticeNote); err != nil {
				t.Fatalf("RequestDeletion: %v", err)
			}
			var logs strings.Builder
			for _, rec := range r.cap.Records() {
				fmt.Fprintf(&logs, "%s %v\n", rec.Message, rec.Attrs)
			}
			for _, secret := range []string{noticeNote, r.me.String(), noticeEmail} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("a log line carries %q", secret)
				}
			}
		})
	}
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

// seqNotifier is a Notifier whose Send answers from a script, one entry per call.
type seqNotifier struct {
	configured bool
	results    []error
	calls      int
}

func (n *seqNotifier) Configured(context.Context) (bool, error) { return n.configured, nil }

func (n *seqNotifier) Send(context.Context, notify.Message) error {
	i := n.calls
	n.calls++
	if i < len(n.results) {
		return n.results[i]
	}
	return nil
}

func manyHolders(n int) []model.PersonID {
	out := make([]model.PersonID, n)
	for i := range out {
		out[i] = model.NewID[model.PersonID]()
	}
	return out
}

func unsupported() error {
	return errs.E("test.Send", errs.Unsupported, notify.ErrNotConfigured)
}

// F60: e-mail is probed once, before any recipient is looked up.
func TestRequestDeletion_Unconfigured_SkipsHolderLookup(t *testing.T) {
	r := newNoticeRig(t)
	r.notify.SetConfigured(false)
	if _, err := r.svc.RequestDeletion(t.Context(), ""); err != nil {
		t.Fatalf("RequestDeletion: %v", err)
	}
	if len(r.holders.asked) != 0 {
		t.Errorf("holders looked up %d times with e-mail off", len(r.holders.asked))
	}
}

// F60: e-mail switched off between the probe and the first Send is still "no e-mail".
func TestRequestDeletion_FirstSendUnsupported_RequestStillCreated(t *testing.T) {
	n := &seqNotifier{configured: true, results: []error{unsupported()}}
	r := newNoticeRig(t, func(o *Options) { o.Notify = n })
	if _, err := r.svc.RequestDeletion(t.Context(), ""); err != nil {
		t.Fatalf("RequestDeletion: %v", err)
	}
	if len(r.st.inserted) != 1 || n.calls != 1 {
		t.Errorf("inserted %d, Send calls %d; want 1 and 1", len(r.st.inserted), n.calls)
	}
}

// F62: Unsupported after batch 1 must not leave only some holders told.
func TestRequestDeletion_UnsupportedAfterFirstBatch_ReturnsUnavailable(t *testing.T) {
	n := &seqNotifier{configured: true, results: []error{nil, unsupported()}}
	r := newNoticeRig(t, func(o *Options) { o.Notify = n })
	r.holders.people = manyHolders(notify.MaxRecipients + 1)
	_, err := r.svc.RequestDeletion(t.Context(), "")
	if !errs.Is(err, errs.Unavailable) {
		t.Fatalf("RequestDeletion = %v, want Unavailable", err)
	}
	if n.calls != 2 {
		t.Errorf("Send calls = %d, want 2", n.calls)
	}
}
