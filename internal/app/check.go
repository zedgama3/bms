package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// skipSchema is the module.SchemaChecker openapi and check use for Build: CheckSchema
// returns nil (the schema is reported by Status instead, decision 7).
type skipSchema struct{}

// CheckSchema returns nil: nothing is checked.
func (skipSchema) CheckSchema(context.Context, string, fs.FS) error { return nil }

// noDatabase is the api.Database of openapi and check: no database behind it, always
// ready, always read-write.
type noDatabase struct{}

func (noDatabase) Ping(context.Context) error { return nil }
func (noDatabase) ReadOnly() bool             { return false }

// statusRank orders the statuses `check` can record: a configuration problem must be
// fixed first, then a schema one, then a runtime failure.
var statusRank = map[int]int{ExitOK: 0, ExitFailure: 1, ExitSchema: 2, ExitUsage: 3}

// checkReport accumulates what `check` found.
type checkReport struct {
	out      io.Writer
	status   int
	problems int
}

// record notes a problem with status, keeping the worst.
func (r *checkReport) record(status int) {
	r.problems++
	if statusRank[status] > statusRank[r.status] {
		r.status = status
	}
}

func (r *checkReport) printf(format string, args ...any) {
	say(r.out, format, args...)
}

// indent returns msg with every line after the first indented, for a multi-line error.
func indent(msg string) string {
	return strings.ReplaceAll(strings.TrimSpace(msg), "\n", "\n    ")
}

// runCheck reports configuration, telemetry, modules, database, schema, API and UI
// without starting, and returns the status serve would ("check, step by step").
func runCheck(ctx context.Context, p *process) int {
	r := &checkReport{out: p.stdout}
	cfg, ok := r.config(ctx, p)
	if !ok {
		return r.status
	}
	p.warnDebugToStderr(cfg)
	a := newAssembly(p, cfg)

	if err := a.startTelemetry(ctx, p.lookup, p.stderr); err != nil {
		r.printf("telemetry: %s\n", indent(problemText(err)))
		r.record(statusOf(err, ExitUsage))
		// Report the rest anyway: continue with telemetry that reads no OTEL_* variable
		// and writes nowhere.
		if derr := a.startTelemetry(ctx, noEnvironment, io.Discard); derr != nil {
			r.printf("telemetry: cannot continue without it: %s\n", indent(problemText(derr)))
		}
	} else {
		r.printf("log: %s %s\n", cfg.Log.Format, strings.ToLower(cfg.Log.Level.String()))
		export := "off"
		if a.tel.Exporting() {
			export = "on"
		}
		r.printf("OTLP export: %s\n", export)
	}
	r.printf("PG* variables set: %s\n", pgNames(p.environ))

	resolved := r.modules(ctx, p, a)
	if a.tel == nil {
		r.printf("database: skipped (telemetry is not usable)\n")
	} else {
		r.database(ctx, a)
		r.schema(ctx, p, a)
		if resolved {
			r.api(ctx, a)
		} else {
			r.printf("api: skipped (the modules did not resolve)\n")
		}
	}
	r.auditSpool(cfg)
	r.ui()

	if err := a.closeQuiet(ctx); err != nil {
		r.printf("shutdown: %s\n", indent(problemText(err)))
		r.record(ExitFailure)
	}
	if r.problems == 0 {
		r.printf("result: ok\n")
	} else {
		r.printf("result: %d problem(s)\n", r.problems)
	}
	return r.status
}

// problemText is the text of err as the commands print it: field lines where present.
func problemText(err error) string {
	var b strings.Builder
	writeProblem(&b, err)
	return b.String()
}

// config loads and prints the configuration (step 1). ok is false when it did not load.
func (r *checkReport) config(ctx context.Context, p *process) (config.Config, bool) {
	cfg, err := p.loadConfig(ctx)
	if err != nil {
		fields := validate.Fields(err)
		if len(fields) == 0 {
			r.printf("configuration: %s\n", indent(err.Error()))
			r.record(statusOf(err, ExitUsage))
			r.printf("result: 1 problem(s)\n")
			return cfg, false
		}
		r.printf("configuration: %d problem(s)\n", len(fields))
		for _, fe := range fields {
			r.printf("  %s\n", fe.Error())
			r.record(ExitUsage)
		}
		r.printf("result: %d problem(s)\n", r.problems)
		return cfg, false
	}
	r.printf("configuration\n")
	for _, k := range config.Keys() {
		line := fmt.Sprintf("  %s = %s", k.Env, valueOf(cfg, k))
		if k.Generated && valueOf(cfg, k) == "[unset]" {
			line += "  (run bms migrate)"
		}
		r.printf("%s\n", line)
	}
	if debugLogging(cfg) {
		r.printf("log level: debug (logs may contain personal data)\n")
	}
	if cfg.Session.Key.IsSet() && requireSessionKey(cfg) != nil {
		r.printf("session key: the documentation placeholder; unset it and run `bms migrate`, or set one of your own\n")
		r.record(ExitUsage)
	}
	if !cfg.Session.Key.IsSet() {
		r.printf("session key: not set; run `bms migrate` on a new installation, or restore %s from your backup\n", cfg.Secrets.File)
		r.record(ExitUsage)
	}
	state := "absent"
	if _, err := os.Stat(cfg.Secrets.File); err == nil {
		state = "present"
	}
	r.printf("secrets file: %s (%s)\n", cfg.Secrets.File, state)
	return cfg, true
}

