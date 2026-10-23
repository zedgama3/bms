package volunteers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// store is what the service needs from storage. postgres.Store implements it; no SQL or
// driver types appear here (D8). Every method runs on the transaction in ctx when there is
// one. The doc of each method is the doc of the query behind it (postgres/sql/*.sql).
type store interface {
	// qualifications
	InsertQualification(ctx context.Context, q vol.Qualification) (vol.Qualification, error)
	GetQualification(ctx context.Context, id vol.QualificationID) (vol.Qualification, error)
	GetQualificationForUpdate(ctx context.Context, id vol.QualificationID) (vol.Qualification, error)
	ListQualifications(ctx context.Context, f vol.QualificationFilter) ([]vol.Qualification, error)
	QualificationsByIDs(ctx context.Context, ids []vol.QualificationID) ([]vol.Qualification, error)
	UpdateQualification(ctx context.Context, q vol.Qualification) (vol.Qualification, error)
	ArchiveQualification(ctx context.Context, id vol.QualificationID, at time.Time) (vol.Qualification, error)

	// records
	InsertRecord(ctx context.Context, r vol.Record) (vol.Record, error)
	GetRecord(ctx context.Context, id vol.RecordID) (vol.RecordRow, error)
	GetRecordForUpdate(ctx context.Context, id vol.RecordID) (vol.RecordRow, error)
	RecordsOf(ctx context.Context, person model.PersonID, all bool, departments []model.DepartmentID) ([]vol.RecordRow, error)
	VerifyRecord(ctx context.Context, id vol.RecordID, by model.PersonID, at time.Time, acquired, expires vol.Date, clearExpiry bool) (vol.Record, error)
	SetRecordCertificate(ctx context.Context, id vol.RecordID, file files.FileID, at time.Time) (vol.Record, error)
	DeleteRecord(ctx context.Context, id vol.RecordID) (vol.RecordGone, error)
	PendingRecords(ctx context.Context, f vol.RecordFilter) ([]vol.RecordRow, error)
	ValidQualifications(ctx context.Context, people []model.PersonID, on vol.Date, quals []vol.QualificationID) ([]vol.Held, error)
	ExpiringRecords(ctx context.Context, from, to vol.Date, limit int) ([]vol.ExpiringRecord, error)
	MarkExpiryNotified(ctx context.Context, id vol.RecordID, on vol.Date) error
	ExpiredRecords(ctx context.Context, cutoff vol.Date, limit int) ([]vol.ExpiredRecord, error)
	DeleteRecords(ctx context.Context, ids []vol.RecordID) (int, error)

	// positions
	InsertPosition(ctx context.Context, p vol.Position) (vol.Position, error)
	GetPosition(ctx context.Context, id vol.PositionID) (vol.Position, error)
	GetPositionForUpdate(ctx context.Context, id vol.PositionID) (vol.Position, error)
	ListPositions(ctx context.Context, dept model.DepartmentID, includeArchived bool) ([]vol.Position, error)
	UpdatePosition(ctx context.Context, p vol.Position) (vol.Position, error)
	ArchivePosition(ctx context.Context, id vol.PositionID, at time.Time) (vol.Position, error)
	CountFutureShifts(ctx context.Context, id vol.PositionID, now time.Time) (int, error)
	DeleteRequirements(ctx context.Context, id vol.PositionID) (int, error)
	InsertRequirements(ctx context.Context, id vol.PositionID, quals []vol.QualificationID, at time.Time) (int, error)
	TouchPosition(ctx context.Context, id vol.PositionID, at time.Time) (vol.Position, error)
	RequirementsOf(ctx context.Context, ids []vol.PositionID) (map[vol.PositionID][]vol.QualificationID, error)

	// members
	InsertMember(ctx context.Context, m vol.Member) (vol.Member, error)
	GetMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (vol.Member, error)
	DeleteMember(ctx context.Context, dept model.DepartmentID, person model.PersonID) (vol.Member, error)
	ListMembers(ctx context.Context, f vol.MemberFilter) ([]vol.Member, error)
	MembershipsOf(ctx context.Context, person model.PersonID) ([]vol.Member, error)
	MembershipsOfPeople(ctx context.Context, people []model.PersonID, all bool, departments []model.DepartmentID) ([]vol.Member, error)
	CancelLeaveSignups(ctx context.Context, person model.PersonID, dept model.DepartmentID, now time.Time) ([]vol.CanceledSignup, error)

	// shifts
	InsertShift(ctx context.Context, sh vol.Shift) (vol.Shift, error)
	InsertShiftCopy(ctx context.Context, sh vol.Shift) (vol.Shift, bool, error)
	GetShift(ctx context.Context, id vol.ShiftID) (vol.Shift, error)
	GetShiftForUpdate(ctx context.Context, id vol.ShiftID) (vol.Shift, error)
	GetShiftSummary(ctx context.Context, id vol.ShiftID, actor model.PersonID) (vol.ShiftRow, error)
	ListShifts(ctx context.Context, f vol.ShiftFilter) ([]vol.ShiftRow, error)
	UpdateShift(ctx context.Context, sh vol.Shift) (vol.Shift, error)
	CancelShift(ctx context.Context, id vol.ShiftID, reason vol.CancelReason, at time.Time) (vol.Shift, error)
	CancelShiftsByDepartment(ctx context.Context, dept model.DepartmentID, now time.Time, limit int) ([]vol.ShiftID, error)
	CancelShiftsByEvent(ctx context.Context, event model.EventID, now time.Time, limit int) ([]vol.ShiftID, error)
	CancelSignupsByShifts(ctx context.Context, shifts []vol.ShiftID, reason vol.CancelReason, at time.Time) ([]vol.CanceledSignup, error)
	ShiftsToCopy(ctx context.Context, dept model.DepartmentID, from, to model.EventID, afterStarts time.Time, after vol.ShiftID) ([]vol.CopyCandidate, error)
	DepartmentsWithShifts(ctx context.Context, event model.EventID) ([]model.DepartmentID, error)

	// sign-ups
	LockPersonBookings(ctx context.Context, person model.PersonID) error
	LockPersonsBookings(ctx context.Context, people []model.PersonID) error
	InsertSignup(ctx context.Context, su vol.Signup) (vol.Signup, error)
	FindOverlap(ctx context.Context, person model.PersonID, times model.TimeRange, exclude vol.ShiftID) (vol.Overlap, bool, error)
	FindOverlapForShiftChange(ctx context.Context, shift vol.ShiftID, times model.TimeRange) (vol.Overlap, bool, error)
	GetSignup(ctx context.Context, id vol.SignupID) (vol.SignupDetail, error)
	GetSignupForUpdate(ctx context.Context, id vol.SignupID) (vol.SignupDetail, error)
	CancelSignup(ctx context.Context, id vol.SignupID, by model.PersonID, reason vol.CancelReason, at time.Time) (vol.Signup, error)
	Roster(ctx context.Context, shift vol.ShiftID) ([]vol.Signup, error)
	Mates(ctx context.Context, shift vol.ShiftID, actor model.PersonID) ([]vol.Mate, error)
	Schedule(ctx context.Context, f vol.ScheduleFilter) ([]vol.ScheduleRow, error)
	BookedWindows(ctx context.Context, person model.PersonID, within model.TimeRange) ([]vol.BookedWindow, error)
	Listed(ctx context.Context, person model.PersonID) (bool, error)
	SetUnlisted(ctx context.Context, person model.PersonID, at time.Time) (bool, error)
	SetListedTrue(ctx context.Context, person model.PersonID) (bool, error)
	UpcomingCounts(ctx context.Context, people []model.PersonID, now time.Time, all bool, departments []model.DepartmentID) (map[model.PersonID]int, error)

	// retention
	PurgeSignups(ctx context.Context, cutoff time.Time, limit int) (int, error)
	PurgeCanceledShifts(ctx context.Context, cutoff time.Time, limit int) (int, error)

	// one person's data
	ExportRecords(ctx context.Context, person model.PersonID) ([]vol.RecordExport, error)
	ExportSignups(ctx context.Context, person model.PersonID) ([]vol.SignupExport, error)
	CountVerifiedBy(ctx context.Context, person model.PersonID) (int, error)
	CountShiftsCreatedBy(ctx context.Context, person model.PersonID) (int, error)
	RecordIDsOf(ctx context.Context, person model.PersonID) ([]vol.ExpiredRecord, error)
	DeleteSignupsByPerson(ctx context.Context, person model.PersonID) (int, error)
	DeleteMembersByPerson(ctx context.Context, person model.PersonID) (int, error)
	DeleteUnlistedByPerson(ctx context.Context, person model.PersonID) (int, error)
	DeleteRecordsByPerson(ctx context.Context, person model.PersonID) (int, error)
	AnonymisePerson(ctx context.Context, person model.PersonID) (vol.Anonymised, error)
}

