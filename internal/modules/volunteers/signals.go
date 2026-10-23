package volunteers

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// onDepartmentArchived hands the cancellation of the department's future shifts (reason
// department_archived) to the volunteers.cancel-shifts job and returns: a department can hold
// hundreds of shifts with thousands of bookings, so the handler stays quick and the work runs
// in batches (decision 23). It runs as the system and is idempotent: the job's Key is
// "department_archived:<department>", so a second delivery while the job waits is a no-op and
// a second run of the job finds nothing to do. Fails errs.Invalid when the payload lacks the
// department, and with Enqueue's kinds (signals logs it and jobs retries by kind).
func (s *Service) onDepartmentArchived(ctx context.Context, sig signals.Signal) error {
	const op = "volunteers.onDepartmentArchived"
	dept, err := signals.Get[model.DepartmentID](sig.Payload, departments.PayloadDepartment)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := s.enqueueCancel(ctx, string(vol.CancelDepartmentArchived), signals.Ref(departments.PayloadDepartment, dept), dept.String()); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// onEventClosed does the same for the future shifts of a closed event (reason event_closed,
// Key "event_closed:<event>").
func (s *Service) onEventClosed(ctx context.Context, sig signals.Signal) error {
	const op = "volunteers.onEventClosed"
	event, err := signals.Get[model.EventID](sig.Payload, events.PayloadEvent)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := s.enqueueCancel(ctx, string(vol.CancelEventClosed), signals.Ref(events.PayloadEvent, event), event.String()); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// enqueueCancel queues volunteers.cancel-shifts for a department or an event: the Key is
// "<reason>:<ID>" and the Args name the record, never a person.
func (s *Service) enqueueCancel(ctx context.Context, reason string, target signals.Entry, id string) error {
	if _, err := s.jobs.Enqueue(ctx, jobs.Request{
		Name: JobCancelShifts, Key: reason + ":" + id, Args: signals.NewPayload(target),
	}); err != nil {
		return fmt.Errorf("enqueue %s: %w", JobCancelShifts, err)
	}
	return nil
}

// cancelBookings cancels the booked sign-ups of shifts just canceled and does what a
// cancellation does (audit, signal, notice).
func (s *Service) cancelBookings(ctx context.Context, shifts []vol.ShiftID, reason vol.CancelReason) error {
	if len(shifts) == 0 {
		return nil
	}
	var all []vol.CanceledSignup
	for chunk := range chunked(shifts, vol.MaxIDs) {
		c, err := s.store.CancelSignupsByShifts(ctx, chunk, reason, s.now())
		if err != nil {
			return fmt.Errorf("cancel bookings: %w", err)
		}
		all = append(all, c...)
	}
	return s.afterShiftCanceled(ctx, shifts, all, reason)
}
