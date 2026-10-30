package volunteers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
	"log/slog"
)

// privileged is where the actor holds shifts.manage or rosters.read: inside it a shift is
// visible whatever its state and its position's access.
func (s *Service) privileged(ctx context.Context) (siteWide bool, depts []model.DepartmentID, err error) {
	for _, p := range []permission{vol.PermissionShiftsManage, vol.PermissionRostersRead} {
		ext, eerr := s.extent(ctx, p)
		if eerr != nil {
			return false, nil, eerr
		}
		siteWide = siteWide || ext.Site
		depts = append(depts, ext.Departments...)
	}
	slices.SortFunc(depts, cmpBytes[model.DepartmentID])
	return siteWide, slices.Compact(depts), nil
}

// eligEnv is what eligibility needs about the actor, loaded once per use case.
type eligEnv struct {
	now     time.Time
	members map[model.DepartmentID]bool
	active  map[model.DepartmentID]bool
	closed  map[model.EventID]bool
	reqs    map[vol.PositionID][]vol.QualificationID
	records []vol.Record
	windows []vol.BookedWindow
}

// loadEnv loads the actor's memberships, records and bookings and the state of the
// departments, events and positions the rows name.
func (s *Service) loadEnv(ctx context.Context, actor model.PersonID, rows []vol.ShiftRow) (*eligEnv, error) {
	env := &eligEnv{
		now: s.now(), members: map[model.DepartmentID]bool{}, active: map[model.DepartmentID]bool{},
		closed: map[model.EventID]bool{}, reqs: map[vol.PositionID][]vol.QualificationID{},
	}
	if len(rows) == 0 {
		return env, nil
	}
	mem, err := s.store.MembershipsOf(ctx, actor)
	if err != nil {
		return nil, fmt.Errorf("memberships: %w", err)
	}
	for _, m := range mem {
		env.members[m.Department] = true
	}
	var posIDs []vol.PositionID
	var evIDs []model.EventID
	lo, hi := rows[0].Starts, rows[0].Ends
	for _, r := range rows {
		if _, seen := env.active[r.Department]; !seen {
			ok, aerr := s.depts.Active(ctx, r.Department)
			if aerr != nil {
				return nil, fmt.Errorf("department: %w", aerr)
			}
			env.active[r.Department] = ok
		}
		posIDs = append(posIDs, r.Position)
		if !r.Event.IsZero() {
			evIDs = append(evIDs, r.Event)
		}
		if r.Starts.Before(lo) {
			lo = r.Starts
		}
		if r.Ends.After(hi) {
			hi = r.Ends
		}
	}
	slices.SortFunc(posIDs, cmpBytes[vol.PositionID])
	posIDs = slices.Compact(posIDs)
	for chunk := range slices.Chunk(posIDs, vol.MaxIDs) {
		rq, rerr := s.store.RequirementsOf(ctx, chunk)
		if rerr != nil {
			return nil, fmt.Errorf("requirements: %w", rerr)
		}
		for k, v := range rq {
			env.reqs[k] = v
		}
	}
	slices.SortFunc(evIDs, cmpBytes[model.EventID])
	evIDs = slices.Compact(evIDs)
	for chunk := range slices.Chunk(evIDs, vol.MaxIDs) {
		infos, ierr := s.events.InfoOf(ctx, chunk)
		if ierr != nil {
			return nil, fmt.Errorf("events: %w", ierr)
		}
		for id, info := range infos {
			env.closed[id] = info.Closed
		}
	}
	recs, err := s.store.RecordsOf(ctx, actor, true, nil)
	if err != nil {
		return nil, fmt.Errorf("records: %w", err)
	}
	for _, r := range recs {
		env.records = append(env.records, r.Record)
	}
	env.windows, err = s.store.BookedWindows(ctx, actor, model.TimeRange{Start: lo, End: hi})
	if err != nil {
		return nil, fmt.Errorf("bookings: %w", err)
	}
	return env, nil
}

// holds reports whether the actor has a valid record of q on date.
func (e *eligEnv) holds(q vol.QualificationID, date vol.Date) bool {
	return slices.ContainsFunc(e.records, func(r vol.Record) bool { return r.Qualification == q && r.ValidOn(date) })
}

// of computes the actor's eligibility for one shift (module-volunteers.md, Behavior).
func (e *eligEnv) of(row vol.ShiftRow) vol.Eligibility {
	if !row.Mine.IsZero() {
		return vol.Eligibility{} // already booked: the card shows "Booked"
	}
	var out vol.Eligibility
	date := vol.DateOf(row.Starts, row.Loc())
	for _, q := range e.reqs[row.Position] {
		if !e.holds(q, date) {
			out.Missing = append(out.Missing, q)
		}
	}
	switch {
	case row.Canceled():
		out.Reason = vol.ReasonCanceled
	case !row.Starts.After(e.now):
		out.Reason = vol.ReasonStarted
	case !e.active[row.Department]:
		out.Reason = vol.ReasonArchived
	case !row.Event.IsZero() && e.closed[row.Event]:
		out.Reason = vol.ReasonClosed
	case row.PositionAccess == vol.AccessMembers && !e.members[row.Department]:
		out.Reason = vol.ReasonNotMember
	case row.Booked >= row.Slots:
		out.Reason = vol.ReasonFull
	case slices.ContainsFunc(e.windows, func(w vol.BookedWindow) bool {
		return w.Shift != row.ID && row.Times().Overlaps(model.TimeRange{Start: w.Starts, End: w.Ends})
	}):
		out.Reason = vol.ReasonOverlap
	}
	out.Eligible = out.Reason == "" && len(out.Missing) == 0
	return out
}

