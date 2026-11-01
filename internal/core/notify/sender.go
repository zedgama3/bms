package notify

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

const (
	// purgeBatch is how many rows one Purge or ExpireStale statement takes.
	purgeBatch = 1000
	// noSMTPLogEvery is how often the "messages wait" line is logged.
	noSMTPLogEvery = time.Hour
	// releaseTimeout bounds the writes that give rows back after a cancel.
	releaseTimeout = 5 * time.Second
	// recordTimeout bounds the write of a row's outcome, which must survive a shutdown so a
	// message delivered just before it is not released and sent twice.
	recordTimeout = 5 * time.Second
)

func readOnlyErr(op string) error {
	return errs.E(op, errs.Unavailable, db.ErrReadOnly)
}

// ---- Run and Close ----------------------------------------------------------------------

// Run is the sender loop: at once and then every PollInterval, when the database is
// writable, SendOnce; at once and then every PurgeInterval, ExpireStale and then Purge. It
// returns nil when ctx is done or after Close, which also cancels a batch in flight (its
// unsent rows are given back). It sends nothing when Close came first, and fails
// errs.Internal when called twice. A failing tick is logged once per change of state and
// retried at the next tick.
func (s *Service) Run(ctx context.Context) error {
	const op = "notify.Service.Run"
	s.runMu.Lock()
	if s.done != nil {
		s.runMu.Unlock()
		return errs.E(op, errs.Internal, errors.New("Run called twice"))
	}
	done := make(chan struct{})
	s.done = done
	s.running.Store(true)
	s.runMu.Unlock()
	defer close(done)

	select {
	case <-s.stop:
		return nil
	default:
	}
	// Close cancels the working context, so a batch in flight ends and gives its rows back.
	var watcher sync.WaitGroup
	defer watcher.Wait()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		select {
		case <-s.stop:
			cancel()
		case <-runCtx.Done():
		}
	}()

	tick, err := s.clock.NewTicker(s.poll)
	if err != nil {
		return errs.E(op, errs.Internal, err)
	}
	defer tick.Stop()
	sweep, err := s.clock.NewTicker(PurgeInterval)
	if err != nil {
		return errs.E(op, errs.Internal, err)
	}
	defer sweep.Stop()
	// A restart works at once instead of one interval later, so frequent restarts never
	// starve the backlog or the sweep.
	s.tick(runCtx)
	s.sweep(runCtx)
	for {
		select {
		case <-runCtx.Done():
			return nil
		case <-tick.C():
			s.tick(runCtx)
		case <-sweep.C():
			s.sweep(runCtx)
		}
	}
}

// Close stops Run, cancels the batch in flight (its unsent rows are given back) and waits
// for Run to return under ctx. Idempotent. Fails with ctx's kind when Run is still
// returning when ctx ends.
func (s *Service) Close(ctx context.Context) error {
	const op = "notify.Service.Close"
	s.stopped.Do(func() { close(s.stop) })
	s.runMu.Lock()
	done := s.done
	s.runMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
	}
}

// tick is one pass of the loop.
func (s *Service) tick(ctx context.Context) {
	ctx, span := s.start(ctx, "tick")
	defer span.End()
	if s.db.ReadOnly() {
		s.logMu.Lock()
		flipped := !s.readOnly
		s.readOnly = true
		s.logMu.Unlock()
		if flipped {
			s.metrics.deliver(ctx, outcomeReadOnly, 1)
		}
		return
	}
	s.logMu.Lock()
	s.readOnly = false
	s.logMu.Unlock()

	res, err := s.SendOnce(ctx)
	span.SetAttrs(telemetry.Int("sent", res.Sent), telemetry.Int("retried", res.Retried),
		telemetry.Int("failed", res.Failed), telemetry.Int("skipped", res.Skipped))
	counts, cerr := s.store.CountOpenByStatus(ctx)
	if cerr == nil {
		s.gauge.Store(&counts)
	} else if err == nil {
		err = wrap("notify.tick: counting", cerr) // the gauge is stale; the tick counts as failing
	}
	if err != nil {
		span.RecordError(err)
	}
	s.noteHealth(ctx, err, true)
}

