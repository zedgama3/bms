package jobs

import (
	"context"
	"log/slog"
)

// purgeHandler is the handler of jobs.purge: it deletes finished rows older than DoneTTL in
// batches of MaxBatch until a batch comes back short. Nothing personal is read or audited. It
// is idempotent: a second run finds nothing more to delete.
func (r *Runner) purgeHandler(ctx context.Context, _ Job) error {
	const op = "jobs.purge"
	before := r.now().Add(-DoneTTL)
	var total int64
	for {
		n, err := r.store.Purge(ctx, before, MaxBatch)
		if err != nil {
			return wrap(op, err)
		}
		total += n
		if n < MaxBatch {
			break
		}
		if err := ctx.Err(); err != nil {
			return wrap(op, err)
		}
	}
	if total > 0 {
		r.logger.InfoContext(ctx, "jobs: purged finished jobs", slog.Int64("purged", total))
	}
	return nil
}
