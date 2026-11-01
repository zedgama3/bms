package files

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// nearDeadline reports whether ctx's deadline is within softDeadline: a long run then stops
// cleanly and leaves the rest to the next one.
func nearDeadline(ctx context.Context) bool {
	d, ok := ctx.Deadline()
	return ok && time.Until(d) < softDeadline
}

// ctxDone returns the errs error of a finished ctx, or nil.
func ctxDone(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	return nil
}

// recordRan writes one files.sweep.ran entry as the system.
func (s *Service) recordRan(ctx context.Context, action audit.Action, reason string, actor audit.Actor) error {
	err := s.audit.Record(ctx, audit.Entry{Action: action, Outcome: audit.Succeeded, Actor: actor, Reason: reason})
	if err != nil {
		return fmt.Errorf("record %s: %w", action, err)
	}
	return nil
}

// Reap deletes the objects of outstanding tombstones and then the tombstones, oldest first.
// A delete that fails counts an attempt and the run goes on; the run ends when none remain or
// the deadline nears. It is the hourly job files.reap and runs on the writing copy only: it
// fails errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and with the store's
// kind when the tombstone table cannot be read.
func (s *Service) Reap(ctx context.Context) (res Swept, err error) {
	const op = "files.Service.Reap"
	ctx, span := s.tracer.Start(ctx, "files.reap")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	if s.db.ReadOnly() {
		return Swept{}, readOnlyErr(op)
	}
	kind := s.objects.Kind().String()
	var afterAt time.Time
	var afterID [16]byte
	for {
		if cerr := ctxDone(ctx, op); cerr != nil {
			return res, cerr
		}
		if nearDeadline(ctx) {
			break
		}
		batch, terr := s.store.Tombstones(ctx, kind, afterAt, afterID, batchSize)
		if terr != nil {
			return res, fmt.Errorf("%s: list tombstones: %w", op, terr)
		}
		for _, t := range batch {
			if nearDeadline(ctx) {
				break
			}
			derr := s.objects.Delete(ctx, objects.Bucket(t.Bucket), objects.Key(t.ObjectKey))
			if derr == nil {
				derr = s.store.DeleteTombstone(ctx, t.ID)
				if derr != nil {
					return res, fmt.Errorf("%s: remove a tombstone: %w", op, derr)
				}
				res.Tombstones++
				continue
			}
			if k, ok := ctxKind(derr); ok {
				return res, errs.E(op, k, derr)
			}
			if berr := s.store.BumpTombstone(ctx, t.ID); berr != nil {
				return res, fmt.Errorf("%s: count an attempt: %w", op, berr)
			}
		}
		if len(batch) < batchSize {
			break
		}
		last := batch[len(batch)-1]
		afterAt, afterID = last.CreatedAt, last.ID
	}
	add(s.metrics.swept, ctx, int64(res.Tombstones), telemetry.Token("what", "tombstone"))
	outstanding, stuck, cerr := s.store.CountTombstones(ctx, stuckAttempts)
	if cerr != nil {
		return res, fmt.Errorf("%s: count tombstones: %w", op, cerr)
	}
	s.tombstones.Store(outstanding)
	if stuck > 0 {
		s.logger.WarnContext(ctx, "object deletions are failing repeatedly",
			slog.Int64("tombstones_stuck", stuck), slog.Int64("tombstones_outstanding", outstanding))
	}
	if res.Tombstones > 0 {
		if rerr := s.recordRan(ctx, actionSwept, "tombstones:"+strconv.Itoa(res.Tombstones), audit.Actor{Kind: audit.ActorSystem}); rerr != nil {
			return res, fmt.Errorf("%s: %w", op, rerr)
		}
	}
	return res, nil
}

