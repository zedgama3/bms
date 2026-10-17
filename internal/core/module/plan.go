package module

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Plan is a resolved, checked set of enabled modules. Immutable.
type Plan struct {
	order      []entry // enabled modules in build order
	graph      Graph   // every registered module, enabled or not
	corePoints []ExtensionPointDecl
}

// ModuleSettings pairs a module name with its declared settings.
//
//nolint:revive // the name is fixed by module-contract.md
type ModuleSettings struct {
	Module   string
	Settings []SettingDecl
}

// Graph is the admin view of the module graph (09 §7). It is serializable; it carries no
// handlers, functions or file systems.
type Graph struct {
	Modules  []Node
	Edges    []Edge
	Warnings []Warning
}

// Node describes one compiled-in module.
type Node struct {
	Name          string
	Enabled       bool
	HasSchema     bool // Migrations != nil; db adds the versions (api.md)
	Permissions   []authz.PermissionDecl
	Settings      []SettingDecl // Check funcs are not serialized
	Publishes     []signals.Name
	Subscribes    []signals.Name
	Provides      []Capability
	Requires      []Capability
	Uses          []Capability
	Defines       []ExtensionPoint
	ContributesTo []ExtensionPoint
}

// Edge is one dependency between two modules (or a module and the core, named by its
// area, e.g. "people").
type Edge struct {
	From string // the module that requires, uses, contributes or subscribes
	To   string // the provider, definer or publisher
	Kind EdgeKind
	Via  string // the capability, extension point or signal name
}

// EdgeKind is the kind of cooperation an edge records.
type EdgeKind uint8

// The edge kinds.
const (
	EdgeRequires EdgeKind = iota + 1
	EdgeUses
	EdgeContributes
	EdgeSubscribes
)

// String returns "requires", "uses", "contributes", "subscribes" or "EdgeKind(<n>)". Cannot fail.
func (k EdgeKind) String() string {
	switch k {
	case EdgeRequires:
		return "requires"
	case EdgeUses:
		return "uses"
	case EdgeContributes:
		return "contributes"
	case EdgeSubscribes:
		return "subscribes"
	default:
		return fmt.Sprintf("EdgeKind(%d)", uint8(k))
	}
}

// Warning is something Resolve allows but an operator should know (decision 6): a
// subscription whose only publishers are disabled, a contribution to a point whose
// definer is disabled, a used capability with no enabled provider.
type Warning struct {
	Module  string
	Message string // English, for logs and the admin screen; no personal data
}

// Resolve checks the registry against env and returns the plan for the enabled modules:
// the build order and the graph. It is pure over the registry's contents and may be
// called more than once. Fails with errs.Invalid or errs.Conflict joining every problem
// found (F14 to F22): Conflict when every problem is a Conflict, Invalid otherwise.
func (r *Registry) Resolve(env Env) (*Plan, error) {
	all := r.snapshot()
	byName := make(map[string]entry, len(all))
	for _, e := range all {
		byName[e.m.Name] = e
	}

	var problems []error
	problems = append(problems, r.recorded()...)
	if err := env.Validate(); err != nil {
		problems = append(problems, err)
	}

	enabled := make(map[string]bool, len(env.Enabled))
	for i, n := range env.Enabled {
		if n == "" {
			continue // Env.Validate reported it
		}
		if _, ok := byName[n]; !ok {
			problems = append(problems, problem(errs.Invalid, fmt.Sprintf("enabled[%d]", i), "unknown",
				"module %q is not registered; registered: %s", n, strings.Join(registeredNames(all), ", ")))
			continue
		}
		enabled[n] = true
	}

	rs := &resolver{env: env, all: all, byName: byName, enabled: enabled}
	rs.checkCapabilities()
	order, cycle := rs.order()
	if cycle != nil {
		rs.problems = append(rs.problems, problem(errs.Invalid, "modules", "cycle",
			"capability cycle among modules: %s", strings.Join(cycle, " -> ")))
	}
	rs.checkSignals()
	rs.checkPoints()
	problems = append(problems, rs.problems...)
	if err := resolveError(problems); err != nil {
		return nil, err
	}

	plan := &Plan{corePoints: slices.Clone(env.CoreExtensionPoints)}
	for _, n := range order {
		plan.order = append(plan.order, byName[n])
	}
	plan.graph = rs.graph()
	return plan, nil
}

