package telemetry_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestClose_Twice_IsNil(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	if err := h.tel.Close(ctx); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.tel.Close(ctx); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestClose_StdoutStillWorksAfter(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.tel.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	h.tel.Logger().ErrorContext(context.Background(), "after close", slog.String("module", "app"))
	if !strings.Contains(h.buf.String(), "after close") {
		t.Errorf("stdout = %q", h.buf.String())
	}
}

func TestClose_ContextExpired_ReturnsTimeout(t *testing.T) {
	// An exporting Telemetry on a closed port has something to flush, so an expired
	// context is observable.
	env := map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:1"}
	build := func(t *testing.T) *telemetry.Telemetry {
		t.Helper()
		tel, err := telemetry.New(context.Background(), baseOptions(&syncBuf{}, clock.NewFake(epoch), env, nil))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		tr, err := tel.Tracer("api")
		if err != nil {
			t.Fatalf("Tracer: %v", err)
		}
		_, sp := tr.Start(context.Background(), "pending")
		sp.End()
		return tel
	}

	t.Run("deadline passed", func(t *testing.T) {
		tel := build(t)
		ctx, cancel := context.WithDeadline(context.Background(), epoch)
		defer cancel()
		if err := tel.Close(ctx); !errs.Is(err, errs.Timeout) {
			t.Errorf("err = %v", err)
		}
		if err := tel.Close(context.Background()); err != nil {
			t.Errorf("second Close must be nil: %v", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		tel := build(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := tel.Close(ctx); !errs.Is(err, errs.Canceled) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestClose_ConcurrentWithUse(t *testing.T) {
	h := newHarness(t, nil)
	tr := h.tracer(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			_, sp := tr.Start(context.Background(), "busy")
			sp.End()
			h.tel.Logger().InfoContext(context.Background(), "busy")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.tel.Close(ctx); err != nil {
		t.Errorf("Close: %v", err)
	}
	<-done
}
