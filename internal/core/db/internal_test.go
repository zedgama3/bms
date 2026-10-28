package db

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// internalURL parses with the driver but is never dialled: a probe stands in for the server.
const internalURL = "postgres://bms:ZQ9MARKER7@127.0.0.1:1/bms?sslmode=disable" //nolint:gosec // an invented marker, never dialled

func internalOptions() Options {
	return Options{
		URL:            secret.New(internalURL),
		MaxConns:       2,
		ConnectTimeout: time.Minute,
		QueryTimeout:   time.Second,
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestOperationOf_Cases(t *testing.T) {
	tests := []struct{ sql, want string }{
		{"select 1", "SELECT"},
		{"  INSERT INTO t VALUES (1)", "INSERT"},
		{"with x as (select 1) select * from x", "WITH"},
		{"begin", "BEGIN"},
		{"-- a header comment\nSELECT 1", "SELECT"},
		{"/* block */ update t set a = 1", "UPDATE"},
		{"/* unterminated", "OTHER"},
		{"(select 1)", "OTHER"},
		{"", "OTHER"},
		{"   ", "OTHER"},
		{"1; drop", "OTHER"},
	}
	for _, tc := range tests {
		t.Run(tc.sql, func(t *testing.T) {
			if got := operationOf(tc.sql); got != tc.want {
				t.Errorf("operationOf(%q) = %q, want %q", tc.sql, got, tc.want)
			}
		})
	}
}

func TestEmbeddedSQL_AllStatementsLoaded(t *testing.T) {
	for name, s := range map[string]string{
		"readonly_probe": sqlReadonlyProbe, "table_exists": sqlTableExists, "applied_all": sqlAppliedAll,
		"applied_for_set": sqlAppliedForSet, "insert_applied": sqlInsertApplied,
		"insert_bootstrap": sqlInsertBootstrap, "advisory_try_lock": sqlAdvisoryTryLock,
		"advisory_unlock": sqlAdvisoryUnlock,
	} {
		if s == "" {
			t.Errorf("%s: empty", name)
		}
	}
	if v, err := Version(bootstrapFiles); err != nil || v != 1 {
		t.Errorf("bootstrap set: version %d, err %v; want 1, nil", v, err)
	}
}

// recordingHandler counts warn records.
type recordingHandler struct {
	mu    sync.Mutex
	warns []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		h.mu.Lock()
		h.warns = append(h.warns, r.Message)
		h.mu.Unlock()
	}
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }
func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.warns)
}

