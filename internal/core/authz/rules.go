package authz

import (
	"context"
	"slices"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func ruleOf(r rows.Rule) Rule {
	out := Rule{
		ID: r.ID, Role: r.Role, Permission: Permission(r.Permission), Note: r.Note,
		Affected: r.Affected, CreatedAt: r.CreatedAt,
	}
	if r.Reaches != "" {
		if level, err := model.ParseSensitivity(r.Reaches); err == nil {
			out.Reaches = level
		}
	}
	return out
}

// declaredRows is the catalogue as list_rules.sql takes it.
func (s *Service) declaredRows() []rows.Declared {
	out := make([]rows.Declared, len(s.lk.cat.list))
	for i, d := range s.lk.cat.list {
		out[i] = rows.Declared{Name: string(d.Name), Reaches: d.Reaches.String()}
	}
	return out
}

// ListRules needs access.roles.read. It returns the rules sorted by role name, each with
// Affected: the people who hold the rule's role and a forbidden permission now. Works on a
// read-only copy.
func (s *Service) ListRules(ctx context.Context) (_ []Rule, err error) {
	const op = "authz.Service.ListRules"
	ctx, span := s.start(ctx, "list-rules")
	defer endSpan(span, &err)
	if err := s.Allow(ctx, PermissionRolesRead); err != nil {
		return nil, wrap(op, err)
	}
	rs, err := s.store.ListRules(ctx, s.now(), s.declaredRows(), RuleID{})
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]Rule, len(rs))
	for i, r := range rs {
		out[i] = ruleOf(r)
	}
	span.SetAttrs(telemetry.Int("rules", len(out)))
	return out, nil
}

// CreateRule needs access.rules.manage. It records the rule in a Write transaction and
// access.rule.created; the rule applies at once, even to people who already break it
// (Affected says how many). A rule that would deny access.grants.manage to every effective
// access admin is refused with errs.Conflict (last_access_admin, F65) and writes nothing.
// Fails errs.Invalid (F53), errs.NotFound for an unknown role
// (F54), errs.Conflict (duplicate, F55) and errs.Unavailable wrapping db.ErrReadOnly.
func (s *Service) CreateRule(ctx context.Context, in RuleInput) (_ Rule, err error) {
	const op = "authz.Service.CreateRule"
	ctx, span := s.start(ctx, "create-rule")
	defer endSpan(span, &err)
	if err := s.Allow(ctx, PermissionRulesManage); err != nil {
		return Rule{}, wrap(op, err)
	}
	if err := in.Validate(); err != nil {
		return Rule{}, err
	}
	r := rows.Rule{
		HeldRule:  rows.HeldRule{ID: model.NewID[RuleID](), Role: in.Role, Permission: string(in.Permission), Note: in.Note},
		CreatedAt: s.now(),
	}
	if in.Reaches != 0 {
		r.Reaches = in.Reaches.String()
	}
	var out Rule
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockInvariants(ctx); err != nil {
			return wrap(op, err)
		}
		if _, err := s.store.GetRole(ctx, in.Role); err != nil {
			return wrap(op, err)
		}
		// Only a rule that forbids access.grants.manage can leave no effective access admin;
		// for any other the two counts are skipped (D37, F65).
		forbidden, err := s.lk.forbiddenBy(rows.HeldRule{Permission: r.Permission, Reaches: r.Reaches})
		if err != nil {
			return wrap(op, err)
		}
		checkAdmins := slices.Contains(forbidden, string(PermissionGrantsManage))
		hq := rows.Holders{Permission: string(PermissionGrantsManage), Now: s.now()}
		var before int
		if checkAdmins {
			if _, before, err = s.effectiveAdmins(ctx, op, hq); err != nil {
				return err
			}
		}
		if err := s.store.InsertRule(ctx, r); err != nil {
			switch constraintOf(err) {
			case "sod_rule_role_permission_key", "sod_rule_role_reaches_key":
				return fieldConflict(op, "permission", codeDuplicate, nil, "this rule exists already")
			}
			return wrap(op, err)
		}
		// The rule applies at once: refuse it when it denies access.grants.manage to every
		// effective access admin there was (D37, F65). Nothing is written: the error rolls back.
		if checkAdmins && before > 0 {
			_, after, err := s.effectiveAdmins(ctx, op, hq)
			if err != nil {
				return err
			}
			if after == 0 {
				return fieldConflict(op, "role", codeLastAdmin, nil, "this rule would leave no one able to manage access")
			}
		}
		back, err := s.store.ListRules(ctx, s.now(), s.declaredRows(), r.ID)
		if err != nil {
			return wrap(op, err)
		}
		if len(back) != 1 {
			return errs.E(op, errs.Internal, errRuleReadBack)
		}
		out = ruleOf(back[0])
		return s.record(ctx, audit.Entry{Action: actionRuleCreated, Outcome: audit.Succeeded, Record: audit.RefOf(kindRule, r.ID)})
	})
	if err != nil {
		return Rule{}, err
	}
	span.SetAttrs(telemetry.ID("rule_id", out.ID))
	return out, nil
}

// DeleteRule needs access.rules.manage. It deletes the rule and records access.rule.deleted
// in one Write transaction. Fails errs.NotFound for an unknown ID (F56).
func (s *Service) DeleteRule(ctx context.Context, id RuleID) (err error) {
	const op = "authz.Service.DeleteRule"
	ctx, span := s.start(ctx, "delete-rule")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.ID("rule_id", id))
	if err := s.Allow(ctx, PermissionRulesManage); err != nil {
		return wrap(op, err)
	}
	if id.IsZero() {
		return fieldInvalid(op, "rule", codeRequired, "must be set")
	}
	return s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.DeleteRule(ctx, id); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{Action: actionRuleDeleted, Outcome: audit.Succeeded, Record: audit.RefOf(kindRule, id)})
	})
}
