// Package telemetry builds the process logger, tracer and meter (D20) behind interfaces
// this project owns, exports over OTLP when OTEL_* names a collector, and enforces the
// privacy policy on everything emitted. It sits at the bottom of the core graph: it
// imports lib and the OpenTelemetry SDK, and no other project package (not even model; the
// ID constructor is generic). It is the only package allowed to import go.opentelemetry.io
// (depguard rule otel-seam).
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"go.opentelemetry.io/contrib/bridges/otelslog"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Format is how log lines are written to Stdout (config.md: log.format).
type Format string

// The log formats.
const (
	FormatJSON Format = "json" // default; what a deployment emits (D20)
	FormatText Format = "text" // a developer's terminal
)

// Constants pinned by tests (Settings: not configurable, with the reason each is not).
const (
	exportTimeout       = 10 * time.Second
	metricInterval      = 60 * time.Second
	exportWarnEvery     = time.Minute
	violationWarnEvery  = time.Hour
	sdkErrorWarnEvery   = time.Minute
	maxTrackedWarnings  = 1024
	closeGraceOnFailure = 5 * time.Second
)

var instanceRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Options is what New needs. app copies config.Log and config.Instance.Name into it
// (config.md decision 12), so telemetry is built in tests with no environment at all.
type Options struct {
	// Level is the minimum level written to Stdout and exported. One of slog.LevelDebug,
	// LevelInfo, LevelWarn, LevelError. At LevelDebug, records also carry source file and
	// line.
	Level slog.Level
	// Format is json or text.
	Format Format
	// Instance is this copy's name (config.Instance.Name): resource attribute
	// service.instance.id and the log attribute "instance". Grammar as config.md:
	// "[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?".
	Instance string
	// Version is the build's version: resource attribute service.version. A token (see
	// Token), at most 64 bytes.
	Version string
	// Stdout receives log lines. Must not be nil.
	Stdout io.Writer
	// Clock drives the rate limits inside this package (violation warnings, export failure
	// warnings). Must not be nil.
	Clock clock.Clock
	// Environ looks up the OTEL_* variables this package reads. nil means os.LookupEnv.
	// Tests pass a map lookup.
	Environ func(key string) (string, bool)
	// Observer receives a copy of everything emitted, after the policy was applied. nil in
	// production; telemetrytest sets it. See Observer.
	Observer Observer
}