// orEmpty returns s, or an empty non-nil slice for nil: a list wrapper encodes {"items":[]},
// never {} (F156).
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// wrap adds an operation name to err and keeps its kind.
func wrap(op string, err error) error { return fmt.Errorf("%s: %w", op, err) }

// begin starts a span named name and returns the ctx that carries it.
func (s *Service) begin(ctx context.Context, name string) (context.Context, telemetry.Span) {
	return s.tracer.Start(ctx, name)
}

// endSpan records *err on span (when set) and ends it. Use with defer and a named result.
func endSpan(span telemetry.Span, err *error) {
	if *err != nil {
		span.RecordError(*err)
	}
	span.End()
}

// actorOf is the first step of every signed-in use case: the signed-in person, or
// errs.Unauthenticated.
func (s *Service) actorOf(ctx context.Context, op string) (model.PersonID, error) {
	p, ok := s.actor(ctx)
	if !ok || p.IsZero() {
		return model.PersonID{}, errs.E(op, errs.Unauthenticated, errNoActor)
	}
	return p, nil
}

// now is the service clock's time, in UTC and truncated to what PostgreSQL stores.
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

// write runs fn in one Write transaction. A read-only copy fails at once with
// errs.Unavailable wrapping db.ErrReadOnly.
func (s *Service) write(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.db.Transact(ctx, db.Write, fn) //nolint:wrapcheck // callers wrap with their operation
}

