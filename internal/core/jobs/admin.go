package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// require fails errs.Unauthenticated without a signed-in person and errs.Permission unless
// the actor holds the site-wide permission p.
func (r *Runner) require(ctx context.Context, op string, p authz.Permission) error {
	a, ok := r.actorF(ctx)
	if !ok || (a.Person.IsZero() && !a.Superuser) {
		return errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	if err := r.authz.Allow(ctx, p); err != nil {
		return wrap(op, err)
	}
	return nil
}

func notFound(op, what string) error {
	return errs.E(op, errs.NotFound, errors.New("no such "+what))
}

// conflict is errs.Conflict carrying one field error.
func conflict(op, field, code, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: field, Code: code, Reason: reason})
}

// Status returns what is registered in this process with each name's last row and next due
// slot, the queue counts, and the Failed, Running, Retrying and Waiting lists, each at most
// MaxRecent rows in its own order, read in one snapshot. Works on a read-only copy. Needs
// jobs.read. Fails errs.Unauthenticated, errs.Permission and store kinds.
func (r *Runner) Status(ctx context.Context) (_ StatusView, err error) {
	const op = "jobs.Runner.Status"
	ctx, span := r.span(ctx, "status")
	defer endSpan(span, &err)
	if err := r.require(ctx, op, PermissionRead); err != nil {
		return StatusView{}, err
	}
	entries := r.snapshot()
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = string(e.decl.Name)
	}
	var (
		last   map[string]rows.Job
		counts map[string]int
		queue  rows.Queue
	)
	err = r.db.Transact(ctx, db.Read, func(ctx context.Context) error {
		var err error
		if last, err = r.store.LastPerName(ctx, names); err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if counts, err = r.store.CountByStatus(ctx); err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		queue, err = r.store.Queue(ctx, MaxRecent)
		return err //nolint:wrapcheck // wrapped below
	})
	if err != nil {
		return StatusView{}, wrap(op, err)
	}

	now := r.now()
	view := StatusView{
		ReadOnly: r.db.ReadOnly(), Instance: r.instance, Registered: make([]Registered, 0, len(entries)),
		Counts: make(map[Status]int, int(Canceled)),
	}
	for _, e := range entries {
		reg := r.registeredOf(e)
		if e.decl.Every > 0 {
			reg.NextDue = now.UTC().Truncate(e.decl.Every).Add(e.decl.Every)
		}
		if row, ok := last[string(e.decl.Name)]; ok {
			rec, err := recordOf(row)
			if err != nil {
				return StatusView{}, wrap(op, err)
			}
			reg.Last = &rec
		}
		view.Registered = append(view.Registered, reg)
	}
	for st := Queued; st <= Canceled; st++ {
		view.Counts[st] = counts[st.String()]
	}
	for _, l := range []struct {
		from []rows.Job
		to   *[]Record
	}{{queue.Failed, &view.Failed}, {queue.Running, &view.Running}, {queue.Retrying, &view.Retrying}, {queue.Waiting, &view.Waiting}} {
		if *l.to, err = recordsOf(l.from); err != nil {
			return StatusView{}, wrap(op, err)
		}
	}
	return view, nil
}

// RunNow enqueues a run of a scheduled name at once (key "admin:<job id>") and records
// jobs.job.run. Needs jobs.manage. Fails errs.Unauthenticated, errs.Permission,
// errs.Invalid for a malformed name or an on-demand one (field "name", code "on_demand"),
// errs.NotFound for a name not registered here, errs.Conflict (code "running") while a row of
// that name is Queued or Running, errs.Unavailable wrapping db.ErrReadOnly, the audit error
// and store kinds.
func (r *Runner) RunNow(ctx context.Context, name Name) (_ JobID, err error) {
	const op = "jobs.Runner.RunNow"
	ctx, span := r.span(ctx, "run_now")
	defer endSpan(span, &err)
	if err := r.require(ctx, op, PermissionManage); err != nil {
		return JobID{}, err
	}
	if err := name.Validate(); err != nil {
		return JobID{}, wrap(op, err)
	}
	e, ok := r.entryOf(name)
	if !ok {
		return JobID{}, notFound(op, "job")
	}
	if e.decl.Every == 0 {
		return JobID{}, fieldError(op, "name", codeOnDemand, nil, "this job runs on demand only")
	}
	if r.db.ReadOnly() {
		return JobID{}, readOnlyErr(op)
	}
	span.SetAttrs(telemetry.Token("job_name", string(name)))
	id := newJobID()
	now := r.now()
	trace, _ := telemetry.TraceIDFrom(ctx)
	err = r.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		ok, err := r.store.RunNow(ctx, id, string(name), now, trace)
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if !ok {
			return conflict(op, "name", codeRunning, "a run of this job is already waiting or running")
		}
		return r.audit.Record(ctx, audit.Entry{
			Action: actionRun, Outcome: audit.Succeeded, Record: audit.RefOf(kindJob, id), Reason: string(name),
		})
	})
	if err != nil {
		return JobID{}, wrap(op, err)
	}
	return id, nil
}

