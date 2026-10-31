package telemetry

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestParseTraceparent_AcceptsOnlyVersion00WithNonZeroIDs(t *testing.T) {
	const good = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	cases := []struct {
		name, in string
		ok       bool
	}{
		{"valid", good, true},
		{"flags ignored", strings.TrimSuffix(good, "01") + "00", true},
		{"unknown version", "01" + good[2:], false},
		{"upper case hex", strings.ToUpper(good), false},
		{"too short", good[:40], false},
		{"zero trace id", "00-00000000000000000000000000000000-b7ad6b7169203331-01", false},
		{"zero parent id", "00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01", false},
		{"trailing data", good + "-extra", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc, ok := parseTraceparent(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && (sc.IsSampled() || !sc.IsRemote()) {
				t.Errorf("flags must be cleared and the parent remote: sampled=%v remote=%v", sc.IsSampled(), sc.IsRemote())
			}
		})
	}
}

func TestCapBytes_FitsLimitAndKeepsValidUTF8(t *testing.T) {
	for _, s := range []string{strings.Repeat("a", 600), strings.Repeat("é", 400), strings.Repeat("€", 300)} {
		got := capBytes(s, MaxTextBytes)
		if len(got) > MaxTextBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, ellipsis) {
			t.Errorf("capBytes(%d bytes) = %d bytes, valid=%v", len(s), len(got), utf8.ValidString(got))
		}
	}
	if got := capBytes("short", MaxTextBytes); got != "short" {
		t.Errorf("short string changed: %q", got)
	}
}

func TestGate_AllowsOncePerKeyPerPeriod(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &stepClock{now: now}
	g := newGate(clk, time.Minute)
	if !g.allow("a") || g.allow("a") {
		t.Fatal("first call must pass and the second must not")
	}
	if !g.allow("b") {
		t.Error("another key is independent")
	}
	clk.now = now.Add(time.Minute)
	if !g.allow("a") {
		t.Error("a key passes again after the period")
	}
}

type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }

func TestNest_WrapsTailInOpenGroupsInOrder(t *testing.T) {
	ops := []op{
		{attrs: []slog.Attr{slog.Int("a", 1)}},
		{group: "g"},
		{attrs: []slog.Attr{slog.Int("b", 2)}},
		{group: "empty"},
	}
	got := nest(ops, []slog.Attr{slog.Int("c", 3)})
	flat := map[string]string{}
	flatten("", got, flat)
	want := map[string]string{"a": "1", "g.empty.c": "3", "g.b": "2"}
	if len(flat) != len(want) {
		t.Fatalf("flat = %v, want %v", flat, want)
	}
	for k, v := range want {
		if flat[k] != v {
			t.Errorf("%s = %q, want %q", k, flat[k], v)
		}
	}
	if got := nest([]op{{group: "g"}}, nil); len(got) != 0 {
		t.Errorf("a group with nothing in it must vanish, got %v", got)
	}
}

func TestSpanNameOK_GrammarEdges(t *testing.T) {
	ok := []string{"db.SELECT", "signal:hello.greeted", "GET /api/people/{id}", "HTTP /x", "_OTHER", strings.Repeat("a", 128)}
	bad := []string{"", "get /x", "GET  /x", "GET x", "GE /x", "GETTINGSOMEX /x", "signal hello", ".x", strings.Repeat("a", 129), "GET /x y"}
	for _, n := range ok {
		if !spanNameOK(n) {
			t.Errorf("%q should be accepted", n)
		}
	}
	for _, n := range bad {
		if spanNameOK(n) {
			t.Errorf("%q should be rejected", n)
		}
	}
}

