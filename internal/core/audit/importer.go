package audit

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Imported says what one import did. Failed holds file names (never contents) renamed
// ".failed" for the operator.
type Imported struct {
	Files      int // spool files imported and deleted
	Entries    int // entries newly inserted into the table
	Duplicates int // lines whose ID was already in the table (a re-run after a crash)
	Truncated  int // final lines without a newline, dropped
	Failed     []string
}

// Outcomes of bms.audit.imported.
const (
	outcomeOK        = "ok"
	outcomeDuplicate = "duplicate"
	outcomeTruncated = "truncated"
	outcomeFailed    = "failed"
)

// maxLineBytes is the longest spool line (newline included) the importer reads; a longer
// one is a bad line. A record is far smaller: 64 fields of 64 bytes plus a reason is
// about 5 KiB.
const maxLineBytes = 64 << 10

// Run is the importer loop, an app runnable: it creates the spool directory (0700; failing
// errs.Invalid under "data.dir", F33), imports once, then every ImportInterval while the
// copy can write. It returns nil when ctx is done or Close is called (F38); errs.Internal
// after Close. Only one Run per Log (a second is errs.Conflict, F39).
func (l *Log) Run(ctx context.Context) error {
	const op = "audit.Log.Run"
	l.mu.Lock()
	switch {
	case l.closed:
		l.mu.Unlock()
		return errs.E(op, errs.Internal, errors.New("the audit log is closed"))
	case l.running:
		l.mu.Unlock()
		return errs.E(op, errs.Conflict, errors.New("the importer is already running"))
	}
	l.running = true
	l.runDone = make(chan struct{})
	done := l.runDone
	l.mu.Unlock()
	defer close(done)

	if err := ensureDir(l.spoolDir); err != nil {
		return errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "data.dir", Code: validate.CodeMalformed,
			Reason: "the audit spool directory cannot be created: " + scrubPathError(err),
		})
	}
	// Close must not return while an import transaction is open, so Close's signal cancels
	// the context the imports run on. The watcher below ends with that context and is
	// waited for before Run returns.
	ictx, cancel := context.WithCancel(ctx)
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		select {
		case <-l.stop:
			cancel()
		case <-ictx.Done():
		}
	}()
	defer func() {
		cancel()
		watcher.Wait()
	}()

	skipping := false
	tick := func() {
		if l.opts.DB.ReadOnly() {
			if !skipping {
				l.logger.DebugContext(ictx, "audit import paused: this copy is read-only")
			}
			skipping = true
			return
		}
		if skipping {
			l.logger.DebugContext(ictx, "audit import resumed: this copy can write")
			skipping = false
		}
		l.importAndLog(ictx)
	}
	tick()
	// The ticker starts after the first import, so a long first import does not queue a
	// tick behind it.
	ticker, err := l.clock.NewTicker(l.opts.interval())
	if err != nil {
		return errs.E(op, errs.Internal, err)
	}
	defer ticker.Stop()
	for {
		select {
		case <-ictx.Done():
			return nil
		case <-ticker.C():
			tick()
		}
	}
}

// scrubPathError returns the operating system's reason without the path it names.
func scrubPathError(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "unusable path"
}

// importAndLog runs one import and reports what it did. It is the place where an import
// error is handled, so it is the one that logs it.
func (l *Log) importAndLog(ctx context.Context) {
	res, err := l.ImportOnce(ctx)
	if res.Files > 0 || res.Entries > 0 || res.Duplicates > 0 || res.Truncated > 0 || len(res.Failed) > 0 {
		l.logger.InfoContext(ctx, "audit spool imported",
			slog.Int("files", res.Files), slog.Int("entries", res.Entries),
			slog.Int("duplicates", res.Duplicates), slog.Int("truncated", res.Truncated),
			slog.Int("failed", len(res.Failed)))
	}
	switch {
	case err == nil, ctx.Err() != nil, readOnlyErr(err):
	case errs.Is(err, errs.Invalid) && len(res.Failed) > 0: // already warned, with the line and field, where the file was set aside
	default:
		l.logger.ErrorContext(ctx, "audit spool import failed; will retry",
			slog.String("kind", errs.KindOf(err).String()), slog.Any("error", err))
	}
}

