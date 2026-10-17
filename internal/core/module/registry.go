package module

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// entry is one registered module.
type entry struct {
	m Manifest
	f Factory
}

// Registry holds the modules compiled into the binary. Default() is the process-wide one
// that Register adds to; NewRegistry gives tests their own. Safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	entries  map[string]entry
	problems []error
}

// NewRegistry returns an empty registry. Cannot fail.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]entry{}}
}

var defaultRegistry = NewRegistry()

// Default returns the process-wide registry. Cannot fail.
func Default() *Registry { return defaultRegistry }

// Register adds a module to Default(). It is called from a module package's init:
//
//	func init() { module.Register(Manifest, New) }
//
// and app imports the package for that side effect (D5). It never panics: a problem
// (F11 to F13) is recorded and reported by Default().Resolve (F14), so a bad manifest stops
// start-up with a message instead of a stack trace.
func Register(m Manifest, f Factory) {
	defaultRegistry.record(m, f)
}

// record adds (m, f) and keeps the error for Resolve instead of returning it.
func (r *Registry) record(m Manifest, f Factory) {
	if err := r.Add(m, f); err != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.problems = append(r.problems, fmt.Errorf("module %q: %w", m.Name, err))
	}
}

// Add validates m and adds (m, f). Fails with errs.Invalid if f is nil or m fails
// Validate, and errs.Conflict if a module with m.Name was already added (F11 to F13).
func (r *Registry) Add(m Manifest, f Factory) error {
	const op = "module.Registry.Add"
	if f == nil {
		return invalid(op, "module %q: the factory is nil", m.Name)
	}
	if err := m.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.entries[m.Name]; dup {
		return errs.E(op, errs.Conflict, fmt.Errorf("module %q is registered twice; remove one import", m.Name))
	}
	r.entries[m.Name] = entry{m: m, f: f}
	return nil
}

// Manifests returns every registered manifest, sorted by name, enabled or not. Cannot
// fail. For `bms check` and the admin graph.
func (r *Registry) Manifests() []Manifest {
	entries := r.snapshot()
	out := make([]Manifest, len(entries))
	for i, e := range entries {
		out[i] = e.m
	}
	return out
}

// snapshot returns the entries sorted by name, and is the only reader of the map.
func (r *Registry) snapshot() []entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].m.Name < out[j].m.Name })
	return out
}

// recorded returns the problems Register recorded.
func (r *Registry) recorded() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.problems...)
}

// Env is what the core brings to the graph.
type Env struct {
	// Enabled names the modules to run, from config (app derives it: every registered
	// module minus the configured exclusions; decision 7).
	Enabled []string
	// CoreSignals are the signals the core publishes (people.person-created, ...), so a
	// module may subscribe to them.
	CoreSignals []signals.Name
	// CoreExtensionPoints are the points the core defines (people.person-page,
	// events.event-created, ...), so modules may contribute to them.
	CoreExtensionPoints []ExtensionPointDecl
}

// Validate fails with errs.Invalid if a name in Enabled is empty or repeated, a core
// signal name is invalid or not prefixed by a core area, or a core point fails Validate or is not prefixed by a core area
// (F16). Fields are "enabled[i]", "coreSignals[i]" and "coreExtensionPoints[i]".
func (e Env) Validate() error {
	acc := validate.Errors{Op: "module.Env.Validate"}
	seen := make(map[string]struct{}, len(e.Enabled))
	for i, n := range e.Enabled {
		field := fmt.Sprintf("enabled[%d]", i)
		if n == "" {
			acc.Addf(field, validate.CodeRequired, "must not be empty")
			continue
		}
		if _, dup := seen[n]; dup {
			acc.Addf(field, "duplicate", "listed more than once")
		}
		seen[n] = struct{}{}
	}
	for i, n := range e.CoreSignals {
		field := fmt.Sprintf("coreSignals[%d]", i)
		if err := n.Validate(); err != nil {
			asField(&acc, field, err)
		} else if !IsCoreArea(n.Publisher()) {
			acc.Addf(field, "prefix", "must start with a core area name")
		}
	}
	for i, p := range e.CoreExtensionPoints {
		field := fmt.Sprintf("coreExtensionPoints[%d]", i)
		if err := p.Validate(); err != nil {
			asNested(&acc, field, err)
		} else if !IsCoreArea(p.Name.Definer()) {
			acc.Addf(field+".name", "prefix", "must start with a core area name")
		}
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// problem builds one Resolve problem of kind k on field.
func problem(k errs.Kind, field, code, format string, args ...any) error {
	return errs.E("", k, &validate.FieldError{Field: field, Code: code, Reason: fmt.Sprintf(format, args...)})
}

// resolveError joins the problems into one error: Conflict if every problem is a
// Conflict, Invalid otherwise. It returns nil for none.
func resolveError(problems []error) error {
	if len(problems) == 0 {
		return nil
	}
	kind := errs.Conflict
	for _, p := range problems {
		if !errs.Is(p, errs.Conflict) {
			kind = errs.Invalid
			break
		}
	}
	return errs.E("module.Registry.Resolve", kind, errors.Join(problems...))
}
