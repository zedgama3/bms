//go:build integration

package telemetry_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// receiver is a plain OTLP/HTTP receiver: it counts requests and body bytes per path.
type receiver struct {
	*httptest.Server
	mu     sync.Mutex
	hits   map[string]int
	bodies map[string][]byte
	ctypes map[string]string
}

func newReceiver(t *testing.T, status int) *receiver {
	t.Helper()
	r := &receiver{hits: map[string]int{}, bodies: map[string][]byte{}, ctypes: map[string]string{}}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.hits[req.URL.Path]++
		r.bodies[req.URL.Path] = append(r.bodies[req.URL.Path], body...)
		r.ctypes[req.URL.Path] = req.Header.Get("Content-Type")
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) snapshot() (map[string]int, map[string][]byte, map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, b, c := map[string]int{}, map[string][]byte{}, map[string]string{}
	for k, v := range r.hits {
		h[k] = v
	}
	for k, v := range r.bodies {
		b[k] = append([]byte(nil), v...)
	}
	for k, v := range r.ctypes {
		c[k] = v
	}
	return h, b, c
}

func exportingTelemetry(t *testing.T, env map[string]string, buf *syncBuf, o telemetry.Observer) *telemetry.Telemetry {
	t.Helper()
	tel, err := telemetry.New(context.Background(), baseOptions(buf, clock.NewFake(epoch), env, o))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tel
}

func emitOne(t *testing.T, tel *telemetry.Telemetry) {
	t.Helper()
	tr, err := tel.Tracer("module.test")
	if err != nil {
		t.Fatal(err)
	}
	m, err := tel.Meter("module.test")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Counter("bms.test.export")
	if err != nil {
		t.Fatal(err)
	}
	ctx, sp := tr.Start(context.Background(), "export.one")
	tel.Logger().WarnContext(ctx, "export log", slog.String("module", "test"))
	c.Add(ctx, 1, telemetry.Token("outcome", "ok"))
	sp.End()
}

func TestExport_ReachesOTLPReceiver(t *testing.T) {
	rcv := newReceiver(t, http.StatusOK)
	buf := &syncBuf{}
	tel := exportingTelemetry(t, map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": rcv.URL,
	}, buf, nil)
	if !tel.Exporting() {
		t.Fatal("Exporting() = false with an endpoint")
	}
	emitOne(t, tel)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tel.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	hits, bodies, ctypes := rcv.snapshot()
	for _, p := range []string{"/v1/traces", "/v1/metrics", "/v1/logs"} {
		if hits[p] < 1 || len(bodies[p]) == 0 {
			t.Errorf("%s: hits=%d bytes=%d", p, hits[p], len(bodies[p]))
		}
		if !strings.HasPrefix(ctypes[p], "application/x-protobuf") {
			t.Errorf("%s: content type %q", p, ctypes[p])
		}
	}
}

func TestClose_FlushesPendingSpansAndLogs(t *testing.T) {
	rcv := newReceiver(t, http.StatusOK)
	tel := exportingTelemetry(t, map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": rcv.URL}, &syncBuf{}, nil)
	emitOne(t, tel)
	hits, _, _ := rcv.snapshot()
	if hits["/v1/traces"] != 0 {
		t.Log("exported before Close (fine); the point is that Close leaves nothing pending")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tel.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	hits, bodies, _ := rcv.snapshot()
	if hits["/v1/traces"] < 1 || hits["/v1/logs"] < 1 || len(bodies["/v1/traces"]) == 0 || len(bodies["/v1/logs"]) == 0 {
		t.Errorf("hits = %v", hits)
	}
}

func TestNew_ResourceAttributes(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"defaults", nil, []string{"service.name", "bms", "service.version", "v1.2.3", "service.instance.id", "test-copy"}},
		{"service name override", map[string]string{"OTEL_SERVICE_NAME": "bms-second"}, []string{"bms-second"}},
		{"extra resource attributes", map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "deployment.environment=staging"}, []string{"deployment.environment", "staging"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rcv := newReceiver(t, http.StatusOK)
			env := map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": rcv.URL}
			for k, v := range tc.env {
				env[k] = v
			}
			tel := exportingTelemetry(t, env, &syncBuf{}, nil)
			emitOne(t, tel)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := tel.Close(ctx); err != nil {
				t.Fatalf("Close: %v", err)
			}
			_, bodies, _ := rcv.snapshot()
			body := bodies["/v1/traces"]
			for _, w := range tc.want {
				if !bytes.Contains(body, []byte(w)) {
					t.Errorf("resource lacks %q", w)
				}
			}
		})
	}
}

