package datarights

import (
	"context"
	"errors"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/datarights/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ---- deletion requests ------------------------------------------------------------------

func requestOf(r rows.Request) Request {
	return Request{
		ID: r.ID, Person: r.Person, Kind: r.Kind, Status: statusOf(r.Status), Note: r.Note, CreatedAt: r.CreatedAt,
		DecidedAt: r.DecidedAt, DecidedBy: r.DecidedBy, DecisionNote: r.DecisionNote,
	}
}

func requestsOf(rs []rows.Request) []Request {
	out := make([]Request, len(rs))
	for i, r := range rs {
		out[i] = requestOf(r)
	}
	return out
}

// RequestDeletion files the actor's own request to be deleted, which a holder of
// people.data.delete fulfils or declines. At most one request is open at a time. Records
// datarights.request.created and publishes datarights.deletion-requested in the transaction, then
// e-mails the holders of people.data.delete that a request is waiting (Options.Notify and
// Options.Holders; nil, nobody to tell, or e-mail not configured means no notice and no
// failure). Fails errs.Unauthenticated, errs.Invalid (F41), errs.Conflict (field request,
// code duplicate: F40; nothing is sent), errs.NotFound (no profile row yet), the kind of a
// failed HoldersOf or Send other than Unsupported (the request is rolled back: F62) and
// store kinds.
func (s *Service) RequestDeletion(ctx context.Context, note string) (_ Request, err error) {
	const op = "datarights.Service.RequestDeletion"
	ctx, span := s.start(ctx, "request-deletion")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Request{}, err
	}
	span.SetAttrs(telemetry.ID("person_id", a.Person))
	note, err = checkNote(op, "note", note)
	if err != nil {
		return Request{}, err
	}
	id := model.NewID[RequestID]()
	var out rows.Request
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		// Serialise with a deletion of the person, then check the profile row is there (a
		// request names a person row; NotFound before the first profile save).
		if err := s.store.LockDeletion(ctx, a.Person); err != nil {
			return wrap(op, err)
		}
		if err := s.people.Exists(ctx, a.Person); err != nil {
			return wrap(op, err)
		}
		var err error
		out, err = s.store.InsertRequest(ctx, id, a.Person, note, s.now())
		if err != nil {
			if errs.Is(err, errs.Conflict) {
				return fieldConflict(op, "request", codeDuplicate, nil, "you already have an open request")
			}
			return wrap(op, err)
		}
		if err := s.record(ctx, audit.Entry{
			Action: actionRequestCreated, Outcome: audit.Succeeded, Subject: a.Person, Record: audit.RefOf(kindRequest, id),
		}); err != nil {
			return err
		}
		if err := s.publish(ctx, SignalDeletionRequested, requestRef(id), personRef(a.Person)); err != nil {
			return err
		}
		return s.notifyDeletionRequested(ctx, op, id)
	})
	if err != nil {
		return Request{}, wrap(op, err)
	}
	return requestOf(out), nil
}

// MyRequests returns the actor's own deletion requests, newest first. Fails
// errs.Unauthenticated and store kinds.
func (s *Service) MyRequests(ctx context.Context) (_ []Request, err error) {
	const op = "datarights.Service.MyRequests"
	ctx, span := s.start(ctx, "my-requests")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return nil, err
	}
	rs, err := s.store.MyRequests(ctx, a.Person, maxRequestsListed)
	if err != nil {
		return nil, wrap(op, err)
	}
	return requestsOf(rs), nil
}

// WithdrawRequest withdraws the actor's own open request. Records datarights.request.withdrawn.
// Fails errs.NotFound (not theirs: F42), errs.Conflict (status: not open) and store kinds.
func (s *Service) WithdrawRequest(ctx context.Context, id RequestID) (err error) {
	const op = "datarights.Service.WithdrawRequest"
	ctx, span := s.start(ctx, "withdraw-request")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return err
	}
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		_, decided, err := s.store.WithdrawRequest(ctx, id, a.Person, s.now())
		if err != nil {
			return wrap(op, err)
		}
		if !decided {
			cur, err := s.store.GetRequest(ctx, id)
			if err != nil {
				return wrap(op, err)
			}
			if cur.Person != a.Person {
				return notFoundRequest(op)
			}
			return fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
		return s.record(ctx, audit.Entry{
			Action: actionRequestWithdraw, Outcome: audit.Succeeded, Subject: a.Person, Record: audit.RefOf(kindRequest, id),
		})
	})
	return wrap(op, err)
}

func notFoundRequest(op string) error {
	return errs.E(op, errs.NotFound, errors.New("no such request"))
}