// Validate fails with errs.Invalid joining one validate.FieldError per problem, fields
// "log.level", "log.format", "instance.name", "telemetry.version", "telemetry.stdout",
// "telemetry.clock" (F1–F5).
func (o Options) Validate() error {
	const op = "telemetry.Options.Validate"
	acc := validate.Errors{Op: op}
	switch o.Level {
	case slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError:
	default:
		acc.Add(fieldErrParams("", "log.level", validate.CodeOneOf,
			map[string]string{"options": "debug, info, warn, error"},
			"must be one of: debug, info, warn, error"))
	}
	if o.Format != FormatJSON && o.Format != FormatText {
		acc.Add(fieldErrParams("", "log.format", validate.CodeOneOf,
			map[string]string{"options": "json, text"}, "must be one of: json, text"))
	}
	if !instanceRE.MatchString(o.Instance) {
		acc.Add(fieldErr("", "instance.name", "malformed",
			"must be 1 to 64 characters of a-z, 0-9 and hyphen, starting and ending with a letter or digit"))
	}
	switch {
	case o.Version == "":
		acc.Add(fieldErr("", "telemetry.version", validate.CodeRequired, "must not be empty"))
	case len(o.Version) > 64 || !IsToken(o.Version):
		acc.Add(fieldErr("", "telemetry.version", "malformed", "must be a token of at most 64 bytes"))
	}
	if o.Stdout == nil {
		acc.Add(fieldErr("", "telemetry.stdout", validate.CodeRequired, "must not be nil"))
	}
	if o.Clock == nil {
		acc.Add(fieldErr("", "telemetry.clock", validate.CodeRequired, "must not be nil"))
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Telemetry is the process's logger, tracer and meter. Safe for concurrent use from New
// until Close returns.
type Telemetry struct {
	obs   Observer
	clk   clock.Clock
	level slog.Level

	tp     *sdktrace.TracerProvider
	mp     *sdkmetric.MeterProvider
	lp     *sdklog.LoggerProvider
	manual *sdkmetric.ManualReader // only with an Observer: lets Collect run gauges
	sink   *sink
	logger *slog.Logger

	exporting [len(signalNames)]bool

	mu     sync.Mutex
	closed bool

	violationGate *gate
	exportGate    *gate
	sdkGate       *gate

	exportFailures   Counter
	dropped          Counter
	policyViolations Counter
	logWriteFailures Counter
}

// New validates opts, reads the OTEL_* variables, builds the resource (service.name "bms"
// unless OTEL_SERVICE_NAME is set, service.version, service.instance.id, merged with
// OTEL_RESOURCE_ATTRIBUTES), the logger, the tracer provider and the meter provider, and
// attaches OTLP/HTTP exporters for each signal that has an endpoint. It never connects to
// the collector: exporters dial on their first export. It fails with errs.Invalid naming
// the variable for an unsupported protocol, a malformed endpoint, an unsupported sampler
// or a bad OTEL_SDK_DISABLED value (F6–F9, F11), and errs.Canceled or errs.Timeout if ctx
// ends first (F10). The caller owns the result and must Close it.
func New(ctx context.Context, opts Options) (*Telemetry, error) {
	const op = "telemetry.New"
	if err := ctx.Err(); err != nil {
		return nil, errs.E(op, errs.KindOf(err), err)
	}
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	environ := opts.Environ
	if environ == nil {
		environ = os.LookupEnv
	}
	env, err := readEnv(environ)
	if err != nil {
		return nil, err
	}

	// The SDK's global logger prints the raw value of malformed variables, among them
	// OTEL_EXPORTER_OTLP_HEADERS, which may carry a credential: silence it before any
	// exporter is built. Problems reach the logger through the error handler instead.
	otel.SetLogger(logr.Discard())
	t := &Telemetry{
		obs: opts.Observer, clk: opts.Clock, level: opts.Level,
		violationGate: newGate(opts.Clock, violationWarnEvery),
		exportGate:    newGate(opts.Clock, exportWarnEvery),
		sdkGate:       newGate(opts.Clock, sdkErrorWarnEvery),
	}
	res, err := buildResource(ctx, opts, env)
	if err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	if err := t.build(ctx, opts, env, res, environ); err != nil {
		t.shutdownQuietly(ctx)
		return nil, err
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(t.sdkError))
	return t, nil
}

// buildResource: service.name, service.version, service.instance.id, then the
// environment's attributes (environment wins), plus the SDK's telemetry.sdk.*. It reads no
// host or process information (decision 15) and not the process environment.
func buildResource(ctx context.Context, opts Options, env envConfig) (*resource.Resource, error) {
	attrs := map[string]string{
		"service.name":        "bms",
		"service.version":     opts.Version,
		"service.instance.id": opts.Instance,
	}
	for k, v := range env.resource {
		attrs[k] = v
	}
	if env.serviceName != "" {
		attrs["service.name"] = env.serviceName
	}
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, attribute.String(k, v))
	}
	res, err := resource.New(ctx, resource.WithTelemetrySDK(), resource.WithAttributes(kvs...))
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}
	return res, nil
}