// valueOf renders the configured value of k for the report: a secret as [redacted] or
// [unset], never its content.
func valueOf(cfg config.Config, k config.Key) string {
	text := ""
	switch k.Name {
	case "instance.name":
		text = cfg.Instance.Name
	case "db.url":
		return secretState(cfg.DB.URL.IsSet())
	case "db.max_conns":
		text = strconv.Itoa(cfg.DB.MaxConns)
	case "db.connect_timeout":
		text = cfg.DB.ConnectTimeout.String()
	case "db.query_timeout":
		text = cfg.DB.QueryTimeout.String()
	case "http.addr":
		text = cfg.HTTP.Addr
	case "http.request_timeout":
		text = cfg.HTTP.RequestTimeout.String()
	case "http.shutdown_timeout":
		text = cfg.HTTP.ShutdownTimeout.String()
	case "http.max_body_bytes":
		text = strconv.FormatInt(cfg.HTTP.MaxBodyBytes, 10)
	case "http.trusted_proxies":
		parts := make([]string, len(cfg.HTTP.TrustedProxies))
		for i, pr := range cfg.HTTP.TrustedProxies {
			parts[i] = pr.String()
		}
		text = strings.Join(parts, ",")
	case "http.allow_plaintext":
		text = strconv.FormatBool(cfg.HTTP.AllowPlaintext)
	case "session.key":
		return secretState(cfg.Session.Key.IsSet())
	case "settings.sealing_key":
		return secretState(cfg.Settings.SealingKey.IsSet())
	case "secrets.file":
		text = cfg.Secrets.File
	case "log.level":
		text = strings.ToLower(cfg.Log.Level.String())
	case "log.format":
		text = string(cfg.Log.Format)
	case "data.dir":
		text = cfg.Data.Dir
	case "modules.disabled":
		text = strings.Join(cfg.Modules.Disabled, ",")
	default:
		return "[unknown key]"
	}
	if text == "" {
		return "[unset]"
	}
	return text
}

func secretState(set bool) string {
	if set {
		return "[redacted]"
	}
	return "[unset]"
}

