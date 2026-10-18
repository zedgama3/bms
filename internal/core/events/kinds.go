package events

import (
	"context"
	"crypto/sha256"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// kindRef is the audit reference of a kind. A kind is named by its key, not by an ID, and a
// reference needs an ID, so the reference carries a fixed ID derived from the key; the entry
// also names the key in Reason. Cannot fail.
func kindRef(key string) audit.Ref {
	sum := sha256.Sum256([]byte(kindKind + ":" + key))
	return audit.RefOf(kindKind, [16]byte(sum[:16]))
}

// ListKinds returns every kind, the seeded ones first. Any signed-in person may read them.
// Fails errs.Unauthenticated and store kinds.
func (s *Service) ListKinds(ctx context.Context) (_ []Kind, err error) {
	const op = "events.Service.ListKinds"
	ctx, span := s.start(ctx, "list-kinds")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return nil, err
	}
	rs, err := s.store.ListKinds(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]Kind, len(rs))
	for i, r := range rs {
		out[i] = kindOf(r)
	}
	return out, nil
}

// CreateKind adds a kind of event. Needs events.manage. Records events.kind.created in the
// transaction. Fails errs.Unauthenticated, errs.Invalid (key, name), errs.Permission,
// errs.Conflict (key, taken), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) CreateKind(ctx context.Context, in KindInput) (_ Kind, err error) {
	const op = "events.Service.CreateKind"
	ctx, span := s.start(ctx, "create-kind")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Kind{}, err
	}
	in = KindInput{Key: strings.TrimSpace(in.Key), Name: strings.TrimSpace(in.Name)}
	if err := in.Validate(); err != nil {
		return Kind{}, err //nolint:wrapcheck // Validate's error already carries its Op
	}
	if err := s.require(ctx, op); err != nil {
		return Kind{}, err
	}
	span.SetAttrs(telemetry.Token("kind", in.Key))
	var row rows.Kind
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		saved, err := s.store.InsertKind(ctx, in.Key, in.Name, s.now())
		if err != nil {
			return fromConstraint(op, err, fieldKey, codeTaken)
		}
		row = saved
		return s.record(ctx, audit.Entry{
			Action: actionKindCreated, Outcome: audit.Succeeded, Record: kindRef(in.Key), Reason: "key:" + in.Key,
		})
	})
	if err != nil {
		return Kind{}, wrap(op, err)
	}
	return kindOf(row), nil
}

// UpdateKind renames the kind with the given key (a seeded kind too); the key is immutable
// and in.Key is ignored. Needs events.manage. Records events.kind.changed in the
// transaction. Fails errs.Unauthenticated, errs.Invalid (name), errs.Permission,
// errs.NotFound, errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) UpdateKind(ctx context.Context, key string, in KindInput) (_ Kind, err error) {
	const op = "events.Service.UpdateKind"
	ctx, span := s.start(ctx, "update-kind")
	defer endSpan(span, &err)
	if _, err := s.actor(ctx, op); err != nil {
		return Kind{}, err
	}
	name := strings.TrimSpace(in.Name)
	v := validate.Errors{Op: op}
	checkLabel(&v, fieldName, name, MaxKindNameBytes)
	if err := v.Err(); err != nil {
		return Kind{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	if err := s.require(ctx, op); err != nil {
		return Kind{}, err
	}
	var row rows.Kind
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, err := s.store.GetKind(ctx, key)
		if err != nil {
			return wrap(op, err)
		}
		if cur.Name == name {
			row = cur
			return nil
		}
		if row, err = s.store.UpdateKind(ctx, key, name, s.now()); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionKindChanged, Outcome: audit.Succeeded, Record: kindRef(key), Reason: "key:" + key,
			Fields: []string{fieldName}, Level: model.SensitivityInternal,
		})
	})
	if err != nil {
		return Kind{}, wrap(op, err)
	}
	return kindOf(row), nil
}
