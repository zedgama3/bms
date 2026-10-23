package volunteers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// SignUp books the actor into a shift. It needs shifts.signup; the shift must be visible to
// the actor (errs.NotFound otherwise), not canceled or started, in an active department and an
// open event; the actor must have a playa name (decision 26), be a member for a members-only
// position, hold every requirement valid on the shift's date, and the shift must have a free
// slot and not overlap another of the actor's bookings. It never waives a requirement (D42). It enqueues the reminder, publishes volunteers.signup-booked
// and records signup.created, all in one transaction serialised per person and per shift.
// Fails errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Conflict (canceled;
// started; archived; closed; unnamed; not_member; unqualified {missing}; full; overlap
// {shift}; duplicate), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) SignUp(ctx context.Context, shift vol.ShiftID) (_ vol.Signup, err error) {
	const op = "volunteers.SignUp"
	ctx, span := s.begin(ctx, "volunteers.sign-up")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Signup{}, err
	}
	if err := s.az.Allow(ctx, vol.PermissionSignUp); err != nil {
		return vol.Signup{}, fmt.Errorf("%s: %s: %w", op, vol.PermissionSignUp, err)
	}
	if _, err := s.shiftFor(ctx, op, actor, shift); err != nil {
		return vol.Signup{}, err
	}
	su, err := s.book(ctx, op, actor, shift, actor, false, false)
	if err != nil {
		return vol.Signup{}, err
	}
	return su, nil
}

// Assign books another person into a shift. It needs shifts.manage at the shift's
// department, after the visibility rule (an invisible shift is errs.NotFound), and is allowed
// until the shift ends (walk-ons). The rules are SignUp's, except that with in.Waive a
// missing requirement is recorded in Waived and WaivedBy and audited with Reason waived instead of
// refused (D42); a lead may assign and waive for themself like for anyone. Waive never
// overrides the playa-name precondition (Conflict unnamed). Fails as SignUp does (Conflict
// ended instead of started), plus errs.Invalid (zero person); an unknown person is
// errs.Conflict unnamed.
func (s *Service) Assign(ctx context.Context, shift vol.ShiftID, in vol.AssignInput) (_ vol.Signup, err error) {
	const op = "volunteers.Assign"
	ctx, span := s.begin(ctx, "volunteers.assign")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Signup{}, err
	}
	if err := in.Validate(); err != nil {
		return vol.Signup{}, wrap(op, err)
	}
	cur, err := s.shiftFor(ctx, op, actor, shift)
	if err != nil {
		return vol.Signup{}, err
	}
	if err := s.allowIn(ctx, vol.PermissionShiftsManage, cur.Department); err != nil {
		return vol.Signup{}, wrap(op, err)
	}
	su, err := s.book(ctx, op, actor, shift, in.Person, true, in.Waive)
	if err != nil {
		return vol.Signup{}, err
	}
	return su, nil
}