func TestKeyDenied_SuffixAfterUnderscoreOnly(t *testing.T) {
	cases := map[string]bool{
		"email": true, "person.email": true, "Http-Request-Body": true, "user_agent": false,
		"emailer": false, "db.name": false, "session_id": false, "my_token": true,
	}
	for k, want := range cases {
		if got := keyDenied(k); got != want {
			t.Errorf("keyDenied(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestParseResourceAttrs_SkipsMalformedPairs(t *testing.T) {
	got := parseResourceAttrs("deployment.environment=prod, bad ,team=a%20b,=x")
	if got["deployment.environment"] != "prod" || got["team"] != "a b" || len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestCollect_RunsGaugeObserversOnlyWithAnObserver(t *testing.T) {
	var buf strings.Builder
	tel, err := New(context.Background(), Options{
		Level: slog.LevelInfo, Format: FormatJSON, Instance: "a", Version: "v1", Stdout: &buf,
		Clock: clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), Environ: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tel.Collect(context.Background()); err != nil {
		t.Errorf("Collect without a reader must be a no-op, got %v", err)
	}
	if err := tel.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type sinkObserver struct {
	mu       sync.Mutex
	measures []Measurement
	records  []LogRecord
}

func (o *sinkObserver) Record(r LogRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = append(o.records, r)
}
func (o *sinkObserver) Span(SpanData) {}
func (o *sinkObserver) Measurement(m Measurement) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.measures = append(o.measures, m)
}
func (o *sinkObserver) Violation(Violation) {}

func TestCollect_GaugeSamplesReachObserverAndPanicsAreRecovered(t *testing.T) {
	obs := &sinkObserver{}
	var buf strings.Builder
	tel, err := New(context.Background(), Options{
		Level: slog.LevelInfo, Format: FormatJSON, Instance: "a", Version: "v1", Stdout: &buf,
		Clock: clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), Observer: obs,
		Environ: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	m, err := tel.Meter("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Gauge("test.level", func(context.Context) []Sample {
		return []Sample{{Value: 2, Attrs: []Attr{Token("state", "idle")}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Gauge("test.boom", func(context.Context) []Sample { panic("observer bug") }); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := tel.Collect(context.Background()); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	levels := 0
	for _, me := range obs.measures {
		if me.Instrument == "test.level" && me.Value == 2 && me.Attrs["state"] == "idle" {
			levels++
		}
	}
	if levels != 2 {
		t.Errorf("gauge samples seen = %d, want 2", levels)
	}
	panics := 0
	for _, r := range obs.records {
		if r.Level == slog.LevelError && r.Attrs["instrument"] == "test.boom" {
			panics++
		}
	}
	if panics != 1 {
		t.Errorf("panic log lines = %d, want 1 (once per instrument)", panics)
	}
}

func TestLogValueCap_OnlyClientStackGetsStackCap(t *testing.T) {
	for key, want := range map[string]int{
		"client.stack":   MaxStackLogBytes,
		"client.message": MaxLogValueBytes,
		"stack":          MaxLogValueBytes,
		"":               MaxLogValueBytes,
	} {
		if got := logValueCap(key); got != want {
			t.Errorf("logValueCap(%q) = %d, want %d", key, got, want)
		}
	}
}

// lockedBuf is a bytes.Buffer safe for the exporters' goroutines to write through the logger.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p) //nolint:wrapcheck // bytes.Buffer.Write never fails
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// newExportingTelemetry builds a Telemetry over env with stdout captured, closed at cleanup.
func newExportingTelemetry(t *testing.T, env map[string]string) (*Telemetry, *lockedBuf) {
	t.Helper()
	buf := &lockedBuf{}
	// The SDK reads these two from the process environment, not from Options.Environ.
	for _, k := range []string{"OTEL_METRIC_EXPORT_INTERVAL", "OTEL_METRIC_EXPORT_TIMEOUT"} {
		if v, ok := env[k]; ok {
			t.Setenv(k, v)
		}
	}
	tel, err := New(context.Background(), Options{
		Level:    slog.LevelDebug,
		Format:   FormatJSON,
		Instance: "test-copy",
		Version:  "v1.2.3",
		Stdout:   buf,
		Clock:    clock.NewFake(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)),
		Environ: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tel.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return tel, buf
}

// TestExport_AcrossIdleGaps_NeverReusesAConnectionTheServerMayClose models the stale
// keep-alive race: the collector closes an idle connection at the moment the exporter
// reuses it, which Go does not retry for a POST and which surfaces as EOF. The receiver here
// kills any request that arrives on a connection that has already served one, so the export
// only succeeds when the exporter's idle timeout is shorter than the gap between exports.
func TestExport_AcrossIdleGaps_NeverReusesAConnectionTheServerMayClose(t *testing.T) {
	cases := []struct {
		name        string
		idle        time.Duration
		wantReused  bool // the dead-on-reuse receiver is hit, so exports fail
		wantAllGood bool
	}{
		{"idle timeout shorter than the export gap", 5 * time.Millisecond, false, true},
		{"the SDK's own 90 s idle timeout (the bug)", 90 * time.Second, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := idleConnTimeout
			idleConnTimeout = tc.idle
			t.Cleanup(func() { idleConnTimeout = old })

			type connKey struct{}
			var (
				killed   atomic.Int64
				served   atomic.Int64
				requests = make(chan struct{}, 64)
			)
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					http.Error(w, "read", http.StatusBadRequest)
					return
				}
				n, ok := r.Context().Value(connKey{}).(*atomic.Int64)
				if !ok {
					t.Error("connection counter missing from the request context")
					return
				}
				if n.Add(1) > 1 {
					// The server's idle timeout fired just as the client reused the connection.
					hj, ok := w.(http.Hijacker)
					if !ok {
						t.Error("not a Hijacker")
						return
					}
					c, _, err := hj.Hijack()
					if err != nil {
						t.Errorf("Hijack: %v", err)
						return
					}
					killed.Add(1)
					_ = c.Close() //nolint:errcheck // the point is to drop the connection
					requests <- struct{}{}
					return
				}
				served.Add(1)
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
				requests <- struct{}{}
			}))
			srv.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context {
				return context.WithValue(ctx, connKey{}, new(atomic.Int64))
			}
			srv.Start()
			t.Cleanup(srv.Close)

			tel, buf := newExportingTelemetry(t, map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": srv.URL + "/v1/metrics",
				// Real-time margin: the client drops an idle connection after 5 ms, and the next
				// export starts about 200 ms after the last, so the pooled connection is gone by
				// a factor of 40 even on a loaded machine. The only way this could flake is a
				// goroutine descheduled for 200 ms inside the 5 ms window, and then the failure
				// is a false alarm, never a hidden one.
				"OTEL_METRIC_EXPORT_INTERVAL": "200", // ms: four exports across idle gaps of ~200 ms
			})
			m, err := tel.Meter("module.test")
			if err != nil {
				t.Fatal(err)
			}
			c, err := m.Counter("bms.test.idle_gap")
			if err != nil {
				t.Fatal(err)
			}
			c.Add(context.Background(), 1)

			for i := range 4 {
				select {
				case <-requests:
				case <-time.After(10 * time.Second):
					t.Fatalf("export %d never reached the receiver", i+1)
				}
			}
			failed := strings.Contains(buf.String(), "telemetry export failed")
			if tc.wantAllGood && (failed || killed.Load() != 0) {
				t.Errorf("exports failed across idle gaps: killed=%d served=%d log=%s", killed.Load(), served.Load(), buf.String())
			}
			if tc.wantReused && killed.Load() == 0 {
				t.Errorf("control: a long idle timeout should have reused a pooled connection (killed=0, served=%d)", served.Load())
			}
		})
	}
}

func TestExporterClient_Defaults_ShortIdleTimeoutAndDefaultTransport(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	c, err := exporterClient(none, "metrics")
	if err != nil {
		t.Fatalf("exporterClient: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.Transport)
	}
	if tr.IdleConnTimeout != idleConnTimeout || idleConnTimeout >= time.Minute {
		t.Errorf("IdleConnTimeout = %v (package default %v), want the short one", tr.IdleConnTimeout, idleConnTimeout)
	}
	if tr.Proxy == nil {
		t.Error("proxy from environment dropped")
	}
	if tr.TLSClientConfig != nil && (tr.TLSClientConfig.RootCAs != nil || len(tr.TLSClientConfig.Certificates) > 0) {
		t.Error("TLS trust or client certificate set with no certificate variable")
	}
	if tr == http.DefaultTransport {
		t.Error("the shared default transport must not be mutated or reused")
	}
	if c.Timeout != exportTimeout {
		t.Errorf("Timeout = %v, want %v", c.Timeout, exportTimeout)
	}
}

func TestExporterClient_TimeoutVariable_SignalWinsOverGeneric(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want time.Duration
	}{
		{"generic", map[string]string{"OTEL_EXPORTER_OTLP_TIMEOUT": "2500"}, 2500 * time.Millisecond},
		{"signal wins", map[string]string{"OTEL_EXPORTER_OTLP_TIMEOUT": "2500", "OTEL_EXPORTER_OTLP_METRICS_TIMEOUT": "700"}, 700 * time.Millisecond},
		{"other signal ignored", map[string]string{"OTEL_EXPORTER_OTLP_LOGS_TIMEOUT": "700"}, exportTimeout},
		{"not a number", map[string]string{"OTEL_EXPORTER_OTLP_TIMEOUT": "soon"}, exportTimeout},
		{"not positive", map[string]string{"OTEL_EXPORTER_OTLP_TIMEOUT": "0"}, exportTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := exporterClient(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }, "metrics")
			if err != nil {
				t.Fatal(err)
			}
			if c.Timeout != tc.want {
				t.Errorf("Timeout = %v, want %v", c.Timeout, tc.want)
			}
		})
	}
}

// TestNew_CertificateVariable_StillHonouredWithCustomClient proves that giving the exporters
// their own HTTP client (which makes the SDK ignore OTEL_EXPORTER_OTLP_CERTIFICATE) did not
// drop the variable: an export to a TLS receiver with a private CA succeeds only when the
// CA file named by the variable is trusted.
func TestNew_CertificateVariable_StillHonouredWithCustomClient(t *testing.T) {
	var hits atomic.Int64
	requests := make(chan struct{}, 16)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		requests <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	cl, err := exporterClient(func(k string) (string, bool) {
		if k == "OTEL_EXPORTER_OTLP_CERTIFICATE" {
			return caFile, true
		}
		return "", false
	}, "metrics")
	if err != nil {
		t.Fatalf("exporterClient: %v", err)
	}
	tr, ok := cl.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Fatal("OTEL_EXPORTER_OTLP_CERTIFICATE did not reach the transport")
	}

	tel, buf := newExportingTelemetry(t, map[string]string{
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": srv.URL + "/v1/metrics",
		"OTEL_EXPORTER_OTLP_CERTIFICATE":      caFile,
		"OTEL_METRIC_EXPORT_INTERVAL":         "50",
	})
	m, err := tel.Meter("module.test")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Counter("bms.test.tls")
	if err != nil {
		t.Fatal(err)
	}
	c.Add(context.Background(), 1)
	select {
	case <-requests:
	case <-time.After(10 * time.Second):
		t.Fatalf("no export reached the TLS receiver; log: %s", buf.String())
	}
	if strings.Contains(buf.String(), "telemetry export failed") {
		t.Errorf("export failed: %s", buf.String())
	}
}

func TestExporterClient_BadCertificateFile_ReturnsInvalid(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"malformed": bad, "missing": filepath.Join(t.TempDir(), "absent.pem")} {
		t.Run(name, func(t *testing.T) {
			_, err := exporterClient(func(k string) (string, bool) {
				if k == "OTEL_EXPORTER_OTLP_METRICS_CERTIFICATE" {
					return path, true
				}
				return "", false
			}, "metrics")
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if strings.Contains(err.Error(), path) {
				t.Errorf("the error echoes the path: %v", err)
			}
		})
	}
}

