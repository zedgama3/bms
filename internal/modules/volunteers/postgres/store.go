// Package postgres is the volunteers module's storage: the only package with its SQL and the
// driver (D8). The statements are the files of sql/, one per query, each documenting its
// arguments, columns and the constraints it can raise.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the module's migration set, rooted at the files (db.md: NNNN_name.sql at the
// set root). Handed to the Manifest; applied by `bms migrate` (D17).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("volunteers/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("volunteers/postgres: embedded SQL missing: " + name)
	}
	return string(b)
}

// Store keeps the volunteers schema in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use. Every method runs on the transaction in ctx when there is one.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil. It reads nothing.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("volunteers.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// ---- argument and scan helpers ----

type rowScanner interface{ Scan(dest ...any) error }

// uuidArg is an optional ID argument: NULL for the zero ID.
func uuidArg[T ~[16]byte](id T) any {
	if id == (T{}) {
		return nil
	}
	return [16]byte(id)
}

// tsArg is an optional timestamp argument: NULL for the zero time.
func tsArg(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// dateArg is an optional date argument: NULL for the zero Date.
func dateArg(d vol.Date) any {
	if d.IsZero() {
		return nil
	}
	return d.Time(time.UTC)
}

// uuids converts IDs to the array argument (never nil, so ANY() is not NULL).
func uuids[T ~[16]byte](in []T) [][16]byte {
	out := make([][16]byte, len(in))
	for i, id := range in {
		out[i] = [16]byte(id)
	}
	return out
}

// idOf converts an optional scanned UUID.
func idOf[T ~[16]byte](p *[16]byte) T {
	if p == nil {
		return T{}
	}
	return T(*p)
}

func timeOf(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.UTC()
}

func dateOf(p *time.Time) vol.Date {
	if p == nil {
		return vol.Date{}
	}
	return vol.DateOf(*p, time.UTC)
}

// translate turns a violated constraint the spec names into a coded error and leaves every
// other error as db.Wrap made it.
func translate(op string, err error) error {
	if err == nil {
		return nil
	}
	err = db.Wrap(op, err)
	var pe *db.Error
	if !errors.As(err, &pe) {
		return err
	}
	switch c := pe.Constraint; {
	case c == "qualification_name_key", c == "position_name_key":
		return conflictErr(op, "name", vol.ConflictTaken, "the name is already used", nil)
	case c == "record_pending_key":
		return conflictErr(op, "record", vol.ConflictPendingExists, "a pending record for this qualification exists", nil)
	case c == "signup_booked_key", c == "member_department_person_key", c == "shift_copied_key":
		return conflictErr(op, "person", vol.ConflictDuplicate, "it already exists", nil)
	case c == "shift_position_department_fkey":
		return fieldErr(op, "position", vol.CodeScope, "the position is unknown or belongs to another department", nil)
	case c == "shift_event_id_fkey":
		return fieldErr(op, "event", vol.CodeUnknown, "no such event", nil)
	case c == "shift_times_chk":
		return fieldErr(op, "ends", "range", "a shift ends after it starts and lasts at most 48 hours", nil)
	case c == "shift_slots_chk":
		return fieldErr(op, "slots", "range", "slots must be between 1 and 200", nil)
	case c == "qualification_record_dates_chk":
		return fieldErr(op, "expires_on", "range", "must not be before acquired_on", nil)
	case c == "qualification_record_not_self_verified_chk":
		return fieldErr(op, "person", vol.CodeSelf, "you cannot verify your own record", nil)
	case strings.HasSuffix(c, "_person_id_fkey"):
		return fieldErr(op, "person", vol.CodeUnknown, "no such person", nil)
	case strings.HasSuffix(c, "_qualification_id_fkey"):
		return fieldErr(op, "qualification", vol.CodeUnknown, "no such qualification", nil)
	case strings.HasSuffix(c, "_department_id_fkey"):
		return fieldErr(op, "department", vol.CodeUnknown, "no such department", nil)
	}
	return err
}

// queryAll runs a query and collects its rows with scan.
func queryAll[T any](ctx context.Context, s *Store, op, sql string, scan func(rowScanner) (T, error), args ...any) ([]T, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, translate(op, err)
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, translate(op, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, translate(op, err)
	}
	return out, nil
}

// queryOne runs a query expected to return one row.
func queryOne[T any](ctx context.Context, s *Store, op, sql string, scan func(rowScanner) (T, error), args ...any) (T, error) {
	v, err := scan(s.db.QueryRow(ctx, sql, args...))
	if err != nil {
		var zero T
		return zero, translate(op, err)
	}
	return v, nil
}

// exec runs a statement and returns the rows it affected.
func (s *Store) exec(ctx context.Context, op, sql string, args ...any) (int, error) {
	tag, err := s.db.Exec(ctx, sql, args...)
	if err != nil {
		return 0, translate(op, err)
	}
	return int(tag.RowsAffected()), nil
}

// count runs a count query.
func (s *Store) count(ctx context.Context, op, sql string, args ...any) (int, error) {
	var n int64
	if err := s.db.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, translate(op, err)
	}
	return int(n), nil
}