// pgNames lists the names, never the values, of the PG* variables set.
func pgNames(environ []string) string {
	var names []string
	for _, e := range environ {
		name, _, ok := strings.Cut(e, "=")
		if ok && strings.HasPrefix(name, "PG") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// modules prints enablement and resolves the registry (step 3). It reports whether the
// plan resolved.
func (r *checkReport) modules(ctx context.Context, p *process, a *assembly) bool {
	off := map[string]bool{}
	for _, d := range a.cfg.Modules.Disabled {
		off[strings.TrimSpace(d)] = true
	}
	r.printf("modules\n")
	all := p.registry.Manifests()
	for _, m := range all {
		state := "enabled"
		if off[m.Name] {
			state = "disabled"
		}
		schema := "no"
		if m.Migrations != nil {
			schema = "yes"
		}
		r.printf("  %s  %s  schema %s\n", m.Name, state, schema)
	}
	enabled, err := enabledModules(p.registry, a.cfg.Modules.Disabled)
	if err != nil {
		r.printf("%s\n", indent(problemText(err)))
		r.record(ExitUsage)
		// The core's migration sets are still known, so the schema section can run.
		if derr := a.declare(); derr != nil {
			r.printf("  %s\n", indent(problemText(derr)))
			r.record(ExitFailure)
		}
		return false
	}
	if err := a.resolve(ctx, p.registry, enabled); err != nil {
		r.printf("  %s\n", indent(problemText(err)))
		r.record(ExitUsage)
		return false
	}
	for _, w := range a.plan.Graph().Warnings {
		r.printf("  warning: %s: %s\n", w.Module, w.Message)
	}
	return true
}

// database opens the pool and pings it (step 4).
func (r *checkReport) database(ctx context.Context, a *assembly) {
	if err := a.openDatabase(ctx); err != nil {
		r.printf("database: %s: %s\n", errs.KindOf(err), indent(err.Error()))
		r.record(statusOf(err, ExitFailure))
		return
	}
	pctx, cancel := context.WithTimeout(ctx, checkTimeout)
	err := a.pool.Ping(pctx)
	cancel()
	if err != nil {
		r.printf("database: %s: %s\n", errs.KindOf(err), indent(err.Error()))
		r.record(ExitFailure)
		return
	}
	mode := "read-write"
	if a.pool.ReadOnly() {
		mode = "read-only"
	}
	r.printf("database: ok, %s, pool max %d\n", mode, a.pool.Stats().Max)
}

// schema prints the state of every registered module's migrations, enabled or not (step 5).
func (r *checkReport) schema(ctx context.Context, p *process, a *assembly) {
	if a.migrator == nil {
		r.printf("schema: skipped (no database)\n")
		return
	}
	disabled := map[string]bool{}
	for _, d := range a.cfg.Modules.Disabled {
		disabled[strings.TrimSpace(d)] = true
	}
	sctx, cancel := context.WithTimeout(ctx, checkTimeout)
	statuses, err := a.migrator.Status(sctx, a.setsFor(p.registry.Manifests()))
	cancel()
	if err != nil {
		r.printf("schema: %s: %s\n", errs.KindOf(err), indent(err.Error()))
		r.record(statusOf(err, ExitSchema))
		return
	}
	r.printf("schema\n")
	for _, st := range statuses {
		r.setLine(st, disabled[st.Set])
	}
}

// setLine prints one set's state. A disabled module that was never migrated is reported
// but is not a problem: serve does not build it, so serve would not refuse to start.
func (r *checkReport) setLine(st db.Status, disabled bool) {
	if st.Err == nil {
		r.printf("  %s  built %d  applied %d  current\n", st.Set, st.Built, st.Applied)
		return
	}
	r.printf("  %s  built %d  applied %d  %s\n", st.Set, st.Built, st.Applied, indent(st.Err.Error()))
	if disabled {
		return
	}
	if errs.Is(st.Err, errs.Conflict) {
		r.record(ExitSchema)
		return
	}
	r.record(ExitFailure)
}

// api builds the modules against a database that refuses every statement and the API over
// them, and prints the operation count (step 6).
func (r *checkReport) api(ctx context.Context, a *assembly) {
	a.ui = nil
	if !a.cfg.Session.Key.IsSet() {
		// Already recorded as a problem (step 1); the build still runs to count the API.
		a.cfg.Session.Key = secret.New(placeholderSessionKey)
	}
	if err := a.newDispatcher(); err != nil {
		r.printf("api: %s\n", indent(err.Error()))
		r.record(ExitFailure)
		return
	}
	a.dbc = db.None()
	if err := a.buildModules(ctx, skipSchema{}); err != nil {
		r.printf("api: %s\n", indent(problemText(err)))
		r.record(statusOf(err, ExitUsage))
		return
	}
	opts, err := a.apiOptions(a.version, noDatabase{}, func(context.Context) ([]db.Status, error) { return nil, nil })
	if err != nil {
		r.printf("api: %s\n", indent(err.Error()))
		r.record(ExitFailure)
		return
	}
	srv, err := newServer(opts)
	if err != nil {
		r.printf("api: %s\n", indent(problemText(err)))
		r.record(statusOf(err, ExitUsage))
		return
	}
	n, err := countOperations(srv.OpenAPI())
	if err != nil {
		r.printf("api: %s\n", indent(err.Error()))
		r.record(ExitFailure)
		return
	}
	r.printf("api: %d operations\n", n)
}

// auditSpool prints how many audit spool files wait to be imported and how many the
// importer set aside (audit.md: bms check may report the spool). It only counts: a read-only
// copy holds waiting files until it can write, and importing would write. A spool path that
// cannot be read is the operator's to fix under BMS_DATA_DIR (ExitUsage), as serve's
// importer start would find (audit F33).
func (r *checkReport) auditSpool(cfg config.Config) {
	waiting, failed, err := audit.SpoolCounts(cfg.Data.Dir)
	if err != nil {
		r.printf("audit spool: %s\n", indent(err.Error()))
		r.record(statusOf(err, ExitFailure))
		return
	}
	if waiting == 0 && failed == 0 {
		r.printf("audit spool: empty\n")
		return
	}
	r.printf("audit spool: %d waiting, %d failed\n", waiting, failed)
	if failed > 0 {
		dir := filepath.Join(cfg.Data.Dir, filepath.FromSlash(audit.SpoolDirName))
		r.printf("  %d spool file(s) hold a line the importer could not read and were set aside as .failed in %s; keep them until you have looked at them\n", failed, dir)
	}
}

// ui prints whether a web build is embedded and its version (step 7).
func (r *checkReport) ui() {
	if _, err := webFS(); err != nil {
		r.printf("ui: not built\n")
		return
	}
	v, err := webVersion()
	if err != nil {
		r.printf("ui: built, version unknown\n")
		return
	}
	r.printf("ui: built, version %s\n", v)
}

// httpMethods are the keys of an OpenAPI path item that are operations.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// countOperations counts the operations in the document's paths.
func countOperations(doc []byte) (int, error) {
	var d struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return 0, errs.E("app.countOperations", errs.Internal, fmt.Errorf("the assembled OpenAPI document is not JSON: %w", err))
	}
	n := 0
	for _, item := range d.Paths {
		for key := range item {
			if httpMethods[key] {
				n++
			}
		}
	}
	return n, nil
}
