package files

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/telemetry"
)

// metrics are the counters of files.md "Owns". Without a Meter every method is a no-op.
type metrics struct {
	uploads        telemetry.Counter
	reads          telemetry.Counter
	swept          telemetry.Counter
	verify         telemetry.Counter
	objectFailures telemetry.Counter
}

func (m *metrics) init(meter telemetry.Meter, s *Service) error {
	if meter == nil {
		return nil
	}
	var err error
	if m.uploads, err = meter.Counter("bms.files.uploads", telemetry.WithUnit("{upload}"),
		telemetry.WithDescription("Stage calls, by outcome")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	if m.reads, err = meter.Counter("bms.files.reads", telemetry.WithUnit("{read}"),
		telemetry.WithDescription("Open calls, by outcome")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	if m.swept, err = meter.Counter("bms.files.swept", telemetry.WithUnit("{object}"),
		telemetry.WithDescription("Objects and temp files removed by Reap and SweepOrphans, by what")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	if m.verify, err = meter.Counter("bms.files.verify", telemetry.WithUnit("{file}"),
		telemetry.WithDescription("Verify results per file, by outcome")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	if m.objectFailures, err = meter.Counter("bms.objects.failures", telemetry.WithUnit("{failure}"),
		telemetry.WithDescription("Failed object store calls, by operation")); err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	err = meter.Gauge("bms.files.tombstones", func(context.Context) []telemetry.Sample {
		n := s.tombstones.Load()
		if n < 0 {
			return nil
		}
		return []telemetry.Sample{{Value: float64(n)}}
	}, telemetry.WithUnit("{tombstone}"), telemetry.WithDescription("Outstanding object deletions, as of the last Reap or Verify"))
	if err != nil {
		return fmt.Errorf("meter: %w", err)
	}
	return nil
}

func add(c telemetry.Counter, ctx context.Context, n int64, attrs ...telemetry.Attr) { //nolint:revive // the counter leads: a nil counter is a no-op
	if c != nil && n > 0 {
		c.Add(ctx, n, attrs...)
	}
}

// instrumented wraps the object store with a Client span per call and a failure counter. It
// retries nothing and changes no result.
type instrumented struct {
	objects.Store
	tracer   telemetry.Tracer
	failures telemetry.Counter
}

func (i instrumented) begin(ctx context.Context, op string) (context.Context, telemetry.Span) {
	return i.tracer.Start(ctx, "objects."+op, telemetry.WithKind(telemetry.SpanClient),
		telemetry.WithAttrs(telemetry.Token("objects.kind", i.Store.Kind().String())))
}

func (i instrumented) end(ctx context.Context, span telemetry.Span, op string, err error) {
	if err != nil {
		span.RecordError(err)
		if !errors.Is(err, objects.ErrMissing) { // an absent object is an answer, not a failure
			add(i.failures, ctx, 1, telemetry.Token("op", op))
		}
	}
	span.End()
}

func (i instrumented) Put(ctx context.Context, in objects.PutInput) (objects.Info, error) {
	ctx, span := i.begin(ctx, "put")
	info, err := i.Store.Put(ctx, in)
	i.end(ctx, span, "put", err)
	return info, err //nolint:wrapcheck // the store's own error
}

func (i instrumented) Get(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Object, error) {
	ctx, span := i.begin(ctx, "get")
	o, err := i.Store.Get(ctx, b, k)
	i.end(ctx, span, "get", err)
	return o, err //nolint:wrapcheck // the store's own error
}

func (i instrumented) Head(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Info, error) {
	ctx, span := i.begin(ctx, "head")
	info, err := i.Store.Head(ctx, b, k)
	i.end(ctx, span, "head", err)
	return info, err //nolint:wrapcheck // the store's own error
}

func (i instrumented) Delete(ctx context.Context, b objects.Bucket, k objects.Key) error {
	ctx, span := i.begin(ctx, "delete")
	err := i.Store.Delete(ctx, b, k)
	i.end(ctx, span, "delete", err)
	return err //nolint:wrapcheck // the store's own error
}

func (i instrumented) List(ctx context.Context, b objects.Bucket, prefix string, fn func(objects.Entry) error) error {
	ctx, span := i.begin(ctx, "list")
	err := i.Store.List(ctx, b, prefix, fn)
	i.end(ctx, span, "list", err)
	return err //nolint:wrapcheck // the store's own error
}

func (i instrumented) Ping(ctx context.Context) error {
	ctx, span := i.begin(ctx, "ping")
	err := i.Store.Ping(ctx)
	i.end(ctx, span, "ping", err)
	return err //nolint:wrapcheck // the store's own error
}
