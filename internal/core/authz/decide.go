package authz

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// extentSet is where one permission is held, while it is being built.
type extentSet struct {
	site  bool
	depts map[model.DepartmentID]struct{}
}

// access is one person's effective permissions after the separation-of-duties rules, or
// everything for the superuser.
type access struct {
	super bool
	sets  map[Permission]*extentSet
}

func (a *access) extent(p Permission) Extent {
	if a.super {
		return Extent{Site: true}
	}
	set := a.sets[p]
	if set == nil {
		return Extent{}
	}
	if set.site {
		return Extent{Site: true}
	}
	out := Extent{Departments: make([]model.DepartmentID, 0, len(set.depts))}
	for d := range set.depts {
		out.Departments = append(out.Departments, d)
	}
	slices.SortFunc(out.Departments, func(x, y model.DepartmentID) int { return strings.Compare(x.String(), y.String()) })
	if len(out.Departments) == 0 {
		return Extent{}
	}
	return out
}

// site reports whether p is held site-wide.
func (a *access) site(p Permission) bool { return a.extent(p).Site }

// at reports whether p is held site-wide or for d.
func (a *access) at(p Permission, d model.DepartmentID) bool { return a.extent(p).Covers(d) }

// anywhere reports whether p is held in any extent.
func (a *access) anywhere(p Permission) bool { return !a.extent(p).IsZero() }

// forbiddenBy returns the permission names rule r forbids: its permission, or every
// declared name reaching at least its level. A level the table cannot hold is a broken
// invariant: errs.Internal, so the decision fails closed instead of forbidding nothing.
func (s *Service) forbiddenBy(r rows.HeldRule) ([]string, error) {
	if r.Permission != "" {
		return []string{r.Permission}, nil
	}
	level, err := model.ParseSensitivity(r.Reaches)
	if err != nil {
		return nil, errs.E("authz.Service.forbiddenBy", errs.Internal, errors.New("a stored rule has no permission and no valid level"))
	}
	var out []string
	for _, d := range s.cat.list {
		if d.Reaches >= level {
			out = append(out, string(d.Name))
		}
	}
	return out, nil
}

// forbiddenSet is the union of what every rule forbids.
func (s *Service) forbiddenSet(rules []rows.HeldRule) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, r := range rules {
		names, err := s.forbiddenBy(r)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			out[name] = struct{}{}
		}
	}
	return out, nil
}

// load reads the person's grants (all permissions, or only one when only is not empty) and
// the rules of the roles they hold, and returns the effective set (decision 3, steps 3a and
// 3b). With no grant rows there is nothing a rule could remove, so the rules are not read.
func (s *Service) load(ctx context.Context, person model.PersonID, only Permission) (*access, error) {
	const op = "authz.Service.load"
	now := s.now()
	var (
		held []rows.Held
		err  error
	)
	if only != "" {
		held, err = s.store.EffectiveFor(ctx, person, now, string(only))
	} else {
		held, err = s.store.EffectivePermissions(ctx, person, now)
	}
	if err != nil {
		return nil, wrap(op, err)
	}
	out := &access{sets: map[Permission]*extentSet{}}
	if len(held) == 0 {
		return out, nil
	}
	rules, err := s.store.RulesHeldBy(ctx, person, now)
	if err != nil {
		return nil, wrap(op, err)
	}
	forbidden, err := s.forbiddenSet(rules)
	if err != nil {
		return nil, wrap(op, err)
	}
	for _, h := range held {
		if _, no := forbidden[h.Permission]; no {
			continue
		}
		p := Permission(h.Permission)
		// Fail closed: a permission declared site-only is never held for a department,
		// whatever the row says (a department role given it while it was undeclared, or a
		// module that changed the scope since).
		if d, ok := s.cat.byName[p]; ok && d.Scope == ScopeSite && !h.Department.IsZero() {
			continue
		}
		set := out.sets[p]
		if set == nil {
			set = &extentSet{depts: map[model.DepartmentID]struct{}{}}
			out.sets[p] = set
		}
		if h.Department.IsZero() {
			set.site = true
		} else {
			set.depts[h.Department] = struct{}{}
		}
	}
	return out, nil
}

// accessOf is the actor's own effective set: everything for the superuser, without a query.
func (s *Service) accessOf(ctx context.Context, a Actor, only Permission) (*access, error) {
	if a.Superuser {
		return &access{super: true}, nil
	}
	return s.load(ctx, a.Person, only)
}

// declaredCheck fails errs.Invalid for a permission nobody declared (F5): the caller
// is a module that asked about a name it did not declare.
func (s *Service) declaredCheck(op string, p Permission) error {
	if err := p.Validate(); err != nil {
		return fieldInvalid(op, "permission", codeMalformed, "must be <module>.<ident>(.<ident>)*")
	}
	if _, ok := s.cat.byName[p]; !ok {
		return fieldInvalid(op, "permission", codeUnknown, "is not declared by the core or an enabled module")
	}
	return nil
}