// SweepOrphans removes temp files and objects older than OrphanAge that no row and no
// tombstone refers to: what a crash between Stage's Put and Attach leaves. It never deletes
// a row, and keeps every younger object (a Staged one may still be attached). It is the
// daily job files.sweep-orphans, on the writing copy only: errs.Unavailable wrapping
// db.ErrReadOnly on a read-only copy, errs.Unavailable when the store cannot list.
func (s *Service) SweepOrphans(ctx context.Context) (res Swept, err error) {
	const op = "files.Service.SweepOrphans"
	ctx, span := s.tracer.Start(ctx, "files.sweep")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	if s.db.ReadOnly() {
		return Swept{}, readOnlyErr(op)
	}
	cutoff := s.now().Add(-OrphanAge)
	temp, terr := s.sweepTemp(ctx, op, cutoff)
	res.TempFiles = temp
	if terr != nil {
		return res, terr
	}
	kind := s.objects.Kind().String()
	buckets, berr := s.store.Buckets(ctx, kind)
	if berr != nil {
		return res, fmt.Errorf("%s: list buckets: %w", op, berr)
	}
	seen := map[string]bool{}
	var all []string
	for _, b := range append(buckets, s.knownOwners()...) {
		if !seen[b] {
			seen[b] = true
			all = append(all, b)
		}
	}
	for _, b := range all {
		if objects.Bucket(b).Validate() != nil {
			continue
		}
		n, lerr := s.sweepBucket(ctx, op, kind, objects.Bucket(b), cutoff)
		res.Orphans += n
		if lerr != nil {
			return res, lerr
		}
	}
	add(s.metrics.swept, ctx, int64(res.Orphans), telemetry.Token("what", "orphan"))
	add(s.metrics.swept, ctx, int64(res.TempFiles), telemetry.Token("what", "temp"))
	if res.Orphans > 0 || res.TempFiles > 0 {
		reason := "orphans:" + strconv.Itoa(res.Orphans) + ",temp:" + strconv.Itoa(res.TempFiles)
		if rerr := s.recordRan(ctx, actionSwept, reason, audit.Actor{Kind: audit.ActorSystem}); rerr != nil {
			return res, fmt.Errorf("%s: %w", op, rerr)
		}
	}
	return res, nil
}

// sweepTemp removes spooled files older than cutoff from the temp directory.
func (s *Service) sweepTemp(ctx context.Context, op string, cutoff time.Time) (int, error) {
	ents, err := os.ReadDir(s.tempDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, errs.E(op, errs.Unavailable, fmt.Errorf("read the temp directory: %w", err))
	}
	n := 0
	for _, e := range ents {
		if cerr := ctxDone(ctx, op); cerr != nil {
			return n, cerr
		}
		if !e.Type().IsRegular() {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			if errors.Is(ierr, os.ErrNotExist) {
				continue
			}
			return n, errs.E(op, errs.Unavailable, fmt.Errorf("stat a temp file: %w", ierr))
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if rerr := os.Remove(filepath.Join(s.tempDir, e.Name())); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			return n, errs.E(op, errs.Unavailable, fmt.Errorf("remove a temp file: %w", rerr))
		}
		n++
	}
	return n, nil
}

