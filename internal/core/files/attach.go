package files

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files/postgres"
	"github.com/zedgama3/bms/internal/core/files/rows"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// checkRecord validates a record reference and that its area is the owner.
func (sc *scoped) checkRecord(op string, rec audit.Ref) error {
	switch {
	case rec.IsZero():
		return fieldErr(op, errs.Invalid, "record", codeRequired, nil, "must be set")
	case rec.Validate() != nil:
		return fieldErr(op, errs.Invalid, "record", codeMalformed, nil, "must be <area>.<ident> with an id")
	case areaOf(rec.Kind) != sc.owner:
		return fieldErr(op, errs.Invalid, "record", codeScope, nil, "must belong to the owner's area")
	}
	return nil
}

// Attach binds a Staged object to a record inside the caller's Write transaction. It fails
// errs.Invalid (fields record: required, malformed, scope; level: one_of; subject: unknown;
// staged: malformed, foreign, expired), errs.Internal outside a Write transaction or for a
// Staged attached twice, errs.Conflict (codes too_many and quota, with the limit as max),
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and the Recorder's kind
// when the audit entry cannot be written (the transaction then rolls back).
func (sc *scoped) Attach(ctx context.Context, st Staged, a Attachment) (f File, err error) {
	const op = "files.Store.Attach"
	s := sc.s
	ctx, span := s.tracer.Start(ctx, "files.attach", telemetry.WithAttrs(telemetry.Token("owner", sc.owner)))
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	if err := a.Validate(); err != nil {
		return File{}, errs.E(op, errs.Invalid, err)
	}
	if err := sc.checkRecord(op, a.Record); err != nil {
		return File{}, err
	}
	if err := st.Validate(); err != nil {
		return File{}, errs.E(op, errs.Invalid, err)
	}
	if st.Bucket != sc.bucket {
		return File{}, fieldErr(op, errs.Invalid, "staged", codeForeign, nil, "was staged for another owner")
	}
	now := s.now()
	if now.Sub(st.StagedAt) > OrphanAge {
		return File{}, fieldErr(op, errs.Invalid, "staged", codeExpired, nil, "was staged too long ago: upload it again")
	}
	if mode, inTx := db.TxMode(ctx); !inTx || mode != db.Write {
		return File{}, errs.E(op, errs.Internal, errors.New("Attach called outside a Write transaction: a file must commit with its record"))
	}
	pol, err := s.readPolicy(ctx, op)
	if err != nil {
		return File{}, err
	}
	id, perr := model.ParseID[FileID](string(st.Key))
	if perr != nil {
		return File{}, fieldErr(op, errs.Invalid, "staged", codeMalformed, nil, "is not an object Stage produced")
	}
	if a.Replace {
		// Replacing deletes the record's rows and, after commit, their objects. If the Staged is
		// one of those rows' object, the removal would take the bytes the new row points at.
		attached, aerr := s.store.Attached(ctx, s.objects.Kind().String(), sc.owner, string(st.Key))
		switch {
		case aerr != nil && !errs.Is(aerr, errs.NotFound):
			return File{}, fmt.Errorf("%s: check the staged object: %w", op, aerr)
		case aerr == nil && !attached:
		default:
			return File{}, errs.E(op, errs.Internal, errors.New("the Staged object is already attached and cannot replace itself"))
		}
		if _, derr := sc.deleteByRecord(ctx, op, a.Record); derr != nil {
			return File{}, derr
		}
	}
	row := rows.File{
		ID: id, Owner: sc.owner, RecordKind: a.Record.Kind, RecordID: a.Record.ID,
		Subject: a.Subject, Level: int16(a.Level), Name: st.Name, ContentType: st.ContentType,
		Size: st.Size, SHA256: st.SHA256, Store: s.objects.Kind().String(), Bucket: sc.owner,
		ObjectKey: string(st.Key), Stripped: st.Stripped, CreatedAt: now,
	}
	if actor, ok := s.actorPerson(ctx); ok {
		row.UploadedBy = actor.Person
	}
	res, ierr := s.store.Insert(ctx, row, pol.MaxPerRecord, pol.QuotaBytes)
	switch {
	case errors.Is(ierr, postgres.ErrUnknownSubject):
		return File{}, fieldErr(op, errs.Invalid, "subject", codeUnknown, nil, "no such person")
	case errors.Is(ierr, postgres.ErrDuplicate):
		return File{}, errs.E(op, errs.Internal, errors.New("the Staged object is already attached"))
	case ierr != nil:
		return File{}, fmt.Errorf("%s: insert: %w", op, ierr)
	case res.TooMany:
		return File{}, fieldErr(op, errs.Conflict, "file", codeTooMany, maxParam(int64(pol.MaxPerRecord)), "the record holds as many files as it may")
	case res.OverQuota:
		return File{}, fieldErr(op, errs.Conflict, "file", codeQuota, maxParam(pol.QuotaBytes), "the storage quota would be exceeded")
	case !res.Inserted:
		return File{}, errs.E(op, errs.Internal, errors.New("the file was not inserted and no limit said why"))
	}
	f = fileOf(row)
	if err := s.recordAudit(ctx, actionCreated, f); err != nil {
		return File{}, err
	}
	return f, nil
}