// resolve is the decision for one permission: where the actor holds p. super reports the
// superuser path (no query). The returned error is already classified; the caller counts
// the outcome.
func (s *Service) resolve(ctx context.Context, op string, span telemetry.Span, p Permission) (ext Extent, super bool, err error) {
	if err := s.declaredCheck(op, p); err != nil {
		return Extent{}, false, err
	}
	span.SetAttrs(telemetry.Token("permission", string(p)))
	a, err := s.actor(ctx, op)
	if err != nil {
		return Extent{}, false, err
	}
	if a.Superuser {
		return Extent{Site: true}, true, nil
	}
	acc, err := s.load(ctx, a.Person, p)
	if err != nil {
		return Extent{}, false, err
	}
	return acc.extent(p), false, nil
}

// countError counts the outcome of a decision that failed before it was made.
func (s *Service) countError(ctx context.Context, err error) {
	if errs.Is(err, errs.Unauthenticated) {
		s.metrics.decided(ctx, outcomeUnauthenticated)
		return
	}
	s.metrics.decided(ctx, outcomeError)
}

func denied(op string, p Permission, what string) error {
	return errs.E(op, errs.Permission, errors.New(what+" "+string(p)+" is not granted"))
}

// Allow fails errs.Permission unless the actor holds p site-wide and no rule forbids it. It
// fails errs.Unauthenticated without an actor, inside a signal delivery or for a restricted
// session; errs.Invalid for a permission nobody declared; and the store's kinds. The
// superuser is allowed before any query. A denial is not audited: the caller audits its
// use case.
func (s *Service) Allow(ctx context.Context, p Permission) (err error) {
	const op = "authz.Service.Allow"
	ctx, span := s.start(ctx, "allow")
	defer endSpan(span, &err)
	ext, super, err := s.resolve(ctx, op, span, p)
	switch {
	case err != nil:
		s.countError(ctx, err)
		return err
	case super:
		s.metrics.decided(ctx, outcomeSuperuser)
		span.SetAttrs(telemetry.Token("outcome", outcomeSuperuser))
		return nil
	case !ext.Site:
		s.metrics.decided(ctx, outcomeDenied)
		span.SetAttrs(telemetry.Token("outcome", outcomeDenied))
		return denied(op, p, "the permission")
	}
	s.metrics.decided(ctx, outcomeAllowed)
	span.SetAttrs(telemetry.Token("outcome", outcomeAllowed))
	return nil
}

// AllowIn fails errs.Permission unless the actor holds p site-wide or for d. It fails
// errs.Invalid for the zero department or an undeclared permission, and as Allow does.
func (s *Service) AllowIn(ctx context.Context, p Permission, d model.DepartmentID) (err error) {
	const op = "authz.Service.AllowIn"
	ctx, span := s.start(ctx, "allow-in")
	defer endSpan(span, &err)
	if err := s.declaredCheck(op, p); err != nil {
		s.countError(ctx, err)
		return err
	}
	if d.IsZero() {
		err := fieldInvalid(op, "department", codeRequired, "must be set")
		s.countError(ctx, err)
		return err
	}
	ext, super, err := s.resolve(ctx, op, span, p)
	switch {
	case err != nil:
		s.countError(ctx, err)
		return err
	case super:
		s.metrics.decided(ctx, outcomeSuperuser)
		span.SetAttrs(telemetry.Token("outcome", outcomeSuperuser))
		return nil
	case !ext.Covers(d):
		s.metrics.decided(ctx, outcomeDenied)
		span.SetAttrs(telemetry.Token("outcome", outcomeDenied))
		return denied(op, p, "the permission")
	}
	s.metrics.decided(ctx, outcomeAllowed)
	span.SetAttrs(telemetry.Token("outcome", outcomeAllowed))
	return nil
}

// ExtentOf returns where the actor holds p, after the rules: {Site: true} for the
// superuser, the distinct departments otherwise; a zero Extent and nil when nowhere. It
// fails as Allow does.
func (s *Service) ExtentOf(ctx context.Context, p Permission) (_ Extent, err error) {
	const op = "authz.Service.ExtentOf"
	ctx, span := s.start(ctx, "extent")
	defer endSpan(span, &err)
	ext, super, err := s.resolve(ctx, op, span, p)
	switch {
	case err != nil:
		s.countError(ctx, err)
		return Extent{}, err
	case super:
		s.metrics.decided(ctx, outcomeSuperuser)
		return ext, nil
	case ext.IsZero():
		s.metrics.decided(ctx, outcomeDenied)
	default:
		s.metrics.decided(ctx, outcomeAllowed)
	}
	return ext, nil
}

