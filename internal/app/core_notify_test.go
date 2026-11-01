package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

func TestLazyAlerter_NotifyNotBuilt_ReturnsUnsupported(t *testing.T) {
	err := lazyAlerter{a: &assembly{}}.SuperuserSignedIn(t.Context(), model.NewID[model.PersonID]())
	if !errs.Is(err, errs.Unsupported) {
		t.Errorf("err = %v, want Unsupported", err)
	}
}

// ---- the alert ----

type fakeHolders struct {
	ids []model.PersonID
	err error
}

func (f fakeHolders) HoldersOf(context.Context, authz.Permission) ([]model.PersonID, error) {
	return f.ids, f.err
}

// fakeSender records the explicit addresses SendTo was given and the messages.
type fakeSender struct {
	to   []model.Email
	msgs []notify.Message
	err  error
}

func (f *fakeSender) SendTo(_ context.Context, to model.Email, m notify.Message) error {
	if f.err != nil {
		return f.err
	}
	f.to = append(f.to, to)
	f.msgs = append(f.msgs, m)
	return nil
}

func addrs(list ...model.Email) func(context.Context) ([]model.Email, error) {
	return func(context.Context) ([]model.Email, error) { return list, nil }
}

func TestAlert_HoldersAndAddresses_SendsToBothWithAnEmptyKey(t *testing.T) {
	holder := model.NewID[model.PersonID]()
	nf := notifytest.NewFake("identity")
	sender := &fakeSender{}
	err := alertSeams{
		holders: fakeHolders{ids: []model.PersonID{holder}}, notifier: nf, sender: sender,
		addresses: addrs("desk@example.org", "night@example.org"),
	}.send(t.Context())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	sent := nf.Sent()
	if len(sent) != 1 || len(sent[0].To) != 1 || sent[0].To[0] != holder || sent[0].Template != templateSuperuserSignedIn ||
		sent[0].Link != "/audit?superuser=1" || sent[0].Key != "" {
		t.Errorf("holders' message = %+v, want one to the holder, the alert template, the audit link and no key", sent)
	}
	if len(sender.to) != 2 || sender.msgs[0].Key != "" || sender.msgs[0].Link != "/audit?superuser=1" || len(sender.msgs[0].To) != 0 {
		t.Errorf("address messages = %+v to %v, want two without key or recipients list", sender.msgs, sender.to)
	}
}

func TestAlert_TwoSignIns_EachSendsAnAlert(t *testing.T) {
	holder := model.NewID[model.PersonID]()
	nf := notifytest.NewFake("identity")
	s := alertSeams{holders: fakeHolders{ids: []model.PersonID{holder}}, notifier: nf, sender: &fakeSender{}, addresses: addrs()}
	for range 2 {
		if err := s.send(t.Context()); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	sent := nf.Sent()
	if len(sent) != 2 || sent[0].Key != "" || sent[1].Key != "" {
		t.Errorf("sent = %+v, want two messages with no key (a key would drop the second)", sent)
	}
}

func TestAlert_PartialFailure_SendsTheRestAndJoinsTheErrors(t *testing.T) {
	holder := model.NewID[model.PersonID]()
	nf := notifytest.NewFake("identity")
	nf.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	sender := &fakeSender{}
	err := alertSeams{
		holders: fakeHolders{ids: []model.PersonID{holder}}, notifier: nf, sender: sender, addresses: addrs("desk@example.org"),
	}.send(t.Context())
	if !errs.Is(err, errs.Unavailable) {
		t.Errorf("err = %v, want Unavailable", err)
	}
	if len(sender.to) != 1 {
		t.Errorf("address sends = %d, want 1 (the holders' failure must not stop the addresses)", len(sender.to))
	}

	sender = &fakeSender{err: errs.E("test", errs.Unavailable, errors.New("down"))}
	nf = notifytest.NewFake("identity")
	err = alertSeams{
		holders: fakeHolders{ids: []model.PersonID{holder}}, notifier: nf, sender: sender, addresses: addrs("a@example.org", "b@example.org"),
	}.send(t.Context())
	if !errs.Is(err, errs.Unavailable) || len(nf.Sent()) != 1 {
		t.Errorf("err = %v, holders sent = %d, want Unavailable with the holders still told", err, len(nf.Sent()))
	}
}

func TestAlert_HoldersLookupFails_StillTellsTheAddresses(t *testing.T) {
	sender := &fakeSender{}
	err := alertSeams{
		holders: fakeHolders{err: errs.E("test", errs.Unavailable, errors.New("down"))}, notifier: notifytest.NewFake("identity"),
		sender: sender, addresses: addrs("desk@example.org"),
	}.send(t.Context())
	if !errs.Is(err, errs.Unavailable) || len(sender.to) != 1 {
		t.Errorf("err = %v, address sends = %d, want Unavailable with the address told", err, len(sender.to))
	}
}

func TestAlert_AddressesReadFails_StillTellsTheHolders(t *testing.T) {
	nf := notifytest.NewFake("identity")
	err := alertSeams{
		holders: fakeHolders{ids: []model.PersonID{model.NewID[model.PersonID]()}}, notifier: nf, sender: &fakeSender{},
		addresses: func(context.Context) ([]model.Email, error) {
			return nil, errs.E("test", errs.Timeout, errors.New("slow"))
		},
	}.send(t.Context())
	if !errs.Is(err, errs.Timeout) || len(nf.Sent()) != 1 {
		t.Errorf("err = %v, holders sent = %d, want Timeout with the holders told", err, len(nf.Sent()))
	}
}

func TestAlert_NotConfigured_ReturnsUnsupported(t *testing.T) {
	nf := notifytest.NewFake("identity")
	nf.SetConfigured(false)
	sender := &fakeSender{err: errs.E("test", errs.Unsupported, notify.ErrNotConfigured)}
	err := alertSeams{
		holders: fakeHolders{ids: []model.PersonID{model.NewID[model.PersonID]()}}, notifier: nf, sender: sender, addresses: addrs("desk@example.org"),
	}.send(t.Context())
	if !errs.Is(err, errs.Unsupported) || !errors.Is(err, notify.ErrNotConfigured) {
		t.Errorf("err = %v, want Unsupported wrapping ErrNotConfigured", err)
	}
}

func TestAlert_ReadOnlyCopy_ReturnsUnavailableWrappingReadOnly(t *testing.T) {
	ro := errs.E("test", errs.Unavailable, db.ErrReadOnly)
	nf := notifytest.NewFake("identity")
	nf.Fail(ro)
	err := alertSeams{
		holders: fakeHolders{ids: []model.PersonID{model.NewID[model.PersonID]()}}, notifier: nf, sender: &fakeSender{err: ro}, addresses: addrs("desk@example.org"),
	}.send(t.Context())
	if !errs.Is(err, errs.Unavailable) || !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("err = %v, want Unavailable wrapping db.ErrReadOnly", err)
	}
}

