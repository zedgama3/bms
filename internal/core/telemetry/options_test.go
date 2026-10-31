package telemetry_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func validOptions() telemetry.Options {
	return baseOptions(&syncBuf{}, clock.NewFake(epoch), nil, nil)
}

func TestOptionsValidate_Valid_ReturnsNil(t *testing.T) {
	for _, f := range []telemetry.Format{telemetry.FormatJSON, telemetry.FormatText} {
		for _, l := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
			o := validOptions()
			o.Format, o.Level = f, l
			if err := o.Validate(); err != nil {
				t.Errorf("format %q level %v: %v", f, l, err)
			}
		}
	}
}

func TestOptionsValidate_BadLevel_ReturnsInvalid(t *testing.T) {
	for _, l := range []slog.Level{slog.Level(2), slog.Level(-8), slog.Level(12)} {
		o := validOptions()
		o.Level = l
		err := o.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "log.level") {
			t.Errorf("level %v: err=%v fields=%v", l, err, fieldNames(err))
		}
	}
}

func TestOptionsValidate_BadFormat_ReturnsInvalid(t *testing.T) {
	for _, f := range []telemetry.Format{"", "yaml", "JSON"} {
		o := validOptions()
		o.Format = f
		err := o.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "log.format") {
			t.Errorf("format %q: err=%v fields=%v", f, err, fieldNames(err))
		}
	}
}

func TestOptionsValidate_BadInstance_ReturnsInvalid(t *testing.T) {
	for _, name := range []string{"", "Upper", "-lead", "trail-", "has space", "under_score", repeat('a', 65)} {
		o := validOptions()
		o.Instance = name
		err := o.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "instance.name") {
			t.Errorf("instance %q: err=%v fields=%v", name, err, fieldNames(err))
		}
	}
	for _, name := range []string{"a", "bms-2026", repeat('a', 64)} {
		o := validOptions()
		o.Instance = name
		if err := o.Validate(); err != nil {
			t.Errorf("instance %q should be valid: %v", name, err)
		}
	}
}

func TestOptionsValidate_BadVersion_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"", "has space", "a@b", ".lead", repeat('v', 65)} {
		o := validOptions()
		o.Version = v
		err := o.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "telemetry.version") {
			t.Errorf("version %q: err=%v fields=%v", v, err, fieldNames(err))
		}
	}
	for _, v := range []string{"dev", "v1.2.3-rc.1", repeat('v', 64)} {
		o := validOptions()
		o.Version = v
		if err := o.Validate(); err != nil {
			t.Errorf("version %q should be valid: %v", v, err)
		}
	}
}

func TestOptionsValidate_NilDepsAndMany_ReportsEach(t *testing.T) {
	o := validOptions()
	o.Stdout = nil
	err := o.Validate()
	if !errs.Is(err, errs.Invalid) || !hasField(err, "telemetry.stdout") {
		t.Errorf("nil stdout: %v", fieldNames(err))
	}
	o = validOptions()
	o.Clock = nil
	err = o.Validate()
	if !errs.Is(err, errs.Invalid) || !hasField(err, "telemetry.clock") {
		t.Errorf("nil clock: %v", fieldNames(err))
	}

	var zero telemetry.Options
	err = zero.Validate()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("zero options: %v", err)
	}
	for _, f := range []string{"log.format", "instance.name", "telemetry.version", "telemetry.stdout", "telemetry.clock"} {
		if !hasField(err, f) {
			t.Errorf("zero options: missing field %s in %v", f, fieldNames(err))
		}
	}
}

func TestNew_InvalidOptions_ReturnsInvalid(t *testing.T) {
	o := validOptions()
	o.Instance = "BAD"
	tel, err := telemetry.New(context.Background(), o)
	if !errs.Is(err, errs.Invalid) || tel != nil {
		t.Fatalf("tel=%v err=%v", tel, err)
	}
}

func newWithEnv(env map[string]string) (*telemetry.Telemetry, error) {
	o := baseOptions(&syncBuf{}, clock.NewFake(epoch), env, nil)
	tel, err := telemetry.New(context.Background(), o)
	if err != nil {
		return nil, fmt.Errorf("newWithEnv: %w", err)
	}
	return tel, nil
}

