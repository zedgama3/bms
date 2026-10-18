package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// placeholderSessionKey stands in for the session key where the command builds the core
// only to describe or count it (openapi, check without a key): identity.New refuses an unset
// key, and nothing built from this one signs or verifies anything. It is not a secret.
const placeholderSessionKey = "bms-placeholder-session-key-for-documentation-builds"

// noEnvironment is the lookup openapi gives telemetry: no OTEL_* variable is read, so a
// build step never exports (app.md "Settings").
func noEnvironment(string) (string, bool) { return "", false }

// runOpenAPI prints the assembled OpenAPI document with no configuration and no I/O
// ("openapi, step by step"): every compiled-in module is documented, built against a
// database that refuses every statement.
func runOpenAPI(ctx context.Context, p *process) int {
	cfg := config.Default()
	cfg.Log.Level = slog.LevelWarn
	cfg.Log.Format = config.LogText
	cfg.Instance.Name = "openapi"
	cfg.Session.Key = secret.New(placeholderSessionKey)
	a := newAssembly(p, cfg)
	if err := a.startTelemetry(ctx, noEnvironment, p.stderr); err != nil {
		return p.earlyFail(ctx, err, ExitUsage)
	}
	bail := func(err error, status int) int {
		code := a.fail(ctx, p, err, status)
		if cerr := a.closeQuiet(ctx); cerr != nil {
			p.problem(cerr)
		}
		return code
	}

	all := p.registry.Manifests()
	enabled := make([]string, len(all))
	for i, m := range all {
		enabled[i] = m.Name
	}
	if err := a.resolve(ctx, p.registry, enabled); err != nil {
		return bail(err, ExitUsage)
	}
	if err := a.newDispatcher(); err != nil {
		return bail(err, ExitFailure)
	}
	a.dbc = db.None()
	if err := a.buildModules(ctx, skipSchema{}); err != nil {
		return bail(err, statusOf(err, ExitUsage))
	}
	opts, err := a.apiOptions(openAPIVersion, noDatabase{}, func(context.Context) ([]db.Status, error) { return nil, nil })
	if err != nil {
		return bail(err, ExitFailure)
	}
	opts.Authenticator = nil // the document is a function of the binary alone (app.md openapi step 3)
	srv, err := newServer(opts)
	if err != nil {
		return bail(fmt.Errorf("api: %w", err), statusOf(err, ExitUsage))
	}
	// Indented two spaces, so the committed web/openapi.json diffs readably and
	// `make openapi-check` is stable (the document's key order is already fixed).
	var doc bytes.Buffer
	if err := json.Indent(&doc, srv.OpenAPI(), "", "  "); err != nil {
		return bail(errs.E("app.openapi", errs.Internal, fmt.Errorf("indent the document: %w", err)), ExitFailure)
	}
	doc.WriteByte('\n')
	if _, err := p.stdout.Write(doc.Bytes()); err != nil {
		return bail(errs.E("app.openapi", errs.Unavailable, fmt.Errorf("write the document: %w", err)), ExitFailure)
	}
	if err := a.closeQuiet(ctx); err != nil {
		p.problem(err)
		return ExitFailure
	}
	return ExitOK
}
