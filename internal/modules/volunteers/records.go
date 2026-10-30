package volunteers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// today is the current civil date in UTC (the unit qualification records expire in).
func (s *Service) today() vol.Date { return vol.DateOf(s.now(), time.UTC) }

// verifyScope checks that the actor may verify records of a qualification of department d
// (zero = org-wide): qualifications.verify at the department, or, for an org-wide one, in
// any extent (D41). Fails errs.Permission.
func (s *Service) verifyScope(ctx context.Context, d model.DepartmentID) error {
	if !d.IsZero() {
		return s.allowIn(ctx, vol.PermissionQualificationsVerify, d)
	}
	ext, err := s.extent(ctx, vol.PermissionQualificationsVerify)
	if err != nil {
		return err
	}
	if ext.IsZero() {
		return errs.E("volunteers.verifyScope", errs.Permission, errors.New("verifying an org-wide qualification needs qualifications.verify somewhere"))
	}
	return nil
}

// readScope reports whether the actor may read another person's records of a qualification
// of department d (zero = org-wide): qualifications.read or .verify at the department, or
// in any extent for an org-wide one.
func (s *Service) readScope(ctx context.Context, d model.DepartmentID) (bool, error) {
	for _, p := range []authz.Permission{vol.PermissionQualificationsRead, vol.PermissionQualificationsVerify} {
		ext, err := s.extent(ctx, p)
		if err != nil {
			return false, err
		}
		if d.IsZero() && !ext.IsZero() || !d.IsZero() && ext.Covers(d) {
			return true, nil
		}
	}
	return false, nil
}

// RecordsOf lists a person's records: their own without a permission, another's within the
// caller's qualifications.read (or .verify) extent, filtered to the qualifications of that
// extent plus org-wide ones. Records are internal (D42), so nothing is audited. On a person's
// own list SelfUpload carries the self_upload setting, read now, so the screen can show or
// hide the upload action; on another person's it is absent. Fails errs.Unauthenticated,
// errs.Invalid (zero person), errs.Permission (another person, no extent) and store kinds.
func (s *Service) RecordsOf(ctx context.Context, person model.PersonID) (_ vol.RecordList, err error) {
	const op = "volunteers.RecordsOf"
	ctx, span := s.begin(ctx, "volunteers.records-of")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.RecordList{}, err
	}
	if person.IsZero() {
		return vol.RecordList{}, invalid(op, "person", validate.CodeRequired, "must be set")
	}
	all, depts := true, []model.DepartmentID(nil)
	if person != actor {
		all = false
		for _, p := range []authz.Permission{vol.PermissionQualificationsRead, vol.PermissionQualificationsVerify} {
			ext, err := s.extent(ctx, p)
			if err != nil {
				return vol.RecordList{}, wrap(op, err)
			}
			all = all || ext.Site
			depts = append(depts, ext.Departments...)
		}
		if !all && len(depts) == 0 {
			return vol.RecordList{}, errs.E(op, errs.Permission, errors.New("another person's qualifications need qualifications.read"))
		}
		slices.SortFunc(depts, cmpBytes[model.DepartmentID])
		depts = slices.Compact(depts)
	}
	rows, err := s.store.RecordsOf(ctx, person, all, depts)
	if err != nil {
		return vol.RecordList{}, wrap(op, err)
	}
	out := make([]vol.Record, len(rows))
	for i := range rows {
		out[i] = rows[i].Record
	}
	if err := filterAll(plainView(actor), out, noRel); err != nil {
		return vol.RecordList{}, wrap(op, err)
	}
	list := vol.RecordList{Items: orEmpty(out)}
	if person == actor {
		on, serr := s.settings.Bool(ctx, SettingSelfUpload)
		if serr != nil {
			return vol.RecordList{}, wrap(op, serr)
		}
		list.SelfUpload = &on
	}
	return list, nil
}

// cmpBytes orders two IDs bytewise.
func cmpBytes[T ~[16]byte](a, b T) int { return strings.Compare(string(a[:]), string(b[:])) }

