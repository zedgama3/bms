package db

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/telemetry"
)

// registerGauges registers the three pool gauges on m, each observed from stats
// (telemetry.md metrics table). stats is a Pool's Stats, which reads zeros after Close;
// an observer returns nil once closed reports true.
func registerGauges(m telemetry.Meter, stats func() Stats, closed func() bool) error {
	unitConn := telemetry.WithUnit("{connection}")
	if err := m.Gauge("db.client.connection.count", func(context.Context) []telemetry.Sample {
		if closed() {
			return nil
		}
		s := stats()
		return []telemetry.Sample{
			{Value: float64(s.Idle), Attrs: []telemetry.Attr{telemetry.Token("state", "idle")}},
			{Value: float64(s.Acquired), Attrs: []telemetry.Attr{telemetry.Token("state", "used")}},
		}
	}, unitConn); err != nil {
		return fmt.Errorf("register db.client.connection.count: %w", err)
	}
	if err := m.Gauge("db.client.connection.max", func(context.Context) []telemetry.Sample {
		if closed() {
			return nil
		}
		return []telemetry.Sample{{Value: float64(stats().Max)}}
	}, unitConn); err != nil {
		return fmt.Errorf("register db.client.connection.max: %w", err)
	}
	if err := m.Gauge("db.client.connection.pending_requests", func(context.Context) []telemetry.Sample {
		if closed() {
			return nil
		}
		return []telemetry.Sample{{Value: float64(stats().Pending)}}
	}, telemetry.WithUnit("{request}")); err != nil {
		return fmt.Errorf("register db.client.connection.pending_requests: %w", err)
	}
	return nil
}
