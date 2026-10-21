package authz

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// seedReasonPrefix starts the Reason of the access.role.changed entry Seed records; the
// rest is the permission name added (a declared name, never personal data).
const seedReasonPrefix = "seed:"

var errRuleReadBack = errors.New("the rule just inserted cannot be read back")

// Seed applies each declaration's StarterRoles once: for every (role key, permission) pair
// not yet recorded in core.role_seed it adds the permission to the role with that key and
// records the pair, so a regional's later edits are never undone and a deleted starter role
// never returns. A pair whose role does not exist is skipped and not recorded (D44), so a
// module may list a starter role that a later migration creates and the pair is applied at
// the first call after the role exists. A site-only permission is not added to the
// department starter role (department-admin) and is retried at the next call. It runs in
// one Write transaction under the invariants lock and is idempotent. Consumer: app's
// migrate command, after db.Migrate, on the writing copy. Fails errs.Invalid for a bad
// declaration and errs.Unavailable wrapping db.ErrReadOnly on a read-only copy. It needs no
// actor; each permission it adds to an existing role is recorded as access.role.changed
// (actor: the operator, Reason "seed:<permission>") in the same transaction.
func (s *Service) Seed(ctx context.Context, declared []PermissionDecl) (_ SeedResult, err error) {
	const op = "authz.Service.Seed"
	ctx, span := s.start(ctx, "seed")
	defer endSpan(span, &err)
	v := validate.Errors{Op: op}
	var keys, perms []string
	for i, d := range declared {
		for _, fe := range validate.Fields(d.Validate()) {
			v.Addf("declared["+strconv.Itoa(i)+"]."+fe.Field, fe.Code, "%s", fe.Reason)
		}
		for _, key := range d.StarterRoles {
			if key == RoleDepartmentAdmin && d.Scope == ScopeSite {
				continue
			}
			keys, perms = append(keys, key), append(perms, string(d.Name))
		}
	}
	if err := v.Err(); err != nil {
		return SeedResult{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	var added int
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		if len(keys) == 0 {
			return nil
		}
		seeded, err := s.store.SeedStarterPermissions(ctx, keys, perms, s.now())
		if err != nil {
			return wrap(op, err)
		}
		added = len(seeded)
		// An upgrade that gives a starter role a permission leaves a trace in the log, in the
		// same transaction, so the log shows why the role gained access (D37).
		for chunk := range slices.Chunk(seeded, audit.MaxBatch) {
			entries := make([]audit.Entry, len(chunk))
			for i, sp := range chunk {
				entries[i] = audit.Entry{
					Action: actionRoleChanged, Outcome: audit.Succeeded, Actor: audit.Actor{Kind: audit.ActorOperator},
					Record: audit.RefOf(kindRole, sp.Role), Reason: seedReasonPrefix + sp.Permission,
				}
			}
			if err := s.record(ctx, entries...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SeedResult{}, err
	}
	span.SetAttrs(telemetry.Int("added", added))
	return SeedResult{Added: added}, nil
}
