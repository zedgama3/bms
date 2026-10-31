package telemetry

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// idleConnTimeout is how long an exporter's HTTP client keeps an idle connection. It is a
// variable only so a test can shorten it. It is deliberately far below the 1 minute idle
// timeout of the OpenTelemetry Collector's (and so Grafana Alloy's) OTLP/HTTP server and
// below the 60 s metric interval: a pooled connection is never old enough for the server to
// be about to close it, so the next export opens a fresh one. The SDK's own client keeps
// idle connections for 90 s, which loses a race against a server closing them at 60 s: Go
// does not retry a POST, and the batch fails with EOF.
var idleConnTimeout = 30 * time.Second

// exporterClient builds the HTTP client one signal's OTLP exporter uses. It exists because
// the SDK offers no option for the idle timeout, and an exporter given a client ignores the
// OTEL_EXPORTER_OTLP_*CERTIFICATE, *CLIENT_CERTIFICATE, *CLIENT_KEY and *TIMEOUT variables,
// so this function applies them itself (signal variant first, then the generic one, as the
// SDK does).
//
// The transport is a fresh one with the settings of the SDK's own (which copies net/http's
// default): proxy from the environment, a 30 s dialer, HTTP/2 where offered, and the same
// handshake and expect-continue timeouts. Only the idle timeout differs. It is built here,
// not cloned from http.DefaultTransport, so another package that changes the default cannot
// weaken the exporters.
//
// It fails with errs.Invalid, naming the variable, when a certificate file is unreadable or
// malformed (F11). It never logs or retains the headers: those stay with the SDK exporter.
func exporterClient(get func(string) (string, bool), signal string) (*http.Client, error) {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	cfg, err := tlsConfig(get, signal)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		tr.TLSClientConfig = cfg
	}
	return &http.Client{Transport: tr, Timeout: requestTimeout(get, signal)}, nil
}

// requestTimeout is the client's overall request timeout: OTEL_EXPORTER_OTLP_<SIGNAL>_TIMEOUT
// or OTEL_EXPORTER_OTLP_TIMEOUT in milliseconds, else the SDK's default (the export
// timeout). A value that is not a whole number is ignored, as the SDK does. A value of 0 or
// less is also ignored here: that is a deliberate difference, because the SDK would then set
// no timeout at all, and an export that can wait forever is what the export timeout exists
// to prevent.
func requestTimeout(get func(string) (string, bool), signal string) time.Duration {
	sig := strings.ToUpper(signal)
	for _, v := range []string{"OTEL_EXPORTER_OTLP_" + sig + "_TIMEOUT", "OTEL_EXPORTER_OTLP_TIMEOUT"} {
		s, ok := get(v)
		if !ok {
			continue
		}
		if ms, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return exportTimeout
}

// tlsConfig reads the certificate variables for signal and returns the TLS settings they
// describe, or nil when none is set. It fails with errs.Invalid, naming the variable, when a
// file that will be used is unreadable or malformed (F11). The SDK only logs such a failure
// and then exports without the certificate, which hides a broken deployment; New refuses
// instead. Paths and file contents are never echoed.
//
// The root certificate is the signal-specific CERTIFICATE, else the generic one. The client
// certificate is resolved as a pair, as the SDK does: the generic CLIENT_CERTIFICATE and
// CLIENT_KEY, overridden by the signal-specific pair when both of its variables are set. A
// certificate is never combined with a key from the other level, and a lone certificate or
// key (no complete pair) is ignored.
func tlsConfig(get func(string) (string, bool), signal string) (*tls.Config, error) {
	const op = "telemetry.New"
	sig := strings.ToUpper(signal)
	generic := func(name string) string { return "OTEL_EXPORTER_OTLP_" + name }
	specific := func(name string) string { return "OTEL_EXPORTER_OTLP_" + sig + "_" + name }
	value := func(v string) (string, bool) {
		p, ok := get(v)
		p = strings.TrimSpace(p)
		return p, ok && p != ""
	}
	readFile := func(v, path string) ([]byte, error) {
		b, err := os.ReadFile(path) //nolint:gosec // operator-supplied path, read once at start
		if err != nil {
			code := "unreadable"
			switch {
			case errors.Is(err, fs.ErrNotExist):
				code = "missing"
			case errors.Is(err, fs.ErrPermission):
				code = "permission"
			}
			return nil, fieldErr(op, v, code, fmt.Sprintf("%s exporter: the file cannot be read", signal))
		}
		return b, nil
	}
	var cfg *tls.Config
	ensure := func() *tls.Config {
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		return cfg
	}

	for _, v := range []string{specific("CERTIFICATE"), generic("CERTIFICATE")} {
		path, ok := value(v)
		if !ok {
			continue
		}
		pem, err := readFile(v, path)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fieldErr(op, v, "malformed", fmt.Sprintf("%s exporter: the file holds no PEM certificate", signal))
		}
		ensure().RootCAs = pool
		break
	}

	for _, names := range [][2]string{
		{specific("CLIENT_CERTIFICATE"), specific("CLIENT_KEY")},
		{generic("CLIENT_CERTIFICATE"), generic("CLIENT_KEY")},
	} {
		certPath, okc := value(names[0])
		keyPath, okk := value(names[1])
		if !okc || !okk {
			continue
		}
		certPEM, err := readFile(names[0], certPath)
		if err != nil {
			return nil, err
		}
		keyPEM, err := readFile(names[1], keyPath)
		if err != nil {
			return nil, err
		}
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fieldErr(op, names[0], "malformed",
				fmt.Sprintf("%s exporter: the client certificate and key do not form a pair", signal))
		}
		ensure().Certificates = []tls.Certificate{pair}
		break
	}
	return cfg, nil
}
