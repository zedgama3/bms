package authz

import (
	"context"
	"errors"
	"strconv"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

var errRuleReadBack = errors.New("the rule just inserted cannot be read back")

// Seed applies each declaration's StarterRoles once: for every (role key, permission) pair
// not yet recorded in core.role_seed it adds the permission to the role with that key (when
// one exists) and records the pair, so a regional's later edits are never undone and a
// deleted starter role never returns. A site-only permission is not added to the
// department starter role (department-admin) and is retried at the next call. It runs in
// one Write transaction under the invariants lock and is idempotent. Consumer: app's
// migrate command, after db.Migrate, on the writing copy. Fails errs.Invalid for a bad
// declaration and errs.Unavailable wrapping db.ErrReadOnly on a read-only copy. Not audited
// and needs no actor.
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
		var err error
		added, err = s.store.SeedStarterPermissions(ctx, keys, perms, s.now())
		return wrap(op, err)
	})
	if err != nil {
		return SeedResult{}, err
	}
	span.SetAttrs(telemetry.Int("added", added))
	return SeedResult{Added: added}, nil
}
