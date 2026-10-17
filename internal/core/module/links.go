package module

import (
	"sync"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Contribution is one module's value for an extension point.
type Contribution struct {
	Module string
	Value  any
}

// linkOwner is the part of the owning manifest that Links enforces.
type linkOwner struct {
	name     string
	requires map[Capability]struct{}
	uses     map[Capability]struct{}
	defines  map[ExtensionPoint]struct{}
}

// Links is a module's view of the modules it cooperates with. Build fills it: providers
// are built before their consumers, so Capability works from the Factory on;
// Contributors works from Start on, after every module is built. Safe for concurrent use
// once Build returns; the zero value fails every call with errs.Internal.
type Links struct {
	mu        sync.RWMutex
	owner     *linkOwner
	providers map[Capability]any
	contrib   map[ExtensionPoint][]Contribution
	frozen    bool
}

// newLinks returns the Links of manifest m with no providers yet.
func newLinks(m Manifest) *Links {
	o := &linkOwner{
		name:     m.Name,
		requires: make(map[Capability]struct{}, len(m.Requires)),
		uses:     make(map[Capability]struct{}, len(m.Uses)),
		defines:  make(map[ExtensionPoint]struct{}, len(m.Defines)),
	}
	for _, c := range m.Requires {
		o.requires[c] = struct{}{}
	}
	for _, c := range m.Uses {
		o.uses[c] = struct{}{}
	}
	for _, d := range m.Defines {
		o.defines[d.Name] = struct{}{}
	}
	return &Links{owner: o, providers: map[Capability]any{}}
}

// provide records the implementation of c, built before the owner.
func (l *Links) provide(c Capability, v any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.providers[c] = v
}

// freeze records the contributions to every point and makes Contributors usable.
func (l *Links) freeze(contrib map[ExtensionPoint][]Contribution) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.contrib = contrib
	l.frozen = true
}

// Capability returns the implementation of name, which the module asserts to the
// contract's interface. Fails with errs.Invalid if name is in neither Requires nor Uses of
// the owning manifest, and errs.NotFound if it is in Uses and no enabled module provides
// it (the module then hides the feature). A required capability never fails here:
// Resolve guaranteed its provider. A nil or zero Links fails with errs.Internal (F40, F41, F43).
func (l *Links) Capability(name Capability) (any, error) {
	const op = "module.Links.Capability"
	if l == nil || l.owner == nil {
		return nil, internal(op, "the Links is not filled by Build")
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, required := l.owner.requires[name]
	_, used := l.owner.uses[name]
	if !required && !used {
		return nil, invalid(op, "module %s declares neither requires nor uses for capability %q", l.owner.name, name)
	}
	if v, ok := l.providers[name]; ok {
		return v, nil
	}
	if used && !required {
		return nil, errs.E(op, errs.NotFound, errNoProvider(name))
	}
	return nil, internal(op, "required capability %q has no provider although Resolve guaranteed one", name)
}

// Contributors returns every enabled module's contribution to point, in build order; each
// value passed the point's Accepts. Fails with errs.Invalid if point is not in the owning
// manifest's Defines, and errs.Internal if called before Build finished (that is, from a
// Factory) or on a nil or zero Links (F42, F43).
func (l *Links) Contributors(point ExtensionPoint) ([]Contribution, error) {
	const op = "module.Links.Contributors"
	if l == nil || l.owner == nil {
		return nil, internal(op, "the Links is not filled by Build")
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if _, ok := l.owner.defines[point]; !ok {
		return nil, invalid(op, "module %s does not define extension point %q", l.owner.name, point)
	}
	if !l.frozen {
		return nil, internal(op, "called before Build finished; call Contributors from Start or later")
	}
	return append([]Contribution{}, l.contrib[point]...), nil
}
