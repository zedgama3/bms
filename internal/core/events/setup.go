package events

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

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

// auditTimeout bounds the events.setup.ran write. The write runs on a context detached from
// the request's (context.WithoutCancel), so a request deadline that expired while the
// providers ran does not lose the entry; this deadline replaces it (D38 y).
const auditTimeout = 5 * time.Second

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
	// The order is actor, input, permission (D38 ac): a malformed call is refused as
	// malformed whatever the caller may do, and no store is reached for it.
	if _, err := s.actor(ctx, op); err != nil {
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
	if err := s.require(ctx, op); err != nil {
		return nil, err
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
// with an error: the error is a done ctx before or after the providers ran, or the audit
// write failing (F64). The entry is recorded whatever the state of ctx (auditTimeout) once
// at least one provider has run; providers a done ctx kept from running are reported as
// failed with code "timeout" or "canceled". With no provider run, nothing is recorded.
func (s *Service) setUp(ctx context.Context, event, from model.EventID) ([]SetupResult, error) {
	const op = "events.Service.setUp"
	provs := s.providers()
	results := make([]SetupResult, 0, len(provs))
	for i, p := range provs {
		if err := ctx.Err(); err != nil {
			if i == 0 {
				// No provider ran, so nothing was done and nothing is recorded.
				return results, errs.E(op, errs.KindOf(err), err)
			}
			// The request ran out part way. The providers that ran may have written, so the
			// entry below still records them; the ones that never ran are reported as failed
			// with the kind of the ctx error ("timeout" or "canceled"), so the admin sees
			// the set-up is incomplete and can run it again. They are not counted as
			// bms.events.setup outcomes: they did not run.
			code := errs.KindOf(err).String()
			for _, rest := range provs[i:] {
				results = append(results, SetupResult{Module: rest.Module, Failed: true, Code: code})
			}
			break
		}
		results = append(results, s.callProvider(ctx, p, eventsetup.Request{Event: event, CopyFrom: from}))
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
	// The providers have run and may have written, so the entry is recorded even when the
	// request's context is done, on a detached context with its own short deadline.
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditTimeout)
	defer cancel()
	err := s.inTx(actx, db.Write, func(ctx context.Context) error { return s.record(ctx, entry) })
	if err != nil {
		return results, wrap(op, err)
	}
	if err := ctx.Err(); err != nil {
		return results, errs.E(op, errs.KindOf(err), err)
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
		out      eventsetup.Result
		err      error
		panicked bool
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				err = errs.E(op, errs.Internal, fmt.Errorf("the provider panicked (%T)", r))
				s.logger.ErrorContext(ctx, "an event set-up provider panicked",
					slog.String("module", p.Module), slog.String("event_id", req.Event.String()),
					slog.String("stack", string(debug.Stack())))
			}
		}()
		out, err = p.SetUp(pctx, req)
	}()

	// The provider's own answer decides (D38 aa): one that succeeds after its deadline is a
	// success with its items. A Timeout of any origin counts as outcome "timeout" (D38 z).
	res := SetupResult{Module: p.Module}
	outcome := outcomeOK
	if err != nil {
		kind := errs.KindOf(err)
		res.Failed, res.Code, outcome = true, kind.String(), outcomeFailed
		switch kind { //nolint:exhaustive // the other kinds are "failed"
		case errs.Permission:
			outcome = outcomePermission
		case errs.Timeout:
			outcome = outcomeTimeout
		}
		span.RecordError(err)
		if !panicked { // a panic was logged, with its stack, where it was recovered
			s.logger.WarnContext(ctx, "an event set-up provider failed",
				slog.String("module", p.Module), slog.String("event_id", req.Event.String()),
				slog.String("kind", res.Code))
		}
	} else {
		res.Items = max(out.Items, 0)
	}
	span.SetAttrs(telemetry.Token("outcome", outcome))
	s.setups.Add(ctx, 1, telemetry.Token("module", p.Module), telemetry.Token("outcome", outcome))
	return res
}