// deptNames returns the names of the departments (distinct, in chunks the directory accepts);
// an unknown department is absent.
func (s *Service) deptNames(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error) {
	ids = slices.Clone(ids)
	slices.SortFunc(ids, cmpBytes[model.DepartmentID])
	ids = slices.Compact(ids)
	names := make(map[model.DepartmentID]string, len(ids))
	for chunk := range slices.Chunk(ids, vol.MaxIDs) {
		m, err := s.depts.NamesOf(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("department names: %w", err)
		}
		for k, n := range m {
			names[k] = n
		}
	}
	return names, nil
}

// ListShifts lists the shifts the actor may see, a page at a time (order starts, id; the
// cursor in q.After, the next one in ShiftList.Next), with Booked, Mine and Eligibility
// computed for the actor: open-access positions for everyone, members-only ones for members,
// and canceled or past ones only inside the actor's shifts.manage or rosters.read extent.
// DepartmentName comes from one Departments.NamesOf per page. A shift whose department is
// archived since the handlers ran (or unknown) is dropped from the page after the fetch, so the
// page may be short; Next comes from the fetched rows, so nothing is skipped. With EligibleOnly
// the store fetches up to vol.MaxList + 1 rows, the rows the actor is not eligible for (and does
// not hold a booking on) are dropped, the first Limit that remain are returned and Next is the
// cursor of the last row the call consumed (a page may be empty with Next set). Mates is never
// filled on a list. Fails errs.Unauthenticated, errs.Invalid (limit, after, within),
// errs.Permission (IncludeCanceled or a past window without such an extent), and store kinds.
func (s *Service) ListShifts(ctx context.Context, q vol.ShiftQuery) (_ vol.ShiftList, err error) {
	const op = "volunteers.ListShifts"
	ctx, span := s.begin(ctx, "volunteers.list-shifts")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.ShiftList{}, err
	}
	if err := q.Validate(); err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	afterStarts, afterID, err := vol.DecodeCursor[vol.ShiftID](q.After)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	now := s.now()
	privAll, privDepts, err := s.privileged(ctx)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	priv := func(d model.DepartmentID) bool { return privAll || slices.Contains(privDepts, d) }
	past := !q.Within.Start.IsZero() && q.Within.Start.Before(now)
	if (q.IncludeCanceled || past) && !privAll && len(privDepts) == 0 {
		return vol.ShiftList{}, errs.E(op, errs.Permission, errors.New("canceled and past shifts need shifts.manage or rosters.read"))
	}
	mem, err := s.store.MembershipsOf(ctx, actor)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	size := q.Size()
	// With EligibleOnly rows are dropped after the fetch by something the store cannot see, so
	// it fetches up to MaxList + 1 rows (one scan) instead of size + 1 (V4.3.4, D58).
	fetch := size + 1
	if q.EligibleOnly {
		fetch = vol.MaxList + 1
	}
	f := vol.ShiftFilter{
		Department: q.Department, Event: q.Event, Position: q.Position, WindowStart: q.Within.Start, WindowEnd: q.Within.End,
		IncludeCanceled: q.IncludeCanceled, PrivilegedAll: privAll, PrivilegedDepartments: privDepts, Actor: actor,
		AfterStarts: afterStarts, AfterID: afterID, Limit: fetch,
	}
	if f.WindowStart.IsZero() {
		f.WindowStart = now
	}
	for _, m := range mem {
		f.MemberDepartments = append(f.MemberDepartments, m.Department)
	}
	fetched, err := s.store.ListShifts(ctx, f)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	// Next comes from the rows as fetched, before anything is dropped below. The rows the call
	// consumes are the first size of them, or, with EligibleOnly, the first MaxList.
	consume := size
	if q.EligibleOnly {
		consume = vol.MaxList
	}
	var next vol.Cursor
	rows := slices.Clone(fetched)
	if len(rows) > consume {
		rows = rows[:consume]
		if !q.EligibleOnly {
			next = vol.NewCursor(rows[size-1].Starts, rows[size-1].ID)
		}
	}
	rows = slices.DeleteFunc(rows, func(r vol.ShiftRow) bool {
		return (r.Canceled() || !r.Ends.After(now)) && !priv(r.Department)
	})
	env, err := s.loadEnv(ctx, actor, rows)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	depts := make([]model.DepartmentID, len(rows))
	for i, r := range rows {
		depts[i] = r.Department
	}
	names, err := s.deptNames(ctx, depts)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	rows = slices.DeleteFunc(rows, func(r vol.ShiftRow) bool {
		_, known := names[r.Department]
		return !known || (!env.active[r.Department] && !priv(r.Department))
	})
	out := make([]vol.ShiftSummary, len(rows))
	for i, r := range rows {
		out[i] = summaryOf(r, env, names[r.Department])
	}
	if q.EligibleOnly {
		out, next = pageEligible(out, fetched, size)
	}
	v, err := s.policyView(ctx, actor)
	if err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	if err := filterAll(v, out, func(x *vol.ShiftSummary) privacy.Relation { return privacy.Relation{Department: x.Department} }); err != nil {
		return vol.ShiftList{}, wrap(op, err)
	}
	return vol.ShiftList{Items: orEmpty(out), Next: next}, nil
}