func registeredNames(all []entry) []string {
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = e.m.Name
	}
	if len(out) == 0 {
		return []string{"(none)"}
	}
	return out
}

// resolver holds the working state of one Resolve call.
type resolver struct {
	env      Env
	all      []entry
	byName   map[string]entry
	enabled  map[string]bool
	problems []error
	warnings []Warning
	edges    []Edge
	// providers maps a capability to the enabled modules providing it, by name.
	providers map[Capability][]string
}

// enabledEntries returns the enabled entries sorted by name.
func (rs *resolver) enabledEntries() []entry {
	var out []entry
	for _, e := range rs.all {
		if rs.enabled[e.m.Name] {
			out = append(out, e)
		}
	}
	return out
}

func (rs *resolver) warn(module, format string, args ...any) {
	rs.warnings = append(rs.warnings, Warning{Module: module, Message: fmt.Sprintf(format, args...)})
}

// checkCapabilities applies F17, F18 and the used-capability warning, and records edges.
func (rs *resolver) checkCapabilities() {
	rs.providers = map[Capability][]string{}
	disabled := map[Capability][]string{}
	for _, e := range rs.all {
		for _, c := range e.m.Provides {
			if rs.enabled[e.m.Name] {
				rs.providers[c] = append(rs.providers[c], e.m.Name)
			} else {
				disabled[c] = append(disabled[c], e.m.Name)
			}
		}
	}
	caps := make([]string, 0, len(rs.providers))
	for c, who := range rs.providers {
		if len(who) > 1 {
			caps = append(caps, string(c))
		}
	}
	sort.Strings(caps)
	for _, c := range caps {
		rs.problems = append(rs.problems, problem(errs.Conflict, "capabilities."+c, "conflict",
			"capability %q is provided by more than one enabled module: %s; disable all but one",
			c, strings.Join(rs.providers[Capability(c)], ", ")))
	}
	for _, e := range rs.enabledEntries() {
		for _, c := range e.m.Requires {
			who := rs.providers[c]
			if len(who) == 0 {
				msg := fmt.Sprintf("module %q requires capability %q but no enabled module provides it", e.m.Name, c)
				if d := disabled[c]; len(d) > 0 {
					msg += "; enable " + strings.Join(d, " or ")
				}
				rs.problems = append(rs.problems, problem(errs.Invalid, e.m.Name+".requires", "missing", "%s", msg))
				continue
			}
			rs.edges = append(rs.edges, Edge{From: e.m.Name, To: who[0], Kind: EdgeRequires, Via: string(c)})
		}
		for _, c := range e.m.Uses {
			who := rs.providers[c]
			if len(who) == 0 {
				rs.warn(e.m.Name, "uses capability %q but no enabled module provides it; the feature is hidden", c)
				continue
			}
			rs.edges = append(rs.edges, Edge{From: e.m.Name, To: who[0], Kind: EdgeUses, Via: string(c)})
		}
	}
}

// order returns the enabled modules in build order (providers first, ties by name). If
// the dependencies have a cycle it returns the names on one cycle.
func (rs *resolver) order() (order, cycle []string) {
	deps := map[string]map[string]struct{}{}
	for n := range rs.enabled {
		deps[n] = map[string]struct{}{}
	}
	for _, e := range rs.enabledEntries() {
		for _, c := range append(slices.Clone(e.m.Requires), e.m.Uses...) {
			for _, p := range rs.providers[c] {
				deps[e.m.Name][p] = struct{}{}
			}
		}
	}
	done := map[string]bool{}
	for len(done) < len(deps) {
		next := ""
		for n, ds := range deps {
			if done[n] || (next != "" && n >= next) {
				continue
			}
			ready := true
			for d := range ds {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				next = n
			}
		}
		if next == "" {
			return nil, findCycle(deps, done)
		}
		done[next] = true
		order = append(order, next)
	}
	return order, nil
}