func TestAlert_NobodyToTell_ReturnsUnsupported(t *testing.T) {
	nf := notifytest.NewFake("identity")
	sender := &fakeSender{}
	err := alertSeams{holders: fakeHolders{}, notifier: nf, sender: sender, addresses: addrs()}.send(t.Context())
	if !errs.Is(err, errs.Unsupported) {
		t.Errorf("err = %v, want Unsupported", err)
	}
	if len(nf.Sent()) != 0 || len(sender.to) != 0 {
		t.Error("something was sent to nobody")
	}
}

// ---- the outbox in export and deletion ----

type fakeOutbox struct {
	exported, deleted []model.PersonID
	n                 int
	err               error
}

func (f *fakeOutbox) ExportPerson(_ context.Context, p model.PersonID) (json.RawMessage, error) {
	f.exported = append(f.exported, p)
	return json.RawMessage(`{"messages":[]}`), f.err
}

func (f *fakeOutbox) DeletePerson(_ context.Context, p model.PersonID) (int, error) {
	f.deleted = append(f.deleted, p)
	return f.n, f.err
}

func TestNotifySource_ExportAndDelete_ReachTheOutbox(t *testing.T) {
	person := model.NewID[model.PersonID]()
	ob := &fakeOutbox{n: 3}
	src := notifySource(ob)
	if src.Name != "notify" {
		t.Errorf("source name = %q, want notify", src.Name)
	}
	exp, err := src.Exporter.ExportPerson(t.Context(), person)
	if err != nil || string(exp.Data) != `{"messages":[]}` || len(ob.exported) != 1 || ob.exported[0] != person {
		t.Errorf("ExportPerson = %s, %v", exp.Data, err)
	}
	del, err := src.Exporter.DeletePerson(t.Context(), person)
	if err != nil || del.Deleted != 3 || del.Anonymized != 0 || len(del.Kept) != 0 || len(ob.deleted) != 1 {
		t.Errorf("DeletePerson = %+v, %v, want 3 deleted", del, err)
	}
}

func TestNotifySource_OutboxFails_KeepsTheKind(t *testing.T) {
	ob := &fakeOutbox{err: errs.E("test", errs.Unavailable, errors.New("down"))}
	src := notifySource(ob)
	person := model.NewID[model.PersonID]()
	if _, err := src.Exporter.ExportPerson(t.Context(), person); !errs.Is(err, errs.Unavailable) {
		t.Errorf("export err = %v, want Unavailable", err)
	}
	if _, err := src.Exporter.DeletePerson(t.Context(), person); !errs.Is(err, errs.Unavailable) {
		t.Errorf("delete err = %v, want Unavailable", err)
	}
}

// ---- the runnable ----

// loopFake is a senderLoop whose Run blocks until Close or its ctx ends.
type loopFake struct {
	started  chan struct{}
	closed   chan struct{}
	closeErr error
	hang     bool // Run ignores Close and its ctx until released
	release  chan struct{}
}