func closeQuietly(t *testing.T, tel *telemetry.Telemetry) {
	t.Helper()
	if tel == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tel.Close(ctx); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNew_GRPCProtocol_ReturnsInvalidNamingVariable(t *testing.T) {
	vars := []string{
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
	}
	for _, v := range vars {
		for _, val := range []string{"grpc", "http/json", "bogus"} {
			t.Run(v+"="+val, func(t *testing.T) {
				tel, err := newWithEnv(map[string]string{
					"OTEL_EXPORTER_OTLP_ENDPOINT": closedEndpoint(t),
					v:                             val,
				})
				if err == nil {
					closeQuietly(t, tel)
				}
				if !errs.Is(err, errs.Invalid) || !hasField(err, v) {
					t.Fatalf("err=%v fields=%v", err, fieldNames(err))
				}
			})
		}
	}
	tel, err := newWithEnv(map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": closedEndpoint(t),
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
	})
	if err != nil {
		t.Fatalf("http/protobuf must be accepted: %v", err)
	}
	closeQuietly(t, tel)
}

func TestNew_BadEndpoint_ReturnsInvalidWithoutEcho(t *testing.T) {
	vars := []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	}
	bad := []string{
		"ftp://user:" + marker + "@collector.example/x",
		"/relative/" + marker,
		"collector.example:4318/" + marker,
		"http://",
	}
	for _, v := range vars {
		for _, val := range bad {
			t.Run(v, func(t *testing.T) {
				tel, err := newWithEnv(map[string]string{v: val})
				if err == nil {
					closeQuietly(t, tel)
				}
				if !errs.Is(err, errs.Invalid) || !hasField(err, v) {
					t.Fatalf("value %q: err=%v fields=%v", val, err, fieldNames(err))
				}
				if strings.Contains(err.Error(), marker) {
					t.Errorf("error echoes the endpoint value: %v", err)
				}
			})
		}
	}
}

func TestNew_UnsupportedSampler_ReturnsInvalid(t *testing.T) {
	cases := []struct {
		name, sampler, arg string
		wantArg            bool
	}{
		{"parentbased_always_on", "parentbased_always_on", "", false},
		{"parentbased_traceidratio", "parentbased_traceidratio", "0.5", false},
		{"jaeger_remote", "jaeger_remote", "", false},
		{"xray", "xray", "", false},
		{"unknown", "bogus", "", false},
		{"ratio_no_arg", "traceidratio", "", true},
		{"ratio_not_number", "traceidratio", "abc", true},
		{"ratio_too_big", "traceidratio", "1.5", true},
		{"ratio_negative", "traceidratio", "-0.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"OTEL_TRACES_SAMPLER": tc.sampler}
			if tc.arg != "" {
				env["OTEL_TRACES_SAMPLER_ARG"] = tc.arg
			}
			tel, err := newWithEnv(env)
			if err == nil {
				closeQuietly(t, tel)
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err=%v", err)
			}
			if !hasField(err, "OTEL_TRACES_SAMPLER") && !hasField(err, "OTEL_TRACES_SAMPLER_ARG") {
				t.Errorf("no sampler variable named: %v", fieldNames(err))
			}
		})
	}
	for _, ok := range []map[string]string{
		{"OTEL_TRACES_SAMPLER": "always_on"},
		{"OTEL_TRACES_SAMPLER": "always_off"},
		{"OTEL_TRACES_SAMPLER": "traceidratio", "OTEL_TRACES_SAMPLER_ARG": "0.5"},
		{"OTEL_TRACES_SAMPLER": "traceidratio", "OTEL_TRACES_SAMPLER_ARG": "0"},
		{"OTEL_TRACES_SAMPLER": "traceidratio", "OTEL_TRACES_SAMPLER_ARG": "1"},
	} {
		tel, err := newWithEnv(ok)
		if err != nil {
			t.Errorf("%v should be accepted: %v", ok, err)
			continue
		}
		closeQuietly(t, tel)
	}
}

func TestNew_BadSDKDisabled_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"yes", "1", "maybe"} {
		tel, err := newWithEnv(map[string]string{"OTEL_SDK_DISABLED": v})
		if err == nil {
			closeQuietly(t, tel)
		}
		if !errs.Is(err, errs.Invalid) || !hasField(err, "OTEL_SDK_DISABLED") {
			t.Errorf("value %q: err=%v fields=%v", v, err, fieldNames(err))
		}
	}
	for _, v := range []string{"true", "false"} {
		tel, err := newWithEnv(map[string]string{"OTEL_SDK_DISABLED": v})
		if err != nil {
			t.Errorf("value %q: %v", v, err)
			continue
		}
		closeQuietly(t, tel)
	}
}