func closedPort(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestExport_CollectorDown_RequestLatencyUnaffected(t *testing.T) {
	buf := &syncBuf{}
	o := &obs{}
	tel := exportingTelemetry(t, map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://" + closedPort(t)}, buf, o)
	tr, err := tel.Tracer("module.test")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for range 1000 {
		ctx, sp := tr.Start(context.Background(), "load")
		tel.Logger().InfoContext(ctx, "load")
		sp.End()
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("1000 spans and records took %v with the collector down", d)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start = time.Now()
	if err := tel.Close(ctx); err != nil {
		t.Logf("Close with a dead collector: %v", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Error("Close ignored its deadline")
	}
	failed := false
	for _, m := range o.measurements() {
		if m.Instrument == "bms.telemetry.export_failures" && m.Value > 0 {
			failed = true
		}
	}
	if !failed {
		t.Error("export failures were not counted")
	}
	warns := 0
	for _, r := range o.records() {
		if r.Level == slog.LevelWarn {
			warns++
		}
	}
	if warns < 1 || warns > 3 {
		t.Errorf("want one warning per signal (1..3), got %d", warns)
	}
}

func TestExport_HeadersNeverLogged(t *testing.T) {
	cases := map[string]string{
		"well formed": "authorization=" + marker,
		"malformed":   "Bearer " + marker,
		"mixed":       "x-ok=1,Bearer " + marker,
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			// The SDK reads the headers from the process environment, not Options.Environ.
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", headers)
			stop := captureStderr(t)
			rcv := newReceiver(t, http.StatusInternalServerError)
			buf := &syncBuf{}
			o := &obs{}
			tel := exportingTelemetry(t, map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": rcv.URL,
				"OTEL_EXPORTER_OTLP_HEADERS":  headers,
			}, buf, o)
			emitOne(t, tel)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := tel.Close(ctx); err != nil {
				t.Logf("Close: %v", err)
			}
			stderr := stop()
			if strings.Contains(stderr, marker) {
				t.Errorf("header value reached stderr: %q", stderr)
			}
			if strings.Contains(buf.String(), marker) {
				t.Errorf("header value in log output: %q", buf.String())
			}
			for _, r := range o.records() {
				if strings.Contains(r.Message, marker) {
					t.Errorf("header value in record %q", r.Message)
				}
				for k, v := range r.Attrs {
					if strings.Contains(v, marker) {
						t.Errorf("header value in attr %s", k)
					}
				}
			}
		})
	}
}

func TestNew_LaterSignalFails_ReturnsInvalidWithoutDialing(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan string, 8)
	go func() {
		defer close(accepted)
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			accepted <- c.RemoteAddr().String()
			if cerr := c.Close(); cerr != nil {
				return
			}
		}
	}()
	// Traces and metrics build fine; the logs exporter then fails on its certificate.
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_CERTIFICATE", "/nonexistent/ca.pem")
	before := runtime.NumGoroutine()
	start := time.Now()
	tel, nerr := telemetry.New(context.Background(), baseOptions(&syncBuf{}, clock.NewFake(epoch), map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT":         "http://" + ln.Addr().String(),
		"OTEL_EXPORTER_OTLP_LOGS_CERTIFICATE": "/nonexistent/ca.pem",
	}, nil))
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("New took %v", d)
	}
	if nerr == nil {
		closeQuietly(t, tel)
		t.Fatal("New succeeded with an unreadable logs certificate")
	}
	if !errs.Is(nerr, errs.Invalid) || tel != nil {
		t.Errorf("tel=%v err=%v", tel, nerr)
	}
	var d net.Dialer
	probe, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	probeAddr := probe.LocalAddr().String()
	if err := probe.Close(); err != nil {
		t.Error(err)
	}
	for a := range accepted {
		if a == probeAddr {
			break
		}
		t.Errorf("New dialed the collector from %s", a)
	}
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	runtime.GC()
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutines before=%d after=%d: providers built before the failure were not shut down", before, after)
	}
}