// book is SignUp and Assign: one Write transaction that locks the person, checks that they
// have a playa name (decision 26), then locks the shift, re-checks everything and inserts the
// sign-up.
func (s *Service) book(ctx context.Context, op string, actor model.PersonID, shift vol.ShiftID, person model.PersonID, assign, waive bool) (vol.Signup, error) {
	var saved vol.Signup
	err := s.write(ctx, func(ctx context.Context) error {
		if lerr := s.store.LockPersonBookings(ctx, person); lerr != nil {
			return fmt.Errorf("lock person: %w", lerr)
		}
		if nerr := s.requireName(ctx, op, person); nerr != nil {
			return nerr
		}
		sh, gerr := s.store.GetShiftForUpdate(ctx, shift)
		if gerr != nil {
			return fmt.Errorf("lock shift: %w", gerr)
		}
		missing, cerr := s.bookingChecks(ctx, person, sh, assign)
		if cerr != nil {
			return cerr
		}
		reason, now := string(vol.CancelSelf), s.now()
		su := vol.Signup{
			ID: model.NewID[vol.SignupID](), Shift: shift, Person: person, Status: vol.SignupBooked,
			BookedAs: vol.BookedSelf, CreatedAt: now, UpdatedAt: now,
		}
		if assign {
			su.BookedBy, su.BookedAs, reason = actor, vol.BookedLead, string(vol.BookedLead)
		}
		if len(missing) > 0 {
			if !assign || !waive {
				return unqualifiedErr(op, missing)
			}
			su.Waived, su.WaivedBy, reason = true, actor, "waived"
		}
		var ierr error
		if saved, ierr = s.store.InsertSignup(ctx, su); ierr != nil {
			return fmt.Errorf("insert: %w", ierr)
		}
		if rerr := s.record(ctx, audit.Entry{
			Action: "volunteers.signup.created", Outcome: audit.Succeeded, Subject: person, Record: ref(kindSignup, saved.ID),
			Fields: []string{"person", "shift"}, Level: model.SensitivityInternal, Reason: reason,
		}); rerr != nil {
			return rerr
		}
		if perr := s.publish(ctx, SignalSignupBooked,
			signals.Ref(PayloadSignup, saved.ID), signals.Ref(PayloadShift, shift),
			signals.Ref(PayloadPerson, person), signals.Ref(PayloadDepartment, sh.Department)); perr != nil {
			return perr
		}
		return s.enqueueReminder(ctx, saved.ID, sh.Starts)
	})
	if err != nil {
		return vol.Signup{}, wrap(op, err)
	}
	if _, err := plainView(actor).filter(&saved, privacy.Relation{Subject: person}); err != nil {
		return vol.Signup{}, wrap(op, err)
	}
	return saved, nil
}

// requireName fails errs.Conflict unnamed when person has no playa name (missing from the
// directory or ""), with or without a waiver: the roster, the shift-mate list and the radio
// go by that name. Uniqueness is people's, not checked here. Called with the person lock held
// and before the shift lock, so a refused booking holds nothing else. Fails with
// People.NamesOf's kinds.
func (s *Service) requireName(ctx context.Context, op string, person model.PersonID) error {
	names, err := s.people.NamesOf(ctx, []model.PersonID{person})
	if err != nil {
		return fmt.Errorf("names: %w", err)
	}
	if strings.TrimSpace(names[person]) == "" {
		return conflict(op, "person", vol.ConflictUnnamed, "the person has no playa name yet", nil)
	}
	return nil
}

// unqualifiedErr is errs.Conflict unqualified {missing}.
func unqualifiedErr(op string, missing []vol.QualificationID) error {
	ids := make([]string, len(missing))
	for i, m := range missing {
		ids[i] = m.String()
	}
	return conflict(op, "qualifications", vol.ConflictUnqualified, "a requirement is missing",
		map[string]string{"missing": strings.Join(ids, ",")})
}