// Discard removes a Staged object that will not be attached, unless a row refers to it. It
// is idempotent. It fails errs.Invalid (field staged) for a Staged that is not this owner's
// and errs.Unavailable when the store cannot delete (the sweep then takes the object).
func (sc *scoped) Discard(ctx context.Context, st Staged) error {
	const op = "files.Store.Discard"
	s := sc.s
	if err := st.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if st.Bucket != sc.bucket {
		return fieldErr(op, errs.Invalid, "staged", codeForeign, nil, "was staged for another owner")
	}
	attached, err := s.store.Attached(ctx, s.objects.Kind().String(), sc.owner, string(st.Key))
	switch {
	case err == nil:
	case errs.Is(err, errs.NotFound):
		// A lookup that cannot answer is read as "attached": the unsafe direction is deleting
		// bytes a row needs. A real orphan is the sweep's.
		return nil
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
	if attached {
		return nil
	}
	if derr := s.objects.Delete(ctx, st.Bucket, st.Key); derr != nil {
		s.logger.WarnContext(ctx, "a staged object could not be removed; the sweep will take it",
			slog.String("error.kind", errs.KindOf(derr).String()))
		if k := errs.KindOf(derr); k == errs.Timeout || k == errs.Canceled {
			return errs.E(op, k, derr)
		}
		return errs.E(op, errs.Unavailable, derr)
	}
	return nil
}

// Get answers one file's metadata. It fails errs.NotFound for an unknown ID and for another
// owner's file alike.
func (sc *scoped) Get(ctx context.Context, id FileID) (File, error) {
	const op = "files.Store.Get"
	if id.IsZero() {
		return File{}, errs.E(op, errs.NotFound, errors.New("no such file"))
	}
	r, err := sc.s.store.Get(ctx, sc.owner, id)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", op, err)
	}
	return fileOf(r), nil
}

// ByRecord lists a record's files, oldest first. It fails errs.Invalid (field record) for a
// reference that is malformed or outside the owner's area.
func (sc *scoped) ByRecord(ctx context.Context, rec audit.Ref) ([]File, error) {
	const op = "files.Store.ByRecord"
	if err := sc.checkRecord(op, rec); err != nil {
		return nil, err
	}
	rs, err := sc.s.store.ByRecord(ctx, sc.owner, rec.Kind, rec.ID, MaxFilesPerRecordCeiling)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	out := make([]File, len(rs))
	for i, r := range rs {
		out[i] = fileOf(r)
	}
	return out, nil
}

// Open answers the metadata and the bytes. For a personal or sensitive file it needs an
// actor (errs.Unauthenticated without one) and records files.file.read before the bytes are
// opened: always for sensitive, and for personal when the actor is not the subject. It fails
// errs.NotFound, the Recorder's kind, errs.Unavailable wrapping objects.ErrMissing when the
// object is absent, errs.Unavailable for an object held by another store or an unreachable
// one. The caller closes the Content before ctx ends.
func (sc *scoped) Open(ctx context.Context, id FileID) (f File, c Content, err error) {
	const op = "files.Store.Open"
	s := sc.s
	ctx, span := s.tracer.Start(ctx, "files.open", telemetry.WithAttrs(telemetry.Token("owner", sc.owner), telemetry.ID("file_id", id)))
	defer span.End()
	outcome := "ok"
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
		add(s.metrics.reads, ctx, 1, telemetry.Token("outcome", outcome))
	}()

	if id.IsZero() {
		outcome = "denied"
		return File{}, nil, errs.E(op, errs.NotFound, errors.New("no such file"))
	}
	row, gerr := s.store.Get(ctx, sc.owner, id)
	if gerr != nil {
		outcome = "unavailable"
		if errs.Is(gerr, errs.NotFound) {
			outcome = "denied"
		}
		return File{}, nil, fmt.Errorf("%s: %w", op, gerr)
	}
	f = fileOf(row)
	if f.Level >= model.SensitivityPersonal {
		actor, ok := s.actorPerson(ctx)
		if !ok {
			// The superuser has a person too; an actor-less caller fails closed (decision 11).
			outcome = "denied"
			return File{}, nil, errs.E(op, errs.Unauthenticated, errors.New("a signed-in person is required to open this file"))
		}
		if f.Level == model.SensitivitySensitive || actor.Person != f.Subject {
			if rerr := s.recordAudit(ctx, actionRead, f); rerr != nil {
				outcome = "unavailable"
				return File{}, nil, fmt.Errorf("%s: %w", op, rerr)
			}
		}
	}
	if want := s.objects.Kind().String(); row.Store != want {
		outcome = "unavailable"
		return File{}, nil, errs.E(op, errs.Unavailable, fmt.Errorf("the file is stored in %s and this process is configured for %s", row.Store, want))
	}
	obj, oerr := s.objects.Get(ctx, objects.Bucket(row.Bucket), objects.Key(row.ObjectKey))
	if oerr != nil {
		outcome = "unavailable"
		if errors.Is(oerr, objects.ErrMissing) {
			outcome = "missing"
			s.logger.WarnContext(ctx, "a file's object is missing from the store", slog.String("file_id", f.ID.String()))
		}
		return File{}, nil, fmt.Errorf("%s: %w", op, oerr)
	}
	return f, obj.Body, nil
}

