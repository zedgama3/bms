package audit

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// File name suffixes of the spool directory.
const (
	spoolSuffix  = ".jsonl"
	failedSuffix = ".failed"

	spoolDirPerm  = 0o700
	spoolFilePerm = 0o600
	openAttempts  = 4
)

// SpoolCounts counts the spool files under dataDir (SpoolDirName): waiting are complete
// files the importer has not yet imported (".jsonl", including the one this process may
// have open), failed are files it set aside (".failed"). It only reads the directory: it
// never rotates, imports or creates anything, so `bms check` can call it on a read-only
// copy. A missing directory is (0, 0, nil). It fails errs.Invalid when the path cannot be a
// spool directory (a component is a regular file) or is not readable by this user, which are
// the operator's to fix under data.dir; errs.Unavailable for any other read failure. The
// message never carries a file name.
func SpoolCounts(dataDir string) (waiting, failed int, err error) {
	const op = "audit.SpoolCounts"
	dir := filepath.Join(dataDir, filepath.FromSlash(SpoolDirName))
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		return 0, 0, nil
	case errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrPermission):
		return 0, 0, errs.E(op, errs.Invalid, fmt.Errorf("the audit spool directory cannot be read under data.dir: %s", scrubPathError(err)))
	default:
		return 0, 0, errs.E(op, errs.Unavailable, fmt.Errorf("read the audit spool directory: %s", scrubPathError(err)))
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
		case strings.HasSuffix(name, spoolSuffix):
			waiting++
		case strings.HasSuffix(name, failedSuffix):
			failed++
		}
	}
	return waiting, failed, nil
}

// spoolFile is the part of *os.File the spool uses; tests wrap it to count syncs or to fail.
type spoolFile interface {
	Write(p []byte) (int, error)
	Sync() error
	Truncate(size int64) error
	Close() error
}

// spool holds entries durably on local disk while this copy cannot write to the database:
// JSON Lines, append-only, one active file per process, an fsync per Record call.
type spool struct {
	dir    string
	clock  clock.Clock
	logger *slog.Logger
	pid    int
	open   func(path string) (spoolFile, error) // a seam for tests; default opens with O_EXCL

	mu     sync.Mutex
	f      spoolFile
	name   string
	size   int64 // bytes of complete, synced lines in the active file
	closed bool  // set by shutdown: no file is ever opened again
}

func newSpool(dir string, clk clock.Clock, logger *slog.Logger) *spool {
	return &spool{dir: dir, clock: clk, logger: logger, pid: os.Getpid(), open: openExclusive}
}

func openExclusive(path string) (spoolFile, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, spoolFilePerm) //nolint:gosec // the path is built from the configured data dir and a generated name
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller adds the operation
	}
	return f, nil
}

// ensureDir creates the spool directory (0700) when it is missing, and tightens one that
// exists with a wider mode: the spool holds audit data (personal), for its owner only.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, spoolDirPerm); err != nil { //nolint:gosec // the configured data dir plus a constant
		return fmt.Errorf("create spool directory: %w", err)
	}
	st, err := os.Stat(dir) //nolint:gosec // the configured data dir plus a constant
	if err != nil {
		return fmt.Errorf("inspect spool directory: %w", err)
	}
	if st.Mode().Perm()&^spoolDirPerm != 0 {
		if err := os.Chmod(dir, spoolDirPerm); err != nil { //nolint:gosec // 0700 is the narrowest the importer can use
			return fmt.Errorf("restrict spool directory to its owner: %w", err)
		}
	}
	return nil
}