func TestWatch_TracksProbeAndLogsTransitionsOnce(t *testing.T) {
	clk := clock.NewFake(epoch)
	rec := &recordingHandler{}
	p := newPool(nil, internalOptions(), clk, slog.New(rec))

	results := make(chan struct {
		ro  bool
		err error
	}, 8)
	called := make(chan struct{})
	p.probe = func(context.Context) (probeResult, error) {
		r := <-results
		called <- struct{}{}
		return probeResult{version: MinServerVersion, readOnly: r.ro}, r.err
	}
	tick, err := clk.NewTicker(probeInterval)
	if err != nil {
		t.Fatalf("NewTicker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go p.watch(ctx, tick)

	step := func(ro bool, perr error) {
		t.Helper()
		results <- struct {
			ro  bool
			err error
		}{ro, perr}
		if err := clk.Advance(probeInterval); err != nil {
			t.Fatalf("Advance: %v", err)
		}
		<-called
	}
	// Each probe call returning means the previous tick's state is stored; the next step
	// therefore observes it.
	step(true, nil)
	step(true, errors.New("down 1"))
	step(true, errors.New("down 2"))
	step(false, nil)
	step(false, nil) // lets the previous tick finish before reading
	if p.ReadOnly() {
		t.Errorf("the state must be writable again")
	}
	// Two state changes (read-only, writable again) and one probe-failure line.
	if got := rec.count(); got != 3 {
		t.Errorf("warn lines = %d, want 3", got)
	}
	cancel()
	<-p.watcherDone
}

func TestConnect_OldServer_ReturnsInvalidWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	probe := func(context.Context) (probeResult, error) {
		calls.Add(1)
		return probeResult{version: MinServerVersion - 1}, nil
	}
	p, err := open(context.Background(), internalOptions(), clock.NewFake(epoch), discardLogger(), probe)
	if p != nil {
		t.Fatalf("open must fail")
	}
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("got kind %v, want Invalid (%v)", errs.KindOf(err), err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("probe called %d times, want 1", n)
	}
}

func TestConnect_Unreachable_FollowsTheBackoffSchedule(t *testing.T) {
	clk := clock.NewFake(epoch)
	var mu sync.Mutex
	var at []time.Duration
	attempt := make(chan struct{}, 32)
	probe := func(context.Context) (probeResult, error) {
		mu.Lock()
		at = append(at, clk.Since(epoch))
		mu.Unlock()
		attempt <- struct{}{}
		return probeResult{}, errs.E("test", errs.Unavailable, errors.New("refused"))
	}
	type result struct {
		p   *Pool
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := open(context.Background(), internalOptions(), clk, discardLogger(), probe)
		done <- result{p, err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	delays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	<-attempt
	for _, d := range delays {
		// the connect timer and the retry wait are both pending
		if err := clk.BlockUntil(ctx, 2); err != nil {
			t.Fatalf("BlockUntil: %v", err)
		}
		if err := clk.Advance(d); err != nil {
			t.Fatalf("Advance: %v", err)
		}
		<-attempt
	}
	if err := clk.BlockUntil(ctx, 2); err != nil {
		t.Fatalf("BlockUntil: %v", err)
	}
	if err := clk.Advance(time.Minute); err != nil { // past ConnectTimeout
		t.Fatalf("Advance: %v", err)
	}
	r := <-done
	if r.p != nil || !errs.Is(r.err, errs.Unavailable) {
		t.Fatalf("got pool %v, kind %v (%v); want nil, Unavailable", r.p, errs.KindOf(r.err), r.err)
	}
	want := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond,
		7500 * time.Millisecond, 12500 * time.Millisecond, 17500 * time.Millisecond}
	mu.Lock()
	defer mu.Unlock()
	for i, w := range want {
		if i >= len(at) || at[i] != w {
			t.Fatalf("attempt times = %v, want prefix %v", at, want)
		}
	}
}

// stubMeter records the gauges registered on it.
type stubMeter struct {
	gauges map[string]func(context.Context) []telemetry.Sample
}

func (*stubMeter) Counter(string, ...telemetry.InstrumentOption) (telemetry.Counter, error) {
	return nil, errors.New("unused")
}

func (*stubMeter) Histogram(string, ...telemetry.InstrumentOption) (telemetry.Histogram, error) {
	return nil, errors.New("unused")
}

func (m *stubMeter) Gauge(name string, observe func(context.Context) []telemetry.Sample, _ ...telemetry.InstrumentOption) error {
	if m.gauges == nil {
		m.gauges = map[string]func(context.Context) []telemetry.Sample{}
	}
	m.gauges[name] = observe
	return nil
}

func TestRegisterGauges_ReadStatsAndStopWhenClosed(t *testing.T) {
	m := &stubMeter{}
	var closed atomic.Bool
	stats := func() Stats { return Stats{Acquired: 2, Idle: 3, Max: 10, Pending: 1} }
	if err := registerGauges(m, stats, closed.Load); err != nil {
		t.Fatalf("registerGauges: %v", err)
	}
	if len(m.gauges) != 3 {
		t.Fatalf("registered %d gauges, want 3", len(m.gauges))
	}
	ctx := context.Background()
	if got := m.gauges["db.client.connection.count"](ctx); len(got) != 2 || got[0].Value != 3 || got[1].Value != 2 {
		t.Errorf("count samples = %+v, want idle 3 and used 2", got)
	}
	if got := m.gauges["db.client.connection.max"](ctx); len(got) != 1 || got[0].Value != 10 {
		t.Errorf("max samples = %+v", got)
	}
	if got := m.gauges["db.client.connection.pending_requests"](ctx); len(got) != 1 || got[0].Value != 1 {
		t.Errorf("pending samples = %+v", got)
	}
	closed.Store(true)
	for name, observe := range m.gauges {
		if got := observe(ctx); got != nil {
			t.Errorf("%s observed %+v after close", name, got)
		}
	}
}

func TestClassifyPg_ReadOnlyKeepsBothSentinelAndNames(t *testing.T) {
	kind, cause := classifyPg(&pgconnErrorForTest)
	if kind != errs.Unavailable || !errors.Is(cause, ErrReadOnly) {
		t.Fatalf("kind %v, ErrReadOnly=%v", kind, errors.Is(cause, ErrReadOnly))
	}
	var pe *Error
	if !errors.As(cause, &pe) || pe.Table != "shift" {
		t.Errorf("names lost: %v", cause)
	}
}

var pgconnErrorForTest = pgconn.PgError{Code: "25006", TableName: "shift", Message: "ZQ9MARKER7"}

func TestWithRaised_OnlyTheMigrationsOwnBlock_PassesTextThrough(t *testing.T) {
	failed := errors.New("migration failed")
	cases := []struct {
		name  string
		pg    *pgconn.PgError
		wants bool
	}{
		{"own DO block", &pgconn.PgError{Code: "P0001", Message: "fix the data", Where: "PL/pgSQL function inline_code_block line 1 at RAISE"}, true},
		{"own DO block, German", &pgconn.PgError{Code: "P0001", Message: "fix the data", Where: "PL/pgSQL-Funktion inline_code_block Zeile 1 bei RAISE"}, true},
		{"own DO block, French", &pgconn.PgError{Code: "P0001", Message: "fix the data", Where: "fonction PL/pgSQL inline_code_block, ligne 1 à RAISE"}, true},
		{"own DO block, Japanese", &pgconn.PgError{Code: "P0001", Message: "fix the data", Where: "PL/pgSQL関数inline_code_blockの1行目 - RAISE"}, true},
		{"own DO block, Korean", &pgconn.PgError{Code: "P0001", Message: "fix the data", Where: "PL/pgSQL 함수 \"inline_code_block\" 의 1번째 RAISE"}, true},
		{"own DO block, Chinese", &pgconn.PgError{Code: "P0001", Message: "fix the data", Where: "在RAISE的第1行的PL/pgSQL函数inline_code_block"}, true},
		{"named function with the block's name", &pgconn.PgError{Code: "P0001", Message: "row 7 of someone", Where: "PL/pgSQL function inline_code_block() line 3 at RAISE"}, false},
		{"named function with arguments and the block's name", &pgconn.PgError{Code: "P0001", Message: "row 7 of someone", Where: "PL/pgSQL function core.inline_code_block(integer) line 3 at RAISE"}, false},
		{"trigger, two lines", &pgconn.PgError{Code: "P0001", Message: "row 7 of someone", Where: "PL/pgSQL function core.guard() line 3 at RAISE\nSQL statement \"x\""}, false},
		{"own block is only the outer frame", &pgconn.PgError{Code: "P0001", Message: "row 7 of someone", Where: "PL/pgSQL function core.guard() line 3 at RAISE\nSQL statement \"x\"\nPL/pgSQL function inline_code_block line 2 at SQL statement"}, false},
		{"trigger the file fires", &pgconn.PgError{Code: "P0001", Message: "row 7 of someone", Where: "PL/pgSQL function core.guard() line 3 at RAISE"}, false},
		{"no where", &pgconn.PgError{Code: "P0001", Message: "fix the data"}, false},
		{"other code in own block", &pgconn.PgError{Code: "22012", Message: "division by zero", Where: "PL/pgSQL function inline_code_block line 1 at SQL statement"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withRaised(failed, tc.pg)
			if raised(tc.pg) != tc.wants {
				t.Errorf("raised = %v, want %v", !tc.wants, tc.wants)
			}
			if tc.wants {
				if !errors.Is(got, failed) || got.Error() == failed.Error() {
					t.Errorf("got %v, want the message appended", got)
				}
				return
			}
			if got != failed { //nolint:errorlint // identity: the error must come back unchanged
				t.Errorf("got %v, want the error unchanged", got)
			}
		})
	}
}