func newLoopFake() *loopFake {
	return &loopFake{started: make(chan struct{}), closed: make(chan struct{}), release: make(chan struct{})}
}

func (f *loopFake) Run(ctx context.Context) error {
	close(f.started)
	if f.hang {
		<-f.release
		return nil
	}
	select {
	case <-f.closed:
	case <-ctx.Done():
	}
	return nil
}

func (f *loopFake) Close(context.Context) error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return f.closeErr
}

func newRunner(f senderLoop) *notifyRunner {
	return &notifyRunner{svc: f, logger: slog.New(slog.DiscardHandler)}
}

func TestNotifyRunner_StartThenStop_RunEndsAndStopReturnsNil(t *testing.T) {
	f := newLoopFake()
	r := newRunner(f)
	if err := r.start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-f.started
	if err := r.stop(t.Context()); err != nil {
		t.Errorf("stop: %v", err)
	}
	select {
	case <-r.done:
	default:
		t.Error("stop returned before Run's goroutine ended")
	}
}

func TestNotifyRunner_StopWithoutStart_ReturnsNil(t *testing.T) {
	if err := newRunner(newLoopFake()).stop(t.Context()); err != nil {
		t.Errorf("stop: %v", err)
	}
}

func TestNotifyRunner_CloseFails_StopReturnsItsKind(t *testing.T) {
	f := newLoopFake()
	f.closeErr = errs.E("test", errs.Timeout, errors.New("slow"))
	r := newRunner(f)
	if err := r.start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := r.stop(t.Context()); !errs.Is(err, errs.Timeout) {
		t.Errorf("stop err = %v, want Timeout", err)
	}
}

func TestNotifyRunner_RunDoesNotEnd_StopFailsWhenCtxEnds(t *testing.T) {
	f := newLoopFake()
	f.hang = true
	r := newRunner(f)
	if err := r.start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-f.started
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := r.stop(ctx); !errs.Is(err, errs.Timeout) {
		t.Errorf("stop err = %v, want Timeout", err)
	}
	close(f.release)
	<-r.done
}

// ---- bms check's e-mail lines ----

func emailLines(t *testing.T, st emailState) (string, int) {
	t.Helper()
	var out bytes.Buffer
	r := &checkReport{out: &out}
	r.printEmail(st)
	return out.String(), r.problems
}

func TestPrintEmail_Table(t *testing.T) {
	configured := notify.SMTPConfig{
		Host: "mail.example.org", Port: 587, Security: notify.StartTLS, Username: "outbox-user",
		Password: secret.New("pw-for-the-test"), Sender: notify.Address{Email: "bms@example.org"},
		AlertAddresses: []model.Email{"desk@example.org"},
	}
	none := configured
	none.Security = notify.None
	counts := map[notify.Status]int{notify.Queued: 2, notify.StatusSent: 5, notify.Failed: 1}
	for _, tc := range []struct {
		name string
		st   emailState
		want []string
	}{
		{"not configured", emailState{cfg: notify.SMTPConfig{Port: 587, Security: notify.StartTLS}, counts: counts},
			[]string{"e-mail: not configured\n", "outbox: queued 2, sending 0, sent 5, failed 1\n"}},
		{"configured and reachable", emailState{cfg: configured, counts: counts, pinged: true},
			[]string{"e-mail: configured mail.example.org:587 (starttls)\n", "smtp: ok\n"}},
		{"security none warns", emailState{cfg: none, counts: counts, pinged: true},
			[]string{"e-mail: configured mail.example.org:587 (none)\n", "warning: security is none", "smtp: ok\n"}},
		{"the server is unreachable", emailState{cfg: configured, counts: counts, pinged: true, pingErr: errs.E("test", errs.Unavailable, errors.New("refused"))},
			[]string{"smtp: unavailable\n"}},
		{"settings cannot be read", emailState{cfgErr: errs.E("test", errs.Unavailable, errors.New("no schema"))},
			[]string{"e-mail: unknown: unavailable: "}},
		{"the outbox cannot be counted", emailState{cfg: configured, countErr: errs.E("test", errs.Unavailable, errors.New("down"))},
			[]string{"outbox: unavailable: unavailable\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, problems := emailLines(t, tc.st)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			if problems != 0 {
				t.Errorf("problems = %d, want 0 (e-mail never fails the check)", problems)
			}
			for _, leak := range []string{"pw-for-the-test", "outbox-user", "bms@example.org", "desk@example.org"} {
				if strings.Contains(got, leak) {
					t.Errorf("output carries %q", leak)
				}
			}
		})
	}
}

func TestEmail_NoDatabase_IsSkipped(t *testing.T) {
	var out bytes.Buffer
	r := &checkReport{out: &out}
	r.email(t.Context(), &assembly{})
	if out.String() != "e-mail: skipped (no database)\n" || r.problems != 0 {
		t.Errorf("output = %q, problems = %d", out.String(), r.problems)
	}
}