// pageEligible keeps the summaries the actor can take or already holds (Eligibility.Eligible or
// Mine), returns the first size of them and the Next cursor of the call: the cursor of the last
// row the call consumed (V4.3.4). out is the fetched rows that survived the visibility drops, in
// order; fetched is every row the store returned, up to vol.MaxList + 1. Next is the last
// returned row's cursor when any fetched row lies beyond it (kept or not), the last scanned
// row's (the MaxList-th) when fewer than size rows passed and a MaxList + 1-th row exists, and
// empty otherwise. Cannot fail.
func pageEligible(out []vol.ShiftSummary, fetched []vol.ShiftRow, size int) ([]vol.ShiftSummary, vol.Cursor) {
	kept := slices.DeleteFunc(out, func(x vol.ShiftSummary) bool {
		return !x.Eligibility.Eligible && x.Mine.IsZero()
	})
	if len(kept) >= size {
		last := kept[size-1]
		kept = kept[:size]
		// Any fetched row after the last returned one, kept or not, means more may follow.
		for i, r := range fetched {
			if r.ID == last.ID {
				if i < len(fetched)-1 {
					return kept, vol.NewCursor(last.Starts, last.ID)
				}
				break
			}
		}
		return kept, ""
	}
	if len(fetched) > vol.MaxList {
		scanned := fetched[vol.MaxList-1]
		return kept, vol.NewCursor(scanned.Starts, scanned.ID)
	}
	return kept, ""
}

// summaryOf builds the summary of a shift row for the viewer behind env.
func summaryOf(r vol.ShiftRow, env *eligEnv, deptName string) vol.ShiftSummary {
	return vol.ShiftSummary{
		Shift: r.Shift, DepartmentName: deptName, PositionName: r.PositionName,
		Booked: r.Booked, Mine: r.Mine, Eligibility: env.of(r),
	}
}

// shiftFor loads a shift for the actor and applies the visibility rule first (decision 20):
// an unknown shift and one the actor may not see are the same errs.NotFound, so a missing
// permission can only be reported on a shift the actor can see. Fails errs.NotFound and store
// kinds.
func (s *Service) shiftFor(ctx context.Context, op string, actor model.PersonID, id vol.ShiftID) (vol.ShiftRow, error) {
	row, err := s.store.GetShiftSummary(ctx, id, actor)
	if err != nil {
		return vol.ShiftRow{}, wrap(op, err)
	}
	vis, err := s.visible(ctx, actor, row)
	if err != nil {
		return vol.ShiftRow{}, wrap(op, err)
	}
	if !vis {
		return vol.ShiftRow{}, errs.E(op, errs.NotFound, errors.New("no such shift"))
	}
	return row, nil
}

// GetShift returns one shift with the same visibility rule as ListShifts (errs.NotFound
// otherwise), plus Mates (who else is booked, by playa name only) when the regional's
// visibility_shift_mates policy shows it to this viewer and the position does not hide it;
// absent otherwise, without a hint why. Not audited: everything shown is internal. Fails
// errs.Unauthenticated, errs.NotFound, People.NamesOf's kinds and store kinds.
func (s *Service) GetShift(ctx context.Context, id vol.ShiftID) (_ vol.ShiftSummary, err error) {
	const op = "volunteers.GetShift"
	ctx, span := s.begin(ctx, "volunteers.get-shift")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.ShiftSummary{}, err
	}
	row, err := s.shiftFor(ctx, op, actor, id)
	if err != nil {
		return vol.ShiftSummary{}, err
	}
	env, err := s.loadEnv(ctx, actor, []vol.ShiftRow{row})
	if err != nil {
		return vol.ShiftSummary{}, wrap(op, err)
	}
	names, err := s.deptNames(ctx, []model.DepartmentID{row.Department})
	if err != nil {
		return vol.ShiftSummary{}, wrap(op, err)
	}
	out := summaryOf(row, env, names[row.Department])
	v, err := s.fullView(ctx, actor)
	if err != nil {
		return vol.ShiftSummary{}, wrap(op, err)
	}
	rel := privacy.Relation{Department: row.Department}
	if !row.Mine.IsZero() {
		rel.Also = []privacy.Audience{vol.AudienceBooked}
	}
	if !row.HideShiftMates && v.pol.Shows(vol.GroupShiftMates, v.viewer, rel) {
		mates, merr := s.mates(ctx, id, actor)
		if merr != nil {
			return vol.ShiftSummary{}, wrap(op, merr)
		}
		out.Mates = &mates
	}
	if _, err := v.filter(&out, rel); err != nil {
		return vol.ShiftSummary{}, wrap(op, err)
	}
	return out, nil
}