// ListRequests returns the deletion requests with the status (open when zero), oldest
// first, optionally for one person. Needs people.data.delete. Reading a request that has a
// note, someone else's, records datarights.request.read (F57). Fails errs.Permission (F44),
// errs.Invalid (status), the recorder's kind and store kinds.
func (s *Service) ListRequests(ctx context.Context, q RequestQuery) (_ []Request, err error) {
	const op = "datarights.Service.ListRequests"
	ctx, span := s.start(ctx, "list-requests")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return nil, err
	}
	status := q.Status
	if status == 0 {
		status = RequestOpen
	}
	if !status.valid() {
		return nil, fieldInvalid(op, "status", codeOneOf, "must be one of: open, withdrawn, declined")
	}
	rs, err := s.store.ListRequests(ctx, status.String(), q.Person, maxRequestsListed)
	if err != nil {
		return nil, wrap(op, err)
	}
	// A note is the person's own words and may say anything: reading someone else's is
	// recorded after loading and before returning, one entry per request with a note (D37).
	var reads []audit.Entry
	for _, r := range rs {
		if r.Note == "" || r.Person == a.Person {
			continue
		}
		reads = append(reads, audit.Entry{
			Action: actionRequestRead, Outcome: audit.Succeeded, Subject: r.Person, Record: audit.RefOf(kindRequest, r.ID),
			Fields: []string{"note"}, Level: model.SensitivityPersonal,
		})
	}
	if len(reads) > 0 {
		if err := s.record(ctx, reads...); err != nil {
			return nil, err
		}
	}
	return requestsOf(rs), nil
}

// DeclineRequest declines an open request with a note for the person. Needs
// people.data.delete. Records datarights.request.declined. Fails errs.Permission (F44),
// errs.Invalid (F41), errs.NotFound, errs.Conflict (status: F43) and store kinds.
func (s *Service) DeclineRequest(ctx context.Context, id RequestID, note string) (_ Request, err error) {
	const op = "datarights.Service.DeclineRequest"
	ctx, span := s.start(ctx, "decline-request")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Request{}, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return Request{}, err
	}
	note, err = checkNote(op, "note", note)
	if err != nil {
		return Request{}, err
	}
	var out rows.Request
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		r, decided, err := s.store.DeclineRequest(ctx, id, a.Person, note, s.now())
		if err != nil {
			return wrap(op, err)
		}
		if !decided {
			if _, err := s.store.GetRequest(ctx, id); err != nil {
				return wrap(op, err)
			}
			return fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
		out = r
		return s.record(ctx, audit.Entry{
			Action: actionRequestDeclined, Outcome: audit.Succeeded, Subject: r.Person, Record: audit.RefOf(kindRequest, id),
		})
	})
	if err != nil {
		return Request{}, wrap(op, err)
	}
	return requestOf(out), nil
}

// FulfilRequest deletes the request's person as Delete does, with Reason "request:<id>",
// and records datarights.request.fulfilled with it. Needs people.data.delete; not for yourself.
// Fails as Delete does, errs.NotFound for an unknown request and errs.Conflict (status:
// F43).
func (s *Service) FulfilRequest(ctx context.Context, id RequestID) (_ DeletionReport, err error) {
	const op = "datarights.Service.FulfilRequest"
	ctx, span := s.start(ctx, "fulfil-request")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return DeletionReport{}, err
	}
	if err := s.require(ctx, op, PermissionDataDelete); err != nil {
		return DeletionReport{}, err
	}
	st, err := s.requireLinked(op)
	if err != nil {
		return DeletionReport{}, err
	}
	// The request names the person, which the lock needs, so it is read first inside the
	// transaction; deleteInTx then re-reads it under the lock.
	var rep DeletionReport
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		r, err := s.store.GetRequest(ctx, id)
		if err != nil {
			return wrap(op, err)
		}
		if r.Person == a.Person {
			return fieldInvalid(op, "person", codeSelf, "you cannot delete yourself; ask another administrator")
		}
		if r.Status != rows.StatusOpen {
			return fieldConflict(op, "request", codeStatus, nil, "the request is not open")
		}
		rep, err = s.deleteInTx(ctx, op, st, deletion{person: r.Person, reason: "request:" + id.String(), request: id})
		return err
	})
	if err != nil {
		s.metrics.deletion(ctx, outcomeFailed)
		return DeletionReport{}, wrap(op, err)
	}
	span.SetAttrs(telemetry.ID("person_id", rep.Person), telemetry.Int("people", rep.People))
	s.metrics.deletion(ctx, outcomeOK)
	return rep, nil
}