// selfSigned returns a PEM certificate and key for an invented host, written to dir.
func selfSigned(t *testing.T, dir, name string) (certFile, keyFile string, cert *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, name+".crt")
	keyFile = filepath.Join(dir, name+".key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile, cert
}

func mapGet(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestExporterClient_ClientCertificatePairs(t *testing.T) {
	dir := t.TempDir()
	gCert, gKey, gParsed := selfSigned(t, dir, "generic")
	sCert, sKey, sParsed := selfSigned(t, dir, "specific")
	mismatch := gCert // a certificate with the other pair's key
	const (
		cert    = "OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE"
		key     = "OTEL_EXPORTER_OTLP_CLIENT_KEY"
		mcert   = "OTEL_EXPORTER_OTLP_METRICS_CLIENT_CERTIFICATE"
		mkey    = "OTEL_EXPORTER_OTLP_METRICS_CLIENT_KEY"
		invalid = "invalid"
		none    = "none"
	)
	cases := []struct {
		name string
		env  map[string]string
		want string // "generic", "specific", none, or invalid
	}{
		{"generic pair", map[string]string{cert: gCert, key: gKey}, "generic"},
		{"signal pair alone", map[string]string{mcert: sCert, mkey: sKey}, "specific"},
		{"complete signal pair overrides generic", map[string]string{cert: gCert, key: gKey, mcert: sCert, mkey: sKey}, "specific"},
		{"A: signal certificate alone keeps the generic pair", map[string]string{cert: gCert, key: gKey, mcert: sCert}, "generic"},
		{"A: signal key alone keeps the generic pair", map[string]string{cert: gCert, key: gKey, mkey: sKey}, "generic"},
		{"B: signal certificate with a generic key is no pair", map[string]string{mcert: sCert, key: gKey}, none},
		{"B: generic certificate with a signal key is no pair", map[string]string{cert: gCert, mkey: sKey}, none},
		{"lone certificate is ignored", map[string]string{cert: gCert}, none},
		{"mismatched pair", map[string]string{cert: mismatch, key: sKey}, invalid},
		{"mismatched signal pair that is used", map[string]string{cert: gCert, key: gKey, mcert: gCert, mkey: sKey}, invalid},
		{"unused generic pair is not read", map[string]string{cert: filepath.Join(dir, "absent.crt"), key: gKey, mcert: sCert, mkey: sKey}, "specific"},
		{"missing file in the used pair", map[string]string{cert: filepath.Join(dir, "absent.crt"), key: gKey}, invalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := tlsConfig(mapGet(tc.env), "metrics")
			if tc.want == invalid {
				if !errs.Is(err, errs.Invalid) {
					t.Fatalf("err = %v, want Invalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("tlsConfig: %v", err)
			}
			if tc.want == none {
				if cfg != nil && len(cfg.Certificates) != 0 {
					t.Fatalf("got a client certificate, want none")
				}
				return
			}
			if cfg == nil || len(cfg.Certificates) != 1 {
				t.Fatalf("Certificates = %v, want one", cfg)
			}
			want := gParsed
			if tc.want == "specific" {
				want = sParsed
			}
			if !bytes.Equal(cfg.Certificates[0].Certificate[0], want.Raw) {
				t.Errorf("wrong client certificate used, want the %s pair", tc.want)
			}
		})
	}
}

func TestExporterClient_ValidClientPair_SetsCertificatesOnTransport(t *testing.T) {
	dir := t.TempDir()
	c, k, _ := selfSigned(t, dir, "pair")
	cl, err := exporterClient(mapGet(map[string]string{
		"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE": c,
		"OTEL_EXPORTER_OTLP_CLIENT_KEY":         k,
	}), "traces")
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := cl.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || len(tr.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("client certificate did not reach the transport: %+v", cl.Transport)
	}
}

func TestExporterClient_SignalCertificate_BeatsGeneric(t *testing.T) {
	dir := t.TempDir()
	gFile, _, gParsed := selfSigned(t, dir, "genericca")
	sFile, _, sParsed := selfSigned(t, dir, "specificca")
	cfg, err := tlsConfig(mapGet(map[string]string{
		"OTEL_EXPORTER_OTLP_CERTIFICATE":         gFile,
		"OTEL_EXPORTER_OTLP_METRICS_CERTIFICATE": sFile,
	}), "metrics")
	if err != nil {
		t.Fatal(err)
	}
	want := x509.NewCertPool()
	want.AddCert(sParsed)
	if cfg == nil || !cfg.RootCAs.Equal(want) {
		t.Error("the signal-specific CERTIFICATE did not win")
	}
	generic := x509.NewCertPool()
	generic.AddCert(gParsed)
	if cfg.RootCAs.Equal(generic) {
		t.Error("the generic CERTIFICATE was used")
	}
	// Another signal still gets the generic one.
	cfg, err = tlsConfig(mapGet(map[string]string{
		"OTEL_EXPORTER_OTLP_CERTIFICATE":         gFile,
		"OTEL_EXPORTER_OTLP_METRICS_CERTIFICATE": sFile,
	}), "logs")
	if err != nil || cfg == nil || !cfg.RootCAs.Equal(generic) {
		t.Errorf("logs should use the generic CERTIFICATE: err=%v", err)
	}
}

func TestExporterClient_UnreadableFile_CodeNamesCauseNotPath(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{"missing": filepath.Join(dir, "absent.pem"), "unreadable": dir}
	for want, path := range cases {
		t.Run(want, func(t *testing.T) {
			_, err := tlsConfig(mapGet(map[string]string{"OTEL_EXPORTER_OTLP_CERTIFICATE": path}), "metrics")
			var fe *validate.FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("err = %v, want a FieldError", err)
			}
			if fe.Code != want {
				t.Errorf("code = %q, want %q", fe.Code, want)
			}
			if strings.Contains(err.Error(), path) {
				t.Errorf("the error echoes the path: %v", err)
			}
		})
	}
}