// bookingChecks re-checks, under the locks, everything that can refuse a booking, in the
// order the spec lists, and returns the requirements the person lacks (a lead may waive
// them with Assign). The caller refuses the booking itself when they are not waived.
func (s *Service) bookingChecks(ctx context.Context, person model.PersonID, sh vol.Shift, assign bool) ([]vol.QualificationID, error) {
	const op = "volunteers.bookingChecks"
	now := s.now()
	switch {
	case sh.Canceled():
		return nil, conflict(op, "shift", vol.ConflictCanceled, "the shift is canceled", nil)
	case !assign && !sh.Starts.After(now):
		return nil, conflict(op, "shift", vol.ConflictStarted, "the shift has started", nil)
	case assign && !sh.Ends.After(now):
		return nil, conflict(op, "shift", vol.ConflictEnded, "the shift has ended", nil)
	}
	if err := s.requireActive(ctx, sh.Department); err != nil {
		if errs.Is(err, errs.NotFound) {
			return nil, errs.E(op, errs.NotFound, errors.New("no such shift"))
		}
		return nil, err
	}
	if !sh.Event.IsZero() {
		infos, err := s.events.InfoOf(ctx, []model.EventID{sh.Event})
		if err != nil {
			return nil, fmt.Errorf("event: %w", err)
		}
		if infos[sh.Event].Closed {
			return nil, conflict(op, "event", vol.ConflictClosed, "the event is closed", nil)
		}
	}
	pos, err := s.store.GetPosition(ctx, sh.Position)
	if err != nil {
		return nil, fmt.Errorf("position: %w", err)
	}
	if pos.Archived() {
		return nil, archivedErr("position")
	}
	if pos.Access == vol.AccessMembers {
		if _, merr := s.store.GetMember(ctx, sh.Department, person); merr != nil {
			if errs.Is(merr, errs.NotFound) {
				return nil, conflict(op, "person", vol.ConflictNotMember, "only members of the department may take this shift", nil)
			}
			return nil, fmt.Errorf("member: %w", merr)
		}
	}
	roster, err := s.store.Roster(ctx, sh.ID)
	if err != nil {
		return nil, fmt.Errorf("roster: %w", err)
	}
	if slices.ContainsFunc(roster, func(su vol.Signup) bool { return su.Person == person }) {
		return nil, conflict(op, "person", vol.ConflictDuplicate, "already booked on this shift", nil)
	}
	missing, err := s.missingFor(ctx, person, sh)
	if err != nil {
		return nil, err
	}
	if len(roster) >= sh.Slots {
		return nil, conflict(op, "shift", vol.ConflictFull, "the shift is full", nil)
	}
	if o, found, ferr := s.store.FindOverlap(ctx, person, sh.Times(), sh.ID); ferr != nil {
		return nil, fmt.Errorf("overlap: %w", ferr)
	} else if found {
		return nil, overlapErr(op, o.Shift)
	}
	return missing, nil
}

// missingFor returns the requirements of the shift's position that person has no valid
// record for on the shift's start date in its zone.
func (s *Service) missingFor(ctx context.Context, person model.PersonID, sh vol.Shift) ([]vol.QualificationID, error) {
	rq, err := s.store.RequirementsOf(ctx, []vol.PositionID{sh.Position})
	if err != nil {
		return nil, fmt.Errorf("requirements: %w", err)
	}
	reqs := rq[sh.Position]
	if len(reqs) == 0 {
		return nil, nil
	}
	held, err := s.store.ValidQualifications(ctx, []model.PersonID{person}, vol.DateOf(sh.Starts, sh.Loc()), reqs)
	if err != nil {
		return nil, fmt.Errorf("qualifications: %w", err)
	}
	have := map[vol.QualificationID]bool{}
	for _, h := range held {
		have[h.Qualification] = true
	}
	var missing []vol.QualificationID
	for _, q := range reqs {
		if !have[q] {
			missing = append(missing, q)
		}
	}
	return missing, nil
}

