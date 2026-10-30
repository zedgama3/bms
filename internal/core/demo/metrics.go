package demo

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// metrics are the counters of demo.md "Owns". Without a Meter every method is a no-op.
type metrics struct {
	seeds    telemetry.Counter
	resets   telemetry.Counter
	captured telemetry.Counter
}

func (m *metrics) init(meter telemetry.Meter) error {
	if meter == nil {
		return nil
	}
	var err error
	if m.seeds, err = meter.Counter("bms.demo.seeds", telemetry.WithUnit("{seed}"),
		telemetry.WithDescription("Seed runs, by outcome")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	if m.resets, err = meter.Counter("bms.demo.resets", telemetry.WithUnit("{reset}"),
		telemetry.WithDescription("Reset runs, by outcome")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	if m.captured, err = meter.Counter("bms.demo.mail_captured", telemetry.WithUnit("{message}"),
		telemetry.WithDescription("Notifications kept instead of delivered")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	return nil
}

// outcomeAttr is the outcome of a seed or reset: "ok" or the error's kind.
func outcomeAttr(err error) telemetry.Attr {
	if err == nil {
		return telemetry.Token("outcome", "ok")
	}
	return telemetry.Token("outcome", errs.KindOf(err).String())
}

func add(c telemetry.Counter, ctx context.Context, attrs ...telemetry.Attr) { //nolint:revive // the counter leads: a nil counter is a no-op
	if c != nil {
		c.Add(ctx, 1, attrs...)
	}
}
