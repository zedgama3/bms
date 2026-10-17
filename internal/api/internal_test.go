package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

type internalDB struct{}

func (internalDB) Ping(context.Context) error { return nil }
func (internalDB) ReadOnly() bool             { return false }

// internalServer builds a server with one public POST route that decodes a JSON body.
func internalServer(t *testing.T) *Server {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(time.Now())
	in, err := intake.New(intake.Options{Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	set := module.ModuleRoutes{
		Module: "alpha",
		Routes: []module.Route{{
			Method: http.MethodPost, Pattern: "/in", Access: module.AccessPublic,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var v struct{ A string }
				if err := httpx.ReadJSON(r, &v); err != nil {
					httpx.Fail(w, r, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}),
		}},
		OpenAPI: []byte(`{"paths":{"/in":{"post":{"responses":{"204":{"description":"ok"}}}}}}`),
	}
	s, err := New(Options{
		Addr: "127.0.0.1:0", RequestTimeout: 30 * time.Second, MaxBodyBytes: 1 << 20,
		Logger: c.Logger(), Tracer: c.Tracer("api"), Meter: c.Meter("api"), Clock: clk, Version: "v1",
		Routes: []module.ModuleRoutes{set}, Database: internalDB{}, Intake: in,
		Schema: func(context.Context) ([]db.Status, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPipeline_SlowBody_ReadDeadlineEnds504(t *testing.T) {
	s := internalServer(t)
	s.testRequestTimeout = 100 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()
	// Half of the promised body, then silence: only the read deadline can end this.
	const head = "POST /api/alpha/in HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 20\r\n\r\n{\"A\":"
	if _, err := io.WriteString(conn, head); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read response: %v (got %q)", err, got)
	}
	if !strings.HasPrefix(string(got), "HTTP/1.1 504 ") || !strings.Contains(string(got), `"code":"timeout"`) {
		t.Fatalf("response = %q, want 504 timeout", got)
	}
}

func TestServe_ListenerClosedUnderneath_ReturnsUnavailable(t *testing.T) {
	s := internalServer(t)
	if _, err := s.Listen(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	err := s.Serve()
	if !errs.Is(err, errs.Unavailable) {
		t.Fatalf("Serve = %v, want Unavailable", err)
	}
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown after a failed Serve = %v", err)
	}
}
