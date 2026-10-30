package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/web"
)

// webManifest is the web build's client manifest, replaceable in internal tests (F41).
var webManifest = web.ClientManifest

// clientAllowlists are what the client manifest hands the two intake services (telemetry
// T1.5): the screen patterns, the action names, and each screen's owning set for the
// module attribute. All empty when the manifest is missing or unusable, and intake then
// applies its fold caps.
type clientAllowlists struct {
	screens []string
	actions []string
	modules map[string]string
}

// clientManifest reads the manifest. A missing or malformed file is not fatal: it returns
// the empty lists and the error, for the caller to warn or report (F41).
func clientManifest() (clientAllowlists, error) {
	m, err := webManifest()
	if err != nil {
		return clientAllowlists{}, err //nolint:wrapcheck // the callers report it with their own step's name
	}
	l := clientAllowlists{modules: make(map[string]string, len(m.Screens))}
	for _, s := range m.Screens {
		l.screens = append(l.screens, s.Pattern)
		l.modules[s.Pattern] = s.Set
	}
	for _, a := range m.Actions {
		l.actions = append(l.actions, a.Name)
	}
	return l, nil
}

// clientServices builds the two services behind POST /api/client-errors and
// POST /api/client-events (telemetry T1.5): intake with the manifest's screens and the API's
// route tokens, usage with the manifest's screens and actions and cfg.Telemetry.Usage. The
// openapi command (a.forDocument) builds both with usage off and empty lists: the document
// depends on neither.
func (a *assembly) clientServices(routes []module.ModuleRoutes) (*intake.Intake, *intake.Usage, error) {
	_, errMeter, err := a.scopes("intake")
	if err != nil {
		return nil, nil, err
	}
	_, usageMeter, err := a.scopes("usage")
	if err != nil {
		return nil, nil, err
	}
	var lists clientAllowlists
	enabled := a.cfg.Telemetry.Usage
	if a.forDocument {
		enabled = false
	} else if l, merr := clientManifest(); merr == nil {
		lists = l
	}
	in, err := intake.New(intake.Options{
		Logger: a.component("intake"), Meter: errMeter, Clock: a.clock,
		Screens: lists.screens, Routes: api.RouteTokens(routes),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("intake: %w", err)
	}
	usage, err := newUsage(a, usageMeter, enabled, lists)
	if err != nil {
		return nil, nil, fmt.Errorf("usage: %w", err)
	}
	return in, usage, nil
}

func newUsage(a *assembly, m telemetry.Meter, enabled bool, lists clientAllowlists) (*intake.Usage, error) {
	return intake.NewUsage(intake.UsageOptions{ //nolint:wrapcheck // the caller names the step
		Logger: a.component("usage"), Meter: m, Clock: a.clock, Enabled: enabled,
		Screens: lists.screens, Actions: lists.actions, Modules: lists.modules,
	})
}

// logUsage writes the start-up line for client usage counts and warns once when the web
// build has no usable client manifest (F41). A build with no UI has nothing to count and no
// manifest to miss.
func (a *assembly) logUsage(ctx context.Context) {
	lists, err := clientManifest()
	switch {
	case err != nil && a.ui != nil:
		a.logger.WarnContext(ctx, "the web build has no usable client manifest; usage counts and client error screens fold at their caps; rebuild it with `make web-build`",
			slog.String("error.kind", errs.KindOf(err).String()))
	case err != nil:
		return
	}
	a.logger.InfoContext(ctx, "usage counts",
		slog.Bool("enabled", a.cfg.Telemetry.Usage),
		slog.Int("screens", len(lists.screens)), slog.Int("actions", len(lists.actions)))
}

// usage prints the client manifest and usage-count lines of `bms check` (F41, F43). A
// missing manifest fails the check in a build that has a UI and is a note in one that has
// none.
func (r *checkReport) usage(a *assembly) {
	lists, err := clientManifest()
	hasUI := false
	if _, uerr := webFS(); uerr == nil {
		hasUI = true
	}
	switch {
	case err == nil:
		r.printf("client manifest: %d screens, %d actions\n", len(lists.screens), len(lists.actions))
	case hasUI:
		r.printf("client manifest: missing\n")
		r.record(ExitFailure)
	default:
		r.printf("client manifest: missing (no UI is built)\n")
	}
	switch {
	case !a.cfg.Telemetry.Usage:
		r.printf("usage counts: off\n")
	case err != nil:
		r.printf("usage counts: on (no manifest; fold caps apply)\n")
	default:
		r.printf("usage counts: on (%d screens, %d actions)\n", len(lists.screens), len(lists.actions))
	}
}
