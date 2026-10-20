package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/settings/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// loadTimeout bounds one load of the snapshot.
const loadTimeout = 10 * time.Second

// snapshot is everything the reads need, as stored. It is immutable once published: a write
// builds the next one and installs it.
type snapshot struct {
	revision int64
	values   map[string]string                       // full key -> text form (plain settings)
	sealed   map[string][]byte                       // full key -> nonce || ciphertext (secrets)
	terms    map[string]map[string]map[string]string // lang -> term -> form -> override
}

// snapshotOf builds a snapshot from stored rows.
func snapshotOf(revision int64, ss []rows.Setting, ts []rows.Term) *snapshot {
	sn := &snapshot{
		revision: revision,
		values:   make(map[string]string, len(ss)),
		sealed:   map[string][]byte{},
		terms:    map[string]map[string]map[string]string{},
	}
	for _, r := range ss {
		switch {
		case r.Value != nil:
			sn.values[r.Key] = *r.Value
		case r.Sealed != nil:
			sn.sealed[r.Key] = r.Sealed
		}
	}
	for _, r := range ts {
		sn.setTerm(r.Lang, r.Term, r.Form, r.Value)
	}
	return sn
}

func (sn *snapshot) setTerm(lang, term, form, value string) {
	byTerm := sn.terms[lang]
	if byTerm == nil {
		byTerm = map[string]map[string]string{}
		sn.terms[lang] = byTerm
	}
	byForm := byTerm[term]
	if byForm == nil {
		byForm = map[string]string{}
		byTerm[term] = byForm
	}
	byForm[form] = value
}

// current returns the snapshot, loading it on the first call (single-flight). Until one
// exists every call fails with the load's error: errs.Unavailable, Timeout or Canceled.
func (s *Service) current(ctx context.Context) (*snapshot, error) {
	if sn := s.snap.Load(); sn != nil {
		return sn, nil
	}
	select {
	case s.loading <- struct{}{}:
	case <-ctx.Done():
		return nil, errs.E("settings.Service.current", errs.KindOf(ctx.Err()), ctx.Err())
	}
	defer func() { <-s.loading }()
	if sn := s.snap.Load(); sn != nil {
		return sn, nil
	}
	sn, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	s.install(sn)
	return s.snap.Load(), nil
}

// load reads the revision, then the rows, in one read transaction under its own deadline
// (span settings.load). A missing revision row reads as revision 0: nothing has been
// written, and a write reports the missing row (F26). The revision is read first, so the
// rows are at least as new as it and the worst case is one extra reload.
func (s *Service) load(ctx context.Context) (_ *snapshot, err error) {
	const op = "settings.Service.load"
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()
	ctx, span := s.start(ctx, "load")
	defer endSpan(span, &err)
	var sn *snapshot
	err = s.inTx(ctx, db.Read, func(ctx context.Context) error {
		rev, err := s.store.Revision(ctx)
		if err != nil && !errs.Is(err, errs.NotFound) {
			return wrap(op, err)
		}
		ss, err := s.store.LoadSettings(ctx)
		if err != nil {
			return wrap(op, err)
		}
		ts, err := s.store.LoadTerminology(ctx)
		if err != nil {
			return wrap(op, err)
		}
		sn = snapshotOf(rev, ss, ts)
		return nil
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	return sn, nil
}

// install makes next the current snapshot unless the current one is as new or newer, so
// two writers in one process cannot install out of order. It reports whether it did.
func (s *Service) install(next *snapshot) bool {
	for {
		cur := s.snap.Load()
		if cur != nil && cur.revision >= next.revision {
			return false
		}
		if s.snap.CompareAndSwap(cur, next) {
			return true
		}
	}
}

// replace makes next the current snapshot although it is older: the caller has seen the
// database itself at a lower revision than the snapshot holds, which only happens after
// a restore to an earlier backup. Without it install would refuse every snapshot until the
// revision caught up. It warns once per process.
func (s *Service) replace(ctx context.Context, next *snapshot) {
	cur := s.snap.Swap(next)
	if cur == nil {
		return
	}
	if _, seen := s.warned.LoadOrStore(regressionKey, struct{}{}); !seen {
		s.logger.WarnContext(ctx, "the settings revision went backwards (a restore?); replacing the snapshot",
			slog.Int64("from_revision", cur.revision), slog.Int64("to_revision", next.revision))
	}
}

// regressionKey is the warned-once key of a revision regression; a full setting key always
// has a dot, so it cannot clash with one.
const regressionKey = "revision"

// errNoRevision is the cause of the Internal a write answers when the revision row is gone.
var errNoRevision = errors.New("core.setting_revision has no row; run bms migrate")

// revisionErr re-kinds a missing revision row as errs.Internal (F26).
func revisionErr(op string, err error) error {
	if errs.Is(err, errs.NotFound) {
		return errs.E(op, errs.Internal, fmt.Errorf("%w", errNoRevision))
	}
	return wrap(op, err)
}