// noteHealth logs the first failure of the loop (a tick or a sweep) after a good spell, and
// the recovery when a tick works again; a cancellation is neither. A sweep's success does not
// count as recovery.
func (s *Service) noteHealth(ctx context.Context, err error, isTick bool) {
	if err != nil && errs.Is(err, errs.Canceled) {
		return
	}
	s.logMu.Lock()
	was := s.unhealthy
	switch {
	case err != nil:
		s.unhealthy = true
	case isTick:
		s.unhealthy = false
	}
	s.logMu.Unlock()
	switch {
	case err != nil && !was:
		s.logger.WarnContext(ctx, "notify: the sender failed", slog.String("kind", errs.KindOf(err).String()))
	case err == nil && isTick && was:
		s.logger.InfoContext(ctx, "notify: the sender works again")
	}
}

// sweep ends rows past their expiry and purges old final rows.
func (s *Service) sweep(ctx context.Context) {
	if s.db.ReadOnly() {
		return
	}
	_, err := s.ExpireStale(ctx)
	if err == nil {
		_, err = s.Purge(ctx)
	}
	if err != nil {
		s.noteHealth(ctx, err, false)
	}
}

// ---- SendOnce ---------------------------------------------------------------------------

// SendOnce leases up to MaxBatch due rows and delivers them, returning counts. Delivery
// failures are recorded on the rows, not returned. When SMTP is not configured nothing is
// leased: it returns Sent{Skipped: n} for the n queued rows and logs once an hour. Fails
// errs.Unavailable wrapping db.ErrReadOnly (nothing leased), the settings Reader's kind, store
// kinds, and errs.Canceled when ctx ends mid-batch (the rows are given back).
func (s *Service) SendOnce(ctx context.Context) (res Sent, err error) {
	const op = "notify.Service.SendOnce"
	if s.db.ReadOnly() {
		return Sent{}, readOnlyErr(op)
	}
	cfg, err := SMTPFromSettings(ctx, s.settings)
	if err != nil {
		return Sent{}, seamError(op, "the SMTP settings", err)
	}
	if !cfg.Configured() {
		return s.skipUnconfigured(ctx, op)
	}
	ch, err := s.channel(cfg)
	if err != nil {
		return Sent{}, channelError(op, err)
	}
	now := s.now()
	var rows []message.Record
	err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var lerr error
		rows, lerr = s.store.Lease(ctx, now, MaxBatch, now.Add(s.lease))
		return wrap("lease", lerr)
	})
	if err != nil {
		return Sent{}, wrap(op, err)
	}
	for i, row := range rows {
		if cerr := ctx.Err(); cerr != nil {
			s.release(ctx, rows[i:])
			return res, errs.E(op, errs.KindOf(cerr), cerr)
		}
		// Every row of a batch shares one lease. Stop before a send that could outlive it:
		// another sender would take the row and the person would get two e-mails.
		if !s.now().Add(s.sendTimeout).Before(row.LeasedUntil) {
			s.release(ctx, rows[i:])
			return res, nil
		}
		if derr := s.deliver(ctx, ch, cfg, row, &res); derr != nil {
			if errs.Is(derr, errs.Canceled) {
				s.release(ctx, rows[i:])
			}
			return res, wrap(op, derr)
		}
	}
	return res, nil
}

// skipUnconfigured counts the queued rows and logs "messages wait" at most hourly.
func (s *Service) skipUnconfigured(ctx context.Context, op string) (Sent, error) {
	counts, err := s.store.CountOpenByStatus(ctx)
	if err != nil {
		return Sent{}, wrap(op, err)
	}
	n := counts[Queued]
	if n > 0 {
		s.metrics.deliver(ctx, outcomeNoSMTP, n)
		s.logMu.Lock()
		due := s.noSMTPLogAt.IsZero() || s.now().Sub(s.noSMTPLogAt) >= noSMTPLogEvery
		if due {
			s.noSMTPLogAt = s.now()
		}
		s.logMu.Unlock()
		if due {
			s.logger.InfoContext(ctx, "notify: e-mail is not configured; messages wait", slog.Int("waiting", n))
		}
	}
	return Sent{Skipped: n}, nil
}