// CancelSignup cancels a booking. The person cancels their own before the shift starts and
// before the cancel_cutoff; a shifts.manage holder at the shift's department cancels any
// (reason lead) and the person is told. Publishes volunteers.signup-canceled and records
// signup.canceled. Fails errs.Unauthenticated, errs.NotFound (unknown, or another person's
// without the permission), errs.Conflict (status: already canceled; started; cutoff),
// errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) CancelSignup(ctx context.Context, id vol.SignupID) (err error) {
	const op = "volunteers.CancelSignup"
	ctx, span := s.begin(ctx, "volunteers.cancel-signup")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return err
	}
	cur, err := s.store.GetSignup(ctx, id)
	if err != nil {
		return wrap(op, err)
	}
	own := cur.Signup.Person == actor
	if !own {
		if aerr := s.allowIn(ctx, vol.PermissionShiftsManage, cur.Department); aerr != nil {
			if errs.Is(aerr, errs.Permission) {
				return errs.E(op, errs.NotFound, errors.New("no such sign-up"))
			}
			return wrap(op, aerr)
		}
	}
	err = s.write(ctx, func(ctx context.Context) error {
		d, gerr := s.store.GetSignupForUpdate(ctx, id)
		if gerr != nil {
			return fmt.Errorf("lock: %w", gerr)
		}
		if !d.Signup.Booked() {
			return conflict(op, "signup", vol.ConflictStatus, "the booking is already canceled", nil)
		}
		now := s.now()
		reason := vol.CancelLead
		if own {
			reason = vol.CancelSelf
			if !d.Starts.After(now) {
				return conflict(op, "shift", vol.ConflictStarted, "the shift has started", nil)
			}
			cutoff, cerr := s.settings.Duration(ctx, SettingCancelCutoff)
			if cerr != nil {
				return fmt.Errorf("cancel cutoff: %w", cerr)
			}
			if cutoff > 0 && now.After(d.Starts.Add(-cutoff)) {
				return conflict(op, "shift", vol.ConflictCutoff, "too late to cancel here: contact the lead", nil)
			}
		}
		if _, cerr := s.store.CancelSignup(ctx, id, actor, reason, now); cerr != nil {
			if errs.Is(cerr, errs.NotFound) {
				return conflict(op, "signup", vol.ConflictStatus, "the booking is already canceled", nil)
			}
			return fmt.Errorf("cancel: %w", cerr)
		}
		c := vol.CanceledSignup{
			Signup: id, Shift: d.Signup.Shift, Person: d.Signup.Person, Department: d.Department,
			Label: d.Label, Starts: d.Starts, Zone: d.Zone,
		}
		if rerr := s.record(ctx, act("volunteers.signup.canceled", ref(kindSignup, id), d.Signup.Person, string(reason))); rerr != nil {
			return rerr
		}
		if perr := s.publish(ctx, SignalSignupCanceled, canceledPayload(c)...); perr != nil {
			return perr
		}
		if own {
			return nil
		}
		return s.send(ctx, notify.Message{
			Template: TemplateSignupRemoved, To: []model.PersonID{d.Signup.Person}, Key: "removed:" + id.String(),
			Vars: removedVars(d.Label, d.Starts, d.Zone, reason),
			Link: "/volunteers/me",
		})
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}

// Roster returns the shift's booked people with person IDs, playa names, waivers and
// current eligibility. It needs rosters.read at the shift's department, after the visibility
// rule (an invisible shift is errs.NotFound, a visible one without the permission
// errs.Permission). A legal name is
// filled from People.LegalNamesOf only for entries where the regional's visibility_legal_name
// policy shows it to this viewer, each entry filtered with its own subject, and the read is
// audited as volunteers.roster.read per subject before returning (D43). People is asked only
// when the viewer holds people.contacts.read somewhere (a plain lead, on their own entry too,
// gets no legal name and people is not called). If people refuses (errs.Permission) every
// legal name is left out and the roster is still returned. Fails
// errs.Unauthenticated, errs.Permission, errs.NotFound, the audit's, People's and store
// kinds.
func (s *Service) Roster(ctx context.Context, shift vol.ShiftID) (_ vol.Roster, err error) {
	const op = "volunteers.Roster"
	ctx, span := s.begin(ctx, "volunteers.roster")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.Roster{}, err
	}
	row, err := s.shiftFor(ctx, op, actor, shift)
	if err != nil {
		return vol.Roster{}, err
	}
	sh := row.Shift
	if err := s.allowIn(ctx, vol.PermissionRostersRead, sh.Department); err != nil {
		return vol.Roster{}, wrap(op, err)
	}
	signups, err := s.store.Roster(ctx, shift)
	if err != nil {
		return vol.Roster{}, wrap(op, err)
	}
	people := make([]model.PersonID, len(signups))
	for i, su := range signups {
		people[i] = su.Person
	}
	names, err := s.namesOf(ctx, people)
	if err != nil {
		return vol.Roster{}, wrap(op, err)
	}
	missing := map[model.PersonID]bool{}
	if len(people) > 0 {
		if reqs, rerr := s.store.RequirementsOf(ctx, []vol.PositionID{sh.Position}); rerr != nil {
			return vol.Roster{}, wrap(op, rerr)
		} else if len(reqs[sh.Position]) > 0 {
			held, herr := s.store.ValidQualifications(ctx, people, vol.DateOf(sh.Starts, sh.Loc()), reqs[sh.Position])
			if herr != nil {
				return vol.Roster{}, wrap(op, herr)
			}
			have := map[model.PersonID]map[vol.QualificationID]bool{}
			for _, h := range held {
				if have[h.Person] == nil {
					have[h.Person] = map[vol.QualificationID]bool{}
				}
				have[h.Person][h.Qualification] = true
			}
			for _, p := range people {
				missing[p] = !holdsAll(have[p], reqs[sh.Position])
			}
		}
	}
	v, err := s.fullView(ctx, actor)
	if err != nil {
		return vol.Roster{}, wrap(op, err)
	}
	entries := make([]vol.RosterEntry, len(signups))
	for i, su := range signups {
		entries[i] = vol.RosterEntry{Signup: su, PlayaName: names[su.Person], Eligible: !missing[su.Person]}
	}
	rel := func(e *vol.RosterEntry) privacy.Relation {
		return privacy.Relation{Subject: e.Person, Department: sh.Department}
	}
	if err := s.fillLegalNames(ctx, v, entries, rel); err != nil {
		return vol.Roster{}, wrap(op, err)
	}
	var audits []audit.Entry
	for i := range entries {
		hadName := entries[i].LegalName != nil
		res, ferr := v.filter(&entries[i], rel(&entries[i]))
		if ferr != nil {
			return vol.Roster{}, wrap(op, ferr)
		}
		if hadName && entries[i].LegalName != nil && slices.Contains(res.Fields, "legal_name") {
			audits = append(audits, audit.Entry{
				Action: "volunteers.roster.read", Outcome: audit.Succeeded, Subject: entries[i].Person,
				Record: ref(kindShift, shift), Fields: []string{"legal_name"}, Level: model.SensitivityPersonal,
			})
		}
	}
	if len(audits) > 0 {
		if err := s.record(ctx, audits...); err != nil {
			return vol.Roster{}, wrap(op, err)
		}
	}
	out := vol.Roster{Shift: sh, Entries: entries}
	if _, err := v.filter(&out, privacy.Relation{Department: sh.Department}); err != nil {
		return vol.Roster{}, wrap(op, err)
	}
	return out, nil
}

