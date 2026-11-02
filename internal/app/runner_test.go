package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// recordHandler collects the messages of the records it handles.
type recordHandler struct {
	mu   *sync.Mutex
	msgs *[]string
}

func newRecordHandler() recordHandler { return recordHandler{mu: &sync.Mutex{}, msgs: &[]string{}} }

func (recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.msgs = append(*h.msgs, r.Message)
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }
func (h recordHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), *h.msgs...)
}

func TestNewRunner_BadArguments_ReturnsInvalid(t *testing.T) {
	run := func(context.Context) error { return nil }
	lg := slog.New(newRecordHandler())
	tests := []struct {
		name   string
		label  string
		run    func(context.Context) error
		logger *slog.Logger
	}{
		{"no label", "", run, lg},
		{"no run", "x", nil, lg},
		{"no logger", "x", run, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newRunner(tc.label, tc.run, nil, tc.logger); !errs.Is(err, errs.Invalid) {
				t.Errorf("err = %v, want Invalid", err)
			}
		})
	}
}

func TestRunner_StopBeforeStart_ReturnsNil(t *testing.T) {
	r, err := newRunner("x", func(context.Context) error { return nil }, nil, slog.New(newRecordHandler()))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.stop(context.Background()); err != nil {
		t.Errorf("stop before start = %v, want nil", err)
	}
}

func TestRunner_StartThenStop_CloseRunsFirstAndRunEnds(t *testing.T) {
	var mu sync.Mutex
	var order []string
	note := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	started := make(chan struct{})
	run := func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		note("run-ended")
		return nil
	}
	closeFn := func(context.Context) error { note("close"); return nil }
	r, err := newRunner("notify sender", run, closeFn, slog.New(newRecordHandler()))
	if err != nil {
		t.Fatal(err)
	}
	// Run must outlive start's own deadline.
	sctx, cancel := context.WithCancel(context.Background())
	if err := r.start(sctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	cancel()
	<-started
	if err := r.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(order, ","); got != "close,run-ended" {
		t.Errorf("order = %s, want close,run-ended", got)
	}
}

func TestRunner_RunFails_LogsOnceAndStopStillReturns(t *testing.T) {
	h := newRecordHandler()
	r, err := newRunner("settings refresher", func(context.Context) error {
		return errs.E("test.run", errs.Unavailable, errors.New("down"))
	}, nil, slog.New(h))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := r.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	msgs := h.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "settings refresher") {
		t.Errorf("log = %v, want one line naming the settings refresher", msgs)
	}
}

func TestRunner_CloseFails_ReturnsItsKindAndCancelsRun(t *testing.T) {
	ended := make(chan struct{})
	run := func(ctx context.Context) error { <-ctx.Done(); close(ended); return nil }
	r, err := newRunner("job runner", run, func(context.Context) error {
		return errs.E("test.close", errs.Unavailable, errors.New("boom"))
	}, slog.New(newRecordHandler()))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.stop(context.Background()); !errs.Is(err, errs.Unavailable) {
		t.Errorf("stop = %v, want Unavailable", err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("Run was not canceled after a failed Close")
	}
}

func TestRunner_RunIgnoresCancel_StopReturnsTimeout(t *testing.T) {
	release := make(chan struct{})
	r, err := newRunner("job runner", func(context.Context) error { <-release; return nil }, nil, slog.New(newRecordHandler()))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a deadline already passed
	err = r.stop(ctx)
	close(release)
	if !errs.Is(err, errs.Canceled) {
		t.Errorf("stop = %v, want Canceled", err)
	}
	expired, cancel2 := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel2()
	release2 := make(chan struct{})
	defer close(release2)
	r2, err := newRunner("job runner", func(context.Context) error { <-release2; return nil }, nil, slog.New(newRecordHandler()))
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r2.stop(expired); !errs.Is(err, errs.Timeout) {
		t.Errorf("stop with passed deadline = %v, want Timeout", err)
	}
}

// TestRunnables_StartInOrderStopInReverse: runners wrapped with runnable(name) start in the
// order listed (audit, settings, notify, files, jobs) and stop in reverse.
func TestRunnables_StartInOrderStopInReverse(t *testing.T) {
	var mu sync.Mutex
	var events []string
	note := func(s string) { mu.Lock(); events = append(events, s); mu.Unlock() }
	a := &assembly{}
	names := []string{"audit", "settings", "notify", "files", "jobs"}
	for _, n := range names {
		started := make(chan struct{})
		run := func(ctx context.Context) error { note("start " + n); close(started); <-ctx.Done(); return nil }
		r, err := newRunner(n, run, func(context.Context) error { note("stop " + n); return nil }, slog.New(newRecordHandler()))
		if err != nil {
			t.Fatal(err)
		}
		rb := r.runnable(n)
		start := rb.start
		rb.start = func(ctx context.Context) error { err := start(ctx); <-started; return err }
		a.runnables = append(a.runnables, rb)
	}
	if err := a.startRunnables(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := a.stopRunnables(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	var want []string
	for _, n := range names {
		want = append(want, "start "+n)
	}
	for i := len(names) - 1; i >= 0; i-- {
		want = append(want, "stop "+names[i])
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(events, ","); got != strings.Join(want, ",") {
		t.Errorf("events = %s\nwant %s", got, strings.Join(want, ","))
	}
}

// An invalid set is refused before the database is read, so a zero pool is enough.
func TestSchemaStatus_InvalidSet_ReturnsInvalid(t *testing.T) {
	m, err := db.NewMigrator(&db.Pool{}, slog.New(newRecordHandler()))
	if err != nil {
		t.Fatal(err)
	}
	s, err := newSchemaStatus(m, []db.Set{{Name: "Not Valid!"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(context.Background()); !errs.Is(err, errs.Invalid) {
		t.Errorf("Status = %v, want Invalid", err)
	}
}
