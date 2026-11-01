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

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

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
