package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// runner runs one service's Run in a goroutine of its own under app's lifecycle (boundaries.md
// decision 16): start launches it, stop ends it and waits, within the ctx it is given. It is
// the one runnable shape of the audit importer, the settings refresher, the notify sender and
// the job runner; what differs is the service's two methods and the words of the log line.
type runner struct {
	label  string                          // the words in the log line and the error: "notify sender"
	run    func(ctx context.Context) error // the service's Run
	close  func(ctx context.Context) error // the service's Close, called first on stop; nil when cancelling run's ctx is the whole stop
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// newRunner returns the runner of a service's run and, when it has one that stops the loop
// and gives its work back, close. The label names the service in the log line and the error.
// Fails errs.Invalid for a nil run or logger.
func newRunner(label string, run, closeFn func(ctx context.Context) error, logger *slog.Logger) (*runner, error) {
	if label == "" || run == nil || logger == nil {
		return nil, errs.E("app.newRunner", errs.Invalid, fmt.Errorf("runner %q needs a label, a Run and a logger", label))
	}
	return &runner{label: label, run: run, close: closeFn, logger: logger}, nil
}

// runnable is the runner as the lifecycle starts and stops it, under name.
func (r *runner) runnable(name string) runnable {
	return runnable{name: name, start: r.start, stop: r.stop}
}

// start launches Run. The goroutine exits when stop (or the service's own Close) ends it; a
// failure, if any (Run fails only when started twice, or when the log's database refuses), is
// logged there, once.
func (r *runner) start(ctx context.Context) error {
	// Run outlives start's own deadline: it stops on stop.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		if err := r.run(runCtx); err != nil {
			r.logger.ErrorContext(runCtx, "the "+r.label+" stopped",
				slog.String("error.kind", errs.KindOf(err).String()), slog.Any("error", err))
		}
	}()
	return nil
}

// stop closes the service when it has a Close, ends Run and waits for it. It fails with the
// Close's error, or with ctx's kind when ctx ends first; the goroutine then exits on its own
// when Run notices its canceled context. A stop before a start returns nil.
func (r *runner) stop(ctx context.Context) error {
	const op = "app.runner.stop"
	if r.cancel == nil {
		return nil
	}
	if r.close != nil {
		if err := r.close(ctx); err != nil {
			r.cancel()
			return fmt.Errorf("%s: %s: %w", op, r.label, err)
		}
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), fmt.Errorf("the %s did not stop: %w", r.label, ctx.Err()))
	}
}