// ImportOnce rotates the active spool file, then imports every complete file in the spool
// directory, in name (time) order, each in batches of MaxBatch per transaction with
// ON CONFLICT (id) DO NOTHING, deleting a file once all its lines are in. Serialized: a
// concurrent call waits. Returns errs.Unavailable wrapping db.ErrReadOnly without touching
// a file when the copy cannot write (F32); errs.Unavailable when the directory or a file
// cannot be read or an insert fails (the file is kept, F36; a deadline or a cancel is
// returned as errs.Timeout or errs.Canceled);
// errs.Invalid, after the other files are done, when a file had an unparseable line (F34) or
// the database refused a record for what it holds (a CHECK violation or another data
// condition; not an authentication or connection failure): the records before it are
// inserted, the file is renamed .failed and the warning names the file and the 1-based line
// (F36b). A missing directory is not an
// error (nothing to import).
func (l *Log) ImportOnce(ctx context.Context) (out Imported, err error) {
	const op = "audit.Log.ImportOnce"
	ctx, span := l.tracer.Start(ctx, spanImport)
	defer span.End()
	defer func() {
		span.SetAttrs(telemetry.Int("files", out.Files), telemetry.Int("entries", out.Entries))
		if err != nil {
			span.RecordError(err)
		}
	}()

	select {
	case l.importMu <- struct{}{}:
		defer func() { <-l.importMu }()
	case <-ctx.Done():
		return out, errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
	}
	if l.opts.DB.ReadOnly() {
		return out, errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	if err := l.spool.rotate(); err != nil {
		return out, fmt.Errorf("%s: rotate the active spool file: %w", op, err)
	}
	entries, err := os.ReadDir(l.spoolDir)
	if err != nil {
		if missing(err) {
			return out, nil
		}
		return out, errs.E(op, errs.Unavailable, fmt.Errorf("read spool directory: %w", err))
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, spoolSuffix) || name == l.spool.active() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, errs.E(op, errs.KindOf(err), err)
		}
		if err := l.importFile(ctx, name, &out); err != nil {
			return out, fmt.Errorf("%s: %w", op, err)
		}
	}
	if len(out.Failed) > 0 {
		return out, errs.E(op, errs.Invalid, fmt.Errorf("%d spool file(s) hold a line that cannot be imported and were renamed %s", len(out.Failed), failedSuffix))
	}
	return out, nil
}

// importFile imports one spool file into the table and removes it, or sets it aside as
// .failed after inserting the lines before the bad one. It adds to out as it goes.
func (l *Log) importFile(ctx context.Context, name string, out *Imported) error {
	path := filepath.Join(l.spoolDir, name)
	bad, err := l.insertFile(ctx, path, name, out)
	if err != nil {
		return err
	}
	if bad {
		failedPath := path + failedSuffix
		if err := os.Rename(path, failedPath); err != nil {
			return errs.E("audit.Log.importFile", errs.Unavailable, fmt.Errorf("set aside %s: %w", name, err))
		}
		out.Failed = append(out.Failed, name)
		l.metrics.importedN(ctx, 1, outcomeFailed)
		return nil
	}
	if err := os.Remove(path); err != nil {
		return errs.E("audit.Log.importFile", errs.Unavailable, fmt.Errorf("remove %s: %w", name, err))
	}
	out.Files++
	return nil
}

// insertFile reads path line by line and inserts the records in batches. It reports bad
// true when a complete line was unusable (the lines before it are in). A truncated final
// line is counted and dropped.
func (l *Log) insertFile(ctx context.Context, path, name string, out *Imported) (bad bool, err error) {
	const op = "audit.Log.insertFile"
	f, err := os.Open(path) //nolint:gosec // a file listed from the spool directory
	if err != nil {
		return false, errs.E(op, errs.Unavailable, fmt.Errorf("open %s: %w", name, err))
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, errs.E(op, errs.Unavailable, fmt.Errorf("close %s: %w", name, cerr)))
		}
	}()

	r := bufio.NewReaderSize(f, maxLineBytes)
	batch := make([]Record, 0, MaxBatch)
	lines := make([]int, 0, MaxBatch) // lines[i] is the 1-based line of batch[i]
	// flush inserts the batch, bisecting when the database refuses a record for what the record
	// holds (dataRefusal), so every record before the first refused one is in. It reports
	// aside true then, after warning with the file name and the 1-based line of the refused
	// record, never a value. Any other failure is returned and the file is kept for the next
	// try (F36).
	flush := func() (aside bool, err error) {
		if len(batch) == 0 {
			return false, nil
		}
		defer func() { batch, lines = batch[:0], lines[:0] }()
		refused, ierr := l.insertRows(ctx, batch, out)
		switch {
		case ierr != nil:
			return false, ierr
		case refused >= 0:
			l.logger.WarnContext(ctx, "audit spool file failed to import and was set aside",
				slog.String("file", name), slog.Int("line", lines[refused]), slog.String("problem", reasonInsertInvalid))
			return true, nil
		}
		return false, nil
	}
	setAside := func(lineNo int, reason string) (bool, error) {
		if aside, ferr := flush(); ferr != nil || aside {
			return aside, ferr // the insert refusal was warned about where it happened
		}
		l.logger.WarnContext(ctx, "audit spool file failed to import and was set aside",
			slog.String("file", name), slog.Int("line", lineNo), slog.String("problem", reason))
		return true, nil
	}
	for lineNo := 1; ; lineNo++ {
		// ReadSlice never grows its buffer, so a line longer than maxLineBytes is refused
		// instead of read into memory.
		line, rerr := r.ReadSlice('\n')
		switch {
		case errors.Is(rerr, bufio.ErrBufferFull):
			return setAside(lineNo, "line too long")
		case errors.Is(rerr, io.EOF):
			if len(line) > 0 {
				out.Truncated++
				l.metrics.importedN(ctx, 1, outcomeTruncated)
				l.logger.WarnContext(ctx, "audit spool file ends in an incomplete line, which was dropped", slog.String("file", name))
			}
			return flush()
		case rerr != nil:
			return false, errs.E(op, errs.Unavailable, fmt.Errorf("read %s: %w", name, rerr))
		}
		rec, derr := decodeWire(bytes.TrimSuffix(line, []byte("\n")))
		if derr != nil {
			return setAside(lineNo, badLineReason(derr))
		}
		batch = append(batch, rec)
		lines = append(lines, lineNo)
		if len(batch) == MaxBatch {
			if aside, ferr := flush(); ferr != nil || aside {
				return aside, ferr
			}
		}
		if cerr := ctx.Err(); cerr != nil {
			return false, errs.E(op, errs.KindOf(cerr), cerr)
		}
	}
}