// findCycle returns one cycle among the modules not done, starting from the smallest name.
func findCycle(deps map[string]map[string]struct{}, done map[string]bool) []string {
	start := ""
	for n := range deps {
		if !done[n] && (start == "" || n < start) {
			start = n
		}
	}
	path := []string{start}
	pos := map[string]int{start: 0}
	cur := start
	for {
		var next string
		for d := range deps[cur] {
			if !done[d] && (next == "" || d < next) {
				next = d
			}
		}
		if i, seen := pos[next]; seen {
			return append(slices.Clone(path[i:]), next)
		}
		pos[next] = len(path)
		path = append(path, next)
		cur = next
	}
}

// publisherOf returns the registered module that publishes name, if any.
func (rs *resolver) publisherOf(name signals.Name) (entry, bool) {
	e, ok := rs.byName[name.Publisher()]
	if !ok || !slices.Contains(e.m.Publishes, name) {
		return entry{}, false
	}
	return e, true
}

// checkSignals applies F20 and the disabled-publisher warning, and records edges.
func (rs *resolver) checkSignals() {
	core := make(map[signals.Name]struct{}, len(rs.env.CoreSignals))
	for _, n := range rs.env.CoreSignals {
		core[n] = struct{}{}
	}
	for _, e := range rs.enabledEntries() {
		for i, n := range e.m.Subscribes {
			if _, ok := core[n]; ok {
				rs.edges = append(rs.edges, Edge{From: e.m.Name, To: n.Publisher(), Kind: EdgeSubscribes, Via: string(n)})
				continue
			}
			pub, ok := rs.publisherOf(n)
			switch {
			case !ok:
				rs.problems = append(rs.problems, problem(errs.Invalid,
					fmt.Sprintf("%s.subscribes[%d]", e.m.Name, i), "unknown",
					"signal %q is published by neither the core nor any registered module", n))
			case !rs.enabled[pub.m.Name]:
				rs.warn(e.m.Name, "subscribes to signal %q, whose publisher %q is disabled; the handler never runs", n, pub.m.Name)
			default:
				rs.edges = append(rs.edges, Edge{From: e.m.Name, To: pub.m.Name, Kind: EdgeSubscribes, Via: string(n)})
			}
		}
	}
}

// definerOf returns the registered module that defines point, if any.
func (rs *resolver) definerOf(point ExtensionPoint) (entry, bool) {
	e, ok := rs.byName[point.Definer()]
	if !ok {
		return entry{}, false
	}
	for _, d := range e.m.Defines {
		if d.Name == point {
			return e, true
		}
	}
	return entry{}, false
}

// checkPoints applies F21 and the disabled-definer warning, and records edges.
func (rs *resolver) checkPoints() {
	core := make(map[ExtensionPoint]struct{}, len(rs.env.CoreExtensionPoints))
	for _, d := range rs.env.CoreExtensionPoints {
		core[d.Name] = struct{}{}
	}
	for _, e := range rs.enabledEntries() {
		for i, p := range e.m.ContributesTo {
			if _, ok := core[p]; ok {
				rs.edges = append(rs.edges, Edge{From: e.m.Name, To: p.Definer(), Kind: EdgeContributes, Via: string(p)})
				continue
			}
			def, ok := rs.definerOf(p)
			switch {
			case !ok:
				rs.problems = append(rs.problems, problem(errs.Invalid,
					fmt.Sprintf("%s.contributesTo[%d]", e.m.Name, i), "unknown",
					"extension point %q is defined by neither the core nor any registered module", p))
			case !rs.enabled[def.m.Name]:
				rs.warn(e.m.Name, "contributes to extension point %q, whose definer %q is disabled; the contribution is not used", p, def.m.Name)
			default:
				rs.edges = append(rs.edges, Edge{From: e.m.Name, To: def.m.Name, Kind: EdgeContributes, Via: string(p)})
			}
		}
	}
}