// PermissionsOf is identity.PermissionLister: the declared names the session's person
// holds after the rules, "p" for site and "p@<department-id>" for a department, sorted and
// unique, never nil; every declared name for the superuser. person must be the actor's
// own (errs.Permission otherwise); fails errs.Unauthenticated without an actor.
func (s *Service) PermissionsOf(ctx context.Context, person model.PersonID) (_ []string, err error) {
	const op = "authz.Service.PermissionsOf"
	ctx, span := s.start(ctx, "permissions-of")
	defer endSpan(span, &err)
	a, err := s.actor(ctx, op)
	if err != nil {
		return nil, err
	}
	if person != a.Person {
		return nil, errs.E(op, errs.Permission, errors.New("a person's permissions are listed for that person only"))
	}
	out := []string{}
	if a.Superuser {
		for _, d := range s.cat.list {
			out = append(out, string(d.Name))
		}
		return out, nil
	}
	acc, err := s.load(ctx, a.Person, "")
	if err != nil {
		return nil, err
	}
	for p := range acc.sets {
		if _, ok := s.cat.byName[p]; !ok {
			continue // not available in this installation: no effect (decision 13)
		}
		ext := acc.extent(p)
		if ext.Site {
			out = append(out, string(p))
			continue
		}
		for _, d := range ext.Departments {
			out = append(out, string(p)+"@"+d.String())
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// HoldersOf returns the persons who hold p site-wide now (D39): through an explicit grant
// that has not ended, on a site-scoped role that holds p, and not denied p by a
// separation-of-duties rule, in person ID order, never nil. The base role applies to
// everyone and is not enumerated, so a permission carried by the base role alone yields
// nobody, and one carried by the base role and a granted role yields the granted holders
// once each. The result is grant holders: a superuser appears only through an explicit
// grant, since the flag is not a grant (decision 6). It is a system read: no
// actor, no permission check, not audited, never on a route; the consumers are app's
// superuser-alert adapter and people's deletion notice. It fails errs.Invalid for a
// permission nobody declared and with the store's kinds (Unavailable, Timeout, Canceled).
func (s *Service) HoldersOf(ctx context.Context, p Permission) (_ []model.PersonID, err error) {
	const op = "authz.Service.HoldersOf"
	ctx, span := s.start(ctx, "holders-of")
	defer endSpan(span, &err)
	if err := s.declaredCheck(op, p); err != nil {
		return nil, err
	}
	span.SetAttrs(telemetry.Token("permission", string(p)))
	now := s.now()
	candidates, err := s.store.PermissionHolders(ctx, string(p), now)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]model.PersonID, 0, len(candidates))
	for _, person := range candidates {
		rules, err := s.store.RulesHeldBy(ctx, person, now)
		if err != nil {
			return nil, wrap(op, err)
		}
		forbidden, err := s.forbiddenSet(rules)
		if err != nil {
			return nil, wrap(op, err)
		}
		if _, no := forbidden[string(p)]; no {
			continue
		}
		out = append(out, person)
	}
	slices.SortFunc(out, func(x, y model.PersonID) int { return strings.Compare(x.String(), y.String()) })
	return slices.Compact(out), nil
}

// TitlesOf is audit.TitleSource: the key (or, when renamed, the name) of each non-base role
// the person holds now, sorted, at most audit.MaxTitles; nil for none. It needs no actor
// and opens no transaction, so the recorder may call it inside a Write transaction.
func (s *Service) TitlesOf(ctx context.Context, person model.PersonID) (_ []string, err error) {
	const op = "authz.Service.TitlesOf"
	ctx, span := s.start(ctx, "titles-of")
	defer endSpan(span, &err)
	if person.IsZero() {
		return nil, nil
	}
	titles, err := s.store.TitlesOf(ctx, person, s.now(), audit.MaxTitles)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := make([]string, 0, len(titles))
	for _, t := range titles {
		if t == "" || len(t) > audit.MaxTitleBytes {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ListPermissions returns the catalogue sorted by name. It needs access.roles.read
// site-wide, or any of access.grants.read, access.grants.manage and
// access.department-grants.manage in any extent (then only the permissions that can be
// granted for a department). Fails errs.Permission, errs.Unauthenticated and store kinds.
func (s *Service) ListPermissions(ctx context.Context) (_ []PermissionInfo, err error) {
	const op = "authz.Service.ListPermissions"
	ctx, span := s.start(ctx, "list-permissions")
	defer endSpan(span, &err)
	view, err := s.viewer(ctx, op)
	if err != nil {
		return nil, err
	}
	out := make([]PermissionInfo, 0, len(s.cat.list))
	for _, d := range s.cat.list {
		if !view.full && d.Scope != ScopeDepartment {
			continue
		}
		d.StarterRoles = slices.Clone(d.StarterRoles)
		out = append(out, PermissionInfo{PermissionDecl: d})
	}
	return out, nil
}

// viewer is the caller of a catalogue or role read: full with access.roles.read, otherwise
// limited to department material when a grants permission is held in any extent.
type viewer struct{ full bool }

func (s *Service) viewer(ctx context.Context, op string) (viewer, error) {
	a, err := s.actor(ctx, op)
	if err != nil {
		return viewer{}, err
	}
	if a.Superuser {
		return viewer{full: true}, nil
	}
	acc, err := s.load(ctx, a.Person, "")
	if err != nil {
		return viewer{}, err
	}
	if acc.site(PermissionRolesRead) {
		return viewer{full: true}, nil
	}
	if acc.anywhere(PermissionGrantsRead) || acc.anywhere(PermissionGrantsManage) || acc.anywhere(PermissionDepartmentGrantsManage) {
		return viewer{}, nil
	}
	return viewer{}, denied(op, PermissionRolesRead, "the permission")
}
