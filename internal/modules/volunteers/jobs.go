package volunteers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// purgeBatch is how many rows one purge statement removes.
const purgeBatch = 500

// runShiftReminder is the volunteers.shift-reminder handler. It re-reads the sign-up and
// sends the reminder only if it is still booked, the shift is not canceled and starts in the
// future, so a stale job sends nothing. When Starts minus the reminder lead is still ahead
// (the shift moved later, or the booking was parked at ReminderHorizon) it sends nothing and
// enqueues itself again under the same Key at the earlier of that time and the horizon. "E-mail is not set up" is logged once and the job
// succeeds; any other send error is returned, so jobs retries by kind. Fails errs.Invalid
// (no retry) when the arguments lack the sign-up.
func (s *Service) runShiftReminder(ctx context.Context, j jobs.Job) error {
	const op = "volunteers.runShiftReminder"
	id, err := signals.Get[vol.SignupID](j.Args, PayloadSignup)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	d, err := s.store.GetSignup(ctx, id)
	if errs.Is(err, errs.NotFound) {
		return nil // the person or the booking is gone
	}
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	lead, err := s.settings.Duration(ctx, SettingReminderLead)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	now := s.now()
	if lead <= 0 || !d.Signup.Booked() || !d.ShiftCanceledAt.IsZero() || !d.Starts.After(now) {
		return nil
	}
	if due := d.Starts.Add(-lead); due.After(now) {
		// The shift was moved later, or the booking was parked at the horizon: carry the
		// reminder forward under the same Key and send nothing yet (decision 25).
		if _, err := s.jobs.Enqueue(ctx, s.reminderRequest(id, due)); err != nil {
			return fmt.Errorf("%s: re-enqueue: %w", op, err)
		}
		return nil
	}
	names, err := s.depts.NamesOf(ctx, []model.DepartmentID{d.Department})
	if err != nil {
		return fmt.Errorf("%s: department: %w", op, err)
	}
	err = s.send(ctx, notify.Message{
		Template: TemplateShiftReminder, To: []model.PersonID{d.Signup.Person},
		Key: "reminder:" + id.String() + ":" + strconv.FormatInt(d.Starts.Unix(), 10),
		Vars: map[string]string{
			"label": d.Label, "department": names[d.Department], "position": d.PositionName,
			"starts": formatInZone(d.Starts, d.Zone), "zone": d.Zone, "location": d.Location,
		},
		Link: "/volunteers/shifts/" + d.Signup.Shift.String(),
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// runCancelShifts is the volunteers.cancel-shifts handler (on demand, queued by the two signal
// handlers): it cancels the future, non-canceled shifts of the department (or event) the Args
// name, and their booked sign-ups, in batches of vol.MaxList shifts, each batch its own Write
// transaction (audit, signal and notice per sign-up, reason department_archived or
// event_closed), until a batch returns no shift (a concurrent cancel can make a batch short
// while more remain). Idempotent on state: a retry resumes where it
// stopped and a second run finds nothing. Fails errs.Invalid (no retry) when the Args name
// neither a department nor an event, and with store, audit, signal and notify kinds.
func (s *Service) runCancelShifts(ctx context.Context, j jobs.Job) error {
	const op = "volunteers.runCancelShifts"
	var batch func(ctx context.Context) ([]vol.ShiftID, error)
	var reason vol.CancelReason
	if dept, err := signals.Get[model.DepartmentID](j.Args, departments.PayloadDepartment); err == nil {
		reason = vol.CancelDepartmentArchived
		batch = func(ctx context.Context) ([]vol.ShiftID, error) {
			return s.store.CancelShiftsByDepartment(ctx, dept, s.now(), vol.MaxList)
		}
	} else if event, eerr := signals.Get[model.EventID](j.Args, events.PayloadEvent); eerr == nil {
		reason = vol.CancelEventClosed
		batch = func(ctx context.Context) ([]vol.ShiftID, error) {
			return s.store.CancelShiftsByEvent(ctx, event, s.now(), vol.MaxList)
		}
	} else {
		return fmt.Errorf("%s: %w", op, err)
	}
	for {
		var n int
		err := s.write(ctx, func(ctx context.Context) error {
			ids, cerr := batch(ctx)
			if cerr != nil {
				return fmt.Errorf("cancel shifts: %w", cerr)
			}
			n = len(ids)
			return s.cancelBookings(ctx, ids, reason)
		})
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		// Stop only on an empty batch: under READ COMMITTED a row canceled concurrently
		// during the UPDATE is skipped, so a short batch does not mean nothing is left.
		if n == 0 {
			return nil
		}
	}
}

// runExpiryScan is the volunteers.expiry-scan handler (daily): for each verified record that
// expires within expiry_notice_days, is not yet notified and has no renewal, it sends
// volunteers.qualification-expiring and marks the record, each in its own transaction, so a
// failure loses one notice, not the batch. It continues after a failure and returns the first
// error after the batch. A record whose notice could not be sent because e-mail is off stays
// unmarked, so it is sent once e-mail is set up.
func (s *Service) runExpiryScan(ctx context.Context, _ jobs.Job) error {
	const op = "volunteers.runExpiryScan"
	days, err := s.settings.Int(ctx, SettingExpiryNoticeDays)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if days <= 0 {
		return nil
	}
	today := s.today()
	recs, err := s.store.ExpiringRecords(ctx, today, today.AddDays(int(days)), purgeBatch)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	var first error
	for _, r := range recs {
		if nerr := s.noticeExpiring(ctx, r, today); nerr != nil {
			first = errors.Join(first, nerr)
		}
	}
	if first != nil {
		return fmt.Errorf("%s: %w", op, first)
	}
	return nil
}

// noticeExpiring sends one expiry notice and marks the record, in one transaction.
func (s *Service) noticeExpiring(ctx context.Context, r vol.ExpiringRecord, today vol.Date) error {
	left := int(r.ExpiresOn.Time(time.UTC).Sub(today.Time(time.UTC)).Hours() / 24)
	return s.write(ctx, func(ctx context.Context) error {
		err := s.notify.Send(ctx, notify.Message{
			Template: TemplateQualificationExpiring, To: []model.PersonID{r.Person}, Key: "expiring:" + r.ID.String(),
			Vars: map[string]string{
				"qualification": r.QualificationName, "expires_on": r.ExpiresOn.String(), "days": strconv.Itoa(left),
			},
			Link: "/volunteers/me",
		})
		if errors.Is(err, notify.ErrNotConfigured) {
			s.mailOff.Do(func() { s.logger.DebugContext(ctx, "notifications are off: e-mail is not set up") })
			return nil
		}
		if err != nil {
			return fmt.Errorf("send: %w", err)
		}
		if merr := s.store.MarkExpiryNotified(ctx, r.ID, today); merr != nil {
			return fmt.Errorf("mark: %w", merr)
		}
		return nil
	})
}

// runPurge is the volunteers.purge handler (daily): with volunteers.retention set it deletes
// sign-ups whose shift ended before the cutoff, canceled shifts with no sign-up left and
// records expired before it (with their certificate files), in batches, and records
// purge.ran with the counts. Memberships and unlisted rows are never purged. Retention 0
// (forever) does nothing.
func (s *Service) runPurge(ctx context.Context, _ jobs.Job) error {
	const op = "volunteers.runPurge"
	keep, err := s.settings.Duration(ctx, SettingRetention)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if keep <= 0 {
		return nil
	}
	now := s.now()
	cutoff := now.Add(-keep)
	var signups, shifts, records int
	for {
		n, perr := s.purgeSignupsBatch(ctx, cutoff)
		if perr != nil {
			return fmt.Errorf("%s: sign-ups: %w", op, perr)
		}
		signups += n
		if n < purgeBatch {
			break
		}
	}
	for {
		var n int
		perr := s.write(ctx, func(ctx context.Context) error {
			var derr error
			n, derr = s.store.PurgeCanceledShifts(ctx, cutoff, purgeBatch)
			return derr //nolint:wrapcheck // wrapped below
		})
		if perr != nil {
			return fmt.Errorf("%s: shifts: %w", op, perr)
		}
		shifts += n
		if n < purgeBatch {
			break
		}
	}
	for {
		n, perr := s.purgeRecordsBatch(ctx, vol.DateOf(cutoff, time.UTC))
		if perr != nil {
			return fmt.Errorf("%s: records: %w", op, perr)
		}
		records += n
		if n < purgeBatch {
			break
		}
	}
	if signups+shifts+records == 0 {
		return nil
	}
	err = s.write(ctx, func(ctx context.Context) error {
		return s.record(ctx, audit.Entry{
			Action: "volunteers.purge.ran", Outcome: audit.Succeeded,
			Reason: fmt.Sprintf("signups=%d shifts=%d records=%d", signups, shifts, records),
		})
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Service) purgeSignupsBatch(ctx context.Context, cutoff time.Time) (int, error) {
	var n int
	err := s.write(ctx, func(ctx context.Context) error {
		var derr error
		n, derr = s.store.PurgeSignups(ctx, cutoff, purgeBatch)
		if derr != nil {
			return fmt.Errorf("purge: %w", derr)
		}
		return nil
	})
	return n, err //nolint:wrapcheck // wrapped by the caller
}

func (s *Service) purgeRecordsBatch(ctx context.Context, cutoff vol.Date) (int, error) {
	var n int
	err := s.write(ctx, func(ctx context.Context) error {
		recs, lerr := s.store.ExpiredRecords(ctx, cutoff, purgeBatch)
		if lerr != nil {
			return fmt.Errorf("list: %w", lerr)
		}
		ids := make([]vol.RecordID, len(recs))
		for i, r := range recs {
			ids[i] = r.ID
			if _, derr := s.files.DeleteByRecord(ctx, ref(kindRecord, r.ID)); derr != nil {
				return fmt.Errorf("delete certificate: %w", derr)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		var derr error
		if n, derr = s.store.DeleteRecords(ctx, ids); derr != nil {
			return fmt.Errorf("delete: %w", derr)
		}
		return nil
	})
	return n, err //nolint:wrapcheck // wrapped by the caller
}
