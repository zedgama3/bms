package intake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// shellModule is the module attribute of a screen the shell itself owns, and the one derived
// for "/" when no manifest maps screens to their sets.
const shellModule = "shell"

// EventSubmission is what api hands Usage.Accept. There is no Person field on purpose
// (decision T1-a): the handler never reads the session for this route.
type EventSubmission struct {
	From      netip.Addr // for the limit; never logged, never on a metric
	UserAgent string     // reduced to two coarse tokens (browser, form_factor); never recorded as text
	Batch     EventBatch
}

// UsageOptions is what NewUsage needs.
type UsageOptions struct {
	Logger  *slog.Logger    // one warn line per hour when rate-limited; nothing per event
	Meter   telemetry.Meter // the bms.client.* instruments and bms.client_events.*
	Clock   clock.Clock
	Enabled bool     // config telemetry.usage (T1.6); false: Accept fails Unsupported and counts nothing
	Screens []string // the screen allowlist from web.ClientManifest (T1.5); empty: fold cap only
	Actions []string // the action allowlist from the same manifest; empty: fold cap only

	// Modules maps a screen pattern to the name of the screen set that owns it (the
	// manifest's "set"), for the module attribute (decision T1-e). A screen it does not
	// list, or any screen when it is nil, gets the first segment of its pattern as module
	// ("shell" for "/"), which is bounded by Screens or the fold cap.
	Modules map[string]string
}

// Usage is the usage-event service. Safe for concurrent use.
type Usage struct {
	log     *slog.Logger
	clk     clock.Clock
	enabled bool
	modules map[string]string

	screenViews, actions       telemetry.Counter
	accepted, rejected, folded telemetry.Counter
	screenReady, lcp, inp, cls telemetry.Histogram
	screens, actionNames       folder
	limiter                    // the token buckets, the same implementation as Intake's
}

// Histogram bucket boundaries (telemetry T1.4): the Web Vitals "good" and "poor" thresholds
// are boundaries, so p75 against them is exact.
var (
	screenReadyBuckets = []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 20, 30}
	lcpBuckets         = []float64{0.5, 1, 1.5, 2, 2.5, 3, 4, 6, 8, 12}
	inpBuckets         = []float64{0.05, 0.1, 0.2, 0.3, 0.5, 0.75, 1, 2}
	clsBuckets         = []float64{0.05, 0.1, 0.25, 0.5, 1}
)

// NewUsage fails with errs.Invalid if Logger, Meter or Clock is nil, or if an allowlist
// entry is not a token (F51). It registers the instruments in T1.4 (F20, F21 apply).
func NewUsage(opts UsageOptions) (*Usage, error) {
	const op = "intake.NewUsage"
	if opts.Logger == nil || opts.Meter == nil || opts.Clock == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil dependency"))
	}
	u := &Usage{log: opts.Logger, clk: opts.Clock, enabled: opts.Enabled, modules: opts.Modules}
	if err := errors.Join(
		u.screens.setup("screens", opts.Screens, MaxDistinctScreens, EntryToken),
		u.actionNames.setup("actions", opts.Actions, MaxDistinctActions),
	); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	for _, m := range opts.Modules {
		if !telemetry.IsToken(m) {
			return nil, errs.E(op, errs.Invalid, errors.New("a module name in Modules is not a token"))
		}
	}
	var err error
	counter := func(dst *telemetry.Counter, name, unit string) {
		if err == nil {
			*dst, err = opts.Meter.Counter(name, telemetry.WithUnit(unit))
		}
	}
	hist := func(dst *telemetry.Histogram, name, unit string, buckets []float64) {
		if err == nil {
			*dst, err = opts.Meter.Histogram(name, telemetry.WithUnit(unit), telemetry.WithBuckets(buckets...))
		}
	}
	counter(&u.screenViews, "bms.client.screen_views", "{view}")
	counter(&u.actions, "bms.client.actions", "{action}")
	hist(&u.screenReady, "bms.client.screen_ready", "s", screenReadyBuckets)
	hist(&u.lcp, "bms.client.lcp", "s", lcpBuckets)
	hist(&u.inp, "bms.client.inp", "s", inpBuckets)
	hist(&u.cls, "bms.client.cls", "1", clsBuckets)
	counter(&u.accepted, "bms.client_events.accepted", "{event}")
	counter(&u.rejected, "bms.client_events.rejected", "{event}")
	counter(&u.folded, "bms.client_events.folded", "{value}")
	if err != nil {
		return nil, fmt.Errorf("%s: instruments: %w", op, err)
	}
	u.setup(EventsPerAddressPerMinute, EventsGlobalPerMinute, opts.Clock.Now())
	return u, nil
}