// graph assembles the graph of every registered module, with sorted edges and warnings.
func (rs *resolver) graph() Graph {
	g := Graph{Modules: make([]Node, 0, len(rs.all)), Edges: slices.Clone(rs.edges), Warnings: slices.Clone(rs.warnings)}
	for _, e := range rs.all {
		g.Modules = append(g.Modules, nodeOf(e.m, rs.enabled[e.m.Name]))
	}
	sort.SliceStable(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Via != b.Via {
			return a.Via < b.Via
		}
		return a.To < b.To
	})
	sort.SliceStable(g.Warnings, func(i, j int) bool {
		a, b := g.Warnings[i], g.Warnings[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Message < b.Message
	})
	return g
}

func nodeOf(m Manifest, enabled bool) Node {
	n := Node{
		Name:          m.Name,
		Enabled:       enabled,
		HasSchema:     m.Migrations != nil,
		Permissions:   slices.Clone(m.Permissions),
		Settings:      cloneSettings(m.Settings),
		Publishes:     slices.Clone(m.Publishes),
		Subscribes:    slices.Clone(m.Subscribes),
		Provides:      slices.Clone(m.Provides),
		Requires:      slices.Clone(m.Requires),
		Uses:          slices.Clone(m.Uses),
		ContributesTo: slices.Clone(m.ContributesTo),
	}
	for _, d := range m.Defines {
		n.Defines = append(n.Defines, d.Name)
	}
	return n
}

func cloneSettings(in []SettingDecl) []SettingDecl {
	out := slices.Clone(in)
	for i := range out {
		out[i].Options = slices.Clone(out[i].Options)
	}
	return out
}

// Order returns the enabled manifests in build order: a provider before every module that
// requires or uses it, ties broken by name. Cannot fail.
func (p *Plan) Order() []Manifest {
	out := make([]Manifest, len(p.order))
	for i, e := range p.order {
		out[i] = cloneManifest(e.m)
	}
	return out
}

// Permissions returns every enabled module's permission declarations, for authz, in build
// order. Cannot fail.
func (p *Plan) Permissions() []authz.PermissionDecl {
	var out []authz.PermissionDecl
	for _, e := range p.order {
		out = append(out, e.m.Permissions...)
	}
	return out
}

// Settings returns every enabled module's setting declarations, keyed by module, in build
// order, for settings. A module without settings is listed with none. Cannot fail.
func (p *Plan) Settings() []ModuleSettings {
	out := make([]ModuleSettings, len(p.order))
	for i, e := range p.order {
		out[i] = ModuleSettings{Module: e.m.Name, Settings: cloneSettings(e.m.Settings)}
	}
	return out
}

// Graph returns the whole registry as data for `GET /api/admin/modules` and `bms check`:
// every compiled-in module, enabled or not, its declarations, the edges between enabled
// modules and the core, and the warnings Resolve produced. Cannot fail.
func (p *Plan) Graph() Graph {
	g := Graph{
		Modules:  make([]Node, len(p.graph.Modules)),
		Edges:    slices.Clone(p.graph.Edges),
		Warnings: slices.Clone(p.graph.Warnings),
	}
	for i, n := range p.graph.Modules {
		n.Permissions = slices.Clone(n.Permissions)
		n.Settings = cloneSettings(n.Settings)
		n.Publishes = slices.Clone(n.Publishes)
		n.Subscribes = slices.Clone(n.Subscribes)
		n.Provides = slices.Clone(n.Provides)
		n.Requires = slices.Clone(n.Requires)
		n.Uses = slices.Clone(n.Uses)
		n.Defines = slices.Clone(n.Defines)
		n.ContributesTo = slices.Clone(n.ContributesTo)
		g.Modules[i] = n
	}
	return g
}

// cloneManifest copies the slices of m so a caller cannot change the Plan through it. The
// file system, the Accepts and Check funcs are shared; they are read-only by contract.
func cloneManifest(m Manifest) Manifest {
	m.Permissions = slices.Clone(m.Permissions)
	m.Settings = cloneSettings(m.Settings)
	m.Publishes = slices.Clone(m.Publishes)
	m.Subscribes = slices.Clone(m.Subscribes)
	m.Provides = slices.Clone(m.Provides)
	m.Requires = slices.Clone(m.Requires)
	m.Uses = slices.Clone(m.Uses)
	m.Defines = slices.Clone(m.Defines)
	m.ContributesTo = slices.Clone(m.ContributesTo)
	return m
}