// build creates the providers and the logger in the order of Behavior "New, step by step".
func (t *Telemetry) build(ctx context.Context, opts Options, env envConfig, res *resource.Resource,
	environ func(string) (string, bool)) error {
	const op = "telemetry.New"
	_, bspTimeoutSet := environ("OTEL_BSP_EXPORT_TIMEOUT")
	_, blrpTimeoutSet := environ("OTEL_BLRP_EXPORT_TIMEOUT")
	_, intervalSet := environ("OTEL_METRIC_EXPORT_INTERVAL")
	_, mtimeoutSet := environ("OTEL_METRIC_EXPORT_TIMEOUT")

	// Phase 1: every certificate check and every exporter, before any provider exists. A
	// failure here has started no goroutine and dialed nothing (exporters are lazy), so
	// there is nothing to flush and "New never dials" holds on the failure path too.
	for _, sig := range signalNames {
		if _, ok := env.endpoints[sig]; ok {
			if err := checkTLSFiles(environ, sig); err != nil {
				return err
			}
		}
	}
	var (
		texp sdktrace.SpanExporter
		mexp sdkmetric.Exporter
		lexp sdklog.Exporter
	)
	releaseExporters := func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeGraceOnFailure)
		defer cancel()
		if texp != nil {
			_ = texp.Shutdown(sctx) //nolint:errcheck // nothing was exported; cleanup of a failed New
		}
		if mexp != nil {
			_ = mexp.Shutdown(sctx) //nolint:errcheck // nothing was exported; cleanup of a failed New
		}
		if lexp != nil {
			_ = lexp.Shutdown(sctx) //nolint:errcheck // nothing was exported; cleanup of a failed New
		}
	}
	if url, ok := env.endpoints["traces"]; ok {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(url))
		if err != nil {
			return exporterErr(op, "traces", err)
		}
		texp = &spanExporter{inner: exp, t: t}
	}
	if url, ok := env.endpoints["metrics"]; ok {
		exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(url))
		if err != nil {
			releaseExporters()
			return exporterErr(op, "metrics", err)
		}
		mexp = &metricExporter{inner: exp, t: t}
	}
	if url, ok := env.endpoints["logs"]; ok {
		exp, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(url))
		if err != nil {
			releaseExporters()
			return exporterErr(op, "logs", err)
		}
		lexp = &logExporter{inner: exp, t: t}
	}

	// Phase 2: providers.
	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithSampler(sampler(env))}
	if texp != nil {
		bo := []sdktrace.BatchSpanProcessorOption{}
		if !bspTimeoutSet {
			bo = append(bo, sdktrace.WithExportTimeout(exportTimeout))
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(texp, bo...))
		t.exporting[0] = true
	}
	t.tp = sdktrace.NewTracerProvider(tpOpts...)

	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if mexp != nil {
		ro := []sdkmetric.PeriodicReaderOption{}
		if !intervalSet {
			ro = append(ro, sdkmetric.WithInterval(metricInterval))
		}
		if !mtimeoutSet {
			ro = append(ro, sdkmetric.WithTimeout(exportTimeout))
		}
		mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, ro...)))
		t.exporting[1] = true
	}
	if t.obs != nil {
		t.manual = sdkmetric.NewManualReader()
		mpOpts = append(mpOpts, sdkmetric.WithReader(t.manual))
	}
	t.mp = sdkmetric.NewMeterProvider(mpOpts...)

	// Own instruments (before any signal that can violate or fail is used).
	own := t.newMeter("telemetry")
	var err error
	if t.exportFailures, err = own.Counter("bms.telemetry.export_failures", WithUnit("{batch}")); err != nil {
		return err
	}
	if t.dropped, err = own.Counter("bms.telemetry.dropped", WithUnit("{record}")); err != nil {
		return err
	}
	if t.policyViolations, err = own.Counter("bms.telemetry.policy_violations", WithUnit("{violation}")); err != nil {
		return err
	}
	if t.logWriteFailures, err = own.Counter("bms.telemetry.log_write_failures", WithUnit("{record}")); err != nil {
		return err
	}

	// Logs.
	hopts := &slog.HandlerOptions{Level: opts.Level, AddSource: opts.Level <= slog.LevelDebug}
	var stdout slog.Handler
	if opts.Format == FormatText {
		stdout = slog.NewTextHandler(opts.Stdout, hopts)
	} else {
		stdout = slog.NewJSONHandler(opts.Stdout, hopts)
	}
	t.sink = &sink{stdout: stdout}
	if lexp != nil {
		bo := []sdklog.BatchProcessorOption{}
		if !blrpTimeoutSet {
			bo = append(bo, sdklog.WithExportTimeout(exportTimeout))
		}
		t.lp = sdklog.NewLoggerProvider(sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(lexp, bo...)))
		t.sink.bridge = otelslog.NewHandler("bms", otelslog.WithLoggerProvider(t.lp))
		t.exporting[2] = true
	}
	t.logger = slog.New(&policyHandler{t: t}).With(slog.String("instance", opts.Instance))
	return nil
}

func exporterErr(op, signal string, err error) error {
	return errs.E(op, errs.Invalid, fmt.Errorf("%s exporter: %w", signal, err))
}

func sampler(env envConfig) sdktrace.Sampler {
	switch env.sampler {
	case samplerAlwaysOff:
		return sdktrace.NeverSample()
	case samplerRatio:
		return sdktrace.TraceIDRatioBased(env.ratio)
	default:
		return sdktrace.AlwaysSample()
	}
}

// Logger returns the base logger: level and format from Options, "instance" attached,
// trace_id and span_id taken from the ctx of every *Context log call. app derives scoped
// loggers with With (module=<name>, component=<name>). Cannot fail.
func (t *Telemetry) Logger() *slog.Logger { return t.logger }

