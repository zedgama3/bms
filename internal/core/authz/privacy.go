package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// exportedGrant is one grant of ExportPerson's JSON.
type exportedGrant struct {
	ID           GrantID            `json:"id"`
	RoleID       RoleID             `json:"role_id"`
	RoleKey      string             `json:"role_key,omitempty"`
	RoleName     string             `json:"role_name,omitempty"`
	DepartmentID model.DepartmentID `json:"department_id,omitzero"`
	EndsAt       time.Time          `json:"ends_at,omitzero"`
	GrantedBy    model.PersonID     `json:"granted_by,omitzero"`
	CreatedAt    time.Time          `json:"created_at"`
}

// ExportPerson returns {"grants": [...]}: every grant of the person, ended ones too, with
// the role's key and name; "grants": [] when there is none. people's per-person export
// calls it through app. Works on a read-only copy; not audited here (people records the
// export). Fails errs.Invalid for a zero ID and store kinds.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (_ json.RawMessage, err error) {
	const op = "authz.Service.ExportPerson"
	ctx, span := s.start(ctx, "export-person")
	defer endSpan(span, &err)
	if person.IsZero() {
		return nil, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	gs, err := s.store.ExportByPerson(ctx, person)
	if err != nil {
		return nil, wrap(op, err)
	}
	body := struct {
		Grants []exportedGrant `json:"grants"`
	}{Grants: make([]exportedGrant, len(gs))}
	for i, g := range gs {
		body.Grants[i] = exportedGrant{
			ID: g.Grant, RoleID: g.Role, RoleKey: g.RoleKey, RoleName: g.RoleName, DepartmentID: g.Dept,
			EndsAt: g.EndsAt, GrantedBy: g.GrantedBy, CreatedAt: g.CreatedAt,
		}
	}
	span.SetAttrs(telemetry.Int("grants", len(gs)))
	out, err := json.Marshal(body)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("encode the export: %w", err))
	}
	return out, nil
}

// DeletePerson deletes every grant of the person (ended ones too), clears granted_by on the
// grants they made and returns how many it deleted, recording access.grants.deleted
// (Subject: the person) when there were any. It runs in one Write transaction under the
// invariants lock; inside people's transaction it joins it. Idempotent: no grants, no
// entry, 0. Fails errs.Invalid for a zero ID, errs.Conflict (last_access_admin) when the
// person is the last active site-wide holder of access.grants.manage, and
// errs.Unavailable wrapping db.ErrReadOnly.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (_ int, err error) {
	const op = "authz.Service.DeletePerson"
	ctx, span := s.start(ctx, "delete-person")
	defer endSpan(span, &err)
	if person.IsZero() {
		return 0, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	var n int
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		if err := s.lastAdmin(ctx, op, rows.Holders{Permission: string(PermissionGrantsManage), Now: s.now(), Person: person}); err != nil {
			return err
		}
		var err error
		if n, err = s.store.DeleteGrantsByPerson(ctx, person); err != nil {
			return wrap(op, err)
		}
		if err := s.store.ClearGrantedBy(ctx, person); err != nil {
			return wrap(op, err)
		}
		if n == 0 {
			return nil
		}
		return s.record(ctx, audit.Entry{Action: actionGrantsGone, Outcome: audit.Succeeded, Subject: person})
	})
	if err != nil {
		return 0, err
	}
	span.SetAttrs(telemetry.Int("grants", n))
	return n, nil
}