// checkNotFuture fails errs.Invalid (acquired_on, range) when d is after today.
func (s *Service) checkNotFuture(op string, d vol.Date) error {
	if today := s.today(); today.Before(d) {
		return invalid(op, "acquired_on", validate.CodeRange, "must not be in the future")
	}
	return nil
}

// RecordQualification is way (a): a lead records a qualification as verified now, by the
// actor, from their own training records. A department-specific qualification needs
// qualifications.verify at that department, an org-wide one in any extent (D41). Never for
// oneself. ExpiresOn defaults to AcquiredOn plus the catalogue validity; NoExpiry makes the
// record indefinite whatever the catalogue says (with an ExpiresOn it is errs.Invalid
// one_of on no_expiry). Publishes
// volunteers.qualification-verified, sends volunteers.qualification-verified to the person
// and records qualification-record.created and .verified. Fails errs.Unauthenticated,
// errs.Invalid (self, dates, unknown person), errs.Permission, errs.NotFound,
// errs.Conflict (archived), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) RecordQualification(ctx context.Context, in vol.RecordInput) (_ vol.Record, err error) {
	const op = "volunteers.RecordQualification"
	ctx, span := s.begin(ctx, "volunteers.record-qualification")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Record{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if in.Person == actor {
		return vol.Record{}, invalid(op, "person", vol.CodeSelf, "you cannot record a qualification for yourself")
	}
	if err := s.checkNotFuture(op, in.AcquiredOn); err != nil {
		return vol.Record{}, err
	}
	q, err := s.store.GetQualification(ctx, in.Qualification)
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if err := s.verifyScope(ctx, q.Department); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if q.Archived() {
		return vol.Record{}, wrap(op, archivedErr("qualification"))
	}
	now := s.now()
	rec := vol.Record{
		ID: model.NewID[vol.RecordID](), Qualification: q.ID, Person: in.Person, AcquiredOn: in.AcquiredOn,
		ExpiresOn: in.ExpiresOn, VerifiedBy: actor, VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if rec.ExpiresOn.IsZero() && q.ValidityDays > 0 && !in.NoExpiry {
		rec.ExpiresOn = in.AcquiredOn.AddDays(q.ValidityDays)
	}
	var saved vol.Record
	err = s.write(ctx, func(ctx context.Context) error {
		var ierr error
		if saved, ierr = s.store.InsertRecord(ctx, rec); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		return s.afterVerified(ctx, saved, q, true)
	})
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	return saved, nil
}

// afterVerified is what a record becoming verified does inside its transaction: the audit
// entries, the signal and the notice to the person. created adds qualification-record.created
// (a record a lead entered); VerifyRecord has already recorded its creation.
func (s *Service) afterVerified(ctx context.Context, rec vol.Record, q vol.Qualification, created bool) error {
	var entries []audit.Entry
	if created {
		entries = append(entries, act("volunteers.qualification-record.created", ref(kindRecord, rec.ID), rec.Person, ""))
	}
	entries = append(entries, act("volunteers.qualification-record.verified", ref(kindRecord, rec.ID), rec.Person, ""))
	if err := s.record(ctx, entries...); err != nil {
		return err
	}
	pl := []signals.Entry{
		signals.Ref(PayloadRecord, rec.ID), signals.Ref(PayloadQualification, rec.Qualification), signals.Ref(PayloadPerson, rec.Person),
	}
	if !q.Department.IsZero() {
		pl = append(pl, signals.Ref(PayloadDepartment, q.Department))
	}
	if err := s.publish(ctx, SignalQualificationVerified, pl...); err != nil {
		return err
	}
	vars := map[string]string{"qualification": q.Name}
	if !rec.ExpiresOn.IsZero() {
		vars["expires_on"] = rec.ExpiresOn.Long()
	}
	return s.send(ctx, notify.Message{
		Template: TemplateQualificationVerified, To: []model.PersonID{rec.Person},
		Key: "verified:" + rec.ID.String(), Vars: vars, Link: "/volunteers/me",
	})
}

// UploadCertificate is way (b): the actor creates a pending record for themself with the
// certificate stored through files (subject the actor, level personal). A pending record
// never counts until a lead verifies it. The handler passes the parsed multipart form; the
// file is staged before the transaction and attached inside it with the record, and a
// failure leaves nothing behind. Fails errs.Unauthenticated, errs.Unsupported (the
// self_upload setting is off), errs.Invalid (file, type, size, dates), errs.NotFound,
// errs.Conflict (archived; pending_exists; quota), errs.Unavailable wrapping db.ErrReadOnly
// and files' and store kinds.
func (s *Service) UploadCertificate(ctx context.Context, in vol.UploadInput) (_ vol.Record, err error) {
	const op = "volunteers.UploadCertificate"
	ctx, span := s.begin(ctx, "volunteers.upload-certificate")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Record{}, err
	}
	on, err := s.settings.Bool(ctx, SettingSelfUpload)
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if !on {
		return vol.Record{}, errs.E(op, errs.Unsupported, errors.New("uploading a certificate is turned off"))
	}
	if err := in.Validate(); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if err := s.checkNotFuture(op, in.AcquiredOn); err != nil {
		return vol.Record{}, err
	}
	q, err := s.store.GetQualification(ctx, in.Qualification)
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if q.Archived() {
		return vol.Record{}, wrap(op, archivedErr("qualification"))
	}
	accept := append(slices.Clone(files.Images), files.Documents...)
	st, err := s.files.Stage(ctx, files.Upload{Name: in.Upload.Name, Accept: accept, Body: in.Upload.Body})
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	defer func() {
		if derr := s.files.Discard(ctx, st); derr != nil {
			s.logger.WarnContext(ctx, "a staged certificate could not be discarded", slog.String("kind", errs.KindOf(derr).String()))
		}
	}()
	now := s.now()
	rec := vol.Record{
		ID: model.NewID[vol.RecordID](), Qualification: q.ID, Person: actor, AcquiredOn: in.AcquiredOn,
		ExpiresOn: in.ExpiresOn, CreatedAt: now, UpdatedAt: now,
	}
	if rec.ExpiresOn.IsZero() && q.ValidityDays > 0 {
		rec.ExpiresOn = in.AcquiredOn.AddDays(q.ValidityDays)
	}
	var saved vol.Record
	err = s.write(ctx, func(ctx context.Context) error {
		var ierr error
		if saved, ierr = s.store.InsertRecord(ctx, rec); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		f, aerr := s.files.Attach(ctx, st, files.Attachment{
			Record: ref(kindRecord, saved.ID), Subject: actor, Level: model.SensitivityPersonal, Replace: true,
		})
		if aerr != nil {
			return fmt.Errorf("attach: %w", aerr)
		}
		if saved, ierr = s.store.SetRecordCertificate(ctx, saved.ID, f.ID, now); ierr != nil {
			return fmt.Errorf("set certificate: %w", ierr)
		}
		return s.record(ctx, act("volunteers.qualification-record.created", ref(kindRecord, saved.ID), actor, "self"))
	})
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{Subject: actor}); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	return saved, nil
}

