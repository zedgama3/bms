package module

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// SchemaChecker is what Build needs from db (D17): compare the schema version recorded
// for module with the one migrations is built for. *db.Migrator implements it
// structurally (db may not import module); app asserts it and passes it to Build (db.md).
type SchemaChecker interface {
	// CheckSchema fails with errs.Conflict if the recorded version is missing or differs,
	// with a message naming the module and both versions and saying to run `bms migrate`;
	// with errs.Unavailable or errs.Timeout if the database cannot be reached in time;
	// and with errs.Invalid if migrations is malformed.
	CheckSchema(ctx context.Context, module string, migrations fs.FS) error
}

// Provision builds the scoped dependencies for one module (app). It sets every field of
// Deps except Links.
type Provision func(ctx context.Context, m Manifest) (Deps, error)

// ctxErr returns nil while ctx is live and otherwise an error of kind Canceled or Timeout.
func ctxErr(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	return nil
}

// verified is what Build learned from one module's optional interfaces.
type verified struct {
	capabilities  map[Capability]any
	contributions map[ExtensionPoint]any
	routes        []Route
	openAPI       []byte
	isRoutes      bool
}

// Build constructs the enabled modules in Order. For each module it checks the schema
// (when Migrations != nil), calls provision, fills Links, calls the factory, and verifies
// the module implements what its manifest declares. Schema errors for every module are
// collected before anything is built, so an operator sees all of them. Fails with the
// kind of the first failing step (F24 to F36), wrapped with the module name; a built module
// is discarded (Stop is not called, since Start was not). Does not start anything.
func (p *Plan) Build(ctx context.Context, schema SchemaChecker, provision Provision) (*Set, error) {
	const op = "module.Plan.Build"
	if schema == nil || provision == nil {
		return nil, invalid(op, "the schema checker and the provision function must not be nil")
	}
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}
	if err := p.checkSchemas(ctx, schema); err != nil {
		return nil, err
	}

	type contribution struct {
		module string
		point  ExtensionPoint
		value  any
	}
	var (
		provided      = map[Capability]any{}
		contributions []contribution
		links         []*Links
		set           = &Set{corePoints: map[ExtensionPoint]struct{}{}}
	)
	for _, d := range p.corePoints {
		set.corePoints[d.Name] = struct{}{}
	}
	for _, e := range p.order {
		name := e.m.Name
		if err := ctxErr(ctx, op); err != nil {
			return nil, err
		}
		deps, err := provision(ctx, e.m)
		if err != nil {
			return nil, fmt.Errorf("module %s: provision: %w", name, err)
		}
		if deps.Logger == nil || deps.Tracer == nil || deps.Clock == nil {
			return nil, invalid(op, "module %s: provision left Logger, Tracer or Clock nil", name)
		}
		l := newLinks(e.m)
		for _, c := range append(append([]Capability{}, e.m.Requires...), e.m.Uses...) {
			if v, ok := provided[c]; ok {
				l.provide(c, v)
			}
		}
		deps.Links = l
		mod, err := e.f(ctx, deps)
		if err != nil {
			return nil, fmt.Errorf("module %s: build: %w", name, err)
		}
		if mod == nil {
			return nil, internal(op, "module %s: the factory returned a nil module and no error", name)
		}
		v, err := verify(e.m, mod)
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", name, err)
		}
		for c, impl := range v.capabilities {
			provided[c] = impl
		}
		for _, point := range e.m.ContributesTo {
			contributions = append(contributions, contribution{name, point, v.contributions[point]})
		}
		links = append(links, l)
		set.built = append(set.built, Built{Manifest: e.m, Module: mod})
		set.pd = append(set.pd, ModulePersonData{Module: name, PersonData: mod})
		if v.isRoutes {
			set.routes = append(set.routes, ModuleRoutes{Module: name, Routes: v.routes, OpenAPI: v.openAPI})
		}
	}
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}

	accepts := map[ExtensionPoint]func(any) bool{}
	for _, d := range p.corePoints {
		accepts[d.Name] = d.Accepts
	}
	for _, e := range p.order {
		for _, d := range e.m.Defines {
			accepts[d.Name] = d.Accepts
		}
	}
	table := map[ExtensionPoint][]Contribution{}
	var rejected []error
	for _, c := range contributions {
		accept, ok := accepts[c.point]
		if !ok {
			continue // its definer is disabled (a warning from Resolve)
		}
		if !accept(c.value) {
			rejected = append(rejected, invalid(op,
				"module %s: its contribution to extension point %q is not accepted by %s, the definer",
				c.module, c.point, c.point.Definer()))
			continue
		}
		table[c.point] = append(table[c.point], Contribution{Module: c.module, Value: c.value})
	}
	if len(rejected) > 0 {
		return nil, errors.Join(rejected...)
	}
	set.contrib = table
	for _, l := range links {
		l.freeze(table)
	}
	return set, nil
}