// release gives leased rows back (due now, the attempt not counted) on a context that
// survives a cancel. A failure leaves the lease to expire.
func (s *Service) release(ctx context.Context, rows []message.Record) {
	if len(rows) == 0 {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	now := s.now()
	for _, r := range rows {
		if _, err := s.store.Release(rctx, r.ID, r.LeasedUntil, now, tokenCanceled); err != nil {
			s.logger.WarnContext(rctx, "notify: could not give a row back; its lease will expire",
				slog.String("message_id", r.ID.String()), slog.String("kind", errs.KindOf(err).String()))
			return
		}
	}
}

// deliver sends one leased row and records the outcome. A returned error ends the batch:
// errs.Canceled when the context ended (the caller gives the rest back), a store error
// otherwise (the leases expire by themselves).
func (s *Service) deliver(ctx context.Context, ch Channel, cfg SMTPConfig, row message.Record, res *Sent) (err error) {
	ctx, span := s.start(ctx, "deliver", telemetry.WithKind(telemetry.SpanClient))
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("message_id", row.ID), telemetry.Token("template", row.Template))
	now := s.now()

	to, aerr := s.addressOf(ctx, row)
	if aerr != nil {
		switch {
		case errs.Is(aerr, errs.NotFound):
			span.SetAttrs(telemetry.Token("outcome", outcomeSkipped), telemetry.Token("reason", tokenNoAddress))
			return s.skip(ctx, row, tokenNoAddress, res, now)
		case errs.Is(aerr, errs.Canceled):
			return aerr
		default:
			return s.retryOrFail(ctx, row, tokenLookup, res, now)
		}
	}
	if to.Validate() != nil { // a malformed address will not get better by waiting
		span.SetAttrs(telemetry.Token("outcome", outcomeRejected))
		return s.finish(ctx, row, tokenAddress, outcomeRejected, res, now)
	}

	env := Envelope{
		From: cfg.Sender, ReplyTo: cfg.ReplyTo, To: to, Subject: row.Subject, Body: row.Body,
		MessageID: row.ID.String() + "@" + s.hostname,
	}
	sctx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	serr := ch.Send(sctx, env)
	cancel()
	switch {
	case serr == nil:
		span.SetAttrs(telemetry.Token("outcome", outcomeOK))
		return s.record(ctx, row, outcomeOK, "", res, func(ctx context.Context) (bool, error) {
			return s.store.MarkSent(ctx, row.ID, row.LeasedUntil, s.now())
		})
	case errors.Is(serr, ErrRejected):
		span.SetAttrs(telemetry.Token("outcome", outcomeRejected))
		return s.finish(ctx, row, tokenRejected, outcomeRejected, res, now)
	case errs.Is(serr, errs.Canceled) || ctx.Err() != nil:
		return errs.E("notify.deliver", errs.Canceled, context.Canceled)
	default:
		return s.retryOrFail(ctx, row, tokenOf(serr), res, now)
	}
}

// tokenOf names a transient channel failure.
func tokenOf(err error) string {
	switch {
	case errors.Is(err, ErrAuth):
		return tokenAuth
	case errors.Is(err, ErrTemporary):
		return tokenSMTP4xx
	case errs.Is(err, errs.Timeout):
		return tokenTimeout
	default:
		return tokenConnect
	}
}

// addressOf returns the row's explicit address, else the person's current one.
func (s *Service) addressOf(ctx context.Context, row message.Record) (to model.Email, err error) {
	if row.Address != "" {
		return model.Email(row.Address), nil
	}
	to, err = s.addresses.AddressOf(ctx, row.Person)
	if err != nil {
		return "", seamError("notify.AddressOf", "the address lookup", err)
	}
	return to, nil
}

// retryOrFail requeues a row after a transient failure with the backoff, or fails it when
// its attempts are used up.
func (s *Service) retryOrFail(ctx context.Context, row message.Record, token string, res *Sent, now time.Time) error {
	if row.Attempts >= MaxAttempts {
		return s.finish(ctx, row, token, outcomeFailed, res, now)
	}
	next := now.Add(backoff.Delay(row.Attempts+1, 1))
	return s.record(ctx, row, outcomeRetry, token, res, func(ctx context.Context) (bool, error) {
		return s.store.Requeue(ctx, row.ID, row.LeasedUntil, token, next)
	})
}

// finish ends a row as failed with a token.
func (s *Service) finish(ctx context.Context, row message.Record, token, outcome string, res *Sent, now time.Time) error {
	return s.record(ctx, row, outcome, token, res, func(ctx context.Context) (bool, error) {
		return s.store.MarkFailed(ctx, row.ID, row.LeasedUntil, token, now)
	})
}

// skip ends a row as skipped with a token: the recipient has no address, so there is nothing
// to retry or to keep (D57). Counted in Sent.Skipped and delivered{outcome=skipped}.
func (s *Service) skip(ctx context.Context, row message.Record, token string, res *Sent, now time.Time) error {
	return s.record(ctx, row, outcomeSkipped, token, res, func(ctx context.Context) (bool, error) {
		return s.store.MarkSkipped(ctx, row.ID, row.LeasedUntil, token, now)
	})
}

