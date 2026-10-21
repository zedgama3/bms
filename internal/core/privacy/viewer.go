package privacy

import (
	"context"
	"errors"
	"slices"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// Policy is the regional's current audience per group name. Consumer: a service, once
// per use case.
type Policy map[string]Audience

// PolicyOf reads visibility_<name> for each group through the area's Reader. Never cached:
// settings are a map lookup and a change applies at once. Fails with the reader's kinds
// (Invalid, Unavailable, Timeout, Canceled), and errs.Internal for a nil reader (a wiring bug).
func PolicyOf(ctx context.Context, r settings.Reader, groups []Group) (Policy, error) {
	const op = "privacy.PolicyOf"
	if r == nil {
		return nil, errs.E(op, errs.Internal, errors.New("nil settings reader"))
	}
	pol := make(Policy, len(groups))
	for _, g := range groups {
		v, err := r.Text(ctx, g.settingKey())
		if err != nil {
			return nil, errs.E(op, errs.KindOf(err), err)
		}
		pol[g.Name] = Audience(v)
	}
	return pol, nil
}

// Shows reports whether a field in group would be returned to viewer for rel. False for an
// unknown group. Cannot fail. Consumer: a service deciding whether to load a field
// (volunteers' shift-mates list needs a NamesOf call it should not make for nothing).
func (p Policy) Shows(group string, viewer Viewer, rel Relation) bool {
	aud, ok := p[group]
	if !ok {
		return false
	}
	return visible(aud, group, viewer, rel)
}

// signedIn reports whether the viewer is a person.
func (v Viewer) signedIn() bool { return !v.Person.IsZero() }

// isSubject reports whether the viewer is the person the record is about; the zero person
// is nobody's subject.
func (v Viewer) isSubject(rel Relation) bool { return v.signedIn() && v.Person == rel.Subject }

// visible is the rule: the viewer's audiences for the record are signed_in (a person), self
// (the subject), holders (the group's extent covers the department) and rel.Also; a field
// is returned when the policy's audience is among them, or when the viewer is the subject
// and the audience is not nobody. Nobody never.
func visible(aud Audience, group string, viewer Viewer, rel Relation) bool {
	switch aud {
	case "", AudienceNobody:
		return false
	}
	if !viewer.signedIn() {
		// The zero Viewer is anonymous and gets public fields only (F40), whatever the
		// policy, its extents or the module's assertions.
		return false
	}
	if viewer.isSubject(rel) {
		return true
	}
	switch aud {
	case AudienceSelf:
		return false
	case AudienceSignedIn:
		return viewer.signedIn()
	case AudienceHolders:
		ext, ok := viewer.Extents[group]
		return ok && ext.Covers(rel.Department)
	default:
		// A module-defined audience: only what the service asserted counts, and a built-in
		// name in Also is ignored, so a module cannot promote a viewer by mistake.
		return !isBuiltin(aud) && slices.Contains(rel.Also, aud)
	}
}

// Viewer is who is looking: the actor and, per group name, where the actor holds that
// group's Permission (keyed by Group.Name, so Filter and Shows, which know only the
// group a field names, need no permission lookup). A group with no entry is not held
// anywhere. The zero Viewer is anonymous (public fields only); a public use case passes
// it to Filter directly and never calls ViewerOf. Consumer: a service, once per use case.
type Viewer struct {
	Person  model.PersonID
	Extents map[string]authz.Extent // by Group.Name
}

// ViewerOf builds the viewer for person, the actor the service already holds from
// identity.PersonFrom(ctx) (an Authorizer answers for the actor in ctx but cannot say who
// that is, and this package may not import identity; person must be that actor, which
// ViewerOf cannot check). It calls ExtentOf once per distinct Group.Permission and stores
// the extent under every group naming it; a Permission error means not held and leaves
// those groups absent. Fails Unauthenticated when person is zero (before any authz call),
// Invalid (a permission not declared in this installation) and the store's kinds. Never
// Permission.
func ViewerOf(ctx context.Context, az authz.Authorizer, person model.PersonID, groups []Group) (Viewer, error) {
	const op = "privacy.ViewerOf"
	if person.IsZero() {
		return Viewer{}, errs.E(op, errs.Unauthenticated, errors.New("no actor"))
	}
	v := Viewer{Person: person}
	found := map[authz.Permission]authz.Extent{}
	asked := map[authz.Permission]bool{}
	for _, g := range groups {
		if g.Permission == "" {
			continue
		}
		if !asked[g.Permission] {
			if az == nil {
				return Viewer{}, errs.E(op, errs.Internal, errors.New("nil authorizer"))
			}
			asked[g.Permission] = true
			ext, err := az.ExtentOf(ctx, g.Permission)
			switch {
			case errs.Is(err, errs.Permission):
				// not held anywhere
			case err != nil:
				return Viewer{}, errs.E(op, errs.KindOf(err), err)
			case !ext.IsZero():
				found[g.Permission] = ext
			}
		}
		if ext, ok := found[g.Permission]; ok {
			if v.Extents == nil {
				v.Extents = map[string]authz.Extent{}
			}
			v.Extents[g.Name] = ext
		}
	}
	return v, nil
}

// Relation is what one record is to the viewer.
type Relation struct {
	Subject    model.PersonID     // the person the record is about; zero = nobody's
	Department model.DepartmentID // the department the record belongs to; zero = none
	Also       []Audience         // Extra audiences the viewer is in for this record
}

// Result says what the caller must audit for this record: the JSON names of the returned
// fields that are personal about someone other than the viewer, or sensitive, and the
// highest level among them.
type Result struct {
	Fields []string
	Level  model.Sensitivity
}

// Audited reports whether an audit entry is due (len(Fields) > 0). Cannot fail.
func (r Result) Audited() bool { return len(r.Fields) > 0 }
