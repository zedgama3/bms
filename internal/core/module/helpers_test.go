package module_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// recorder is a goroutine-safe ordered log shared by the fakes.
type recorder struct {
	mu  sync.Mutex
	log []string
}

func (r *recorder) add(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, fmt.Sprintf(format, args...))
}

func (r *recorder) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log)
}

// ---- manifests ----

func perm(name string) authz.PermissionDecl {
	return authz.PermissionDecl{Name: authz.Permission(name), Scope: authz.ScopeSite, Reaches: model.SensitivityInternal}
}

func acceptsString(v any) bool { _, ok := v.(string); return ok }

// mf builds a valid manifest called name and applies the mutations.
func mf(name string, mutate ...func(*module.Manifest)) module.Manifest {
	m := module.Manifest{Name: name}
	for _, f := range mutate {
		f(&m)
	}
	return m
}

func provides(c ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range c {
			m.Provides = append(m.Provides, module.Capability(x))
		}
	}
}

func requires(c ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range c {
			m.Requires = append(m.Requires, module.Capability(x))
		}
	}
}

func uses(c ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range c {
			m.Uses = append(m.Uses, module.Capability(x))
		}
	}
}

func publishes(n ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range n {
			m.Publishes = append(m.Publishes, signals.Name(x))
		}
	}
}

func subscribes(n ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range n {
			m.Subscribes = append(m.Subscribes, signals.Name(x))
		}
	}
}

func defines(p ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range p {
			m.Defines = append(m.Defines, module.ExtensionPointDecl{Name: module.ExtensionPoint(x), Accepts: acceptsString})
		}
	}
}

func contributes(p ...string) func(*module.Manifest) {
	return func(m *module.Manifest) {
		for _, x := range p {
			m.ContributesTo = append(m.ContributesTo, module.ExtensionPoint(x))
		}
	}
}

func withMigrations(m *module.Manifest) { m.Migrations = fstest.MapFS{} }

// ---- fake modules ----

// plain implements only the mandatory Module interface and records Start/Stop.
type plain struct {
	name     string
	rec      *recorder
	startErr error
	stopErr  error
}

func (p *plain) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (p *plain) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

func (p *plain) Start(context.Context) error {
	if p.rec != nil {
		p.rec.add("start:%s", p.name)
	}
	return p.startErr
}

func (p *plain) Stop(context.Context) error {
	if p.rec != nil {
		p.rec.add("stop:%s", p.name)
	}
	return p.stopErr
}

type capsMod struct {
	*plain
	caps map[module.Capability]any
}

func (m *capsMod) Capabilities() map[module.Capability]any { return m.caps }

type contribMod struct {
	*plain
	contribs map[module.ExtensionPoint]any
}

func (m *contribMod) Contributions() map[module.ExtensionPoint]any { return m.contribs }

// routesMod is a RouteProvider that does NOT implement APIDescriber.
type routesMod struct {
	*plain
	routes []module.Route
}

func (m *routesMod) Routes() []module.Route { return m.routes }

// apiMod is a RouteProvider and an APIDescriber.
type apiMod struct {
	*plain
	routes  []module.Route
	openapi []byte
}

func (m *apiMod) Routes() []module.Route { return m.routes }
func (m *apiMod) OpenAPI() []byte        { return m.openapi }

var okHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

func route(method, pattern string) module.Route {
	return module.Route{Method: method, Pattern: pattern, Handler: okHandler}
}

// ---- factories, provision, schema ----

func constFactory(m module.Module) module.Factory {
	return func(context.Context, module.Deps) (module.Module, error) { return m, nil }
}

// recFactory records "factory:<name>" and returns m.
func recFactory(rec *recorder, name string, m module.Module) module.Factory {
	return func(context.Context, module.Deps) (module.Module, error) {
		rec.add("factory:%s", name)
		return m, nil
	}
}

func okFactory(name string) module.Factory { return constFactory(&plain{name: name}) }

// provisioner returns a Provision that fills every field Build checks and records calls.
func provisioner(t *testing.T, rec *recorder) module.Provision {
	t.Helper()
	capture := telemetrytest.New(t)
	return func(_ context.Context, m module.Manifest) (module.Deps, error) {
		if rec != nil {
			rec.add("provision:%s", m.Name)
		}
		return module.Deps{
			Logger: slog.New(slog.DiscardHandler),
			Tracer: capture.Tracer("module." + m.Name),
			Clock:  clock.NewFake(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
		}, nil
	}
}

type fakeSchema struct {
	rec  *recorder
	errs map[string]error
}

func (f *fakeSchema) CheckSchema(_ context.Context, name string, _ fs.FS) error {
	if f.rec != nil {
		f.rec.add("schema:%s", name)
	}
	return f.errs[name]
}

// okSchema accepts everything.
func okSchema() *fakeSchema { return &fakeSchema{} }

// ---- registry / plan helpers ----

type entry struct {
	m module.Manifest
	f module.Factory
}

func reg(t *testing.T, entries ...entry) *module.Registry {
	t.Helper()
	r := module.NewRegistry()
	for _, e := range entries {
		if err := r.Add(e.m, e.f); err != nil {
			t.Fatalf("Add(%s): %v", e.m.Name, err)
		}
	}
	return r
}

func e(m module.Manifest, f module.Factory) entry { return entry{m, f} }

// simple registers manifests with a plain factory per name.
func simple(t *testing.T, ms ...module.Manifest) *module.Registry {
	t.Helper()
	var es []entry
	for _, m := range ms {
		es = append(es, e(m, okFactory(m.Name)))
	}
	return reg(t, es...)
}

func resolve(t *testing.T, r *module.Registry, env module.Env) *module.Plan {
	t.Helper()
	p, err := r.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return p
}

func names(ms []module.Manifest) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

func indexOf(s []string, v string) int { return slices.Index(s, v) }

// ---- error assertions ----

func wantKind(t *testing.T, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("error kind = %v, want %v (err: %v)", errs.KindOf(err), kind, err)
	}
}

func fieldSet(err error) map[string]bool {
	out := map[string]bool{}
	for _, f := range validate.Fields(err) {
		out[f.Field] = true
	}
	return out
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	if !fieldSet(err)[field] {
		got := make([]string, 0)
		for f := range fieldSet(err) {
			got = append(got, f)
		}
		slices.Sort(got)
		t.Fatalf("no field error for %q; fields: %s", field, strings.Join(got, ", "))
	}
}

func wantFieldPrefix(t *testing.T, err error, prefix string) {
	t.Helper()
	wantKind(t, err, errs.Invalid)
	for f := range fieldSet(err) {
		if strings.HasPrefix(f, prefix) {
			return
		}
	}
	t.Fatalf("no field error starting with %q; fields: %v", prefix, fieldSet(err))
}

var errBoom = errors.New("boom")
