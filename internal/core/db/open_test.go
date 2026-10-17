package db_test

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func fakeClock() *clock.Fake { return clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) }

// openUnreachable runs Open against a closed port and drives the fake clock until Open
// returns, so the retry schedule costs no real time.
func openUnreachable(t *testing.T, o db.Options, logger *slog.Logger) (*db.Pool, error) {
	t.Helper()
	clk := fakeClock()
	type result struct {
		p   *db.Pool
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := db.Open(context.Background(), o, clk, logger)
		done <- result{p, err}
	}()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case r := <-done:
			return r.p, r.err
		case <-deadline:
			t.Fatalf("Open did not return")
			return nil, nil
		default:
			if err := clk.Advance(time.Second); err != nil {
				t.Fatalf("advance: %v", err)
			}
			runtime.Gosched()
		}
	}
}

// F4
func TestOpen_NilClockOrLogger_ReturnsInvalid(t *testing.T) {
	o := validOptions()
	o.URL = secret.New(closedPortURL(t))
	tests := []struct {
		name   string
		clk    clock.Clock
		logger *slog.Logger
	}{
		{"nil clock", nil, quietLogger()},
		{"nil logger", fakeClock(), nil},
		{"both nil", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := db.Open(context.Background(), o, tc.clk, tc.logger)
			if p != nil {
				t.Errorf("pool must be nil on error")
			}
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestOpen_InvalidOptions_ReturnsInvalid(t *testing.T) {
	o := validOptions()
	o.MaxConns = 0
	p, err := db.Open(context.Background(), o, fakeClock(), quietLogger())
	if p != nil {
		t.Errorf("pool must be nil on error")
	}
	wantKind(t, err, errs.Invalid)
	noMarker(t, err)
}

// F5
func TestOpen_DriverRejectsURL_ReturnsInvalidWithoutEcho(t *testing.T) {
	for name, u := range map[string]string{
		"bad sslmode":    "postgres://bms:" + marker + "@127.0.0.1:1/bms?sslmode=" + marker,
		"bad pool param": "postgres://bms:" + marker + "@127.0.0.1:1/bms?pool_max_conns=" + marker,
		"bad port":       "postgres://bms:" + marker + "@127.0.0.1:notaport/bms",
	} {
		t.Run(name, func(t *testing.T) {
			o := validOptions()
			o.URL = secret.New(u)
			c := telemetrytest.New(t)
			p, err := db.Open(context.Background(), o, fakeClock(), c.Logger())
			if p != nil {
				t.Errorf("pool must be nil on error")
			}
			wantKind(t, err, errs.Invalid)
			noMarker(t, err)
			if c.Contains(marker) {
				t.Errorf("a log record carries the marker")
			}
		})
	}
}

// F6
func TestOpen_Unreachable_RetriesThenReturnsUnavailable(t *testing.T) {
	o := validOptions()
	o.URL = secret.New(closedPortURL(t))
	o.ConnectTimeout = 2 * time.Second
	p, err := openUnreachable(t, o, quietLogger())
	if p != nil {
		t.Errorf("pool must be nil on error")
	}
	wantKind(t, err, errs.Unavailable)
	noMarker(t, err)
}

// F9
func TestOpen_ContextCanceled_ReturnsCanceled(t *testing.T) {
	o := validOptions()
	o.URL = secret.New(closedPortURL(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := db.Open(ctx, o, fakeClock(), quietLogger())
	if p != nil {
		t.Errorf("pool must be nil on error")
	}
	wantKind(t, err, errs.Canceled)
	noMarker(t, err)
}

// F9
func TestOpen_ContextDeadlinePassed_ReturnsTimeout(t *testing.T) {
	o := validOptions()
	o.URL = secret.New(closedPortURL(t))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	p, err := db.Open(ctx, o, fakeClock(), quietLogger())
	if p != nil {
		t.Errorf("pool must be nil on error")
	}
	wantKind(t, err, errs.Timeout)
}

func TestOpen_NeverLogsOrEchoesPassword(t *testing.T) {
	o := validOptions()
	o.URL = secret.New(closedPortURL(t))
	o.ConnectTimeout = time.Second
	c := telemetrytest.New(t)
	_, err := openUnreachable(t, o, c.Logger())
	if err == nil {
		t.Fatalf("want an error")
	}
	noMarker(t, err)
	if c.Contains(marker) {
		t.Fatalf("a log record carries the password marker")
	}
}

// F51
func TestNewMigrator_NilDependency_ReturnsInvalid(t *testing.T) {
	m, err := db.NewMigrator(nil, quietLogger())
	if m != nil {
		t.Errorf("migrator must be nil on error")
	}
	wantKind(t, err, errs.Invalid)
	m, err = db.NewMigrator(nil, nil)
	if m != nil {
		t.Errorf("migrator must be nil on error")
	}
	wantKind(t, err, errs.Invalid)
}