// visible applies the visibility rule to one shift for the actor: inside the actor's
// shifts.manage or rosters.read extent a shift is visible whatever its state; elsewhere it is
// visible when not canceled, in an active department, and its position is open-access or the
// actor is a member of the department.
func (s *Service) visible(ctx context.Context, actor model.PersonID, row vol.ShiftRow) (bool, error) {
	privAll, privDepts, err := s.privileged(ctx)
	if err != nil {
		return false, err
	}
	if privAll || slices.Contains(privDepts, row.Department) {
		return true, nil
	}
	if row.Canceled() {
		return false, nil
	}
	active, err := s.depts.Active(ctx, row.Department)
	if err != nil {
		return false, fmt.Errorf("department: %w", err)
	}
	if !active {
		return false, nil
	}
	if row.PositionAccess == vol.AccessMembers {
		if _, merr := s.store.GetMember(ctx, row.Department, actor); merr != nil {
			if errs.Is(merr, errs.NotFound) {
				return false, nil
			}
			return false, fmt.Errorf("member: %w", merr)
		}
	}
	return true, nil
}

// mates loads who else is booked on a shift, by playa name only. A person with no playa
// name, no profile, or who chose not to be listed is counted in Unlisted and never named.
func (s *Service) mates(ctx context.Context, shift vol.ShiftID, actor model.PersonID) (vol.ShiftMates, error) {
	list, err := s.store.Mates(ctx, shift, actor)
	if err != nil {
		return vol.ShiftMates{}, fmt.Errorf("mates: %w", err)
	}
	var out vol.ShiftMates
	var listed []model.PersonID
	for _, m := range list {
		if m.Listed {
			listed = append(listed, m.Person)
		} else {
			out.Unlisted++
		}
	}
	if len(listed) == 0 {
		return out, nil
	}
	names, err := s.namesOf(ctx, listed)
	if err != nil {
		return vol.ShiftMates{}, err
	}
	for _, p := range listed {
		if n := strings.TrimSpace(names[p]); n != "" {
			out.Names = append(out.Names, n)
		} else {
			out.Unlisted++
		}
	}
	sort.Strings(out.Names)
	return out, nil
}

// shiftTarget resolves a ShiftInput against the department: the position (existing, of the
// department, active) and the event and zone (the event's zone when an event is set).
func (s *Service) shiftTarget(ctx context.Context, dept model.DepartmentID, in vol.ShiftInput, checkEvent bool) (zone string, err error) {
	pos, perr := s.store.GetPosition(ctx, in.Position)
	switch {
	case errs.Is(perr, errs.NotFound):
		return "", invalid("volunteers.shiftTarget", "position", vol.CodeUnknown, "no such position")
	case perr != nil:
		return "", fmt.Errorf("position: %w", perr)
	case pos.Department != dept:
		return "", invalid("volunteers.shiftTarget", "position", vol.CodeScope, "the position belongs to another department")
	case pos.Archived():
		return "", archivedErr("position")
	}
	if in.Event.IsZero() {
		return in.Zone, nil
	}
	infos, err := s.events.InfoOf(ctx, []model.EventID{in.Event})
	if err != nil {
		return "", fmt.Errorf("event: %w", err)
	}
	info, ok := infos[in.Event]
	switch {
	case !ok:
		return "", invalid("volunteers.shiftTarget", "event", vol.CodeUnknown, "no such event")
	case info.Closed && checkEvent:
		return "", conflict("volunteers.shiftTarget", "event", vol.ConflictClosed, "the event is closed", nil)
	}
	return info.Zone, nil
}