// append writes lines (each already newline-terminated) to the active file in one Write
// call and syncs it. A short write or a sync error closes the file, so the next call opens
// a new one (the partial tail of the old file is the importer's truncated-line case), and
// fails errs.Unavailable (F18). Safe for concurrent use.
func (s *spool) append(lines []byte) error {
	const op = "audit.spool.append"
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errs.E(op, errs.Unavailable, errors.New("the audit log is closed"))
	}
	if s.f == nil {
		if err := s.openLocked(); err != nil {
			s.logger.Error("audit spool cannot be opened", slog.Any("error", err))
			return errs.E(op, errs.Unavailable, err)
		}
	}
	n, err := s.f.Write(lines)
	if err == nil && n < len(lines) {
		err = fmt.Errorf("short write: %d of %d bytes", n, len(lines))
	}
	if err == nil {
		err = s.f.Sync()
	}
	if err != nil {
		// The caller is told "failed", so none of this batch may import later: cut the file
		// back to the last complete batch (best effort), then start a new file next time.
		if terr := s.f.Truncate(s.size); terr != nil {
			s.logger.Warn("audit spool file could not be cut back after a failed write", slog.String("file", s.name), slog.Any("error", terr))
		}
		s.abandonLocked()
		err = fmt.Errorf("write spool file: %w", err)
		s.logger.Error("audit spool write failed", slog.Any("error", err))
		return errs.E(op, errs.Unavailable, err)
	}
	s.size += int64(len(lines))
	return nil
}

// abandonLocked drops the active file after a failure, closing it on a best-effort basis:
// the error that brought us here is the one that matters.
func (s *spool) abandonLocked() {
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			s.logger.Warn("audit spool file close failed after an earlier error", slog.String("file", s.name), slog.Any("error", err))
		}
	}
	s.f, s.name, s.size = nil, "", 0
}

// openLocked creates the directory and a new active file with a unique name.
func (s *spool) openLocked() error {
	if err := ensureDir(s.dir); err != nil {
		return err
	}
	var lastErr error
	for range openAttempts {
		var suffix [4]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return fmt.Errorf("random file name: %w", err)
		}
		name := fmt.Sprintf("%d-%d-%08x%s", s.clock.Now().UnixMilli(), s.pid, binary.BigEndian.Uint32(suffix[:]), spoolSuffix)
		f, err := s.open(filepath.Join(s.dir, name))
		if errors.Is(err, fs.ErrExist) {
			lastErr = err
			continue
		}
		if err != nil {
			return fmt.Errorf("open spool file: %w", err)
		}
		s.f, s.name, s.size = f, name, 0
		if err := syncDir(s.dir); err != nil {
			s.abandonLocked()
			return err
		}
		s.logger.Warn("audit spooling started", slog.String("file", name))
		return nil
	}
	return fmt.Errorf("open spool file: %w", lastErr)
}

// syncDir makes a newly created file's name durable.
func syncDir(dir string) (err error) {
	d, err := os.Open(dir) //nolint:gosec // the configured spool directory
	if err != nil {
		return fmt.Errorf("open spool directory: %w", err)
	}
	defer func() {
		if cerr := d.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close spool directory: %w", cerr))
		}
	}()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync spool directory: %w", err)
	}
	return nil
}

// rotate syncs and closes the active file, so the next write opens a new one and the old
// one can be imported. A no-op when nothing is open.
func (s *spool) rotate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked(true)
}

// shutdown closes the active file, syncing it first when doSync, and refuses every later
// append (it is how Close wins a race with a Record). Idempotent.
func (s *spool) shutdown(doSync bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeLocked(doSync)
}

func (s *spool) closeLocked(doSync bool) error {
	if s.f == nil {
		return nil
	}
	var err error
	if doSync {
		if serr := s.f.Sync(); serr != nil {
			err = fmt.Errorf("sync spool file: %w", serr)
		}
	}
	if cerr := s.f.Close(); cerr != nil {
		err = errors.Join(err, fmt.Errorf("close spool file: %w", cerr))
	}
	s.f, s.name, s.size = nil, "", 0
	if err != nil {
		return errs.E("audit.spool.close", errs.Unavailable, err)
	}
	return nil
}

// active returns the name of the file this process is writing, or "".
func (s *spool) active() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}