// reasonInsertInvalid is the reason class logged when the database refuses a batch as
// errs.Invalid; it names no value.
const reasonInsertInvalid = "the database refused a record as invalid"

// badLineReason says why a line was refused without repeating any of it: the name of the
// first field that failed validation, or the class of the structural problem.
func badLineReason(err error) string {
	if fields := validate.Fields(err); len(fields) > 0 {
		return "field " + fields[0].Field
	}
	return "not a valid record"
}

// dataRefusal reports whether the database refused a record for what the record holds: a
// *db.Error whose SQLSTATE is a data exception (class 22) or a not-null, foreign-key,
// unique or check violation (23502, 23503, 23505, 23514). Retrying cannot help. An
// errs.Invalid from anything else (a failed login after a rotated password, a missing
// database) says nothing about the record and is transient for the importer.
func dataRefusal(err error) bool {
	var de *db.Error
	if !errors.As(err, &de) {
		return false
	}
	return strings.HasPrefix(de.Code, "22") || slices.Contains([]string{"23502", "23503", "23505", "23514"}, de.Code)
}

// insertRows inserts batch and returns -1, or, when the database refuses a record for what
// it holds, the index of the first refused record after inserting every record before it
// (bisecting the batch; inserts are idempotent through ON CONFLICT). Any other failure is
// returned unchanged as an error.
func (l *Log) insertRows(ctx context.Context, batch []Record, out *Imported) (int, error) {
	err := l.insertBatch(ctx, batch, out)
	switch {
	case err == nil:
		return -1, nil
	case !errs.Is(err, errs.Invalid):
		return -1, err
	case len(batch) == 1:
		return 0, nil
	}
	mid := len(batch) / 2
	first, err := l.insertRows(ctx, batch[:mid], out)
	if err != nil || first >= 0 {
		return first, err
	}
	second, err := l.insertRows(ctx, batch[mid:], out)
	if err != nil || second < 0 {
		return -1, err
	}
	return mid + second, nil
}

// insertBatch inserts one batch in a Write transaction, dropping IDs already present. It
// fails Invalid when the database refuses a record for what it holds (dataRefusal), Timeout
// or Canceled as the context says, and Unavailable otherwise.
func (l *Log) insertBatch(ctx context.Context, batch []Record, out *Imported) error {
	const op = "audit.Log.insertBatch"
	rows := make([]postgres.Row, len(batch))
	for i, r := range batch {
		rows[i] = toRow(r)
	}
	var inserted int
	err := l.opts.DB.Transact(ctx, db.Write, func(ctx context.Context) error {
		n, err := l.store.Import(ctx, rows)
		if err != nil {
			return fmt.Errorf("import batch: %w", err)
		}
		inserted = n
		return nil
	})
	if err != nil {
		kind := errs.Unavailable
		switch k := errs.KindOf(err); {
		case k == errs.Canceled || k == errs.Timeout:
			kind = k
		case k == errs.Invalid && dataRefusal(err):
			kind = errs.Invalid
		}
		return errs.E(op, kind, err)
	}
	inserted = min(inserted, len(batch))
	dups := len(batch) - inserted
	out.Entries += inserted
	out.Duplicates += dups
	l.metrics.importedN(ctx, inserted, outcomeOK)
	l.metrics.importedN(ctx, dups, outcomeDuplicate)
	return nil
}