// record runs the update of a row's outcome in a transaction of its own, on a context that
// survives a shutdown (a message delivered just before it must be marked sent, or it would
// be released and sent again), then counts it and logs a final failure by ID, template and
// error token. A fenced update (the row is gone or was re-leased) is logged by ID and is not
// an error. Any store error ends the batch.
func (s *Service) record(ctx context.Context, row message.Record, outcome, token string, res *Sent, update func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	var changed bool
	err := s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var uerr error
		changed, uerr = update(ctx)
		return uerr
	})
	if err != nil {
		return err //nolint:wrapcheck // SendOnce wraps it
	}
	if !changed {
		s.logger.InfoContext(ctx, "notify: a row changed under the sender; its outcome was not recorded",
			slog.String("message_id", row.ID.String()))
		return nil
	}
	s.metrics.deliver(ctx, outcome, 1)
	switch outcome {
	case outcomeOK:
		res.Sent++
	case outcomeRetry:
		res.Retried++
	case outcomeSkipped:
		res.Skipped++
		s.logger.InfoContext(ctx, "notify: a message was skipped",
			slog.String("message_id", row.ID.String()), slog.String("template", row.Template),
			slog.String("reason", token))
	default:
		res.Failed++
		s.logger.WarnContext(ctx, "notify: a message failed for good",
			slog.String("message_id", row.ID.String()), slog.String("template", row.Template),
			slog.String("error", token))
	}
	return nil
}

// ---- Ping, Purge, ExpireStale -----------------------------------------------------------

// Ping builds a channel from the current settings and checks the conversation. Fails
// errs.Unsupported wrapping ErrNotConfigured, the settings Reader's kind, and the channel's kinds.
func (s *Service) Ping(ctx context.Context) (err error) {
	const op = "notify.Service.Ping"
	ctx, span := s.start(ctx, "ping", telemetry.WithKind(telemetry.SpanClient))
	defer endSpan(span, &err)
	cfg, err := SMTPFromSettings(ctx, s.settings)
	if err != nil {
		return seamError(op, "the SMTP settings", err)
	}
	if !cfg.Configured() {
		return errs.E(op, errs.Unsupported, ErrNotConfigured)
	}
	ch, err := s.channel(cfg)
	if err != nil {
		return channelError(op, err)
	}
	pctx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()
	return channelError(op, ch.Ping(pctx))
}

// Purge deletes Sent, Failed and Skipped rows older than the retention, in batches, and returns how
// many. A retention of 0 deletes nothing. A retention under
// MinRetention is read as MinRetention. Fails errs.Unavailable wrapping db.ErrReadOnly, the
// settings Reader's kind and store kinds.
func (s *Service) Purge(ctx context.Context) (n int64, err error) {
	const op = "notify.Service.Purge"
	ctx, span := s.start(ctx, "purge")
	defer endSpan(span, &err)
	if s.db.ReadOnly() {
		return 0, readOnlyErr(op)
	}
	keep, err := retentionFrom(ctx, s.settings)
	if err != nil {
		return 0, seamError(op, "the retention setting", err)
	}
	if keep <= 0 {
		return 0, nil
	}
	keep = max(keep, MinRetention)
	before := s.now().Add(-keep)
	for {
		got, err := s.store.Purge(ctx, before, purgeBatch)
		if err != nil {
			return n, wrap(op, err)
		}
		n += got
		s.metrics.purge(ctx, got)
		if got < purgeBatch {
			span.SetAttrs(telemetry.Int("purged", int(n)))
			return n, nil
		}
	}
}

// ExpireStale clears the text and address of every row past its expiry, in batches, and
// returns how many rows it changed. A queued row (or one whose lease ran out) becomes failed
// "expired" and counts under delivered{expired}; a failed row is only cleared and does not.
// A row being sent is left to its sender. It does not depend on the retention. Fails
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) ExpireStale(ctx context.Context) (n int64, err error) {
	const op = "notify.Service.ExpireStale"
	ctx, span := s.start(ctx, "expire")
	defer endSpan(span, &err)
	if s.db.ReadOnly() {
		return 0, readOnlyErr(op)
	}
	var ended int64
	for {
		changed, expired, err := s.store.ExpireStale(ctx, s.now(), purgeBatch)
		if err != nil {
			return n, wrap(op, err)
		}
		n += changed
		ended += expired
		if changed < purgeBatch {
			s.metrics.deliver(ctx, outcomeExpired, int(ended))
			span.SetAttrs(telemetry.Int("expired", int(n)))
			return n, nil
		}
	}
}
