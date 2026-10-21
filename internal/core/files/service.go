package files

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files/postgres"
	"github.com/zedgama3/bms/internal/core/files/rows"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// store is the storage the service declares (D8); postgres.Store implements it. Every error
// is already classified by db.Wrap.
type store interface {
	Insert(ctx context.Context, f rows.File, maxPerRecord int, quotaBytes int64) (rows.InsertResult, error)
	Get(ctx context.Context, owner string, id [16]byte) (rows.File, error)
	ByRecord(ctx context.Context, owner, kind string, record [16]byte, limit int) ([]rows.File, error)
	Delete(ctx context.Context, owner string, id [16]byte, now time.Time) (rows.Deleted, error)
	DeleteByRecord(ctx context.Context, owner, kind string, record [16]byte, now time.Time) ([]rows.Deleted, error)
	DeleteTombstone(ctx context.Context, id [16]byte) error
	BumpTombstone(ctx context.Context, id [16]byte) error
	Tombstones(ctx context.Context, store string, afterCreated time.Time, afterID [16]byte, limit int) ([]rows.Tombstone, error)
	CountTombstones(ctx context.Context, stuckAttempts int) (outstanding, stuck int64, err error)
	Attached(ctx context.Context, store, bucket, key string) (bool, error)
	Usage(ctx context.Context) (files, bytes int64, err error)
	Buckets(ctx context.Context, store string) ([]string, error)
	UnknownKeys(ctx context.Context, store, bucket string, keys []string) ([]string, error)
	VerifyPage(ctx context.Context, afterID [16]byte, limit int) ([]rows.VerifyRow, error)
	ExportAbout(ctx context.Context, person [16]byte, afterCreated time.Time, afterID [16]byte, limit int) ([]rows.File, error)
	ExportUploaded(ctx context.Context, person [16]byte, afterCreated time.Time, afterID [16]byte, limit int) ([]rows.File, error)
	AnonymiseUploads(ctx context.Context, person [16]byte) (int, error)
	FilesAboutPerson(ctx context.Context, person [16]byte) ([]rows.OwnerCount, error)
}

var _ store = (*postgres.Store)(nil)

// Tuning that is a constant, not a setting.
const (
	removalBudget   = 5 * time.Second  // the whole after-commit object removal of one Delete
	pingTimeout     = 10 * time.Second // Start's probe
	batchSize       = 1000             // tombstones, sweep keys and verify rows per page
	exportBatch     = 100              // export rows per page
	stuckAttempts   = 24               // a tombstone tried this often is stuck
	softDeadline    = 2 * time.Second  // Reap and the sweep stop this long before their deadline
	tempFilePattern = ".ping-"
)

// Service is the attachment service. Safe for concurrent use; there is nothing to close.
type Service struct {
	store   store
	db      db.DB
	clock   clock.Clock
	logger  *slog.Logger
	tracer  telemetry.Tracer
	audit   audit.Recorder
	actor   authz.ActorFunc
	objects objects.Store
	tempDir string
	policy  PolicySource

	metrics metrics

	mu     sync.Mutex
	owners map[string]struct{} // every owner Scoped was asked for: buckets the sweep walks

	tombstones atomic.Int64 // the last known outstanding count; -1 until one is known
}

// New validates o and builds the service over postgres.Store. It does no I/O: it works
// against db.None() and on a read-only copy. Fails errs.Invalid from Options.Validate.
func New(o Options) (*Service, error) {
	const op = "files.New"
	if err := o.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	st, err := postgres.NewStore(o.DB)
	if err != nil {
		return nil, err //nolint:wrapcheck // NewStore names itself
	}
	s := &Service{
		store:   st,
		db:      o.DB,
		clock:   o.Clock,
		logger:  o.Logger,
		tracer:  o.Tracer,
		audit:   o.Audit,
		actor:   o.Actor,
		tempDir: o.TempDir,
		policy:  o.Policy,
		owners:  map[string]struct{}{},
	}
	s.tombstones.Store(-1)
	if err := s.metrics.init(o.Meter, s); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	s.objects = instrumented{Store: o.Objects, tracer: o.Tracer, failures: s.metrics.objectFailures}
	return s, nil
}