// CreateShift adds a shift to an active department. It needs shifts.manage at the
// department; the position must be active and the department's; an event must exist and not
// be closed, and its zone is used (standing work needs a valid IANA zone). Records
// shift.created. Fails errs.Unauthenticated, errs.Invalid, errs.Permission, errs.NotFound
// (unknown department), errs.Conflict (archived; closed), errs.Unavailable wrapping
// db.ErrReadOnly, Events.InfoOf's kinds and store kinds.
func (s *Service) CreateShift(ctx context.Context, dept model.DepartmentID, in vol.ShiftInput) (_ vol.Shift, err error) {
	const op = "volunteers.CreateShift"
	ctx, span := s.begin(ctx, "volunteers.create-shift")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Shift{}, err
	}
	if dept.IsZero() {
		return vol.Shift{}, invalid(op, "department", validate.CodeRequired, "must be set")
	}
	if err := in.Validate(); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	if err := s.allowIn(ctx, vol.PermissionShiftsManage, dept); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	if err := s.requireActive(ctx, dept); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	zone, err := s.shiftTarget(ctx, dept, in, true)
	if err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	now := s.now()
	sh := newShift(model.NewID[vol.ShiftID](), dept, in, zone, actor, now)
	if err := sh.Validate(); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	var saved vol.Shift
	err = s.write(ctx, func(ctx context.Context) error {
		var ierr error
		if saved, ierr = s.store.InsertShift(ctx, sh); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		return s.record(ctx, act("volunteers.shift.created", ref(kindShift, saved.ID), model.PersonID{}, ""))
	})
	if err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{Department: dept}); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	return saved, nil
}

// newShift builds a Shift from an input (times in UTC, text trimmed).
func newShift(id vol.ShiftID, dept model.DepartmentID, in vol.ShiftInput, zone string, by model.PersonID, now time.Time) vol.Shift {
	return vol.Shift{
		ID: id, Department: dept, Position: in.Position, Event: in.Event, Label: strings.TrimSpace(in.Label),
		Starts: in.Starts.UTC(), Ends: in.Ends.UTC(), Zone: zone, Slots: in.Slots,
		Location: strings.TrimSpace(in.Location), Notes: strings.TrimSpace(in.Notes), CreatedBy: by,
		CreatedAt: now, UpdatedAt: now,
	}
}

// UpdateShift changes a shift (the department never changes). A time change moves every
// booked person's reminder and refuses when a booked person would then overlap another
// booking (errs.Conflict overlap), or when Slots would fall below the booked count
// (errs.Conflict full). It needs shifts.manage at the shift's department. Records
// shift.changed. The visibility rule runs before the permission check (decision 20): an
// invisible shift is errs.NotFound, a visible one without shifts.manage there errs.Permission.
// The shift's event is immutable: an input whose Event differs fails errs.Invalid (scope on
// "event"). Fails errs.Unauthenticated, errs.Invalid, errs.Permission, errs.NotFound,
// errs.Conflict (canceled; full; overlap; archived; closed), errs.Unavailable wrapping
// db.ErrReadOnly (or when people kept signing up while it locked the roster: retry) and store
// kinds. The department must be active and a shift's event not closed, as for CreateShift.
func (s *Service) UpdateShift(ctx context.Context, id vol.ShiftID, in vol.ShiftInput) (_ vol.Shift, err error) {
	const op = "volunteers.UpdateShift"
	ctx, span := s.begin(ctx, "volunteers.update-shift")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Shift{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	cur, err := s.shiftFor(ctx, op, actor, id)
	if err != nil {
		return vol.Shift{}, err
	}
	if err := s.allowIn(ctx, vol.PermissionShiftsManage, cur.Department); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	if in.Event != cur.Event {
		return vol.Shift{}, wrap(op, invalid(op, "event", vol.CodeScope, "a shift's event cannot change: cancel it and create another, or copy it"))
	}
	if err := s.requireActive(ctx, cur.Department); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	var saved vol.Shift
	for attempt := 1; ; attempt++ {
		saved, err = s.updateShiftOnce(ctx, id, in)
		if !errors.Is(err, errRosterMoved) {
			break
		}
		if attempt >= updateShiftAttempts {
			return vol.Shift{}, errs.E(op, errs.Unavailable, errors.New("people kept signing up to the shift; try again"))
		}
	}
	if err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{Department: saved.Department}); err != nil {
		return vol.Shift{}, wrap(op, err)
	}
	return saved, nil
}

// updateShiftAttempts bounds how often UpdateShift starts over when someone books the shift
// between reading the roster and locking the shift.
const updateShiftAttempts = 3

// errRosterMoved ends an UpdateShift transaction that found a booked person it had not
// locked; UpdateShift then starts over with that person in the set.
var errRosterMoved = errors.New("the roster changed while the shift was being locked")