// record writes audit entries on ctx; inside a Write transaction they commit with it.
func (s *Service) record(ctx context.Context, entries ...audit.Entry) error {
	if err := s.audit.Record(ctx, entries...); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// ref builds the audit reference of a record of the module.
func ref[T ~[16]byte](kind string, id T) audit.Ref { return audit.RefOf(kind, id) }

// The audit record kinds.
const (
	kindQualification = "volunteers.qualification"
	kindRecord        = RecordKind
	kindPosition      = "volunteers.position"
	kindMember        = "volunteers.member"
	kindShift         = "volunteers.shift"
	kindSignup        = "volunteers.signup"
	kindListing       = "volunteers.listing"
)

// act builds an audit.Entry of a change.
func act(action string, r audit.Ref, subject model.PersonID, reason string) audit.Entry {
	return audit.Entry{Action: audit.Action(action), Outcome: audit.Succeeded, Record: r, Subject: subject, Reason: reason}
}

// publish publishes a signal on ctx (inside the change's transaction).
func (s *Service) publish(ctx context.Context, name signals.Name, entries ...signals.Entry) error {
	if err := s.bus.Publish(ctx, name, signals.NewPayload(entries...)); err != nil {
		return fmt.Errorf("publish %s: %w", name, err)
	}
	return nil
}

// send queues a notification inside the caller's transaction. "E-mail is not set up" is not
// the caller's problem: it is logged once at debug level and swallowed; any other error is
// returned.
func (s *Service) send(ctx context.Context, m notify.Message) error {
	err := s.notify.Send(ctx, m)
	if errors.Is(err, notify.ErrNotConfigured) {
		s.mailOff.Do(func() { s.logger.DebugContext(ctx, "notifications are off: e-mail is not set up") })
		return nil
	}
	if err != nil {
		return fmt.Errorf("send %s: %w", m.Template, err)
	}
	return nil
}

// ---- privacy ----

// view is who is looking (privacy.Viewer) and the regional's policy, once per use case.
type view struct {
	viewer privacy.Viewer
	pol    privacy.Policy
}

// plainView is the view for response types with no grouped field: Filter needs no policy
// and no extents for them, so no authz or settings call is made.
func plainView(actor model.PersonID) view {
	return view{viewer: privacy.Viewer{Person: actor}}
}

// fullView is the view for a use case that returns a type with a grouped field
// (ShiftSummary.Mates, RosterEntry.LegalName): the viewer's extents per group and the
// regional's current audiences, read now and never cached.
func (s *Service) fullView(ctx context.Context, actor model.PersonID) (view, error) {
	viewer, err := privacy.ViewerOf(ctx, s.az, actor, vol.Groups)
	if err != nil {
		return view{}, fmt.Errorf("viewer: %w", err)
	}
	pol, err := privacy.PolicyOf(ctx, s.settings, vol.Groups)
	if err != nil {
		return view{}, fmt.Errorf("policy: %w", err)
	}
	return view{viewer: viewer, pol: pol}, nil
}

// policyView is the viewer without extents and the current policy: for a use case that
// returns a grouped type whose grouped field it never fills (ListShifts).
func (s *Service) policyView(ctx context.Context, actor model.PersonID) (view, error) {
	pol, err := privacy.PolicyOf(ctx, s.settings, vol.Groups)
	if err != nil {
		return view{}, fmt.Errorf("policy: %w", err)
	}
	return view{viewer: privacy.Viewer{Person: actor}, pol: pol}, nil
}

// filter passes *v through privacy.Filter for this view and returns what to audit.
func (v view) filter(x any, rel privacy.Relation) (privacy.Result, error) {
	res, err := privacy.Filter(x, v.pol, v.viewer, rel)
	if err != nil {
		return privacy.Result{}, fmt.Errorf("filter: %w", err)
	}
	return res, nil
}

// filterAll filters each element of items in place with the relation rel(item).
func filterAll[T any](v view, items []T, rel func(*T) privacy.Relation) error {
	for i := range items {
		if _, err := v.filter(&items[i], rel(&items[i])); err != nil {
			return err
		}
	}
	return nil
}

// noRel is the relation of a record with no subject or department.
func noRel[T any](*T) privacy.Relation { return privacy.Relation{} }

// ---- authorization helpers ----

// allowIn checks p at department d for the actor in ctx.
func (s *Service) allowIn(ctx context.Context, p authz.Permission, d model.DepartmentID) error {
	if err := s.az.AllowIn(ctx, p, d); err != nil {
		return fmt.Errorf("%s at department: %w", p, err)
	}
	return nil
}

// extent returns where the actor holds p.
func (s *Service) extent(ctx context.Context, p authz.Permission) (authz.Extent, error) {
	e, err := s.az.ExtentOf(ctx, p)
	if err != nil {
		return authz.Extent{}, fmt.Errorf("%s extent: %w", p, err)
	}
	return e, nil
}

// requireActive fails errs.Conflict (archived) when the department exists but is archived,
// and errs.NotFound when it does not exist.
func (s *Service) requireActive(ctx context.Context, d model.DepartmentID) error {
	ok, err := s.depts.Active(ctx, d)
	if err != nil {
		return fmt.Errorf("department: %w", err)
	}
	if ok {
		return nil
	}
	names, err := s.depts.NamesOf(ctx, []model.DepartmentID{d})
	if err != nil {
		return fmt.Errorf("department: %w", err)
	}
	if _, known := names[d]; !known {
		return errs.E("volunteers.requireActive", errs.NotFound, errors.New("no such department"))
	}
	return conflict("volunteers.requireActive", "department", vol.ConflictArchived, "the department is archived", nil)
}

// invalid is errs.Invalid carrying one validate.FieldError.
func invalid(op, field, code, reason string) error {
	return fieldProblem(op, field, code, reason, nil)
}

// permission is the authz permission type, for helpers that take one.
type permission = authz.Permission

// conflict is a coded errs.Conflict (vol.ConflictProblem).
func conflict(op, field, code, reason string, params map[string]string) error {
	return vol.ConflictProblem(op, field, code, reason, params) //nolint:wrapcheck // built with errs.E
}

// fieldProblem is a coded errs.Invalid (vol.FieldProblem).
func fieldProblem(op, field, code, reason string, params map[string]string) error {
	return vol.FieldProblem(op, field, code, reason, params) //nolint:wrapcheck // built with errs.E
}
