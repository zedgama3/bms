package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
)

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

func emailLines(t *testing.T, st notify.EmailState, err error) (string, int) {
	t.Helper()
	var out bytes.Buffer
	r := &checkReport{out: &out}
	r.printEmail(st, err)
	return out.String(), r.problems
}

func TestPrintEmail_Table(t *testing.T) {
	// The state holds what Probe may return: a host, a port, a security and counts; the
	// leak list below proves the printer adds nothing else.
	configured := notify.EmailState{Known: true, Configured: true, Host: "mail.example.org", Port: 587, Security: notify.StartTLS}
	none := configured
	none.Security = notify.None
	counts := map[notify.Status]int{notify.Queued: 2, notify.StatusSent: 5, notify.Failed: 1}
	with := func(st notify.EmailState, f func(*notify.EmailState)) notify.EmailState {
		f(&st)
		return st
	}
	for _, tc := range []struct {
		name string
		st   notify.EmailState
		err  error
		want []string
	}{
		{"not configured", notify.EmailState{Known: true, Port: 587, Security: notify.StartTLS, Outbox: counts}, nil,
			[]string{"e-mail: not configured\n", "outbox: queued 2, sending 0, sent 5, failed 1\n"}},
		{"configured and reachable", with(configured, func(s *notify.EmailState) { s.Outbox, s.Pinged = counts, true }), nil,
			[]string{"e-mail: configured mail.example.org:587 (starttls)\n", "smtp: ok\n"}},
		{"security none warns", with(none, func(s *notify.EmailState) { s.Outbox, s.Pinged = counts, true }), nil,
			[]string{"e-mail: configured mail.example.org:587 (none)\n", "warning: security is none", "smtp: ok\n"}},
		{"the server is unreachable", with(configured, func(s *notify.EmailState) {
			s.Outbox, s.Pinged, s.PingErr = counts, true, errs.E("test", errs.Unavailable, errors.New("refused"))
		}), nil, []string{"smtp: unavailable\n"}},
		{"settings cannot be read", notify.EmailState{}, errs.E("test", errs.Unavailable, errors.New("no schema")),
			[]string{"e-mail: unknown: unavailable: "}},
		{"the outbox cannot be counted", with(configured, func(s *notify.EmailState) {
			s.OutboxErr = errs.E("test", errs.Unavailable, errors.New("down"))
		}), nil, []string{"outbox: unavailable: unavailable\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, problems := emailLines(t, tc.st, tc.err)
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