// sweepBucket deletes the old, unknown objects of one bucket.
func (s *Service) sweepBucket(ctx context.Context, op, kind string, b objects.Bucket, cutoff time.Time) (int, error) {
	removed := 0
	var keys []string
	var failed []error // deletes that failed: the sweep goes on and reports them at the end
	flush := func() error {
		if len(keys) == 0 {
			return nil
		}
		unknown, err := s.store.UnknownKeys(ctx, kind, string(b), keys)
		keys = keys[:0]
		if err != nil {
			return fmt.Errorf("%s: look up objects: %w", op, err)
		}
		for _, k := range unknown {
			if derr := s.objects.Delete(ctx, b, objects.Key(k)); derr != nil {
				if _, done := ctxKind(derr); done {
					return fmt.Errorf("%s: delete an orphan: %w", op, derr)
				}
				failed = append(failed, fmt.Errorf("%s: delete an orphan: %w", op, derr))
				continue
			}
			removed++
		}
		return nil
	}
	err := s.objects.List(ctx, b, "", func(e objects.Entry) error {
		if e.Key == objects.ProbeKey || !e.Modified.Before(cutoff) {
			return nil
		}
		keys = append(keys, string(e.Key))
		if len(keys) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("%s: list %s: %w", op, b, err)
	}
	if err := flush(); err != nil {
		return removed, err
	}
	return removed, errors.Join(failed...)
}

// Verify heads the object of every row and counts the ones that are missing, or whose size or
// checksum differs from the row's; it also counts the tombstones outstanding. It deletes and
// changes nothing, and runs on any copy. It fails errs.Unavailable when the store cannot be
// reached (a missing object is a count, not an error). Rows kept in another store than the
// configured one are skipped.
func (s *Service) Verify(ctx context.Context) (res Verified, err error) {
	const op = "files.Service.Verify"
	ctx, span := s.tracer.Start(ctx, "files.verify")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	kind := s.objects.Kind().String()
	var after [16]byte
	skipped := 0
	for {
		if cerr := ctxDone(ctx, op); cerr != nil {
			return res, cerr
		}
		batch, perr := s.store.VerifyPage(ctx, after, batchSize)
		if perr != nil {
			return res, fmt.Errorf("%s: list files: %w", op, perr)
		}
		for _, r := range batch {
			if r.Store != kind {
				skipped++
				continue
			}
			info, herr := s.objects.Head(ctx, objects.Bucket(r.Bucket), objects.Key(r.ObjectKey))
			switch {
			case errors.Is(herr, objects.ErrMissing):
				res.Missing++
				add(s.metrics.verify, ctx, 1, telemetry.Token("outcome", "missing"))
				s.logger.WarnContext(ctx, "a file's object is missing", slog.String("file_id", FileID(r.ID).String()))
			case herr != nil:
				return res, fmt.Errorf("%s: head an object: %w", op, herr)
			default:
				res.Checked++
				switch {
				case info.Size != r.Size || (info.SHA256 != ([32]byte{}) && info.SHA256 != r.SHA256):
					res.Mismatched++
					add(s.metrics.verify, ctx, 1, telemetry.Token("outcome", "mismatched"))
					s.logger.WarnContext(ctx, "a file's object differs from its row", slog.String("file_id", FileID(r.ID).String()))
				case info.SHA256 == ([32]byte{}):
					add(s.metrics.verify, ctx, 1, telemetry.Token("outcome", "unchecked"))
				default:
					add(s.metrics.verify, ctx, 1, telemetry.Token("outcome", "ok"))
				}
			}
		}
		if len(batch) < batchSize {
			break
		}
		after = batch[len(batch)-1].ID
	}
	if skipped > 0 {
		s.logger.WarnContext(ctx, "files held by another store were not checked", slog.Int("files_skipped", skipped), slog.String("store", kind))
	}
	outstanding, _, cerr := s.store.CountTombstones(ctx, stuckAttempts)
	if cerr != nil {
		return res, fmt.Errorf("%s: count tombstones: %w", op, cerr)
	}
	res.Tombstones = int(outstanding)
	s.tombstones.Store(outstanding)
	reason := "checked:" + strconv.Itoa(res.Checked) + ",missing:" + strconv.Itoa(res.Missing) +
		",mismatched:" + strconv.Itoa(res.Mismatched) + ",tombstones:" + strconv.Itoa(res.Tombstones)
	if rerr := s.recordRan(ctx, actionVerified, reason, audit.Actor{}); rerr != nil {
		return res, fmt.Errorf("%s: %w", op, rerr)
	}
	return res, nil
}

// Ping reports whether the temp directory is writable and the object store usable. It fails
// errs.Unavailable, or errs.Invalid wrapping objects.ErrPublic when the store answers an
// anonymous read, or errs.Timeout.
func (s *Service) Ping(ctx context.Context) error {
	const op = "files.Service.Ping"
	if cerr := ctxDone(ctx, op); cerr != nil {
		return cerr
	}
	probe := filepath.Join(s.tempDir, tempFilePattern+model.NewID[FileID]().String())
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a fresh name under the temp directory
	if err != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("the temp directory is not writable: %w", err))
	}
	_, werr := f.Write([]byte("bms"))
	cerr := f.Close()
	rerr := os.Remove(probe)
	if err := errors.Join(werr, cerr, rerr); err != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("the temp directory is not writable: %w", err))
	}
	if err := s.objects.Ping(ctx); err != nil {
		if errors.Is(err, objects.ErrPublic) {
			return errs.E(op, errs.Invalid, err)
		}
		if k := errs.KindOf(err); k == errs.Timeout || k == errs.Canceled {
			return errs.E(op, k, err)
		}
		return errs.E(op, errs.Unavailable, err)
	}
	return nil
}