// ---- qualifications ----

var (
	sqlInsertQualification       = mustSQL("insert_qualification.sql")
	sqlGetQualification          = mustSQL("get_qualification.sql")
	sqlGetQualificationForUpdate = mustSQL("get_qualification_for_update.sql")
	sqlListQualifications        = mustSQL("list_qualifications.sql")
	sqlQualificationsByIDs       = mustSQL("list_qualifications_by_ids.sql")
	sqlUpdateQualification       = mustSQL("update_qualification.sql")
	sqlArchiveQualification      = mustSQL("archive_qualification.sql")
)

func scanQualification(r rowScanner) (vol.Qualification, error) {
	var (
		id               [16]byte
		deptP            *[16]byte
		name, desc       string
		validity         *int32
		archived         *time.Time
		created, updated time.Time
	)
	if err := r.Scan(&id, &deptP, &name, &desc, &validity, &archived, &created, &updated); err != nil {
		return vol.Qualification{}, err //nolint:wrapcheck // translate classifies it
	}
	q := vol.Qualification{
		ID: vol.QualificationID(id), Department: idOf[model.DepartmentID](deptP), Name: name, Description: desc,
		ArchivedAt: timeOf(archived), CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
	}
	if validity != nil {
		q.ValidityDays = int(*validity)
	}
	return q, nil
}

func validityArg(days int) any {
	if days <= 0 {
		return nil
	}
	return int32(days) //nolint:gosec // validated to 1..3650
}

// InsertQualification adds an entry. Fails errs.Conflict (name, taken), errs.Invalid
// (unknown department; a check) and db's kinds.
func (s *Store) InsertQualification(ctx context.Context, q vol.Qualification) (vol.Qualification, error) {
	return queryOne(ctx, s, "volunteers.postgres.InsertQualification", sqlInsertQualification, scanQualification,
		[16]byte(q.ID), uuidArg(q.Department), q.Name, q.Description, validityArg(q.ValidityDays), q.CreatedAt)
}

// GetQualification returns one entry. Fails errs.NotFound.
func (s *Store) GetQualification(ctx context.Context, id vol.QualificationID) (vol.Qualification, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetQualification", sqlGetQualification, scanQualification, [16]byte(id))
}

// GetQualificationForUpdate is GetQualification with a row lock. Fails errs.NotFound.
func (s *Store) GetQualificationForUpdate(ctx context.Context, id vol.QualificationID) (vol.Qualification, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetQualificationForUpdate", sqlGetQualificationForUpdate, scanQualification, [16]byte(id))
}

// ListQualifications lists the catalogue in the caller's view.
func (s *Store) ListQualifications(ctx context.Context, f vol.QualificationFilter) ([]vol.Qualification, error) {
	return queryAll(ctx, s, "volunteers.postgres.ListQualifications", sqlListQualifications, scanQualification,
		uuidArg(f.Department), f.IncludeOrgWide, f.ArchivedOrgWide, uuids(f.ArchivedDepartments), int32(f.Limit)) //nolint:gosec // bounded by vol.MaxList
}

// QualificationsByIDs returns the entries that exist among ids.
func (s *Store) QualificationsByIDs(ctx context.Context, ids []vol.QualificationID) ([]vol.Qualification, error) {
	return queryAll(ctx, s, "volunteers.postgres.QualificationsByIDs", sqlQualificationsByIDs, scanQualification, uuids(ids))
}