// fillLegalNames asks people for the legal names of the roster entries the policy shows
// them to, and sets them. People is never asked when no entry shows one or the viewer does
// not hold people.contacts.read anywhere.
func (s *Service) fillLegalNames(ctx context.Context, v view, entries []vol.RosterEntry, rel func(*vol.RosterEntry) privacy.Relation) error {
	var want []model.PersonID
	for i := range entries {
		if v.pol.Shows(vol.GroupLegalName, v.viewer, rel(&entries[i])) {
			want = append(want, entries[i].Person)
		}
	}
	if len(want) == 0 {
		return nil
	}
	if ext, holds := v.viewer.Extents[vol.GroupLegalName]; !holds || ext.IsZero() {
		// People answers only a holder of people.contacts.read: without it nobody is asked
		// about, the viewer's own entry included (Q-VOL8).
		return nil
	}
	names := map[model.PersonID]string{}
	for chunk := range slices.Chunk(want, vol.MaxIDs) {
		m, err := s.people.LegalNamesOf(ctx, chunk)
		if errs.Is(err, errs.Permission) {
			s.legalNamesRefused.Do(func() {
				s.logger.WarnContext(ctx, "legal names were left off a roster: people refused the read")
			})
			return nil
		}
		if err != nil {
			return fmt.Errorf("legal names: %w", err)
		}
		for k, n := range m {
			names[k] = n
		}
	}
	for i := range entries {
		if n, ok := names[entries[i].Person]; ok && n != "" {
			name := n
			entries[i].LegalName = &name
		}
	}
	return nil
}