// Retry puts a Failed row back to Queued with run_at = now and attempts = 0, recording
// jobs.job.retried. A scheduled name (Every > 0) never has two open rows, so Retry of one
// is refused while another row of that name is Queued or Running; it checks under the name's
// advisory lock, in the same Write transaction as RunNow. Needs jobs.manage. Fails
// errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Conflict (code "status") for a
// row not Failed, errs.Conflict (code "running") for a scheduled name with a row open,
// errs.Unavailable wrapping db.ErrReadOnly, the audit error and store kinds.
func (r *Runner) Retry(ctx context.Context, id JobID) (err error) {
	const op = "jobs.Runner.Retry"
	ctx, span := r.span(ctx, "retry")
	defer endSpan(span, &err)
	return r.change(ctx, op, id, actionRetried, "only a failed job can be retried",
		func(ctx context.Context, now time.Time) (string, bool, error) {
			name, status, err := r.store.StatusOf(ctx, id)
			if err != nil {
				return "", false, err //nolint:wrapcheck // NotFound or a store kind; wrapped by change
			}
			// An unregistered name is not known to be scheduled; only a registered one with
			// an Every is held to one open row.
			e, registered := r.entryOf(Name(name))
			exclusive := registered && e.decl.Every > 0 && status == Failed.String()
			if exclusive {
				if err := r.store.LockName(ctx, name); err != nil {
					return "", false, err //nolint:wrapcheck // wrapped by change
				}
			}
			return r.store.Retry(ctx, id, now, exclusive)
		}, "a run of this job is already waiting or running")
}

// Cancel marks a Queued row Canceled, recording jobs.job.canceled. Needs jobs.manage. Fails
// errs.Unauthenticated, errs.Permission, errs.NotFound, errs.Conflict (code "status") for a
// row not Queued, errs.Unavailable wrapping db.ErrReadOnly, the audit error and store kinds.
func (r *Runner) Cancel(ctx context.Context, id JobID) (err error) {
	const op = "jobs.Runner.Cancel"
	ctx, span := r.span(ctx, "cancel")
	defer endSpan(span, &err)
	return r.change(ctx, op, id, actionCanceled, "only a waiting job can be canceled",
		func(ctx context.Context, now time.Time) (string, bool, error) { return r.store.Cancel(ctx, id, now) }, "")
}

// change is the shared shape of Retry and Cancel: check, change the row and audit it in one
// Write transaction; when nothing matched, say why (openRefusal, when set, is the reason for a
// row that is still Failed).
func (r *Runner) change(ctx context.Context, op string, id JobID, action audit.Action, refusal string,
	apply func(ctx context.Context, now time.Time) (string, bool, error), openRefusal string) error {
	if err := r.require(ctx, op, PermissionManage); err != nil {
		return err
	}
	if id.IsZero() {
		return notFound(op, "job")
	}
	if r.db.ReadOnly() {
		return readOnlyErr(op)
	}
	err := r.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		name, ok, err := apply(ctx, r.now())
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		if !ok {
			_, status, err := r.store.StatusOf(ctx, id)
			if err != nil {
				return err //nolint:wrapcheck // NotFound or a store kind; wrapped below
			}
			// A row still Failed after a Retry that matched nothing was refused by the
			// open-row test, not by its status.
			if openRefusal != "" && status == Failed.String() {
				return conflict(op, "name", codeRunning, openRefusal)
			}
			return conflict(op, "status", codeStatus, refusal)
		}
		return r.audit.Record(ctx, audit.Entry{
			Action: action, Outcome: audit.Succeeded, Record: audit.RefOf(kindJob, id), Reason: name,
		})
	})
	return wrap(op, err)
}

// recordsOf converts rows to records, never nil (the wire form is an array).
func recordsOf(js []rows.Job) ([]Record, error) {
	out := make([]Record, 0, len(js))
	for _, row := range js {
		rec, err := recordOf(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}