// updateShiftOnce is one Write transaction of UpdateShift. Lock order (decision 7): every
// person booked on the shift (ascending), then the shift. The people are read before the
// shift lock, so someone may book in between; after the shift lock (which settles who is
// booked) the roster is compared with the locked set and, when it holds anyone new, the
// transaction ends with errRosterMoved and nobody is locked after the shift.
func (s *Service) updateShiftOnce(ctx context.Context, id vol.ShiftID, in vol.ShiftInput) (vol.Shift, error) {
	const op = "volunteers.UpdateShift"
	var saved vol.Shift
	err := s.write(ctx, func(ctx context.Context) error {
		first, rerr := s.store.Roster(ctx, id)
		if rerr != nil {
			return fmt.Errorf("roster: %w", rerr)
		}
		lockedPeople := signupPeople(first)
		if lerr := s.store.LockPersonsBookings(ctx, lockedPeople); lerr != nil {
			return fmt.Errorf("lock people: %w", lerr)
		}
		locked, gerr := s.store.GetShiftForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Canceled() {
			return conflict(op, "shift", vol.ConflictCanceled, "the shift is canceled", nil)
		}
		roster, rerr := s.store.Roster(ctx, id)
		if rerr != nil {
			return fmt.Errorf("roster: %w", rerr)
		}
		for _, p := range signupPeople(roster) {
			if _, held := slices.BinarySearchFunc(lockedPeople, p, cmpBytes[model.PersonID]); !held {
				return errRosterMoved
			}
		}
		zone, terr := s.shiftTarget(ctx, locked.Department, in, true)
		if terr != nil {
			return terr
		}
		if in.Slots < len(roster) {
			return conflict(op, "slots", vol.ConflictFull, "more people are booked than the new number of slots", nil)
		}
		next := newShift(id, locked.Department, in, zone, locked.CreatedBy, s.now())
		next.CreatedAt, next.CopiedFrom, next.CanceledAt = locked.CreatedAt, locked.CopiedFrom, locked.CanceledAt
		moved := !next.Starts.Equal(locked.Starts) || !next.Ends.Equal(locked.Ends)
		if moved && len(roster) > 0 {
			if o, found, ferr := s.store.FindOverlapForShiftChange(ctx, id, next.Times()); ferr != nil {
				return fmt.Errorf("overlap: %w", ferr)
			} else if found {
				return overlapErr(op, o.Shift)
			}
		}
		var uerr error
		if saved, uerr = s.store.UpdateShift(ctx, next); uerr != nil {
			return fmt.Errorf("update: %w", uerr)
		}
		if moved {
			for _, su := range roster {
				if eerr := s.enqueueReminder(ctx, su.ID, saved.Starts); eerr != nil {
					return eerr
				}
			}
		}
		return s.record(ctx, act("volunteers.shift.changed", ref(kindShift, id), model.PersonID{}, ""))
	})
	return saved, err //nolint:wrapcheck // UpdateShift wraps with its operation
}

// overlapErr is errs.Conflict overlap {shift}.
func overlapErr(op string, other vol.ShiftID) error {
	return conflict(op, "shift", vol.ConflictOverlap, "it overlaps another booked shift", map[string]string{"shift": other.String()})
}

// signupPeople lists the people of sign-ups, deduplicated and in ascending order.
func signupPeople(sus []vol.Signup) []model.PersonID {
	out := make([]model.PersonID, len(sus))
	for i, su := range sus {
		out[i] = su.Person
	}
	slices.SortFunc(out, cmpBytes[model.PersonID])
	return slices.Compact(out)
}

// ReminderHorizon is how far ahead a reminder is parked at most: jobs cannot schedule more
// than jobs.MaxDelay ahead, and a day of slack keeps the request inside it whatever the clock
// skew between the enqueueing request and the runner (decision 25).
const ReminderHorizon = jobs.MaxDelay - 24*time.Hour

// reminderRequest is the jobs request that reminds the sign-up at due, parked at the horizon
// when due is further ahead than that. Key is the sign-up's own ID, so a changed start moves
// the queued reminder (jobs decision 3) and the reminder handler carries a parked one forward.
func (s *Service) reminderRequest(id vol.SignupID, due time.Time) jobs.Request {
	if horizon := s.now().Add(ReminderHorizon); due.After(horizon) {
		due = horizon
	}
	return jobs.Request{
		Name: JobShiftReminder, Key: id.String(), RunAt: due,
		Args: signals.NewPayload(signals.Ref(PayloadSignup, id)),
	}
}

// enqueueReminder (re)schedules the reminder of a sign-up for the shift start minus the
// reminder lead, at most ReminderHorizon ahead (decision 25). A lead of 0 or a reminder time
// already past schedules nothing.
func (s *Service) enqueueReminder(ctx context.Context, id vol.SignupID, starts time.Time) error {
	lead, err := s.settings.Duration(ctx, SettingReminderLead)
	if err != nil {
		return fmt.Errorf("reminder lead: %w", err)
	}
	due := starts.Add(-lead)
	if lead <= 0 || !due.After(s.now()) {
		return nil // no reminders, or already past
	}
	if _, err := s.jobs.Enqueue(ctx, s.reminderRequest(id, due)); err != nil {
		return fmt.Errorf("enqueue reminder: %w", err)
	}
	return nil
}

