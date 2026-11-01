package audit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zedgama3/bms/internal/core/audit/postgres"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Instrument names (telemetry.md).
const (
	metricRecorded       = "bms.audit.recorded"
	metricRecordFailures = "bms.audit.record_failures"
	metricImported       = "bms.audit.imported"
	metricSpoolFiles     = "bms.audit.spool_files"
)

// Span names, and the labels of the record metric and the failed-spool subtype.
const (
	spanRecord    = "audit.record"
	spanImport    = "audit.import"
	viaDB         = "db"
	viaSpool      = "spool"
	subtypeFailed = "failed"
)

// Log is the audit log of this process. Safe for concurrent use.
type Log struct {
	opts     Options
	clock    clock.Clock
	logger   *slog.Logger
	tracer   telemetry.Tracer
	store    *postgres.Store
	spoolDir string
	spool    *spool
	encode   func(wireRecord) ([]byte, error) // the line encoder; a seam for fault injection in tests

	metrics *counterSet

	mu       sync.Mutex
	closed   bool
	running  bool
	stop     chan struct{} // closed by Close
	runDone  chan struct{} // closed when Run has returned; nil until Run starts
	importMu chan struct{} // capacity 1: serialises ImportOnce, ctx-aware
}

// counterSet holds the optional instruments. Every method is a no-op when the Meter was nil.
type counterSet struct {
	recorded telemetry.Counter
	failures telemetry.Counter
	imported telemetry.Counter
}

func (c *counterSet) record(ctx context.Context, n int, via string) {
	if c == nil || c.recorded == nil {
		return
	}
	c.recorded.Add(ctx, int64(n), telemetry.Token("via", via))
}

func (c *counterSet) fail(ctx context.Context, err error) {
	if c == nil || c.failures == nil {
		return
	}
	c.failures.Add(ctx, 1, telemetry.Token("kind", errs.KindOf(err).String()))
}

func (c *counterSet) importedN(ctx context.Context, n int, outcome string) {
	if c == nil || c.imported == nil || n <= 0 {
		return
	}
	c.imported.Add(ctx, int64(n), telemetry.Token("outcome", outcome))
}

// New validates Options and returns the log. No I/O: the spool directory is created by
// Run or by the first spool write, so New succeeds against db.None() (app decision 7) and
// on a read-only copy. Fails errs.Invalid (F1-F4), or the Meter's kind when an instrument
// cannot be created.
func New(o Options) (*Log, error) {
	const op = "audit.New"
	if err := o.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	store, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	dir := filepath.Join(o.DataDir, filepath.FromSlash(SpoolDirName))
	l := &Log{
		opts:     o,
		clock:    o.Clock,
		logger:   o.Logger,
		tracer:   o.Tracer,
		store:    store,
		spoolDir: dir,
		encode:   encodeWire,
		stop:     make(chan struct{}),
		importMu: make(chan struct{}, 1),
	}
	l.spool = newSpool(dir, o.Clock, o.Logger)
	if o.Meter != nil {
		if l.metrics, err = l.instruments(o.Meter); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	}
	return l, nil
}

func (l *Log) instruments(m telemetry.Meter) (*counterSet, error) {
	var (
		c   counterSet
		err error
	)
	if c.recorded, err = m.Counter(metricRecorded); err != nil {
		return nil, fmt.Errorf("instrument %s: %w", metricRecorded, err)
	}
	if c.failures, err = m.Counter(metricRecordFailures); err != nil {
		return nil, fmt.Errorf("instrument %s: %w", metricRecordFailures, err)
	}
	if c.imported, err = m.Counter(metricImported); err != nil {
		return nil, fmt.Errorf("instrument %s: %w", metricImported, err)
	}
	if err = m.Gauge(metricSpoolFiles, l.observeSpool); err != nil {
		return nil, fmt.Errorf("instrument %s: %w", metricSpoolFiles, err)
	}
	return &c, nil
}

// observeSpool is the spool_files gauge: how many importable files wait in the directory
// (0 when it does not exist or cannot be read: the importer reports that).
func (l *Log) observeSpool(context.Context) []telemetry.Sample {
	names, err := os.ReadDir(l.spoolDir)
	if err != nil {
		return []telemetry.Sample{{Value: 0}}
	}
	n := 0
	for _, e := range names {
		if !e.IsDir() && strings.HasSuffix(e.Name(), spoolSuffix) {
			n++
		}
	}
	return []telemetry.Sample{{Value: float64(n)}}
}

// Scoped returns a Recorder whose entries must carry actions in area. app calls it once per
// module in provision (area = Manifest.Name) and once per core part (area = the part's
// name, which module.IsCoreArea accepts: "identity", "people", "access", "settings", ...).
// Fails errs.Invalid for an area outside "[a-z][a-z0-9]{1,31}" (F5); errs.Internal after
// Close (F6).
func (l *Log) Scoped(area string) (Recorder, error) {
	const op = "audit.Log.Scoped"
	if !areaRE.MatchString(area) {
		return nil, fieldErr(op, "area", codeMalformed, "must be [a-z][a-z0-9]{1,31}")
	}
	l.mu.Lock()
	closed := l.closed
	l.mu.Unlock()
	if closed {
		return nil, errs.E(op, errs.Internal, errors.New("the audit log is closed"))
	}
	return &scoped{log: l, area: area}, nil
}

// scoped is the Recorder of one area.
type scoped struct {
	log  *Log
	area string
}

// Record implements Recorder.
func (s *scoped) Record(ctx context.Context, entries ...Entry) error {
	return s.log.record(ctx, s.area, entries)
}

// Close stops Run, syncs and closes the active spool file, and refuses further Record
// calls (errs.Unavailable). Idempotent. app calls it after Set.Stop and before db.Close.
// Fails errs.Timeout/Canceled when ctx ends first (the file stays importable, F42).
func (l *Log) Close(ctx context.Context) error {
	const op = "audit.Log.Close"
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	close(l.stop)
	done := l.runDone
	l.mu.Unlock()

	var err error
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
	// Every spooled line was synced when it was written, so a Close that cannot finish its
	// own sync leaves a complete, importable file.
	ctxErr := ctx.Err()
	if cerr := l.spool.shutdown(ctxErr == nil); cerr != nil {
		err = errs.E(op, errs.KindOf(cerr), cerr)
	}
	if ctxErr != nil {
		err = errors.Join(errs.E(op, errs.KindOf(ctxErr), fmt.Errorf("close did not finish: %w", ctxErr)), err)
	}
	return err
}

// isClosed reports whether Close has run.
func (l *Log) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// readOnlyErr reports whether err is the refusal of a write on a read-only copy.
func readOnlyErr(err error) bool {
	return errors.Is(err, db.ErrReadOnly) && errs.Is(err, errs.Unavailable)
}

// missing reports whether err says a file or directory does not exist.
func missing(err error) bool { return errors.Is(err, fs.ErrNotExist) }