// UpdateQualification changes name, description and validity of an active entry. Fails
// errs.NotFound (unknown or archived), errs.Conflict (name, taken).
func (s *Store) UpdateQualification(ctx context.Context, q vol.Qualification) (vol.Qualification, error) {
	return queryOne(ctx, s, "volunteers.postgres.UpdateQualification", sqlUpdateQualification, scanQualification,
		[16]byte(q.ID), q.Name, q.Description, validityArg(q.ValidityDays), q.UpdatedAt)
}

// ArchiveQualification archives an active entry. Fails errs.NotFound.
func (s *Store) ArchiveQualification(ctx context.Context, id vol.QualificationID, at time.Time) (vol.Qualification, error) {
	return queryOne(ctx, s, "volunteers.postgres.ArchiveQualification", sqlArchiveQualification, scanQualification, [16]byte(id), at)
}

// ---- records ----

var (
	sqlInsertRecord         = mustSQL("insert_record.sql")
	sqlGetRecord            = mustSQL("get_record.sql")
	sqlGetRecordForUpdate   = mustSQL("get_record_for_update.sql")
	sqlRecordsOf            = mustSQL("list_records_by_person.sql")
	sqlVerifyRecord         = mustSQL("verify_record.sql")
	sqlSetRecordCertificate = mustSQL("set_record_certificate.sql")
	sqlDeleteRecord         = mustSQL("delete_record.sql")
	sqlPendingRecords       = mustSQL("list_pending_records.sql")
	sqlValidQualifications  = mustSQL("list_valid_qualifications.sql")
	sqlExpiringRecords      = mustSQL("list_expiring_records.sql")
	sqlMarkExpiryNotified   = mustSQL("mark_expiry_notified.sql")
	sqlExpiredRecords       = mustSQL("list_expired_records.sql")
	sqlDeleteRecords        = mustSQL("delete_records.sql")
)

func recordFrom(id, qual, person [16]byte, acq time.Time, exp *time.Time, by *[16]byte, at *time.Time, cert *[16]byte, created, updated time.Time) vol.Record {
	return vol.Record{
		ID: vol.RecordID(id), Qualification: vol.QualificationID(qual), Person: model.PersonID(person),
		AcquiredOn: vol.DateOf(acq, time.UTC), ExpiresOn: dateOf(exp), VerifiedBy: idOf[model.PersonID](by),
		VerifiedAt: timeOf(at), Certificate: idOf[files.FileID](cert), CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
	}
}

func scanRecord(r rowScanner) (vol.Record, error) {
	var (
		id, qual, person [16]byte
		acq              time.Time
		exp, at          *time.Time
		by, cert         *[16]byte
		created, updated time.Time
	)
	if err := r.Scan(&id, &qual, &person, &acq, &exp, &by, &at, &cert, &created, &updated); err != nil {
		return vol.Record{}, err //nolint:wrapcheck // translate classifies it
	}
	return recordFrom(id, qual, person, acq, exp, by, at, cert, created, updated), nil
}

