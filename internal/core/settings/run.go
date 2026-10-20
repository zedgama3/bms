package settings

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// pollWarnEvery is how often a failing poll may log.
const pollWarnEvery = time.Minute

// Run is the refresher: every RefreshInterval it reads the revision and reloads the
// snapshot when it moved, so a replica follows the writer (span settings.refresh, counter
// bms.settings.refresh{outcome}). A failed poll or reload keeps the old snapshot, counts
// "failed" and logs at most once a minute; it never ends Run. Run returns nil when ctx ends
// and errs.Conflict when it is already running.
func (s *Service) Run(ctx context.Context) error {
	const op = "settings.Service.Run"
	if !s.running.CompareAndSwap(false, true) {
		return errs.E(op, errs.Conflict, errors.New("the refresher is already running"))
	}
	defer s.running.Store(false)
	tick, err := s.clock.NewTicker(s.interval)
	if err != nil {
		return wrap(op, err)
	}
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C():
			s.refresh(ctx)
		}
	}
}

// refresh polls the revision once and reloads when it differs from the snapshot's. The
// outcome is counted before a reloaded snapshot becomes visible, so whoever sees the new
// values also sees the count.
func (s *Service) refresh(ctx context.Context) {
	ctx, span := s.start(ctx, "refresh")
	var err error
	defer endSpan(span, &err)

	rev, err := s.store.Revision(ctx)
	if err != nil && errs.Is(err, errs.NotFound) {
		rev, err = 0, nil
	}
	if err == nil {
		if cur := s.snap.Load(); cur != nil && cur.revision == rev {
			s.metrics.refreshed(ctx, outcomeUnchanged)
			return
		}
		var sn *snapshot
		if sn, err = s.load(ctx); err == nil {
			s.metrics.refreshed(ctx, outcomeOK)
			s.adopt(ctx, sn)
			return
		}
	}
	if ctx.Err() != nil { // shutting down: not a failure of the database
		return
	}
	s.metrics.refreshed(ctx, outcomeFailed)
	s.warnPoll(ctx, err)
}

// warnPoll logs a failed poll at most once a minute.
func (s *Service) warnPoll(ctx context.Context, err error) {
	s.pollMu.Lock()
	now := s.clock.Now()
	due := s.pollWarn.IsZero() || now.Sub(s.pollWarn) >= pollWarnEvery
	if due {
		s.pollWarn = now
	}
	s.pollMu.Unlock()
	if due {
		s.logger.WarnContext(ctx, "the settings refresh failed; keeping the last snapshot", slog.Any("error", err))
	}
}

// adopt installs a snapshot the poller loaded. One older than the current snapshot is
// replaced only when a second read of the revision confirms that the database itself is
// there (a restore); otherwise a local write moved on while the load ran, and the next
// poll sorts it out.
func (s *Service) adopt(ctx context.Context, sn *snapshot) {
	cur := s.snap.Load()
	if cur == nil || sn.revision >= cur.revision {
		s.install(sn)
		return
	}
	again, err := s.store.Revision(ctx)
	if err == nil && again == sn.revision {
		s.replace(ctx, sn)
	}
}