// CancelShift cancels the shift and every booked sign-up on it (reason shift_canceled),
// publishes volunteers.signup-canceled and sends volunteers.signup-removed for each. It
// needs shifts.manage at the shift's department, after the visibility rule (an invisible shift
// is errs.NotFound, a visible one without the permission errs.Permission). Records
// shift.canceled and signup.canceled. Fails errs.Unauthenticated, errs.Permission, errs.NotFound,
// errs.Conflict (canceled: a second call), errs.Unavailable wrapping db.ErrReadOnly and
// store kinds.
func (s *Service) CancelShift(ctx context.Context, id vol.ShiftID) (err error) {
	const op = "volunteers.CancelShift"
	ctx, span := s.begin(ctx, "volunteers.cancel-shift")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return err
	}
	cur, err := s.shiftFor(ctx, op, actor, id)
	if err != nil {
		return err
	}
	if err := s.allowIn(ctx, vol.PermissionShiftsManage, cur.Department); err != nil {
		return wrap(op, err)
	}
	err = s.write(ctx, func(ctx context.Context) error {
		locked, gerr := s.store.GetShiftForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if locked.Canceled() {
			return conflict(op, "shift", vol.ConflictCanceled, "the shift is already canceled", nil)
		}
		now := s.now()
		if _, cerr := s.store.CancelShift(ctx, id, vol.CancelShiftCanceled, now); cerr != nil {
			if errs.Is(cerr, errs.NotFound) {
				return conflict(op, "shift", vol.ConflictCanceled, "the shift is already canceled", nil)
			}
			return fmt.Errorf("cancel: %w", cerr)
		}
		canceled, cerr := s.store.CancelSignupsByShifts(ctx, []vol.ShiftID{id}, vol.CancelShiftCanceled, now)
		if cerr != nil {
			return fmt.Errorf("cancel bookings: %w", cerr)
		}
		return s.afterShiftCanceled(ctx, []vol.ShiftID{id}, canceled, vol.CancelShiftCanceled)
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// afterShiftCanceled is what canceling shifts does inside its transaction: one audit entry
// per shift and per sign-up, one signal and one notice per sign-up.
func (s *Service) afterShiftCanceled(ctx context.Context, shifts []vol.ShiftID, canceled []vol.CanceledSignup, reason vol.CancelReason) error {
	entries := make([]audit.Entry, 0, len(shifts)+len(canceled))
	for _, sh := range shifts {
		entries = append(entries, act("volunteers.shift.canceled", ref(kindShift, sh), model.PersonID{}, string(reason)))
	}
	for _, c := range canceled {
		entries = append(entries, act("volunteers.signup.canceled", ref(kindSignup, c.Signup), c.Person, string(reason)))
	}
	for chunk := range slices.Chunk(entries, 500) {
		if err := s.record(ctx, chunk...); err != nil {
			return err
		}
	}
	for _, c := range canceled {
		if err := s.publish(ctx, SignalSignupCanceled, canceledPayload(c)...); err != nil {
			return err
		}
		if err := s.send(ctx, notify.Message{
			Template: TemplateSignupRemoved, To: []model.PersonID{c.Person}, Key: "removed:" + c.Signup.String(),
			Vars: removedVars(c.Label, c.Starts, c.Zone, reason),
			Link: "/volunteers/me",
		}); err != nil {
			return err
		}
	}
	return nil
}

// removedVars is the Vars of volunteers.signup-removed: the shift, and the reason as a token
// plus a flag var named after it (is_lead, is_self, ...). The notice templates may not call
// functions such as eq, so the flag is what lets a template word each reason.
func removedVars(label string, starts time.Time, zone string, reason vol.CancelReason) map[string]string {
	return map[string]string{
		"label": label, "starts": formatInZone(starts, zone), "zone": zone, "reason": string(reason),
		"is_" + string(reason): "1",
	}
}

// formatInZone formats t in the named zone with English day and month names.
func formatInZone(t time.Time, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc).Format("Monday 2 January 2006, 15:04")
}

// CopyShifts copies the non-canceled shifts of one department from one event to another; a
// shift already copied from the same source is skipped, so it is idempotent, and one whose
// position is archived is skipped and counted in the log. It copies all of them, in batches.
// Times are copied as wall-clock times in the destination event's zone: each shift's civil
// start and end (in its own zone) move by the whole days between the two events' start dates
// and are re-anchored with time.Date in the destination zone, so "08:00-14:00" stays
// "08:00-14:00" across a DST change or a zone change; a shift that would then not end after
// it starts, or exceed vol.MaxShiftDuration, keeps its source duration. It needs shifts.manage
// at the department. Returns the number created.
// Records shift.copied for each. Fails errs.Unauthenticated, errs.Invalid (self, unknown),
// errs.Permission, errs.Conflict (archived; closed), errs.Unavailable wrapping
// db.ErrReadOnly and store kinds.
func (s *Service) CopyShifts(ctx context.Context, dept model.DepartmentID, from, to model.EventID) (_ int, err error) {
	const op = "volunteers.CopyShifts"
	ctx, span := s.begin(ctx, "volunteers.copy-shifts")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return 0, err
	}
	n, err := s.copyShifts(ctx, actor, dept, from, to, "")
	if err != nil {
		return 0, wrap(op, err)
	}
	return n, nil
}