func scanRecordRow(r rowScanner) (vol.RecordRow, error) {
	var (
		id, qual, person [16]byte
		acq              time.Time
		exp, at          *time.Time
		by, cert, dept   *[16]byte
		created, updated time.Time
	)
	if err := r.Scan(&id, &qual, &person, &acq, &exp, &by, &at, &cert, &created, &updated, &dept); err != nil {
		return vol.RecordRow{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.RecordRow{
		Record: recordFrom(id, qual, person, acq, exp, by, at, cert, created, updated), QualificationDepartment: idOf[model.DepartmentID](dept),
	}, nil
}

// InsertRecord adds a record: verified when VerifiedAt is set, pending otherwise. Fails
// errs.Conflict (pending_exists), errs.Invalid (unknown person or qualification; dates).
func (s *Store) InsertRecord(ctx context.Context, r vol.Record) (vol.Record, error) {
	return queryOne(ctx, s, "volunteers.postgres.InsertRecord", sqlInsertRecord, scanRecord,
		[16]byte(r.ID), [16]byte(r.Qualification), [16]byte(r.Person), r.AcquiredOn.Time(time.UTC), dateArg(r.ExpiresOn),
		uuidArg(r.VerifiedBy), tsArg(r.VerifiedAt), r.CreatedAt)
}

// GetRecord returns one record with the department of its qualification. Fails errs.NotFound.
func (s *Store) GetRecord(ctx context.Context, id vol.RecordID) (vol.RecordRow, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetRecord", sqlGetRecord, scanRecordRow, [16]byte(id))
}

// GetRecordForUpdate is GetRecord with a lock on the record. Fails errs.NotFound.
func (s *Store) GetRecordForUpdate(ctx context.Context, id vol.RecordID) (vol.RecordRow, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetRecordForUpdate", sqlGetRecordForUpdate, scanRecordRow, [16]byte(id))
}

// RecordsOf lists a person's records; all=false limits them to org-wide qualifications and
// those of departments.
func (s *Store) RecordsOf(ctx context.Context, person model.PersonID, all bool, departments []model.DepartmentID) ([]vol.RecordRow, error) {
	return queryAll(ctx, s, "volunteers.postgres.RecordsOf", sqlRecordsOf, scanRecordRow,
		[16]byte(person), all, uuids(departments), int32(vol.MaxList))
}

// VerifyRecord marks a pending record verified; a zero date keeps the stored value and
// clearExpiry sets the expiry to none (the certificate never expires). Fails errs.NotFound
// (gone or already verified), errs.Invalid (dates).
func (s *Store) VerifyRecord(ctx context.Context, id vol.RecordID, by model.PersonID, at time.Time, acquired, expires vol.Date, clearExpiry bool) (vol.Record, error) {
	return queryOne(ctx, s, "volunteers.postgres.VerifyRecord", sqlVerifyRecord, scanRecord,
		[16]byte(id), [16]byte(by), at, dateArg(acquired), dateArg(expires), clearExpiry)
}

// SetRecordCertificate points a record at its certificate file. Fails errs.NotFound.
func (s *Store) SetRecordCertificate(ctx context.Context, id vol.RecordID, file files.FileID, at time.Time) (vol.Record, error) {
	return queryOne(ctx, s, "volunteers.postgres.SetRecordCertificate", sqlSetRecordCertificate, scanRecord, [16]byte(id), [16]byte(file), at)
}

// DeleteRecord deletes a record row. Fails errs.NotFound.
func (s *Store) DeleteRecord(ctx context.Context, id vol.RecordID) (vol.RecordGone, error) {
	return queryOne(ctx, s, "volunteers.postgres.DeleteRecord", sqlDeleteRecord, func(r rowScanner) (vol.RecordGone, error) {
		var rid, person, qual [16]byte
		var cert *[16]byte
		if err := r.Scan(&rid, &person, &qual, &cert); err != nil {
			return vol.RecordGone{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.RecordGone{ID: vol.RecordID(rid), Person: model.PersonID(person), Qualification: vol.QualificationID(qual), Certificate: idOf[files.FileID](cert)}, nil
	}, [16]byte(id))
}

// PendingRecords lists the records awaiting verification that the filter allows.
func (s *Store) PendingRecords(ctx context.Context, f vol.RecordFilter) ([]vol.RecordRow, error) {
	return queryAll(ctx, s, "volunteers.postgres.PendingRecords", sqlPendingRecords, scanRecordRow,
		uuidArg(f.Department), f.All, uuids(f.Departments), [16]byte(f.Actor), int32(f.Limit)) //nolint:gosec // bounded by vol.MaxList
}

// ValidQualifications returns which of people hold which of quals, verified and unexpired on
// the date.
func (s *Store) ValidQualifications(ctx context.Context, people []model.PersonID, on vol.Date, quals []vol.QualificationID) ([]vol.Held, error) {
	return queryAll(ctx, s, "volunteers.postgres.ValidQualifications", sqlValidQualifications, func(r rowScanner) (vol.Held, error) {
		var p, q [16]byte
		if err := r.Scan(&p, &q); err != nil {
			return vol.Held{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.Held{Person: model.PersonID(p), Qualification: vol.QualificationID(q)}, nil
	}, uuids(people), on.Time(time.UTC), uuids(quals))
}

// ExpiringRecords lists verified records expiring in [from, to], not yet notified, with no
// renewal.
func (s *Store) ExpiringRecords(ctx context.Context, from, to vol.Date, limit int) ([]vol.ExpiringRecord, error) {
	return queryAll(ctx, s, "volunteers.postgres.ExpiringRecords", sqlExpiringRecords, func(r rowScanner) (vol.ExpiringRecord, error) {
		var id, p, q [16]byte
		var name string
		var exp time.Time
		if err := r.Scan(&id, &p, &q, &name, &exp); err != nil {
			return vol.ExpiringRecord{}, err //nolint:wrapcheck // translate classifies it
		}
		return vol.ExpiringRecord{
			ID: vol.RecordID(id), Person: model.PersonID(p), Qualification: vol.QualificationID(q),
			QualificationName: name, ExpiresOn: vol.DateOf(exp, time.UTC),
		}, nil
	}, from.Time(time.UTC), to.Time(time.UTC), int32(limit)) //nolint:gosec // bounded batch
}

// MarkExpiryNotified records that the expiry notice was sent on a date.
func (s *Store) MarkExpiryNotified(ctx context.Context, id vol.RecordID, on vol.Date) error {
	_, err := s.exec(ctx, "volunteers.postgres.MarkExpiryNotified", sqlMarkExpiryNotified, [16]byte(id), on.Time(time.UTC))
	return err
}

// ExpiredRecords lists records that expired before the cutoff.
func (s *Store) ExpiredRecords(ctx context.Context, cutoff vol.Date, limit int) ([]vol.ExpiredRecord, error) {
	return queryAll(ctx, s, "volunteers.postgres.ExpiredRecords", sqlExpiredRecords, scanExpired, cutoff.Time(time.UTC), int32(limit)) //nolint:gosec // bounded batch
}

func scanExpired(r rowScanner) (vol.ExpiredRecord, error) {
	var id [16]byte
	var cert *[16]byte
	if err := r.Scan(&id, &cert); err != nil {
		return vol.ExpiredRecord{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.ExpiredRecord{ID: vol.RecordID(id), Certificate: idOf[files.FileID](cert)}, nil
}

// DeleteRecords deletes records by ID and returns how many.
func (s *Store) DeleteRecords(ctx context.Context, ids []vol.RecordID) (int, error) {
	return s.exec(ctx, "volunteers.postgres.DeleteRecords", sqlDeleteRecords, uuids(ids))
}

// ---- positions ----

var (
	sqlInsertPosition       = mustSQL("insert_position.sql")
	sqlGetPosition          = mustSQL("get_position.sql")
	sqlGetPositionForUpdate = mustSQL("get_position_for_update.sql")
	sqlListPositions        = mustSQL("list_positions_by_department.sql")
	sqlUpdatePosition       = mustSQL("update_position.sql")
	sqlArchivePosition      = mustSQL("archive_position.sql")
	sqlCountFutureShifts    = mustSQL("count_future_shifts_by_position.sql")
	sqlDeleteRequirements   = mustSQL("delete_requirements_by_position.sql")
	sqlInsertRequirements   = mustSQL("insert_requirements.sql")
	sqlTouchPosition        = mustSQL("touch_position.sql")
	sqlRequirementsOf       = mustSQL("list_requirements_by_positions.sql")
)

func scanPosition(r rowScanner) (vol.Position, error) {
	var (
		id, dept         [16]byte
		name, desc, acc  string
		hide             bool
		archived         *time.Time
		created, updated time.Time
	)
	if err := r.Scan(&id, &dept, &name, &desc, &acc, &hide, &archived, &created, &updated); err != nil {
		return vol.Position{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.Position{
		ID: vol.PositionID(id), Department: model.DepartmentID(dept), Name: name, Description: desc, Access: vol.Access(acc),
		HideShiftMates: hide, ArchivedAt: timeOf(archived), CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
	}, nil
}

// InsertPosition adds a position. Fails errs.Conflict (name, taken).
func (s *Store) InsertPosition(ctx context.Context, p vol.Position) (vol.Position, error) {
	return queryOne(ctx, s, "volunteers.postgres.InsertPosition", sqlInsertPosition, scanPosition,
		[16]byte(p.ID), [16]byte(p.Department), p.Name, p.Description, string(p.Access), p.HideShiftMates, p.CreatedAt)
}

// GetPosition returns one position without its requirements. Fails errs.NotFound.
func (s *Store) GetPosition(ctx context.Context, id vol.PositionID) (vol.Position, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetPosition", sqlGetPosition, scanPosition, [16]byte(id))
}

// GetPositionForUpdate is GetPosition with a row lock. Fails errs.NotFound.
func (s *Store) GetPositionForUpdate(ctx context.Context, id vol.PositionID) (vol.Position, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetPositionForUpdate", sqlGetPositionForUpdate, scanPosition, [16]byte(id))
}

// ListPositions lists a department's positions without requirements.
func (s *Store) ListPositions(ctx context.Context, dept model.DepartmentID, includeArchived bool) ([]vol.Position, error) {
	return queryAll(ctx, s, "volunteers.postgres.ListPositions", sqlListPositions, scanPosition,
		[16]byte(dept), includeArchived, int32(vol.MaxList))
}

// UpdatePosition changes an active position. Fails errs.NotFound, errs.Conflict (name, taken).
func (s *Store) UpdatePosition(ctx context.Context, p vol.Position) (vol.Position, error) {
	return queryOne(ctx, s, "volunteers.postgres.UpdatePosition", sqlUpdatePosition, scanPosition,
		[16]byte(p.ID), p.Name, p.Description, string(p.Access), p.HideShiftMates, p.UpdatedAt)
}

// ArchivePosition archives an active position. Fails errs.NotFound.
func (s *Store) ArchivePosition(ctx context.Context, id vol.PositionID, at time.Time) (vol.Position, error) {
	return queryOne(ctx, s, "volunteers.postgres.ArchivePosition", sqlArchivePosition, scanPosition, [16]byte(id), at)
}

// CountFutureShifts counts the position's non-canceled shifts that have not ended.
func (s *Store) CountFutureShifts(ctx context.Context, id vol.PositionID, now time.Time) (int, error) {
	return s.count(ctx, "volunteers.postgres.CountFutureShifts", sqlCountFutureShifts, [16]byte(id), now)
}

// DeleteRequirements clears the position's requirement set.
func (s *Store) DeleteRequirements(ctx context.Context, id vol.PositionID) (int, error) {
	return s.exec(ctx, "volunteers.postgres.DeleteRequirements", sqlDeleteRequirements, [16]byte(id))
}

// InsertRequirements adds the qualifications to the position's set.
func (s *Store) InsertRequirements(ctx context.Context, id vol.PositionID, quals []vol.QualificationID, at time.Time) (int, error) {
	return s.exec(ctx, "volunteers.postgres.InsertRequirements", sqlInsertRequirements, [16]byte(id), uuids(quals), at)
}

// TouchPosition bumps the position's updated_at. Fails errs.NotFound.
func (s *Store) TouchPosition(ctx context.Context, id vol.PositionID, at time.Time) (vol.Position, error) {
	return queryOne(ctx, s, "volunteers.postgres.TouchPosition", sqlTouchPosition, scanPosition, [16]byte(id), at)
}

// RequirementsOf returns the sorted requirement sets of the positions.
func (s *Store) RequirementsOf(ctx context.Context, ids []vol.PositionID) (map[vol.PositionID][]vol.QualificationID, error) {
	type pair struct {
		p vol.PositionID
		q vol.QualificationID
	}
	rows, err := queryAll(ctx, s, "volunteers.postgres.RequirementsOf", sqlRequirementsOf, func(r rowScanner) (pair, error) {
		var p, q [16]byte
		if err := r.Scan(&p, &q); err != nil {
			return pair{}, err //nolint:wrapcheck // translate classifies it
		}
		return pair{vol.PositionID(p), vol.QualificationID(q)}, nil
	}, uuids(ids))
	if err != nil {
		return nil, err
	}
	out := map[vol.PositionID][]vol.QualificationID{}
	for _, r := range rows {
		out[r.p] = append(out[r.p], r.q)
	}
	return out, nil
}

// ---- members ----

var (
	sqlInsertMember        = mustSQL("insert_member.sql")
	sqlGetMember           = mustSQL("get_member.sql")
	sqlDeleteMember        = mustSQL("delete_member.sql")
	sqlListMembers         = mustSQL("list_members_by_department.sql")
	sqlMembershipsOf       = mustSQL("list_members_by_person.sql")
	sqlMembershipsOfPeople = mustSQL("list_members_by_people.sql")
	sqlCancelLeaveSignups  = mustSQL("cancel_leave_signups.sql")
)

func scanMember(r rowScanner) (vol.Member, error) {
	var (
		id, dept, person [16]byte
		by               *[16]byte
		created, updated time.Time
	)
	if err := r.Scan(&id, &dept, &person, &by, &created, &updated); err != nil {
		return vol.Member{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.Member{
		ID: vol.MemberID(id), Department: model.DepartmentID(dept), Person: model.PersonID(person),
		AddedBy: idOf[model.PersonID](by), CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
	}, nil
}

// InsertMember adds a membership. Fails errs.Conflict (duplicate), errs.Invalid (unknown person).
func (s *Store) InsertMember(ctx context.Context, m vol.Member) (vol.Member, error) {
	return queryOne(ctx, s, "volunteers.postgres.InsertMember", sqlInsertMember, scanMember,
		[16]byte(m.ID), [16]byte(m.Department), [16]byte(m.Person), uuidArg(m.AddedBy), m.CreatedAt)
}

// GetMember returns a membership. Fails errs.NotFound when the person is not a member.
func (s *Store) GetMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (vol.Member, error) {
	return queryOne(ctx, s, "volunteers.postgres.GetMember", sqlGetMember, scanMember, [16]byte(dept), [16]byte(person))
}

// DeleteMember deletes a membership. Fails errs.NotFound when the person is not a member.
func (s *Store) DeleteMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (vol.Member, error) {
	return queryOne(ctx, s, "volunteers.postgres.DeleteMember", sqlDeleteMember, scanMember, [16]byte(dept), [16]byte(person))
}

// ListMembers lists up to f.Limit of a department's members in (created_at, id) order, after
// the keyset cursor (f.AfterCreated, f.AfterID) when f.AfterCreated is set.
func (s *Store) ListMembers(ctx context.Context, f vol.MemberFilter) ([]vol.Member, error) {
	var afterCreated, afterID any // nil = the first page
	if !f.AfterCreated.IsZero() {
		afterCreated, afterID = f.AfterCreated, [16]byte(f.AfterID)
	}
	return queryAll(ctx, s, "volunteers.postgres.ListMembers", sqlListMembers, scanMember,
		[16]byte(f.Department), afterCreated, afterID, int32(f.Limit)) //nolint:gosec // bounded by vol.MaxList + 1
}

// MembershipsOf lists a person's memberships.
func (s *Store) MembershipsOf(ctx context.Context, person model.PersonID) ([]vol.Member, error) {
	return queryAll(ctx, s, "volunteers.postgres.MembershipsOf", sqlMembershipsOf, scanMember, [16]byte(person))
}

// MembershipsOfPeople lists the memberships of people, limited to departments unless all.
func (s *Store) MembershipsOfPeople(ctx context.Context, people []model.PersonID, all bool, departments []model.DepartmentID) ([]vol.Member, error) {
	return queryAll(ctx, s, "volunteers.postgres.MembershipsOfPeople", sqlMembershipsOfPeople, scanMember,
		uuids(people), all, uuids(departments))
}

func scanCanceled(r rowScanner) (vol.CanceledSignup, error) {
	var su, sh, p, d [16]byte
	var label, zone string
	var starts time.Time
	if err := r.Scan(&su, &sh, &p, &d, &label, &starts, &zone); err != nil {
		return vol.CanceledSignup{}, err //nolint:wrapcheck // translate classifies it
	}
	return vol.CanceledSignup{
		Signup: vol.SignupID(su), Shift: vol.ShiftID(sh), Person: model.PersonID(p), Department: model.DepartmentID(d),
		Label: label, Starts: starts.UTC(), Zone: zone,
	}, nil
}

// CancelLeaveSignups cancels the person's future bookings on members-only positions of the
// department (reason self) and returns them.
func (s *Store) CancelLeaveSignups(ctx context.Context, person model.PersonID, dept model.DepartmentID, now time.Time) ([]vol.CanceledSignup, error) {
	return queryAll(ctx, s, "volunteers.postgres.CancelLeaveSignups", sqlCancelLeaveSignups, scanCanceled,
		[16]byte(person), [16]byte(dept), now, string(vol.CancelSelf))
}

// conflictErr is a coded errs.Conflict.
func conflictErr(op, field, code, reason string, params map[string]string) error {
	return vol.ConflictProblem(op, field, code, reason, params) //nolint:wrapcheck // built with errs.E
}

// fieldErr is a coded errs.Invalid.
func fieldErr(op, field, code, reason string, params map[string]string) error {
	return vol.FieldProblem(op, field, code, reason, params) //nolint:wrapcheck // built with errs.E
}