// Delete removes one file inside the caller's Write transaction: the row goes, a tombstone
// is written and files.file.deleted is recorded, all in the transaction; the object and then
// the tombstone are deleted after commit (a failure is logged and left to Reap). It fails
// errs.NotFound for an unknown ID or another owner's file, errs.Internal outside a Write
// transaction and errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (sc *scoped) Delete(ctx context.Context, id FileID) (err error) {
	const op = "files.Store.Delete"
	s := sc.s
	ctx, span := s.tracer.Start(ctx, "files.delete", telemetry.WithAttrs(telemetry.Token("owner", sc.owner)))
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	if err := needWriteTx(ctx, op); err != nil {
		return err
	}
	if id.IsZero() {
		return errs.E(op, errs.NotFound, errors.New("no such file"))
	}
	d, derr := s.store.Delete(ctx, sc.owner, id, s.now())
	if derr != nil {
		return fmt.Errorf("%s: %w", op, derr)
	}
	return sc.afterDelete(ctx, op, []rows.Deleted{d})
}

// DeleteByRecord removes every file of a record, as Delete does each, and returns how many.
func (sc *scoped) DeleteByRecord(ctx context.Context, rec audit.Ref) (n int, err error) {
	const op = "files.Store.DeleteByRecord"
	s := sc.s
	ctx, span := s.tracer.Start(ctx, "files.delete", telemetry.WithAttrs(telemetry.Token("owner", sc.owner)))
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	if err := needWriteTx(ctx, op); err != nil {
		return 0, err
	}
	if err := sc.checkRecord(op, rec); err != nil {
		return 0, err
	}
	return sc.deleteByRecord(ctx, op, rec)
}

func needWriteTx(ctx context.Context, op string) error {
	if mode, inTx := db.TxMode(ctx); !inTx || mode != db.Write {
		return errs.E(op, errs.Internal, errors.New("called outside a Write transaction: a file is deleted with its record"))
	}
	return nil
}

// deleteByRecord deletes the record's rows and schedules the objects' removal.
func (sc *scoped) deleteByRecord(ctx context.Context, op string, rec audit.Ref) (int, error) {
	ds, err := sc.s.store.DeleteByRecord(ctx, sc.owner, rec.Kind, rec.ID, sc.s.now())
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	if len(ds) == 0 {
		return 0, nil
	}
	if err := sc.afterDelete(ctx, op, ds); err != nil {
		return 0, err
	}
	return len(ds), nil
}

// afterDelete records files.file.deleted for each row in the transaction and registers the
// removal of the objects for after the commit.
func (sc *scoped) afterDelete(ctx context.Context, op string, ds []rows.Deleted) error {
	s := sc.s
	for _, d := range ds {
		if err := s.recordAudit(ctx, actionDeleted, fileOf(d.File)); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
	}
	err := db.AfterCommit(ctx, func(ctx context.Context) {
		s.removeObjects(ctx, ds)
	})
	if err != nil {
		return errs.E(op, errs.Internal, fmt.Errorf("register the object removal: %w", err))
	}
	return nil
}

// removeObjects deletes each object and then its tombstone, after the transaction that
// deleted the rows committed. The whole removal shares one short budget (removalBudget): the
// caller waits for it, so a dead store must not hold a committed Delete for long. The first
// failure is logged once with the file ID and ends the removal; the tombstones that are left
// guarantee Reap finishes it.
func (s *Service) removeObjects(ctx context.Context, ds []rows.Deleted) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removalBudget)
	defer cancel()
	for _, d := range ds {
		if err := s.objects.Delete(ctx, objects.Bucket(d.Bucket), objects.Key(d.ObjectKey)); err != nil {
			s.logger.WarnContext(ctx, "an object could not be deleted; the reap will retry the rest",
				slog.String("file_id", FileID(d.ID).String()), slog.String("error.kind", errs.KindOf(err).String()))
			return
		}
		if err := s.store.DeleteTombstone(ctx, d.TombstoneID); err != nil {
			s.logger.WarnContext(ctx, "a tombstone could not be removed; the reap will retry the rest",
				slog.String("file_id", FileID(d.ID).String()), slog.String("error.kind", errs.KindOf(err).String()))
			return
		}
	}
}