// Accept takes one batch: fails errs.Unsupported when usage counting is off (F52), checks
// ctx (F53), charges the batch's event count against the global bucket then the address's
// bucket (F63, F64), validates the batch (F54–F62), then for each event replaces ID-shaped
// segments in Screen and From with "{id}", folds Screen, From and Action through the
// allowlists (F65, F66), reduces UserAgent to browser and form_factor tokens, and records
// the event's instrument. It writes no log record per event and returns nil once every
// event is recorded.
func (u *Usage) Accept(ctx context.Context, s EventSubmission) error {
	const op = "intake.Usage.Accept"
	n := len(s.Batch.Events)
	if !u.enabled {
		u.rejected.Add(ctx, int64(n), telemetry.Token("reason", "disabled"))
		return errs.E(op, errs.Unsupported, errors.New("usage counting is turned off (telemetry.usage)"))
	}
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	if !s.From.IsValid() {
		return errs.E(op, errs.Invalid, errors.New("client address is not valid"))
	}
	if err := validate.Range("events", n, 1, MaxEvents); err != nil {
		u.rejected.Add(ctx, int64(n), telemetry.Token("reason", "invalid"))
		return fmt.Errorf("%s: %w", op, err)
	}
	if ok, logNow, seen := u.take(u.clk.Now(), s.From, n); !ok {
		u.rejected.Add(ctx, int64(n), telemetry.Token("reason", "rate_limited"))
		if logNow {
			u.log.WarnContext(ctx, "client usage events rate-limited", slog.Int("events", seen))
		}
		return errs.E(op, errs.Unavailable, ErrRateLimited)
	}
	now := u.clk.Now()
	acc := validate.Errors{Op: op}
	acc.Add(s.Batch.Validate())
	for idx, e := range s.Batch.Events {
		if d := now.Sub(e.OccurredAt); e.OccurredAt.IsZero() {
			acc.Add(fieldErrf("events[%d].occurred_at", idx, validate.CodeRequired, "must be set"))
		} else if d >= OccurredWithin || -d >= OccurredWithin {
			acc.Add(fieldErrf("events[%d].occurred_at", idx, validate.CodeRange,
				"must be within %s of now", OccurredWithin))
		}
	}
	if err := acc.Err(); err != nil {
		u.rejected.Add(ctx, int64(n), telemetry.Token("reason", "invalid"))
		return err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}

	browser, form := userAgentClass(s.UserAgent)
	for _, e := range s.Batch.Events {
		u.record(ctx, e, browser, form)
	}
	return nil
}

// record folds e's attributes and records its instrument and the accepted counter.
func (u *Usage) record(ctx context.Context, e Event, browser, form string) {
	screen := u.foldScreen(ctx, "screen", replaceIDSegments(e.Screen))
	module := u.moduleOf(screen)
	count := int64(max(e.Count, 1))
	scr, mod := telemetry.Token("screen", screen), telemetry.Token("module", module)
	perf := []telemetry.Attr{scr, mod, telemetry.Token("browser", browser), telemetry.Token("form_factor", form)}

	switch e.Kind {
	case EventScreenView:
		attrs := []telemetry.Attr{scr, mod}
		if e.From != "" {
			attrs = append(attrs, telemetry.Token("from", u.foldScreen(ctx, "from", replaceIDSegments(e.From))))
		}
		u.screenViews.Add(ctx, count, attrs...)
	case EventAction:
		action, folded := u.actionNames.fold(e.Action)
		if folded {
			u.folded.Add(ctx, 1, telemetry.Token("field", "action"))
		}
		u.actions.Add(ctx, count, telemetry.Token("action", action), scr, mod)
	case EventScreenReady:
		u.screenReady.Record(ctx, e.Value/1000, perf...)
	case EventVital:
		switch e.Name {
		case vitalLCP:
			u.lcp.Record(ctx, e.Value/1000, perf...)
		case vitalINP:
			u.inp.Record(ctx, e.Value/1000, perf...)
		case vitalCLS:
			u.cls.Record(ctx, e.Value, perf...)
		}
	}
	u.accepted.Add(ctx, count, telemetry.Token("kind", string(e.Kind)))
}

// foldScreen folds a screen pattern (a screen or a from) and counts the fold under field.
func (u *Usage) foldScreen(ctx context.Context, field, pattern string) string {
	out, folded := u.screens.fold(pattern)
	if folded {
		u.folded.Add(ctx, 1, telemetry.Token("field", field))
	}
	return out
}

// moduleOf is the module attribute of an already folded screen (F68, decision T1-e).
func (u *Usage) moduleOf(screen string) string {
	if screen == OtherToken {
		return OtherToken
	}
	if m, ok := u.modules[screen]; ok {
		return m
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(screen, "/"), "/")
	if first == "" || !telemetry.IsToken(first) {
		return shellModule
	}
	return first
}