// VerifyRecord marks a pending record verified by the actor, optionally correcting the
// dates; a zero date keeps what the person entered and NoExpiry clears the expiry (a
// certificate that never expires; with an ExpiresOn it is errs.Invalid one_of on no_expiry).
// Permission as RecordQualification; never one's own record. Publishes
// volunteers.qualification-verified, sends volunteers.qualification-verified to the person
// and records qualification-record.verified. Fails errs.Unauthenticated, errs.Invalid (self,
// dates), errs.Permission, errs.NotFound, errs.Conflict (status: already verified),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) VerifyRecord(ctx context.Context, id vol.RecordID, in vol.VerifyInput) (_ vol.Record, err error) {
	const op = "volunteers.VerifyRecord"
	ctx, span := s.begin(ctx, "volunteers.verify-record")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Record{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	var saved vol.Record
	err = s.write(ctx, func(ctx context.Context) error {
		row, gerr := s.store.GetRecordForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if err := s.verifyScope(ctx, row.QualificationDepartment); err != nil {
			return err
		}
		if row.Person == actor {
			return invalid(op, "person", vol.CodeSelf, "you cannot verify your own record")
		}
		if row.Verified() {
			return conflict(op, "record", vol.ConflictStatus, "the record is already verified", nil)
		}
		acquired, expires := row.AcquiredOn, row.ExpiresOn
		if !in.AcquiredOn.IsZero() {
			acquired = in.AcquiredOn
		}
		switch {
		case in.NoExpiry:
			expires = vol.Date{}
		case !in.ExpiresOn.IsZero():
			expires = in.ExpiresOn
		}
		if !expires.IsZero() && expires.Before(acquired) {
			return invalid(op, "expires_on", validate.CodeRange, "must not be before acquired_on")
		}
		var verr error
		if saved, verr = s.store.VerifyRecord(ctx, id, actor, s.now(), in.AcquiredOn, in.ExpiresOn, in.NoExpiry); verr != nil {
			return fmt.Errorf("verify: %w", verr)
		}
		q, qerr := s.store.GetQualification(ctx, saved.Qualification)
		if qerr != nil {
			return fmt.Errorf("qualification: %w", qerr)
		}
		return s.afterVerified(ctx, saved, q, false)
	})
	if err != nil {
		return vol.Record{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{}); err != nil {
		return vol.Record{}, wrap(op, err)
	}
	return saved, nil
}

// RejectRecord deletes a pending record and its certificate (nothing to keep: it never
// counted) and sends volunteers.qualification-rejected. Permission as VerifyRecord. Records
// qualification-record.rejected. Fails errs.Unauthenticated, errs.Permission, errs.NotFound,
// errs.Conflict (status: already verified), errs.Unavailable wrapping db.ErrReadOnly, files'
// and store kinds.
func (s *Service) RejectRecord(ctx context.Context, id vol.RecordID) (err error) {
	const op = "volunteers.RejectRecord"
	ctx, span := s.begin(ctx, "volunteers.reject-record")
	defer endSpan(span, &err)
	if _, err := s.actorOf(ctx, op); err != nil {
		return err
	}
	err = s.write(ctx, func(ctx context.Context) error {
		row, gerr := s.store.GetRecordForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if err := s.verifyScope(ctx, row.QualificationDepartment); err != nil {
			return err
		}
		if row.Verified() {
			return conflict(op, "record", vol.ConflictStatus, "a verified record is removed, not rejected", nil)
		}
		q, qerr := s.store.GetQualification(ctx, row.Qualification)
		if qerr != nil {
			return fmt.Errorf("qualification: %w", qerr)
		}
		if err := s.removeRecord(ctx, id); err != nil {
			return err
		}
		if err := s.record(ctx, act("volunteers.qualification-record.rejected", ref(kindRecord, id), row.Person, "")); err != nil {
			return err
		}
		return s.send(ctx, notify.Message{
			Template: TemplateQualificationRejected, To: []model.PersonID{row.Person},
			Key: "rejected:" + id.String(), Vars: map[string]string{"qualification": q.Name}, Link: "/volunteers/me",
		})
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// removeRecord deletes a record row and its certificate files inside the caller's Write
// transaction.
func (s *Service) removeRecord(ctx context.Context, id vol.RecordID) error {
	if _, err := s.store.DeleteRecord(ctx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if _, err := s.files.DeleteByRecord(ctx, ref(kindRecord, id)); err != nil {
		return fmt.Errorf("delete certificate: %w", err)
	}
	return nil
}

// RemoveRecord deletes a record and its certificate: the person removes their own pending
// one; a lead who may verify removes any (a mistaken entry). Records
// qualification-record.deleted. Fails errs.Unauthenticated, errs.Permission (one's own
// verified record), errs.NotFound (unknown, or another person's without the permission),
// errs.Unavailable wrapping db.ErrReadOnly, files' and store kinds.
func (s *Service) RemoveRecord(ctx context.Context, id vol.RecordID) (err error) {
	const op = "volunteers.RemoveRecord"
	ctx, span := s.begin(ctx, "volunteers.remove-record")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(ctx context.Context) error {
		row, gerr := s.store.GetRecordForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if row.Person == actor {
			if row.Verified() {
				return errs.E(op, errs.Permission, errors.New("a verified record is removed by a lead"))
			}
		} else if verr := s.verifyScope(ctx, row.QualificationDepartment); verr != nil {
			if errs.Is(verr, errs.Permission) {
				return errs.E(op, errs.NotFound, errors.New("no such record"))
			}
			return verr
		}
		if err := s.removeRecord(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, act("volunteers.qualification-record.deleted", ref(kindRecord, id), row.Person, ""))
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// PendingRecords lists the records awaiting verification that the actor may verify, oldest
// first, never the actor's own. Fails errs.Unauthenticated, errs.Permission (no
// qualifications.verify extent, or q.Department outside it) and store kinds.
func (s *Service) PendingRecords(ctx context.Context, q vol.PendingQuery) (_ []vol.Record, err error) {
	const op = "volunteers.PendingRecords"
	ctx, span := s.begin(ctx, "volunteers.pending-records")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return nil, err
	}
	ext, err := s.extent(ctx, vol.PermissionQualificationsVerify)
	if err != nil {
		return nil, wrap(op, err)
	}
	if ext.IsZero() || (!q.Department.IsZero() && !ext.Covers(q.Department)) {
		return nil, errs.E(op, errs.Permission, errors.New("verifying records needs qualifications.verify"))
	}
	rows, err := s.store.PendingRecords(ctx, vol.RecordFilter{
		Department: q.Department, All: ext.Site, Departments: ext.Departments, Actor: actor, Limit: vol.MaxList,
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]vol.Record, len(rows))
	for i := range rows {
		out[i] = rows[i].Record
	}
	if err := filterAll(plainView(actor), out, noRel); err != nil {
		return nil, wrap(op, err)
	}
	return out, nil
}

// OpenCertificate opens the certificate of a record for the person themself or for a lead
// who may read or verify the record. When the actor is not the person it records
// qualification-record.read (personal, Fields [certificate]) before opening, so a failed
// audit opens nothing; files audits the bytes. The handler passes the result to files.Serve
// and closes the content. Fails errs.Unauthenticated, errs.NotFound (unknown record, no
// certificate, or another person's without the permission), the audit's kinds, and files'
// kinds (errs.Unavailable wrapping files.ErrBlobMissing when the bytes are gone).
func (s *Service) OpenCertificate(ctx context.Context, id vol.RecordID) (_ files.File, _ files.Content, err error) {
	const op = "volunteers.OpenCertificate"
	ctx, span := s.begin(ctx, "volunteers.open-certificate")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return files.File{}, nil, err
	}
	row, err := s.store.GetRecord(ctx, id)
	if err != nil {
		return files.File{}, nil, wrap(op, err)
	}
	notFound := errs.E(op, errs.NotFound, errors.New("no such certificate"))
	if row.Person != actor {
		ok, rerr := s.readScope(ctx, row.QualificationDepartment)
		if rerr != nil {
			return files.File{}, nil, wrap(op, rerr)
		}
		if !ok {
			return files.File{}, nil, notFound
		}
	}
	if row.Certificate.IsZero() {
		return files.File{}, nil, notFound
	}
	if row.Person != actor {
		if err := s.record(ctx, audit.Entry{
			Action: "volunteers.qualification-record.read", Outcome: audit.Succeeded, Subject: row.Person,
			Record: ref(kindRecord, id), Fields: []string{"certificate"}, Level: model.SensitivityPersonal,
		}); err != nil {
			return files.File{}, nil, wrap(op, err)
		}
	}
	f, c, err := s.files.Open(ctx, row.Certificate)
	if err != nil {
		return files.File{}, nil, wrap(op, err)
	}
	return f, c, nil
}