// copyShifts is CopyShifts (and SetUp's per-department copy) with the audit Reason to record.
func (s *Service) copyShifts(ctx context.Context, actor model.PersonID, dept model.DepartmentID, from, to model.EventID, reason string) (int, error) {
	if dept.IsZero() {
		return 0, invalid("volunteers.copyShifts", "department", validate.CodeRequired, "must be set")
	}
	if from.IsZero() || to.IsZero() {
		return 0, invalid("volunteers.copyShifts", "from", validate.CodeRequired, "both events must be set")
	}
	if from == to {
		return 0, invalid("volunteers.copyShifts", "to", vol.CodeSelf, "copy to a different event")
	}
	if err := s.allowIn(ctx, vol.PermissionShiftsManage, dept); err != nil {
		return 0, err
	}
	infos, err := s.events.InfoOf(ctx, []model.EventID{from, to})
	if err != nil {
		return 0, fmt.Errorf("events: %w", err)
	}
	src, ok := infos[from]
	if !ok {
		return 0, invalid("volunteers.copyShifts", "from", vol.CodeUnknown, "no such event")
	}
	dst, ok := infos[to]
	if !ok {
		return 0, invalid("volunteers.copyShifts", "to", vol.CodeUnknown, "no such event")
	}
	if dst.Closed {
		return 0, conflict("volunteers.copyShifts", "to", vol.ConflictClosed, "the target event is closed", nil)
	}
	if err := s.requireActive(ctx, dept); err != nil {
		return 0, err
	}
	dstLoc, lerr := time.LoadLocation(dst.Zone)
	if lerr != nil {
		return 0, invalid("volunteers.copyShifts", "to", vol.CodeUnknown, "the target event has no usable zone")
	}
	days := civilDays(src.Dates.Start, src.Zone, dst.Dates.Start, dst.Zone)
	created, skipped := 0, 0
	err = s.write(ctx, func(ctx context.Context) error {
		// Batches of vol.MaxList in (starts, id) order, each starting after the last shift of
		// the one before, so shifts skipped for an archived position are not listed again.
		var after vol.ShiftID
		var afterStarts time.Time
		for {
			cands, cerr := s.store.ShiftsToCopy(ctx, dept, from, to, afterStarts, after)
			if cerr != nil {
				return fmt.Errorf("candidates: %w", cerr)
			}
			var entries []audit.Entry
			for _, c := range cands {
				if c.PositionArchived {
					skipped++
					continue
				}
				cp := c.Shift
				cp.ID, cp.Event, cp.Zone = model.NewID[vol.ShiftID](), to, dst.Zone
				cp.Starts, cp.Ends = reanchor(c.Shift, days, dstLoc)
				cp.CopiedFrom, cp.CreatedBy = c.ID, actor
				cp.CanceledAt, cp.CancelReason = time.Time{}, ""
				cp.CreatedAt, cp.UpdatedAt = s.now(), s.now()
				saved, made, ierr := s.store.InsertShiftCopy(ctx, cp)
				if ierr != nil {
					return fmt.Errorf("insert copy: %w", ierr)
				}
				if made {
					created++
					entries = append(entries, act("volunteers.shift.copied", ref(kindShift, saved.ID), model.PersonID{}, reason))
				}
			}
			for chunk := range slices.Chunk(entries, 500) {
				if rerr := s.record(ctx, chunk...); rerr != nil {
					return rerr
				}
			}
			if len(cands) < vol.MaxList {
				return nil
			}
			last := cands[len(cands)-1]
			after, afterStarts = last.ID, last.Starts
		}
	})
	if err != nil {
		return 0, err
	}
	if skipped > 0 {
		s.logger.InfoContext(ctx, "shifts with an archived position were not copied", slog.Int("skipped", skipped))
	}
	return created, nil
}

// civilDays is the whole days from the civil date of a (seen in zone aZone) to that of b (seen
// in bZone). An unknown zone counts as UTC.
func civilDays(a time.Time, aZone string, b time.Time, bZone string) int {
	ad, bd := vol.DateOf(a, zoneOrUTC(aZone)), vol.DateOf(b, zoneOrUTC(bZone))
	return int(bd.Time(time.UTC).Sub(ad.Time(time.UTC)) / (24 * time.Hour))
}

func zoneOrUTC(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// reanchor moves a shift by whole civil days and re-anchors its wall-clock start and end in
// loc (the destination event's zone). When the result would not end after it starts, or would
// be longer than vol.MaxShiftDuration (a shift inside a skipped hour), the end is the start
// plus the source duration.
func reanchor(sh vol.Shift, days int, loc *time.Location) (starts, ends time.Time) {
	from := sh.Loc()
	move := func(t time.Time) time.Time {
		y, mo, d := t.In(from).Date()
		h, mi, sec := t.In(from).Clock()
		return time.Date(y, mo, d+days, h, mi, sec, t.Nanosecond(), loc).UTC()
	}
	starts, ends = move(sh.Starts), move(sh.Ends)
	if !ends.After(starts) || ends.Sub(starts) > vol.MaxShiftDuration {
		ends = starts.Add(sh.Ends.Sub(sh.Starts))
	}
	return starts, ends
}