// MySchedule lists the actor's bookings, a page at a time, with shift, department and
// position names. Upcoming mode (the default): shifts that have not ended, soonest first,
// including bookings someone else canceled (reason lead, shift_canceled, department_archived,
// event_closed) until the shift ends, so a removal is seen even without e-mail; the person's
// own cancellations are not listed. Past mode (q.Past): booked sign-ups whose shift has ended,
// newest first. Own records: no permission. Fails errs.Unauthenticated, errs.Invalid (limit,
// after, within) and store kinds.
func (s *Service) MySchedule(ctx context.Context, q vol.ScheduleQuery) (_ vol.ScheduleList, err error) {
	const op = "volunteers.MySchedule"
	ctx, span := s.begin(ctx, "volunteers.my-schedule")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return vol.ScheduleList{}, err
	}
	if err := q.Validate(); err != nil {
		return vol.ScheduleList{}, wrap(op, err)
	}
	afterStarts, afterID, err := vol.DecodeCursor[vol.SignupID](q.After)
	if err != nil {
		return vol.ScheduleList{}, wrap(op, err)
	}
	size := q.Size()
	rows, err := s.store.Schedule(ctx, vol.ScheduleFilter{
		Person: actor, Now: s.now(), Past: q.Past, Within: q.Within,
		AfterStarts: afterStarts, AfterID: afterID, Limit: size + 1,
	})
	if err != nil {
		return vol.ScheduleList{}, wrap(op, err)
	}
	var next vol.Cursor
	if len(rows) > size {
		rows = rows[:size]
		next = vol.NewCursor(rows[size-1].Shift.Starts, rows[size-1].Signup.ID)
	}
	ids := make([]model.DepartmentID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Shift.Department)
	}
	names, err := s.deptNames(ctx, ids)
	if err != nil {
		return vol.ScheduleList{}, wrap(op, err)
	}
	out := make([]vol.ScheduleEntry, len(rows))
	for i, r := range rows {
		out[i] = vol.ScheduleEntry{Signup: r.Signup, Shift: r.Shift, Department: names[r.Shift.Department], Position: r.PositionName}
	}
	if err := filterAll(plainView(actor), out, func(e *vol.ScheduleEntry) privacy.Relation {
		return privacy.Relation{Subject: e.Person, Department: e.Shift.Department}
	}); err != nil {
		return vol.ScheduleList{}, wrap(op, err)
	}
	return vol.ScheduleList{Items: orEmpty(out), Next: next}, nil
}

// Listed reports whether the actor's playa name is shown to other volunteers on their
// shifts (true unless SetListed(false) was called). Own record. Fails errs.Unauthenticated
// and store kinds.
func (s *Service) Listed(ctx context.Context) (_ bool, err error) {
	const op = "volunteers.Listed"
	ctx, span := s.begin(ctx, "volunteers.listed")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return false, err
	}
	ok, err := s.store.Listed(ctx, actor)
	if err != nil {
		return false, wrap(op, err)
	}
	return ok, nil
}

// SetListed records the actor's choice; false hides them from every shift-mate list (they
// are counted in Unlisted) from the next read on. It never affects rosters, bookings or what
// the actor sees. It is idempotent and records listing.changed only when a row changed.
// Fails errs.Unauthenticated, errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) SetListed(ctx context.Context, listed bool) (err error) {
	const op = "volunteers.SetListed"
	ctx, span := s.begin(ctx, "volunteers.set-listed")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(ctx context.Context) error {
		var (
			changed bool
			serr    error
			reason  = "listed"
		)
		if listed {
			changed, serr = s.store.SetListedTrue(ctx, actor)
		} else {
			changed, serr = s.store.SetUnlisted(ctx, actor, s.now())
			reason = "unlisted"
		}
		if serr != nil {
			return fmt.Errorf("set: %w", serr)
		}
		if !changed {
			return nil
		}
		return s.record(ctx, act("volunteers.listing.changed", ref(kindListing, actor), actor, reason))
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}
