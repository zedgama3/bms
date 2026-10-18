package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The outcomes bms.events.setup takes.
const (
	outcomeOK         = "ok"
	outcomeFailed     = "failed"
	outcomeTimeout    = "timeout"
	outcomePermission = "permission"
)

// RunSetup runs every linked provider again for the open event id, copying from the event
// from (providers are idempotent per event and module), and records events.setup.ran. It
// answers one SetupResult per provider, in link order; with no provider linked it answers
// an empty list and records nothing. Needs events.manage. Fails errs.Unauthenticated,
// errs.Permission, errs.NotFound (id), errs.Invalid (copy_from: required, self, unknown),
// errs.Conflict (closed), errs.Unavailable wrapping db.ErrReadOnly and store kinds. If the
// audit write fails after the providers ran, the results are returned with the error.
func (s *Service) RunSetup(ctx context.Context, id, from model.EventID) (_ []SetupResult, err error) {
	const op = "events.Service.RunSetup"
	ctx, span := s.start(ctx, "run-setup")
	defer endSpan(span, &err)
	if err := s.manage(ctx, op); err != nil {
		return nil, err
	}
	switch {
	case id.IsZero():
		return nil, notFound(op, "event")
	case from.IsZero():
		return nil, fieldInvalid(op, "copy_from", codeRequired, "must be set")
	case from == id:
		return nil, fieldInvalid(op, "copy_from", codeSelf, "must be a different event")
	}
	span.SetAttrs(telemetry.ID("event_id", id), telemetry.ID("copy_from", from))
	// The checks run in a write transaction so a read-only copy refuses before any provider
	// is called; the event's lock is held only for them.
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.openEvent(ctx, op, id); err != nil {
			return err
		}
		if _, err := s.store.GetEvent(ctx, from); err != nil {
			if errs.Is(err, errs.NotFound) {
				return fieldInvalid(op, "copy_from", codeUnknown, "no such event")
			}
			return wrap(op, err)
		}
		return nil
	})
	if err != nil {
		return nil, wrap(op, err)
	}
	if len(s.providers()) == 0 {
		return []SetupResult{}, nil
	}
	results, err := s.setUp(ctx, id, from)
	if err != nil {
		return results, wrap(op, err)
	}
	return results, nil
}

// setUp calls every provider in link order, one at a time, then records events.setup.ran
// in a transaction of its own. The event is already committed. It returns the results even
// with an error: the error is a done ctx or the audit write failing (F64).
func (s *Service) setUp(ctx context.Context, event, from model.EventID) ([]SetupResult, error) {
	const op = "events.Service.setUp"
	provs := s.providers()
	results := make([]SetupResult, 0, len(provs))
	for _, p := range provs {
		if err := ctx.Err(); err != nil {
			return results, errs.E(op, errs.KindOf(err), err)
		}
		results = append(results, s.callProvider(ctx, p, eventsetup.Request{Event: event, CopyFrom: from}))
	}
	if err := ctx.Err(); err != nil {
		return results, errs.E(op, errs.KindOf(err), err)
	}
	var failed []string
	for _, r := range results {
		if r.Failed {
			failed = append(failed, r.Module)
		}
	}
	entry := audit.Entry{
		Action: actionSetupRan, Outcome: audit.Succeeded, Record: audit.RefOf(kindEvent, event),
		Reason: reasonCopyFromHead + from.String(),
	}
	if len(failed) > 0 {
		entry.Outcome, entry.Fields, entry.Level = audit.Failed, failed, model.SensitivityInternal
	}
	err := s.inTx(ctx, db.Write, func(ctx context.Context) error { return s.record(ctx, entry) })
	if err != nil {
		return results, wrap(op, err)
	}
	return results, nil
}

// callProvider runs one provider under SetupTimeout and turns its answer, its failure, its
// timeout or its panic into a SetupResult. It counts bms.events.setup and logs a failure
// once, by module and kind only.
func (s *Service) callProvider(ctx context.Context, p SetupProvider, req eventsetup.Request) SetupResult {
	const op = "events.Service.callProvider"
	ctx, span := s.tracer.Start(ctx, "events.setup")
	defer span.End()
	span.SetAttrs(telemetry.Module(p.Module), telemetry.ID("event_id", req.Event))

	pctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var (
		out eventsetup.Result
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errs.E(op, errs.Internal, fmt.Errorf("the provider panicked (%T)", r))
				s.logger.ErrorContext(ctx, "an event set-up provider panicked",
					slog.String("module", p.Module), slog.String("event_id", req.Event.String()),
					slog.String("stack", string(debug.Stack())))
			}
		}()
		out, err = p.SetUp(pctx, req)
	}()

	timedOut := ctx.Err() == nil && errors.Is(pctx.Err(), context.DeadlineExceeded)
	res := SetupResult{Module: p.Module}
	outcome := outcomeOK
	switch {
	case timedOut:
		res.Failed, res.Code, outcome = true, errs.Timeout.String(), outcomeTimeout
	case err != nil:
		kind := errs.KindOf(err)
		res.Failed, res.Code, outcome = true, kind.String(), outcomeFailed
		if kind == errs.Permission {
			outcome = outcomePermission
		}
	default:
		res.Items = max(out.Items, 0)
	}
	if res.Failed {
		if err == nil {
			err = errs.E(op, errs.Timeout, errors.New("the provider took too long"))
		}
		span.RecordError(err)
		s.logger.WarnContext(ctx, "an event set-up provider failed",
			slog.String("module", p.Module), slog.String("event_id", req.Event.String()),
			slog.String("kind", res.Code))
	}
	span.SetAttrs(telemetry.Token("outcome", outcome))
	s.setups.Add(ctx, 1, telemetry.Token("module", p.Module), telemetry.Token("outcome", outcome))
	return res
}