// checkSchemas runs CheckSchema for every module with migrations and joins every failure.
func (p *Plan) checkSchemas(ctx context.Context, schema SchemaChecker) error {
	var failed []error
	for _, e := range p.order {
		if e.m.Migrations == nil {
			continue
		}
		if err := schema.CheckSchema(ctx, e.m.Name, e.m.Migrations); err != nil {
			failed = append(failed, fmt.Errorf("module %s: schema: %w", e.m.Name, err))
		}
	}
	return errors.Join(failed...)
}

// verify checks that mod implements what m declares (F29 to F31, F33, F34) and returns what
// it found. Every problem of the module is joined.
func verify(m Manifest, mod Module) (verified, error) {
	const op = "module.Plan.Build"
	var v verified
	var problems []error

	provider, isProvider := mod.(CapabilityProvider)
	switch {
	case isProvider:
		v.capabilities = provider.Capabilities()
		problems = append(problems, checkKeys(op, "Capabilities", "Provides", v.capabilities, m.Provides)...)
		for c, impl := range v.capabilities {
			if impl == nil {
				problems = append(problems, invalid(op, "capability %q is provided as nil", c))
			}
		}
	case len(m.Provides) > 0:
		problems = append(problems, invalid(op, "the manifest provides %v but the module does not implement CapabilityProvider", m.Provides))
	}

	contributor, isContributor := mod.(Contributor)
	switch {
	case isContributor:
		v.contributions = contributor.Contributions()
		problems = append(problems, checkKeys(op, "Contributions", "ContributesTo", v.contributions, m.ContributesTo)...)
	case len(m.ContributesTo) > 0:
		problems = append(problems, invalid(op, "the manifest contributes to %v but the module does not implement Contributor", m.ContributesTo))
	}

	if rp, ok := mod.(RouteProvider); ok {
		v.isRoutes = true
		v.routes = append([]Route(nil), rp.Routes()...)
		seen := make(map[string]struct{}, len(v.routes))
		for i, r := range v.routes {
			if err := r.Validate(); err != nil {
				problems = append(problems, fmt.Errorf("routes[%d]: %w", i, err))
				continue
			}
			key := r.Method + " " + r.Pattern
			if _, dup := seen[key]; dup {
				problems = append(problems, invalid(op, "routes[%d]: %s is declared twice", i, key))
			}
			seen[key] = struct{}{}
		}
		desc, ok := mod.(APIDescriber)
		switch {
		case !ok:
			problems = append(problems, invalid(op, "the module has routes but does not implement APIDescriber"))
		default:
			v.openAPI = append([]byte(nil), desc.OpenAPI()...)
			if len(v.openAPI) == 0 {
				problems = append(problems, invalid(op, "the module has routes but its OpenAPI fragment is empty"))
			}
		}
	}
	if len(problems) > 0 {
		return verified{}, errors.Join(problems...)
	}
	return v, nil
}

// checkKeys reports every difference between the keys of got and the declared list.
func checkKeys[K ~string](op, method, field string, got map[K]any, declared []K) []error {
	var out []error
	want := make(map[K]struct{}, len(declared))
	for _, k := range declared {
		want[k] = struct{}{}
		if _, ok := got[k]; !ok {
			out = append(out, invalid(op, "%s() lacks %q, which the manifest lists in %s", method, k, field))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			out = append(out, invalid(op, "%s() returns %q, which the manifest does not list in %s", method, k, field))
		}
	}
	return out
}