func TestNew_ContextCanceled_ReturnsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tel, err := telemetry.New(ctx, validOptions())
	if !errs.Is(err, errs.Canceled) || tel != nil {
		t.Fatalf("tel=%v err=%v", tel, err)
	}

	dctx, dcancel := context.WithDeadline(context.Background(), epoch)
	defer dcancel()
	tel, err = telemetry.New(dctx, validOptions())
	if !errs.Is(err, errs.Timeout) || tel != nil {
		t.Fatalf("deadline: tel=%v err=%v", tel, err)
	}
}

func TestNew_ExporterConstructionFails_ReturnsInvalidAndCleansUp(t *testing.T) {
	// The certificate variable is read by the SDK from the process environment, so it is
	// set there as well as in the Environ map.
	const cert = "OTEL_EXPORTER_OTLP_CERTIFICATE"
	t.Setenv(cert, "/nonexistent/dir/ca.pem")
	tel, err := newWithEnv(map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": closedEndpoint(t),
		cert:                          "/nonexistent/dir/ca.pem",
	})
	if err == nil {
		closeQuietly(t, tel)
	}
	if !errs.Is(err, errs.Invalid) || tel != nil {
		t.Fatalf("tel=%v err=%v", tel, err)
	}
}

func TestNew_NoEnvironment_WorksWithoutExport(t *testing.T) {
	h := newHarness(t, nil)
	if h.tel.Exporting() {
		t.Error("Exporting() must be false with no OTEL_* variables")
	}
	ctx, sp := h.tracer(t).Start(context.Background(), "work")
	id, ok := telemetry.TraceIDFrom(ctx)
	if !ok || len(id) != 32 {
		t.Fatalf("span has no trace ID: %q %v", id, ok)
	}
	h.tel.Logger().InfoContext(ctx, "inside")
	sp.End()
	recs := h.obs.records()
	if len(recs) != 1 || recs[0].TraceID != id {
		t.Fatalf("log record trace ID mismatch: %+v want %s", recs, id)
	}
}

func TestNew_EndpointPerSignal_Table(t *testing.T) {
	const base = "OTEL_EXPORTER_OTLP_ENDPOINT"
	ep, ep2 := closedEndpoint(t), closedEndpoint(t)
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"none", nil, false},
		{"base only", map[string]string{base: ep}, true},
		{"traces only", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": ep + "/v1/traces"}, true},
		{"metrics only", map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": ep + "/v1/metrics"}, true},
		{"logs only", map[string]string{"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": ep + "/v1/logs"}, true},
		{"base and signal", map[string]string{base: ep, "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": ep2 + "/v1/logs"}, true},
		{"disabled with base", map[string]string{base: ep, "OTEL_SDK_DISABLED": "true"}, false},
		{"disabled with signal", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": ep + "/v1/traces", "OTEL_SDK_DISABLED": "true"}, false},
		{"disabled false with base", map[string]string{base: ep, "OTEL_SDK_DISABLED": "false"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tel, err := newWithEnv(tc.env)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer closeQuietly(t, tel)
			if got := tel.Exporting(); got != tc.want {
				t.Errorf("Exporting() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNew_NeverDials(t *testing.T) {
	var lc net.ListenConfig
	ctx := context.Background()
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	remotes := make(chan string, 16)
	go func() {
		defer close(remotes)
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			remotes <- c.RemoteAddr().String()
			if cerr := c.Close(); cerr != nil {
				return
			}
		}
	}()

	start := time.Now()
	tel, err := newWithEnv(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://" + ln.Addr().String()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("New took %v", d)
	}

	// Probe: once the accept loop has seen our own connection, anything it saw earlier was
	// a dial made by New.
	var d net.Dialer
	probe, err := d.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	probeAddr := probe.LocalAddr().String()
	if err := probe.Close(); err != nil {
		t.Errorf("close probe: %v", err)
	}
	for r := range remotes {
		if r == probeAddr {
			break
		}
		t.Errorf("New dialed the collector (connection from %s)", r)
	}
	closeQuietly(t, tel)
	if err := ln.Close(); err != nil {
		t.Errorf("close listener: %v", err)
	}
}

// closedEndpoint returns an http endpoint on the loopback interface whose port was free a
// moment ago and has nothing listening, so exporters never reach a real collector.
func closedEndpoint(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + addr
}