// Start runs the store probe once, for serve. Only a positive finding that the bucket is
// publicly readable fails it (errs.Invalid wrapping objects.ErrPublic): the process must not
// add files to an exposed bucket. Any other failure is logged once at error level and Start
// returns nil, so the rest of bms serves while an operator fixes storage. It never writes a
// row.
func (s *Service) Start(ctx context.Context) error {
	const op = "files.Service.Start"
	pctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	err := s.Ping(pctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, objects.ErrPublic):
		return errs.E(op, errs.Invalid, err)
	}
	s.logger.ErrorContext(ctx, "the file store is not usable; file operations will fail until it is fixed",
		slog.String("error.kind", errs.KindOf(err).String()), slog.String("store", s.objects.Kind().String()))
	return nil
}

// Purge deletes every object of every bucket this service has scoped (Scoped was asked for
// its owner) and every entry of the temp directory, and counts them. It leaves the file
// rows alone: the demo reset that calls it drops the schema next. It runs on the writing
// copy only: it fails errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and
// errs.Unavailable when the object store or the temp directory cannot be listed or deleted
// from; a canceled or expired ctx is errs.Canceled or errs.Timeout. A bucket the store has
// not created yet counts as empty. On failure the count is what was deleted so far.
func (s *Service) Purge(ctx context.Context) (deleted int, err error) {
	const op = "files.Service.Purge"
	ctx, span := s.tracer.Start(ctx, "files.purge")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	if s.db.ReadOnly() {
		return 0, readOnlyErr(op)
	}
	for _, name := range s.knownOwners() {
		if cerr := ctxDone(ctx, op); cerr != nil {
			return deleted, cerr
		}
		b := objects.Bucket(name)
		var keys []objects.Key
		lerr := s.objects.List(ctx, b, "", func(e objects.Entry) error {
			keys = append(keys, e.Key)
			return nil
		})
		if lerr != nil && !errs.Is(lerr, errs.NotFound) {
			return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("list bucket %s: %w", name, lerr))
		}
		for _, k := range keys {
			if cerr := ctxDone(ctx, op); cerr != nil {
				return deleted, cerr
			}
			if derr := s.objects.Delete(ctx, b, k); derr != nil {
				return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("delete in bucket %s: %w", name, derr))
			}
			deleted++
		}
	}
	ents, rerr := os.ReadDir(s.tempDir)
	switch {
	case errors.Is(rerr, os.ErrNotExist):
		return deleted, nil
	case rerr != nil:
		return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("read the temp directory: %w", rerr))
	}
	for _, e := range ents {
		if derr := os.RemoveAll(filepath.Join(s.tempDir, e.Name())); derr != nil {
			return deleted, errs.E(op, errs.Unavailable, fmt.Errorf("remove a temp file: %w", derr))
		}
		deleted++
	}
	return deleted, nil
}
