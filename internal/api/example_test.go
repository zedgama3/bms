package api_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// quietTracer, quietMeter and friends let an example build a server without a telemetry
// stack. Tests use telemetrytest instead.
type quietTracer struct{}

func (quietTracer) Start(ctx context.Context, _ string, _ ...telemetry.StartOption) (context.Context, telemetry.Span) {
	return ctx, quietSpan{}
}

type quietSpan struct{}

func (quietSpan) SetAttrs(...telemetry.Attr) {}
func (quietSpan) RecordError(error)          {}
func (quietSpan) End()                       {}

type quietMeter struct{}

func (quietMeter) Counter(string, ...telemetry.InstrumentOption) (telemetry.Counter, error) {
	return quietInstrument{}, nil
}

func (quietMeter) Histogram(string, ...telemetry.InstrumentOption) (telemetry.Histogram, error) {
	return quietInstrument{}, nil
}

func (quietMeter) Gauge(string, func(context.Context) []telemetry.Sample, ...telemetry.InstrumentOption) error {
	return nil
}

type quietInstrument struct{}

func (quietInstrument) Add(context.Context, int64, ...telemetry.Attr)      {}
func (quietInstrument) Record(context.Context, float64, ...telemetry.Attr) {}

type readWriteDB struct{}

func (readWriteDB) Ping(context.Context) error { return nil }
func (readWriteDB) ReadOnly() bool             { return false }

// New mounts the route sets it is given under /api/<name>/, checks them against their
// OpenAPI fragments and returns a server whose Handler can be tested without a network.
func ExampleNew() {
	logger := slog.New(slog.DiscardHandler)
	in, err := intake.New(intake.Options{Logger: logger, Meter: quietMeter{}, Clock: clock.NewFake(time.Unix(0, 0))})
	if err != nil {
		fmt.Println("intake:", err)
		return
	}
	hello := module.ModuleRoutes{
		Module: "alpha",
		Routes: []module.Route{{
			Method:  http.MethodGet,
			Pattern: "/greeting",
			Access:  module.AccessPublic,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := httpx.WriteJSON(w, http.StatusOK, map[string]string{"greeting": "hello"}); err != nil {
					httpx.Fail(w, r, err)
				}
			}),
		}},
		OpenAPI: []byte(`{"paths":{"/greeting":{"get":{"responses":{"200":{"description":"A greeting."}}}}}}`),
	}
	srv, err := api.New(api.Options{
		Addr: "127.0.0.1:0", RequestTimeout: 30 * time.Second, MaxBodyBytes: 1 << 20,
		Logger: logger, Tracer: quietTracer{}, Meter: quietMeter{}, Clock: clock.NewFake(time.Unix(0, 0)),
		Version:  "v0.0.0",
		Routes:   []module.ModuleRoutes{hello},
		Database: readWriteDB{}, Intake: in,
		Schema: func(context.Context) ([]db.Status, error) { return nil, nil },
	})
	if err != nil {
		fmt.Println("api:", err)
		return
	}
	for _, target := range []string{"/api/alpha/greeting", "/api/alpha/nothing"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		fmt.Println(rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	// Output:
	// 200 {"greeting":"hello"}
	// 404 {"status":404,"code":"not_found"}
}
