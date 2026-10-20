package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/jobs/internal/rules"
	"github.com/zedgama3/bms/internal/core/jobs/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// scoped is a Registrar bound to one owner.
type scoped struct {
	r     *Runner
	owner string
}

// Scoped returns the Registrar for owner (a module name or core area, the module-name
// grammar). A view: nothing to close. Fails errs.Invalid for a bad owner and errs.Internal
// after Close.
func (r *Runner) Scoped(owner string) (Registrar, error) {
	const op = "jobs.Runner.Scoped"
	if !ownerRE.MatchString(owner) {
		return nil, fieldError(op, "owner", codeMalformed, nil, "must be a module or core area name: 2 to 32 characters of a-z and 0-9, starting with a letter")
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, errs.E(op, errs.Internal, errors.New("the job runner is closed"))
	}
	return scoped{r: r, owner: owner}, nil
}

// Register implements Registrar.
func (s scoped) Register(d Decl) error {
	const op = "jobs.Registrar.Register"
	if err := d.Validate(); err != nil {
		return wrap(op, err)
	}
	if err := rules.Scope(op, s.owner, string(d.Name)); err != nil {
		return err //nolint:wrapcheck // rules.Scope names itself
	}
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	switch {
	case s.r.frozen:
		return errs.E(op, errs.Internal, errors.New("jobs are registered in Start, before Run begins"))
	case s.r.closed:
		return errs.E(op, errs.Internal, errors.New("the job runner is closed"))
	}
	if _, dup := s.r.entries[d.Name]; dup {
		return errs.E(op, errs.Conflict, fmt.Errorf("job %q is already registered", d.Name))
	}
	s.r.entries[d.Name] = &entry{decl: d}
	return nil
}

// Enqueue implements Registrar.
func (s scoped) Enqueue(ctx context.Context, req Request) (id JobID, err error) {
	const op = "jobs.Registrar.Enqueue"
	ctx, span := s.r.span(ctx, "enqueue", telemetry.WithKind(telemetry.SpanProducer))
	defer endSpan(span, &err)
	now := s.r.now()
	if err := req.validate(now); err != nil {
		return JobID{}, wrap(op, err)
	}
	if err := rules.Scope(op, s.owner, string(req.Name)); err != nil {
		return JobID{}, err //nolint:wrapcheck // rules.Scope names itself
	}
	span.SetAttrs(telemetry.Token("job", string(req.Name)))
	if _, ok := s.r.entryOf(req.Name); !ok {
		return JobID{}, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: "name", Code: codeUnknown, Reason: "no handler is registered for this job in this process",
		})
	}
	return s.r.enqueue(ctx, op, req, now)
}

// enqueue stores req (already validated and in scope) on the transaction ctx carries, or
// standalone.
func (r *Runner) enqueue(ctx context.Context, op string, req Request, now time.Time) (JobID, error) {
	if r.db.ReadOnly() {
		return JobID{}, readOnlyErr(op)
	}
	args, err := encodeArgs(req.Args)
	if err != nil {
		return JobID{}, wrap(op, err)
	}
	runAt := req.RunAt.UTC()
	if runAt.IsZero() || runAt.Before(now) {
		runAt = now
	}
	trace, _ := telemetry.TraceIDFrom(ctx)
	// A Write transaction joins the caller's, starts one when there is none (the insert and
	// the lookup of an existing key are then one unit) and refuses a Read transaction.
	var id JobID
	err = r.db.Transact(ctx, db.Write, func(ctx context.Context) error {
		var err error
		id, err = r.store.Enqueue(ctx, rows.New{
			ID: newJobID(), Name: string(req.Name), Key: req.Key, Args: args, RunAt: runAt, TraceID: trace,
		}, now)
		return err //nolint:wrapcheck // wrapped below
	})
	if err != nil {
		return JobID{}, wrap(op, err)
	}
	return id, nil
}

func (r *Runner) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// encodeArgs is the JSON form of Args: an object of identifiers, {} when empty.
func encodeArgs(a Args) ([]byte, error) {
	if len(a) == 0 {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, errs.E("jobs.encodeArgs", errs.Internal, err)
	}
	return b, nil
}

// decodeArgs reads the JSON form back. Fails errs.Invalid for stored arguments that are not
// an object of identifiers (a bug or a hand edit).
func decodeArgs(raw []byte) (Args, error) {
	var a Args
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, errs.E("jobs.decodeArgs", errs.Invalid, errors.New("stored arguments are not a set of record references"))
	}
	if len(a) == 0 {
		return nil, nil
	}
	return a, nil
}