// Scoped answers a Store that sees only owner's files and writes to bucket owner. Fails
// errs.Invalid (field "owner") when owner is not a valid objects.Bucket (a module name or
// core area).
func (s *Service) Scoped(owner string) (Store, error) {
	const op = "files.Service.Scoped"
	if err := objects.Bucket(owner).Validate(); err != nil {
		return nil, fieldErr(op, errs.Invalid, "owner", codeMalformed, nil, "must be a module name or core area: lower-case letters and digits")
	}
	s.mu.Lock()
	s.owners[owner] = struct{}{}
	s.mu.Unlock()
	return &scoped{s: s, owner: owner, bucket: objects.Bucket(owner)}, nil
}

// knownOwners lists every owner Scoped was asked for, sorted.
func (s *Service) knownOwners() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.owners))
	for o := range s.owners {
		out = append(out, o)
	}
	sort.Strings(out)
	return out
}

// readPolicy asks the PolicySource (DefaultPolicy when none) and validates the answer.
func (s *Service) readPolicy(ctx context.Context, op string) (Policy, error) {
	if s.policy == nil {
		return DefaultPolicy(), nil
	}
	p, err := s.policy.Policy(ctx)
	if err != nil {
		return Policy{}, fmt.Errorf("%s: read the policy: %w", op, err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, errs.E(op, errs.Internal, fmt.Errorf("the policy source answered an invalid policy: %w", err))
	}
	return p, nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

// actorPerson returns the signed-in person, if any.
func (s *Service) actorPerson(ctx context.Context) (authz.Actor, bool) {
	a, ok := s.actor(ctx)
	if !ok || a.Person.IsZero() {
		return authz.Actor{}, false
	}
	return a, true
}

// fileOf converts a row to a File.
func fileOf(r rows.File) File {
	return File{
		ID:          FileID(r.ID),
		Record:      audit.Ref{Kind: r.RecordKind, ID: r.RecordID},
		Subject:     model.PersonID(r.Subject),
		UploadedBy:  model.PersonID(r.UploadedBy),
		Level:       model.Sensitivity(r.Level), //nolint:gosec // the column is checked 1..4
		Name:        r.Name,
		ContentType: r.ContentType,
		Size:        r.Size,
		SHA256:      r.SHA256,
		Stripped:    r.Stripped,
		CreatedAt:   r.CreatedAt,
	}
}

// areaOf is the part of a record kind before its first dot.
func areaOf(kind string) string {
	for i := 0; i < len(kind); i++ {
		if kind[i] == '.' {
			return kind[:i]
		}
	}
	return ""
}

// reasonOf is the audit Reason that names the owning record: kind and ID, never a name.
func reasonOf(r audit.Ref) string {
	return "record:" + r.Kind + ":" + model.ID[fileTag](r.ID).String()
}

// recordAudit records one entry with the file's level and subject (fields: content).
func (s *Service) recordAudit(ctx context.Context, action audit.Action, f File) error {
	err := s.audit.Record(ctx, audit.Entry{
		Action:  action,
		Outcome: audit.Succeeded,
		Subject: f.Subject,
		Record:  audit.RefOf(kindFile, f.ID),
		Fields:  []string{"content"},
		Level:   f.Level,
		Reason:  reasonOf(f.Record),
	})
	if err != nil {
		return fmt.Errorf("record %s: %w", action, err)
	}
	return nil
}

// ctxKind returns the errs kind of a context error and whether err is one.
func ctxKind(err error) (errs.Kind, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return errs.Canceled, true
	case errors.Is(err, context.DeadlineExceeded):
		return errs.Timeout, true
	}
	return 0, false
}

// outcomeOf is the metric token for an error: "ok" for nil, else the kind's name.
func outcomeOf(err error) string {
	if err == nil {
		return "ok"
	}
	return errs.KindOf(err).String()
}

// readOnlyErr is the error of a job run on a read-only copy.
func readOnlyErr(op string) error {
	return errs.E(op, errs.Unavailable, db.ErrReadOnly)
}