// Tracer returns a tracer whose spans carry the instrumentation scope name: "api", "db",
// "signals", "jobs", "module.<name>". Fails with errs.Invalid for a name that is not a
// token of at most 64 bytes (F12) and errs.Internal after Close (F13).
func (t *Telemetry) Tracer(name string) (Tracer, error) {
	const op = "telemetry.Telemetry.Tracer"
	if err := t.scopeCheck(op, name); err != nil {
		return nil, err
	}
	return &tracer{t: t, scope: name, tr: t.tp.Tracer(name)}, nil
}

// Meter returns a meter under scope name; same rules as Tracer (F12, F13).
func (t *Telemetry) Meter(name string) (Meter, error) {
	const op = "telemetry.Telemetry.Meter"
	if err := t.scopeCheck(op, name); err != nil {
		return nil, err
	}
	return t.newMeter(name), nil
}

func (t *Telemetry) scopeCheck(op, name string) error {
	if !scopeOK(name) {
		return errs.E(op, errs.Invalid, errors.New("scope name must be a token of at most 64 bytes"))
	}
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return errs.E(op, errs.Internal, errors.New("telemetry is closed"))
	}
	return nil
}

// Exporting reports whether at least one signal has an OTLP endpoint. Cannot fail.
func (t *Telemetry) Exporting() bool {
	for _, e := range t.exporting {
		if e {
			return true
		}
	}
	return false
}

// Collect runs one metric collection now, so gauge observers are called and, with an
// Observer, their samples are reported. Exporters collect on their own interval; this is
// for tests and `bms check`. It is a no-op when nothing collects (no Observer and no
// metric export). Fails with errs.Timeout or errs.Canceled when ctx ends first.
func (t *Telemetry) Collect(ctx context.Context) error {
	const op = "telemetry.Telemetry.Collect"
	if t.manual == nil {
		return nil
	}
	var rm metricdata.ResourceMetrics
	if err := t.manual.Collect(ctx, &rm); err != nil {
		return errs.E(op, kindOfCtx(err, errs.Unavailable), err)
	}
	return nil
}

// Close flushes every provider and shuts them down, each within ctx, and returns their
// errors joined: errs.Timeout or errs.Canceled when ctx ends first (F26). Idempotent: a
// second Close returns nil. After Close, log records still reach Stdout (the stdout handler
// is never closed), and Tracer and Meter fail (F13).
func (t *Telemetry) Close(ctx context.Context) error {
	const op = "telemetry.Telemetry.Close"
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	var joined error
	if t.tp != nil {
		joined = errors.Join(joined, shutdownErr(op+".traces", t.tp.Shutdown(ctx)))
	}
	if t.mp != nil {
		joined = errors.Join(joined, shutdownErr(op+".metrics", t.mp.Shutdown(ctx)))
	}
	if t.lp != nil {
		joined = errors.Join(joined, shutdownErr(op+".logs", t.lp.Shutdown(ctx)))
	}
	if cerr := ctx.Err(); cerr != nil && joined == nil {
		// The context ended before Close was called or while it ran: report it, the flush
		// may be incomplete.
		joined = errs.E(op, errs.KindOf(cerr), cerr)
	}
	return joined
}

// shutdownErr keeps only context errors: a collector that failed to answer was already
// counted and warned about, and must not fail a shutdown (no shutdown ever fails because of
// the backend).
func shutdownErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errs.E(op, errs.KindOf(err), err)
	}
	return nil
}

// shutdownQuietly releases what a failed New built, within a short grace.
func (t *Telemetry) shutdownQuietly(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeGraceOnFailure)
	defer cancel()
	if t.tp != nil {
		_ = t.tp.Shutdown(ctx) //nolint:errcheck // best effort cleanup of a failed New
	}
	if t.mp != nil {
		_ = t.mp.Shutdown(ctx) //nolint:errcheck // best effort cleanup of a failed New
	}
	if t.lp != nil {
		_ = t.lp.Shutdown(ctx) //nolint:errcheck // best effort cleanup of a failed New
	}
}

func kindOfCtx(err error, fallback errs.Kind) errs.Kind {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errs.Timeout
	case errors.Is(err, context.Canceled):
		return errs.Canceled
	default:
		return fallback
	}
}
